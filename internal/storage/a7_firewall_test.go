package storage

import (
	"context"
	"strings"
	"testing"
)

// A7 firewall (brain.md §13.3 / §15.4, WBS §5.1 T7). T7 is the Ledger
// aggregate read face: its only write is an inert, append-only
// pending_human_approval draft. These tests prove at the storage layer that
// persisting a proposal never auto-applies it (no policy/context/Gate/outbox/
// budget/state side write) and that a T7 draft plus historical Ledger data
// cannot relax a single frozen Gate verdict or suppress a single HITL.

const (
	a7AggregateKey  = "aggregate:v1:global:all:1:2"
	a7PromptVersion = "T7/v1/a7firewall"
	a7T7ValidOutput = `{"proposal_kind":"policy","target_scope":"global","title":"Review trend","body":"Human review only; no field is auto-applied.","evidence_entry_ids":["cat"],"requires_human_approval":true}`
)

// reserveTerminalT7CallForTest creates a terminal valid T7 brain_call so the
// sole proposal write port can target it. The validated output JSON only needs
// to be stored; SaveProposalDraft never parses it as a second schema gate.
func reserveTerminalT7CallForTest(t *testing.T, ctx context.Context, db *DB, promptVersion string) string {
	t.Helper()
	reserved, err := db.ReserveBrainCall(ctx, ReserveBrainCallCmd{
		Scope: BrainScopeAggregate, SubjectKey: a7AggregateKey, Touchpoint: "T7",
		PromptVersion: promptVersion, OutputSchemaVersion: 1,
		InputJSON:   []byte(`{"aggregate_key":"` + a7AggregateKey + `","window":{"start_ms":1,"end_ms":2}}`),
		InputDigest: testCallIDDigest, StartedAtMS: testNow,
	})
	if err != nil {
		t.Fatalf("ReserveBrainCall: %v", err)
	}
	inTok, outTok := int64(4), int64(2)
	if _, err := db.RecordBrainAttempt(ctx, BrainAttemptCmd{
		CallID: reserved.ID, ProviderAttempt: 1, Outcome: BrainAttemptValid,
		RequestDigest: testCallIDDigest, RawOutputText: ptr(a7T7ValidOutput),
		RawOutputDigest: ptr(strings.Repeat("a", 64)), RawOutputBytes: ptrInt64(int64(len(a7T7ValidOutput))),
		InputTokens: &inTok, OutputTokens: &outTok,
		StartedAtMS: testNow, FinishedAtMS: testNow + 1, TokenLimit: 100,
	}); err != nil {
		t.Fatalf("RecordBrainAttempt: %v", err)
	}
	attempt := 1
	if err := db.FinalizeBrainCall(ctx, FinalizeBrainCallCmd{
		CallID: reserved.ID, Status: BrainCallValid, SelectedAttemptNo: &attempt,
		ValidatedOutputJSON: []byte(a7T7ValidOutput), FinishedAtMS: testNow + 2,
	}); err != nil {
		t.Fatalf("FinalizeBrainCall: %v", err)
	}
	return reserved.ID
}

func ptr(s string) *string    { return &s }
func ptrInt64(v int64) *int64 { return &v }

func rowCount(t *testing.T, db *DB, table string) int {
	t.Helper()
	var got int
	if err := db.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&got); err != nil {
		t.Fatal(err)
	}
	return got
}

// TestT7ProposalDraftPersistsButNeverAutoApplies proves the only effect of the
// proposal write port is one inert proposal_drafts row. It creates no outbox
// operation, no budget charge, no Gate snapshot/verdict, no Interrupt, no
// state transition: policy/context stay untouched until a human approves
// through a separate, audited write.
func TestT7ProposalDraftPersistsButNeverAutoApplies(t *testing.T) {
	ctx := context.Background()
	db, _ := openTestDB(t)
	if err := db.SeedProjectForTest(ctx, "cfg", "p", testNow); err != nil {
		t.Fatal(err)
	}
	callID := reserveTerminalT7CallForTest(t, ctx, db, a7PromptVersion)

	draftCmd := SaveProposalDraftCmd{
		LogicalCallID: callID, PromptVersion: a7PromptVersion, OutputSchemaVersion: 1,
		AggregateKey: a7AggregateKey, ProposalKind: "policy", TargetScope: "global",
		Title: "Review trend", Body: "Human review only; no field is auto-applied.",
		EvidenceEntryIDs: []string{"cat"}, CreatedAtMS: testNow + 3,
	}

	// Baseline captured after the terminal T7 call exists: the call itself
	// charged its own budget entry (the single Brain charge path); the point
	// under test is that SaveProposalDraft adds no second charge or side write.
	baseline := map[string]int{}
	for _, table := range []string{
		"proposal_drafts", "outbox_operations", "budget_entries",
		"gate_input_snapshots", "gate_evaluations", "calibration_entries",
		"interrupts", "ledger_entries",
	} {
		baseline[table] = rowCount(t, db, table)
	}
	if baseline["budget_entries"] != 1 || baseline["proposal_drafts"] != 0 {
		t.Fatalf("baseline = %+v: expected exactly one Brain charge and no draft yet", baseline)
	}

	draft, err := db.SaveProposalDraft(ctx, draftCmd)
	if err != nil {
		t.Fatalf("SaveProposalDraft: %v", err)
	}
	if draft.Status != "pending_human_approval" {
		t.Fatalf("draft status = %q, want pending_human_approval", draft.Status)
	}
	if draft.LogicalCallID != callID || draft.AggregateKey != a7AggregateKey ||
		draft.ProposalKind != "policy" || draft.TargetScope != "global" {
		t.Fatalf("draft identity = %#v", draft)
	}

	// Only proposal_drafts grew by exactly one row. No second write path ran:
	// no outbox action, no second charge, no Gate/Interrupt/state mutation.
	assertCount(t, db, "proposal_drafts", baseline["proposal_drafts"]+1)
	for _, table := range []string{
		"outbox_operations", "budget_entries", "gate_input_snapshots",
		"gate_evaluations", "calibration_entries", "interrupts", "ledger_entries",
	} {
		assertCount(t, db, table, baseline[table])
	}

	// Idempotent: re-saving the identical draft returns the same row and does
	// not duplicate. A divergent payload for the same call is rejected.
	again, err := db.SaveProposalDraft(ctx, draftCmd)
	if err != nil {
		t.Fatalf("re-save: %v", err)
	}
	if again.ID != draft.ID {
		t.Fatalf("re-save id = %q, want %q (insert-or-return identical)", again.ID, draft.ID)
	}
	assertCount(t, db, "proposal_drafts", 1)

	conflict := draftCmd
	conflict.Title = "mutated title"
	if _, err := db.SaveProposalDraft(ctx, conflict); err == nil {
		t.Fatal("divergent draft for the same call was accepted")
	}
	assertCount(t, db, "proposal_drafts", 1)
}

// TestT7ProposalDraftIsAppendOnly proves the persisted draft cannot be mutated
// or deleted into an "applied" state; the trigger is the structural A7 fence.
func TestT7ProposalDraftIsAppendOnly(t *testing.T) {
	ctx := context.Background()
	db, _ := openTestDB(t)
	if err := db.SeedProjectForTest(ctx, "cfg", "p", testNow); err != nil {
		t.Fatal(err)
	}
	callID := reserveTerminalT7CallForTest(t, ctx, db, a7PromptVersion)
	if _, err := db.SaveProposalDraft(ctx, SaveProposalDraftCmd{
		LogicalCallID: callID, PromptVersion: a7PromptVersion, OutputSchemaVersion: 1,
		AggregateKey: a7AggregateKey, ProposalKind: "policy", TargetScope: "global",
		Title: "Review trend", Body: "Human review only.", EvidenceEntryIDs: []string{"cat"}, CreatedAtMS: testNow,
	}); err != nil {
		t.Fatal(err)
	}
	if err := mustFail(t, db, `UPDATE proposal_drafts SET status='applied' WHERE logical_call_id=?`, callID); err == nil {
		t.Fatal("proposal_drafts UPDATE succeeded; append-only fence missing")
	}
	if err := mustFail(t, db, `DELETE FROM proposal_drafts WHERE logical_call_id=?`, callID); err == nil {
		t.Fatal("proposal_drafts DELETE succeeded; append-only fence missing")
	}
	assertCount(t, db, "proposal_drafts", 1)
}

// TestT7ProposalDraftRequiresTerminalValidT7Call proves the write port refuses
// non-T7 calls, fallback calls, and version drift, so no other touchpoint or
// replay artifact can mint an inert draft.
func TestT7ProposalDraftRequiresTerminalValidT7Call(t *testing.T) {
	ctx := context.Background()
	db, _ := openTestDB(t)
	if err := db.SeedProjectForTest(ctx, "cfg", "p", testNow); err != nil {
		t.Fatal(err)
	}
	draftCmd := func(callID, prompt string, schema int) SaveProposalDraftCmd {
		return SaveProposalDraftCmd{
			LogicalCallID: callID, PromptVersion: prompt, OutputSchemaVersion: schema,
			AggregateKey: a7AggregateKey, ProposalKind: "policy", TargetScope: "global",
			Title: "Review trend", Body: "Human review only.", EvidenceEntryIDs: []string{"cat"}, CreatedAtMS: testNow,
		}
	}
	// Unknown call id.
	if _, err := db.SaveProposalDraft(ctx, draftCmd("missing", a7PromptVersion, 1)); err == nil {
		t.Fatal("unknown logical_call_id was accepted")
	}
	// Wrong touchpoint: a valid T3 call cannot host a T7 draft.
	if err := db.SeedForgeRunForTest(ctx, "r", "p", "cfg", "42", testNow); err != nil {
		t.Fatal(err)
	}
	reserved, err := db.ReserveBrainCall(ctx, ReserveBrainCallCmd{
		Scope: BrainScopeRun, SubjectKey: "run:r", RunID: "r", Touchpoint: "T3",
		PromptVersion: "T3/v1", OutputSchemaVersion: 1,
		InputJSON: []byte(`{"run_id":"r"}`), InputDigest: testCallIDDigest, StartedAtMS: testNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	attempt := 1
	if _, err := db.RecordBrainAttempt(ctx, BrainAttemptCmd{CallID: reserved.ID, ProviderAttempt: 1, Outcome: BrainAttemptValid, RequestDigest: testCallIDDigest, RawOutputDigest: ptr(strings.Repeat("a", 64)), InputTokens: ptrInt64(1), OutputTokens: ptrInt64(1), StartedAtMS: testNow, FinishedAtMS: testNow, TokenLimit: 100}); err != nil {
		t.Fatal(err)
	}
	if err := db.FinalizeBrainCall(ctx, FinalizeBrainCallCmd{CallID: reserved.ID, Status: BrainCallValid, SelectedAttemptNo: &attempt, ValidatedOutputJSON: []byte(`{}`), FinishedAtMS: testNow}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SaveProposalDraft(ctx, draftCmd(reserved.ID, "T3/v1", 1)); err == nil {
		t.Fatal("non-T7 touchpoint was accepted")
	}
	// Fallback T7 call cannot host a draft.
	fallback, err := db.ReserveBrainCall(ctx, ReserveBrainCallCmd{
		Scope: BrainScopeAggregate, SubjectKey: a7AggregateKey, Touchpoint: "T7",
		PromptVersion: a7PromptVersion, OutputSchemaVersion: 1,
		InputJSON: []byte(`{"aggregate_key":"` + a7AggregateKey + `"}`), InputDigest: testCallIDDigest, StartedAtMS: testNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.FinalizeBrainCall(ctx, FinalizeBrainCallCmd{CallID: fallback.ID, Status: BrainCallFallback, FallbackReason: "provider_disabled", FinishedAtMS: testNow}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SaveProposalDraft(ctx, draftCmd(fallback.ID, a7PromptVersion, 1)); err == nil {
		t.Fatal("fallback T7 call was accepted")
	}
	// Prompt/schema version drift on a valid T7 call is rejected.
	validID := reserveTerminalT7CallForTest(t, ctx, db, a7PromptVersion)
	if _, err := db.SaveProposalDraft(ctx, draftCmd(validID, "T7/v1/different", 1)); err == nil {
		t.Fatal("prompt version drift was accepted")
	}
	if _, err := db.SaveProposalDraft(ctx, draftCmd(validID, a7PromptVersion, 2)); err == nil {
		t.Fatal("schema version drift was accepted")
	}
	assertCount(t, db, "proposal_drafts", 0)
}

// a7HITLGateRecord builds a frozen code_review HITL gate record + emission
// command bound to cmd.RunID so the firewall test can assert the Interrupt
// still fires when T7/Ledger data is present.
func a7HITLGateRecord(cmd EmitInterruptCmd) (GateEvaluationRecord, EmitInterruptCmd) {
	head := "0123456789012345678901234567890123456789"
	record := GateEvaluationRecord{
		RunID: cmd.RunID, GateInputHash: strings.Repeat("a", 64), GateVersion: "gate/v1", SnapshotSchemaVersion: 1,
		SnapshotJSON: []byte(`{"schema_version":1,"identity":{"run_id":"r"}}`),
		VerdictJSON:  []byte(`{"schema_version":1,"kind":"hitl","code":"code_review"}`),
		HeadSHA:      head, EffectivePolicyHash: strings.Repeat("c", 64), CertificationVersion: strings.Repeat("d", 64),
		RiskSourceVersion: "T3/fallback/v1", VerdictDigest: strings.Repeat("e", 64), ShadowDecision: "block",
		FeaturesJSON: []byte(`{"schema_version":1}`), NowMS: cmd.NowMS,
	}
	cmd.Reason = InterruptCodeReview
	cmd.GatePhase = GateReview
	cmd.GuardrailLevel = GuardrailNone
	cmd.Generation = InterruptGeneration{ChangeID: "change-01", HeadSHA: head}
	cmd.Facts = map[string]string{
		"change_ref": "https://forge.example/change/1", "head_sha": head,
		"review_requirement": "required", "recommended_action": "approve",
		"diff_ref": "https://forge.example/change/1/diff",
	}
	return record, cmd
}

// TestT7ProposalAndLedgerDataDoNotRelaxGateOrSuppressHITL proves that with a
// pending T7 proposal draft, historical T7 brain_calls and Ledger semantic
// material all present, the frozen Gate snapshot/verdict/digest is unchanged
// and the single HITL Interrupt is still emitted — never relaxed to ready and
// never suppressed or batched away by calibration proposals.
func TestT7ProposalAndLedgerDataDoNotRelaxGateOrSuppressHITL(t *testing.T) {
	ctx := context.Background()
	db, _ := openTestDB(t)
	if err := db.SeedProjectForTest(ctx, "cfg", "p", testNow); err != nil {
		t.Fatal(err)
	}
	if err := db.SeedForgeRunForTest(ctx, "r", "p", "cfg", "42", testNow); err != nil {
		t.Fatal(err)
	}
	head := "0123456789012345678901234567890123456789"
	if err := db.SetRunChangeHeadForTest(ctx, "r", "change-01", head); err != nil {
		t.Fatal(err)
	}

	baseCmd := EmitInterruptCmd{RunID: "r", ExpectedRunVersion: 1, AttentionDailyQuota: interruptQuota(), DayTimezone: "UTC", Source: SourceSystem, NowMS: testNow}
	record, cmd := a7HITLGateRecord(baseCmd)

	// Seed the A7 "attack" surface FIRST: a pending T7 draft, extra historical
	// T7 brain_calls, and Ledger semantic material that a relaxed gate might
	// want to cite. None of these are inputs to the frozen Gate verdict.
	extraCall := reserveTerminalT7CallForTest(t, ctx, db, a7PromptVersion)
	if _, err := db.SaveProposalDraft(ctx, SaveProposalDraftCmd{
		LogicalCallID: extraCall, PromptVersion: a7PromptVersion, OutputSchemaVersion: 1, AggregateKey: a7AggregateKey,
		ProposalKind: "context", TargetScope: "global", Title: "Loosen review", Body: "draft only",
		EvidenceEntryIDs: []string{"cat"}, CreatedAtMS: testNow + 4,
	}); err != nil {
		t.Fatal(err)
	}
	features := []byte(`{"schema_version":1,"material_kind":"reject_reason","text":"historical"}`)
	if _, err := db.db.ExecContext(ctx, `INSERT INTO ledger_entries (id,run_id,interrupt_id,entry_kind,features_schema_version,features_json,features_digest,natural_language,created_at_ms) VALUES (?,?,?,?,1,?,?,?,?)`,
		"ledger-a7", "r", nil, "semantic_material", string(features), sha256Hex(features), "historical", testNow); err != nil {
		t.Fatal(err)
	}

	// The single HITL still fires with the proposal, extra T7 trace and Ledger
	// material all present: it is neither relaxed to ready nor suppressed.
	rec1, int1, err := db.RecordGateEvaluationAndEmitInterrupt(ctx, record, cmd)
	if err != nil {
		t.Fatalf("HITL emission with T7 data present: %v", err)
	}
	if int1.ID == "" || int1.Reason != InterruptCodeReview || int1.Severity != SeverityNormal {
		t.Fatalf("HITL interrupt not emitted at full severity: %#v", int1)
	}
	baselineInterrupts := rowCount(t, db, "interrupts")
	baselineOutbox := rowCount(t, db, "outbox_operations")
	baselineSnapshots := rowCount(t, db, "gate_input_snapshots")
	if baselineInterrupts != 1 || baselineOutbox != 1 || baselineSnapshots != 1 {
		t.Fatalf("baseline after HITL = interrupts %d outbox %d snapshots %d", baselineInterrupts, baselineOutbox, baselineSnapshots)
	}

	// Re-freeze the identical Gate input. The snapshot is content-addressed, so
	// the verdict/digest/row counts are unchanged: the proposal did not relax
	// the gate or relax the frozen evidence, and it created no side writes.
	rec2, err := db.RecordGateEvaluation(ctx, record)
	if err != nil {
		t.Fatalf("second gate record after T7 data: %v", err)
	}
	if rec2.SnapshotID != rec1.SnapshotID {
		t.Fatalf("frozen gate snapshot changed under T7 data: rec1=%#v rec2=%#v", rec1, rec2)
	}
	// Content-addressed: one frozen input row and one cached verdict, despite
	// the pending proposal, extra T7 calls and Ledger semantic material.
	assertCount(t, db, "gate_input_snapshots", baselineSnapshots)
	assertCount(t, db, "gate_cache", 1)
	assertCount(t, db, "proposal_drafts", 1)
	// The original HITL is still open and unsuppressed; the proposal created no
	// second Interrupt and no extra outbox action against it.
	assertCount(t, db, "interrupts", baselineInterrupts)
	assertCount(t, db, "outbox_operations", baselineOutbox)
}
