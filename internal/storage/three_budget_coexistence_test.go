package storage

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

// three_budget_coexistence_test.go — WBS §5.6 / issue #760.
//
// The three budgets that must coexist (token, Forge API, attention) are
// exercised together against one production-shaped database. Each budget is
// charged only through its single documented port: tokens via the Brain
// post-charge port (RecordBrainAttempt, brain.md §6), Forge API calls via the
// Forge charge port (ChargeForgeAPICall, forge.md §9) and attention via the
// sole Interrupt creation port (EmitInterrupt, interrupt.md §1). No charge is
// reimplemented here. The degrade-isolation tests freeze the invariant that
// token/API degrade (over-limit / warning / refused charges) cannot break
// through the attention quota: those alerts are forge_alert operations that
// never write the attention counter, and any Interrupt that surfaces the
// degrade still charges or batches through EmitInterrupt.

// coexistOpsChannel is a webhook channel whose capabilities cover every
// Interrupt min_modality, so a quota-batched Interrupt resolves a delivery
// snapshot without coupling these tests to a per-reason channel matrix.
func coexistOpsChannel() InterruptChannel {
	return InterruptChannel{
		ID:           "ops",
		Type:         "webhook",
		TargetRef:    "secret_ref:OPS",
		Renderer:     "plain-v1",
		Default:      true,
		Capabilities: []string{"text", "voice", "visual"},
	}
}

// emitCoexistDesignApproval emits one design_approval Interrupt (base
// severity normal) charged solely through EmitInterrupt. design_approval is
// the simplest reason that emits directly: its effect binding needs only a
// task_spec_snapshot row (no Gate calibration record). Each test emits at most
// one Interrupt per run so the Run version contract stays explicit.
func emitCoexistDesignApproval(t *testing.T, ctx context.Context, db *DB, runID string, quota map[InterruptSeverity]int) Interrupt {
	t.Helper()
	specID := "spec-" + runID
	insertTaskSpec(t, db, specID, runID, 1)
	in, err := db.EmitInterrupt(ctx, EmitInterruptCmd{
		RunID:              runID,
		ExpectedRunVersion: 1,
		Reason:             InterruptDesignApproval,
		Facts: map[string]string{
			"risk_summary":       "high",
			"recommended_action": "approve",
			"task_spec_ref":      "/r/task/" + runID,
		},
		Generation: InterruptGeneration{
			TaskSpecSnapshotID: specID,
		},
		GatePhase:           GateNone,
		GuardrailLevel:      GuardrailNone,
		MaxEscalations:      2,
		AttentionDailyQuota: quota,
		DayTimezone:         "UTC",
		DailySummaryAt:      "09:00",
		Channels:            []InterruptChannel{coexistOpsChannel()},
		Source:              SourceSystem,
		NowMS:               testNow,
	})
	if err != nil {
		t.Fatalf("EmitInterrupt design_approval (%s): %v", runID, err)
	}
	return in
}

// coexistSeed installs one config + project and one queued forge run per id,
// each carrying its own verified Issue publish target for forge_comment.
func coexistSeed(t *testing.T, ctx context.Context, db *DB, runIDs ...string) {
	t.Helper()
	insertConfigSnapshot(t, db, "cfg")
	insertProject(t, db, "proj", "cfg")
	for _, id := range runIDs {
		insertForgeRun(t, db, id, "proj", "cfg", "issue-"+id)
	}
}

func countBudgetKind(t *testing.T, db *DB, kind string) int {
	t.Helper()
	var n int
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM budget_entries WHERE kind=?`, kind).Scan(&n); err != nil {
		t.Fatalf("count %s entries: %v", kind, err)
	}
	return n
}

func attentionConsumed(t *testing.T, db *DB, severity InterruptSeverity) int64 {
	t.Helper()
	var consumed int64
	err := db.db.QueryRow(`SELECT consumed_value FROM budget_counters WHERE kind='attention' AND scope='severity' AND scope_id=?`, string(severity)).Scan(&consumed)
	if errors.Is(err, sql.ErrNoRows) {
		return 0
	}
	if err != nil {
		t.Fatalf("attention counter %s: %v", severity, err)
	}
	return consumed
}

// TestThreeBudgetsCoexistAcrossSingleChargePorts runs all three budgets in one
// production-shaped path (a Brain call, a Forge API call and an Interrupt for
// the same run/project) and asserts each wrote exactly one entry under its own
// kind, with mutually distinct operation keys — i.e. no duplicate charge path.
func TestThreeBudgetsCoexistAcrossSingleChargePorts(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	coexistSeed(t, ctx, db, "run-coexist")

	// 1. Token via the Brain post-charge port (brain.md §6).
	call, err := db.ReserveBrainCall(ctx, ReserveBrainCallCmd{
		Scope: BrainScopeRun, SubjectKey: "run:run-coexist", RunID: "run-coexist",
		Touchpoint: "T2", PromptVersion: "T2/v1/coexist", OutputSchemaVersion: 1,
		InputJSON: []byte(`{"run_id":"run-coexist"}`), InputDigest: testCallIDDigest,
		StartedAtMS: testNow,
	})
	if err != nil {
		t.Fatalf("ReserveBrainCall: %v", err)
	}
	raw := `{"result_text":"{}","usage":{"input_tokens":7,"output_tokens":5}}`
	tres, err := db.RecordBrainAttempt(ctx, BrainAttemptCmd{
		CallID: call.ID, ProviderAttempt: 1, Outcome: BrainAttemptValid,
		RequestDigest: testCallIDDigest,
		RawOutputText: strp(raw), RawOutputDigest: strp("rd"), RawOutputBytes: int64p(int64(len(raw))),
		InputTokens: int64p(7), OutputTokens: int64p(5),
		StartedAtMS: testNow, FinishedAtMS: testNow + 1000, TokenLimit: 1000,
	})
	if err != nil {
		t.Fatalf("RecordBrainAttempt: %v", err)
	}
	if tres.ChargedTokens != 12 {
		t.Fatalf("token charge = %d, want 12", tres.ChargedTokens)
	}

	// 2. Forge API via the Forge charge port (forge.md §9).
	if _, err := db.ChargeForgeAPICall(ctx, ChargeForgeAPICallCmd{
		ProjectID: "proj", CallAttemptKey: "forge-call:run-coexist:1",
		NowMS: testNow, Limit: 1000, WarningRatio: 0.8,
	}); err != nil {
		t.Fatalf("ChargeForgeAPICall: %v", err)
	}

	// 3. Attention via the sole Interrupt creation port (interrupt.md §1).
	in := emitCoexistDesignApproval(t, ctx, db, "run-coexist", interruptQuota())
	if in.Severity != SeverityNormal || in.ChargedBudgetEntryID == "" {
		t.Fatalf("design_approval interrupt = %#v", in)
	}

	// Each budget wrote exactly one entry under its own kind.
	if got := countBudgetKind(t, db, "token"); got != 1 {
		t.Fatalf("token entries = %d, want 1", got)
	}
	if got := countBudgetKind(t, db, "forge_api"); got != 1 {
		t.Fatalf("forge_api entries = %d, want 1", got)
	}
	if got := countBudgetKind(t, db, "attention"); got != 1 {
		t.Fatalf("attention entries = %d, want 1", got)
	}

	// The three operation keys are mutually distinct: no budget reuses another
	// budget's charge key, and each matches its documented format.
	keys := make(map[string]bool)
	rows, err := db.db.Query(`SELECT operation_key FROM budget_entries`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			t.Fatal(err)
		}
		if keys[k] {
			t.Fatalf("duplicate operation key across budgets: %q", k)
		}
		keys[k] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	wantTokenKey := BrainTokenOperationKey(call.ID, 1)
	wantAPIKey := "forge-call:run-coexist:1"
	wantAttentionKey := "interrupt-charge:" + in.GenerationKey
	if !keys[wantTokenKey] || !keys[wantAPIKey] || !keys[wantAttentionKey] {
		t.Fatalf("charge keys = %v; want %q, %q and %q present", keys, wantTokenKey, wantAPIKey, wantAttentionKey)
	}

	// Counters are independent and scoped to their own bucket: token (global
	// day), forge_api (project hour), attention (severity day). None leaked.
	var tokenConsumed, apiConsumed int64
	if err := db.db.QueryRow(`SELECT consumed_value FROM budget_counters WHERE kind='token'`).Scan(&tokenConsumed); err != nil {
		t.Fatalf("token counter: %v", err)
	}
	if err := db.db.QueryRow(`SELECT consumed_value FROM budget_counters WHERE kind='forge_api' AND scope_id='proj'`).Scan(&apiConsumed); err != nil {
		t.Fatalf("forge_api counter: %v", err)
	}
	att := attentionConsumed(t, db, SeverityNormal)
	if tokenConsumed != 12 || apiConsumed != 1 || att != 1 {
		t.Fatalf("counters token=%d api=%d attention=%d, want 12/1/1", tokenConsumed, apiConsumed, att)
	}
}

// TestTokenDegradeDoesNotBreakAttentionQuota charges tokens past the daily
// limit (brain.md §6 single over-limit crossing) so the token_budget_exceeded
// forge_alert fires, then proves the attention quota is still a hard
// constraint: the degrade alert wrote no attention counter/entry, and a
// subsequent Interrupt that would exceed the quota is batched rather than
// allowed through.
func TestTokenDegradeDoesNotBreakAttentionQuota(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	coexistSeed(t, ctx, db, "run-a", "run-b")

	call, err := db.ReserveBrainCall(ctx, ReserveBrainCallCmd{
		Scope: BrainScopeRun, SubjectKey: "run:run-a", RunID: "run-a",
		Touchpoint: "T2", PromptVersion: "T2/v1/degrade", OutputSchemaVersion: 1,
		InputJSON: []byte(`{"run_id":"run-a"}`), InputDigest: testCallIDDigest,
		StartedAtMS: testNow,
	})
	if err != nil {
		t.Fatalf("ReserveBrainCall: %v", err)
	}
	raw := `{"result_text":"{}","usage":{"input_tokens":7,"output_tokens":5}}`
	tres, err := db.RecordBrainAttempt(ctx, BrainAttemptCmd{
		CallID: call.ID, ProviderAttempt: 1, Outcome: BrainAttemptValid,
		RequestDigest: testCallIDDigest,
		RawOutputText: strp(raw), RawOutputDigest: strp("rd"), RawOutputBytes: int64p(int64(len(raw))),
		InputTokens: int64p(7), OutputTokens: int64p(5),
		StartedAtMS: testNow, FinishedAtMS: testNow + 1000, TokenLimit: 10,
	})
	if err != nil {
		t.Fatalf("RecordBrainAttempt: %v", err)
	}
	if !tres.OverLimit {
		t.Fatalf("token charge not flagged over limit: %+v", tres)
	}

	// The over-limit alert is a forge_alert with the stable
	// token_budget_exceeded key (outbox.md §5.1). It must not have touched the
	// attention budget: that counter is written only inside EmitInterrupt.
	var alertPurpose string
	if err := db.db.QueryRow(`SELECT json_extract(payload_json,'$.purpose') FROM outbox_operations WHERE kind='forge_alert'`).Scan(&alertPurpose); err != nil {
		t.Fatalf("token budget alert missing: %v", err)
	}
	if alertPurpose != "token_budget_exceeded" {
		t.Fatalf("alert purpose = %q, want token_budget_exceeded", alertPurpose)
	}
	if got := countBudgetKind(t, db, "attention"); got != 0 {
		t.Fatalf("token degrade wrote %d attention entries, want 0", got)
	}
	var attentionCounters int
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM budget_counters WHERE kind='attention'`).Scan(&attentionCounters); err != nil {
		t.Fatal(err)
	}
	if attentionCounters != 0 {
		t.Fatalf("token degrade created %d attention counters, want 0", attentionCounters)
	}

	// Attention quota stays hard under token degrade: with a daily normal
	// quota of 1 the first Interrupt charges and the second is batched.
	normalOne := map[InterruptSeverity]int{SeverityNormal: 1}
	first := emitCoexistDesignApproval(t, ctx, db, "run-a", normalOne)
	if first.ChargedBudgetEntryID == "" {
		t.Fatalf("first interrupt should charge attention, got %#v", first)
	}
	if got := attentionConsumed(t, db, SeverityNormal); got != 1 {
		t.Fatalf("attention consumed after first = %d, want 1", got)
	}
	second := emitCoexistDesignApproval(t, ctx, db, "run-b", normalOne)
	if second.Delivery != "batch" || second.ChargedBudgetEntryID != "" {
		t.Fatalf("quota-exceeding interrupt = %#v, want batched without charge", second)
	}
	// The quota was not breached: the counter is still at its limit, not 2.
	if got := attentionConsumed(t, db, SeverityNormal); got != 1 {
		t.Fatalf("attention consumed after batched = %d, want 1 (quota not breached)", got)
	}
}

// TestForgeAPIDegradeDoesNotBreakAttentionQuota drives the Forge API budget to
// its warning ratio (forge_api_budget_warning alert) and then to exhaustion
// (CAS refusal, forge.md §9), then proves the attention quota is still a hard
// constraint under that degrade.
func TestForgeAPIDegradeDoesNotBreakAttentionQuota(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	coexistSeed(t, ctx, db, "run-a", "run-b")

	// Limit 3, warning at 50%: the 2nd charge crosses the warning ratio and
	// the 4th is refused by the CAS without recording.
	chargeOK(t, db, "proj", "forge-call:run-a:1", testNow, 3, 0.5)
	chargeOK(t, db, "proj", "forge-call:run-a:2", testNow, 3, 0.5) // warning alert
	chargeOK(t, db, "proj", "forge-call:run-a:3", testNow, 3, 0.5) // last allowed
	_, err := db.ChargeForgeAPICall(ctx, ChargeForgeAPICallCmd{
		ProjectID: "proj", CallAttemptKey: "forge-call:run-a:4",
		NowMS: testNow, Limit: 3, WarningRatio: 0.5,
	})
	if !errors.Is(err, ErrForgeBudgetExhausted) {
		t.Fatalf("4th API charge err = %v, want ErrForgeBudgetExhausted", err)
	}

	// The warning alert and the refused charge never touched attention.
	var alertPurpose string
	if err := db.db.QueryRow(`SELECT json_extract(payload_json,'$.purpose') FROM outbox_operations WHERE kind='forge_alert'`).Scan(&alertPurpose); err != nil {
		t.Fatalf("forge api alert missing: %v", err)
	}
	if alertPurpose != ForgeAPIBudgetAlertPurpose {
		t.Fatalf("alert purpose = %q, want %q", alertPurpose, ForgeAPIBudgetAlertPurpose)
	}
	if got := countBudgetKind(t, db, "attention"); got != 0 {
		t.Fatalf("api degrade wrote %d attention entries, want 0", got)
	}

	// Attention quota is still enforced under API degrade.
	normalOne := map[InterruptSeverity]int{SeverityNormal: 1}
	first := emitCoexistDesignApproval(t, ctx, db, "run-a", normalOne)
	if first.ChargedBudgetEntryID == "" {
		t.Fatalf("first interrupt should charge attention, got %#v", first)
	}
	second := emitCoexistDesignApproval(t, ctx, db, "run-b", normalOne)
	if second.Delivery != "batch" || second.ChargedBudgetEntryID != "" {
		t.Fatalf("quota-exceeding interrupt = %#v, want batched without charge", second)
	}
	if got := attentionConsumed(t, db, SeverityNormal); got != 1 {
		t.Fatalf("attention consumed after batched = %d, want 1 (quota not breached)", got)
	}
}
