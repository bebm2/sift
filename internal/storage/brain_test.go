package storage

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// Brain write-port tests (specs/storage.md §10.1/§9.1, specs/brain.md §5/§6):
// reserve sequencing, request-digest assertion, one-time finalize, token
// post-charge with over-limit crossing, idempotent operation keys, zero-usage
// no-entry, provider_attempt=0 preflight rows and the replay export.

const (
	testCallIDDigest = "dgef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcd"
	testBucket       = int64(1698796800000) // 2023-11-01 00:00:00 UTC
)

func reserveT2Call(t *testing.T, db *DB, runID string) ReservedBrainCall {
	t.Helper()
	res, err := db.ReserveBrainCall(context.Background(), ReserveBrainCallCmd{
		Scope:               BrainScopeRun,
		SubjectKey:          "run:" + runID,
		RunID:               runID,
		Touchpoint:          "T2",
		PromptVersion:       "T2/v1/abcdef012345",
		OutputSchemaVersion: 1,
		InputJSON:           []byte(`{"run_id":"` + runID + `"}`),
		InputDigest:         testCallIDDigest,
		StartedAtMS:         testBucket + 60_000,
	})
	if err != nil {
		t.Fatalf("ReserveBrainCall: %v", err)
	}
	return res
}

func seedT2Run(t *testing.T, db *DB, id string) {
	t.Helper()
	insertConfigSnapshot(t, db, "cfg-"+id)
	insertProject(t, db, "proj-"+id, "cfg-"+id)
	insertForgeRun(t, db, id, "proj-"+id, "cfg-"+id, "issue-"+id)
}

func int64p(v int64) *int64 { return &v }
func strp(s string) *string { return &s }

func TestReserveBrainCallSequence(t *testing.T) {
	db, _ := openTestDB(t)
	seedT2Run(t, db, "run-seq")

	first := reserveT2Call(t, db, "run-seq")
	second := reserveT2Call(t, db, "run-seq")
	if first.CallSeq != 1 || second.CallSeq != 2 {
		t.Fatalf("call_seq = %d, %d; want 1, 2", first.CallSeq, second.CallSeq)
	}
	// The counter row tracks next_call_seq without max()+1.
	var next int64
	if err := db.db.QueryRow(`SELECT next_call_seq FROM brain_call_counters
		WHERE scope='run' AND subject_key='run:run-seq' AND touchpoint='T2'`).Scan(&next); err != nil {
		t.Fatal(err)
	}
	if next != 3 {
		t.Fatalf("next_call_seq = %d, want 3", next)
	}

	// Scope CHECK: T2 without a run is rejected at the database.
	if _, err := db.ReserveBrainCall(context.Background(), ReserveBrainCallCmd{
		Scope: BrainScopeRun, SubjectKey: "run:orphan", Touchpoint: "T2",
		PromptVersion: "T2/v1/x", OutputSchemaVersion: 1,
		InputJSON: []byte(`{}`), InputDigest: testCallIDDigest, StartedAtMS: testBucket,
	}); err == nil {
		t.Fatal("T2 without run_id must be rejected by CHECK")
	}
}

func TestRecordBrainAttemptCharging(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	seedT2Run(t, db, "run-charge")
	call := reserveT2Call(t, db, "run-charge")

	raw := `{"result_text":"{}","usage":{"input_tokens":7,"output_tokens":5}}`
	res, err := db.RecordBrainAttempt(ctx, BrainAttemptCmd{
		CallID: call.ID, ProviderAttempt: 1, Outcome: BrainAttemptValid,
		RequestDigest: testCallIDDigest,
		RawOutputText: strp(raw), RawOutputDigest: strp("rd"), RawOutputBytes: int64p(int64(len(raw))),
		InputTokens: int64p(7), OutputTokens: int64p(5),
		StartedAtMS: testBucket + 60_000, FinishedAtMS: testBucket + 61_000,
		TokenLimit: 10,
	})
	if err != nil {
		t.Fatalf("RecordBrainAttempt: %v", err)
	}
	// Post-charge crosses the limit once, fully booked (brain.md §6.2).
	if res.ChargedTokens != 12 || res.ConsumedTokens != 12 || !res.OverLimit {
		t.Fatalf("charge = %+v, want 12/12/over", res)
	}

	// Replay with identical facts returns the original charge, no double billing.
	replay, err := db.RecordBrainAttempt(ctx, BrainAttemptCmd{
		CallID: call.ID, ProviderAttempt: 1, Outcome: BrainAttemptValid,
		RequestDigest: testCallIDDigest,
		StartedAtMS:   testBucket + 60_000, FinishedAtMS: testBucket + 61_000,
		TokenLimit: 10,
	})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if replay.ChargedTokens != 12 || replay.AttemptID != res.AttemptID {
		t.Fatalf("replay = %+v, want original charge", replay)
	}
	var entries int
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM budget_entries WHERE operation_key=?`,
		BrainTokenOperationKey(call.ID, 1)).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if entries != 1 {
		t.Fatalf("budget entries = %d, want 1", entries)
	}

	// Overage alert: exactly one forge_alert per UTC bucket (outbox.md §5.1).
	var alertKey string
	if err := db.db.QueryRow(`SELECT operation_key FROM outbox_operations WHERE kind='forge_alert'`).Scan(&alertKey); err != nil {
		t.Fatalf("alert operation missing: %v", err)
	}
	wantKey := "alert:token_budget_exceeded:global:1698796800000:1"
	if alertKey != wantKey {
		t.Fatalf("alert key = %q, want %q", alertKey, wantKey)
	}

	// A second over-limit charge in the same bucket does not create a second
	// alert operation (stable key dedupe).
	if _, err := db.RecordBrainAttempt(ctx, BrainAttemptCmd{
		CallID: call.ID, ProviderAttempt: 2, Outcome: BrainAttemptValid,
		RequestDigest: testCallIDDigest,
		RawOutputText: strp(raw), RawOutputDigest: strp("rd"), RawOutputBytes: int64p(1),
		InputTokens: int64p(1), OutputTokens: int64p(1),
		StartedAtMS: testBucket + 62_000, FinishedAtMS: testBucket + 63_000,
		TokenLimit: 10,
	}); err != nil {
		t.Fatal(err)
	}
	var alerts int
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM outbox_operations WHERE kind='forge_alert'`).Scan(&alerts); err != nil {
		t.Fatal(err)
	}
	if alerts != 1 {
		t.Fatalf("alerts = %d, want 1 per bucket", alerts)
	}
}

func TestRecordBrainAttemptZeroUsageNoEntry(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	seedT2Run(t, db, "run-zero")
	call := reserveT2Call(t, db, "run-zero")

	raw := `{"result_text":"{}","usage":{"input_tokens":0,"output_tokens":0}}`
	res, err := db.RecordBrainAttempt(ctx, BrainAttemptCmd{
		CallID: call.ID, ProviderAttempt: 1, Outcome: BrainAttemptValid,
		RequestDigest: testCallIDDigest,
		RawOutputText: strp(raw), RawOutputDigest: strp("rd"), RawOutputBytes: int64p(1),
		InputTokens: int64p(0), OutputTokens: int64p(0),
		StartedAtMS: testBucket, FinishedAtMS: testBucket + 1,
		TokenLimit: 1000,
	})
	if err != nil {
		t.Fatalf("RecordBrainAttempt: %v", err)
	}
	if res.ChargedTokens != 0 {
		t.Fatalf("zero usage charged %d", res.ChargedTokens)
	}
	var n int
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM budget_entries WHERE kind='token'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("zero usage wrote %d budget entries", n)
	}
}

func TestRecordBrainAttemptGuards(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	seedT2Run(t, db, "run-guard")
	call := reserveT2Call(t, db, "run-guard")

	// Digest drift is a contract violation (brain.md §3).
	if _, err := db.RecordBrainAttempt(ctx, BrainAttemptCmd{
		CallID: call.ID, ProviderAttempt: 1, Outcome: BrainAttemptInvalidOutput,
		RequestDigest: "drifted", StartedAtMS: testBucket, FinishedAtMS: testBucket,
	}); !errors.Is(err, ErrBrainRequestDrift) {
		t.Fatalf("digest drift: %v", err)
	}
	// provider_error requires the stable code.
	if _, err := db.RecordBrainAttempt(ctx, BrainAttemptCmd{
		CallID: call.ID, ProviderAttempt: 1, Outcome: BrainAttemptProviderError,
		RequestDigest: testCallIDDigest, StartedAtMS: testBucket, FinishedAtMS: testBucket,
	}); err == nil {
		t.Fatal("provider_error without code must fail")
	}
	// provider_attempt=0 rows carry no token/exit/raw facts (schema CHECK).
	if _, err := db.RecordBrainAttempt(ctx, BrainAttemptCmd{
		CallID: call.ID, ProviderAttempt: 0, Outcome: BrainAttemptFallback,
		RequestDigest: testCallIDDigest, InputTokens: int64p(1), OutputTokens: int64p(1),
		StartedAtMS: testBucket, FinishedAtMS: testBucket,
	}); err == nil {
		t.Fatal("attempt 0 with tokens must be rejected by CHECK")
	}
}

func TestFinalizeBrainCallOnce(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	seedT2Run(t, db, "run-fin")
	call := reserveT2Call(t, db, "run-fin")

	raw := `{"result_text":"{}","usage":{"input_tokens":1,"output_tokens":1}}`
	if _, err := db.RecordBrainAttempt(ctx, BrainAttemptCmd{
		CallID: call.ID, ProviderAttempt: 1, Outcome: BrainAttemptValid,
		RequestDigest: testCallIDDigest,
		RawOutputText: strp(raw), RawOutputDigest: strp("rd"), RawOutputBytes: int64p(1),
		InputTokens: int64p(1), OutputTokens: int64p(1),
		StartedAtMS: testBucket, FinishedAtMS: testBucket + 1, TokenLimit: 1000,
	}); err != nil {
		t.Fatal(err)
	}

	// A valid finalize must point at a valid attempt of the same call.
	wrong := 2
	if err := db.FinalizeBrainCall(ctx, FinalizeBrainCallCmd{
		CallID: call.ID, Status: BrainCallValid, SelectedAttemptNo: &wrong,
		ValidatedOutputJSON: []byte(`{}`), FinishedAtMS: testBucket + 2,
	}); err == nil {
		t.Fatal("finalize valid pointing at a missing attempt must fail")
	}

	one := 1
	if err := db.FinalizeBrainCall(ctx, FinalizeBrainCallCmd{
		CallID: call.ID, Status: BrainCallValid, SelectedAttemptNo: &one,
		ValidatedOutputJSON: []byte(`{"ok":true}`), FinishedAtMS: testBucket + 2,
	}); err != nil {
		t.Fatalf("finalize valid: %v", err)
	}
	// Second finalize is refused by port and trigger alike.
	if err := db.FinalizeBrainCall(ctx, FinalizeBrainCallCmd{
		CallID: call.ID, Status: BrainCallFallback, FallbackReason: "late", FinishedAtMS: testBucket + 3,
	}); !errors.Is(err, ErrBrainCallFinalized) {
		t.Fatalf("second finalize: %v", err)
	}
	// Attempts after finalize are refused.
	if _, err := db.RecordBrainAttempt(ctx, BrainAttemptCmd{
		CallID: call.ID, ProviderAttempt: 2, Outcome: BrainAttemptInvalidOutput,
		RequestDigest: testCallIDDigest, StartedAtMS: testBucket, FinishedAtMS: testBucket,
	}); !errors.Is(err, ErrBrainCallFinalized) {
		t.Fatalf("attempt after finalize: %v", err)
	}

	trace, attempts, err := db.BrainCallTrace(ctx, call.ID)
	if err != nil {
		t.Fatal(err)
	}
	if trace.Status != BrainCallValid || trace.SelectedAttemptNo == nil || *trace.SelectedAttemptNo != 1 {
		t.Fatalf("trace = %+v", trace)
	}
	if len(attempts) != 1 {
		t.Fatalf("attempts = %d", len(attempts))
	}
}

func TestExportBrainCallsJSONL(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	seedT2Run(t, db, "run-exp")
	call := reserveT2Call(t, db, "run-exp")

	for i, outcome := range []string{BrainAttemptInvalidOutput, BrainAttemptValid} {
		cmd := BrainAttemptCmd{
			CallID: call.ID, ProviderAttempt: i + 1, Outcome: outcome,
			RequestDigest: testCallIDDigest,
			RawOutputText: strp("raw"), RawOutputDigest: strp("rd"), RawOutputBytes: int64p(3),
			StartedAtMS: testBucket, FinishedAtMS: testBucket + 1, TokenLimit: 1000,
		}
		if outcome == BrainAttemptValid {
			cmd.InputTokens, cmd.OutputTokens = int64p(2), int64p(3)
		}
		if _, err := db.RecordBrainAttempt(ctx, cmd); err != nil {
			t.Fatal(err)
		}
	}
	two := 2
	if err := db.FinalizeBrainCall(ctx, FinalizeBrainCallCmd{
		CallID: call.ID, Status: BrainCallValid, SelectedAttemptNo: &two,
		ValidatedOutputJSON: []byte(`{"kind":"chore"}`), FinishedAtMS: testBucket + 5,
	}); err != nil {
		t.Fatal(err)
	}

	var buf strings.Builder
	if err := db.ExportBrainCallsJSONL(ctx, &buf); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("records = %d, want 1", len(lines))
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatal(err)
	}
	if rec["record_type"] != "brain_call" || rec["touchpoint"] != "T2" || rec["status"] != "valid" {
		t.Fatalf("record = %v", rec)
	}
	attempts, ok := rec["attempts"].([]any)
	if !ok || len(attempts) != 2 {
		t.Fatalf("attempts = %v", rec["attempts"])
	}
	first := attempts[0].(map[string]any)
	second := attempts[1].(map[string]any)
	if first["provider_attempt"].(float64) != 1 || second["provider_attempt"].(float64) != 2 {
		t.Fatalf("attempt ordering broken: %v / %v", first["provider_attempt"], second["provider_attempt"])
	}
	if first["outcome"] != "invalid_output" || second["outcome"] != "valid" {
		t.Fatalf("outcomes: %v / %v", first["outcome"], second["outcome"])
	}
}

func TestRunningBrainCallsRecoveryView(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	seedT2Run(t, db, "run-run")
	call := reserveT2Call(t, db, "run-run")

	running, err := db.RunningBrainCalls(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(running) != 1 || running[0].ID != call.ID {
		t.Fatalf("running = %+v", running)
	}
	if err := db.FinalizeBrainCall(ctx, FinalizeBrainCallCmd{
		CallID: call.ID, Status: BrainCallFallback, FallbackReason: "x", FinishedAtMS: testBucket + 9,
	}); err != nil {
		t.Fatal(err)
	}
	running, err = db.RunningBrainCalls(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(running) != 0 {
		t.Fatalf("running after finalize = %+v", running)
	}
}
