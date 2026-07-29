package storage

import (
	"context"
	"strings"
	"testing"
)

func TestAdvanceInterruptEscalatesOnceAndRotatesNonce(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	if err := db.SeedProjectForTest(ctx, "cfg", "project", testNow); err != nil {
		t.Fatal(err)
	}
	if err := db.SeedForgeRunForTest(ctx, "run", "project", "cfg", "42", testNow); err != nil {
		t.Fatal(err)
	}
	batch := int64(testNow + 2)
	in, err := db.EmitInterrupt(ctx, EmitInterruptCmd{
		RunID: "run", ExpectedRunVersion: 1, Reason: InterruptAgentBlocked,
		Facts:      map[string]string{"blocker_summary": "blocked", "attempted_summary": "tried", "recommended_action": "ask", "agent_log_ref": "/log"},
		Generation: InterruptGeneration{AttemptNo: 1, Generation: 1, ReportID: "report-1"}, GatePhase: GateNone, GuardrailLevel: GuardrailNone,
		ExpiresAfterMS: 10, OnExpire: ExpireEscalate, OnMaxEscalations: ExpireHold, MaxEscalations: 1,
		AttentionDailyQuota: interruptQuota(), DayTimezone: "UTC", Source: SourceSystem, NowMS: testNow,
		Channels: []InterruptChannel{{ID: "voice", Capabilities: []string{"voice"}}}, BatchAtMS: &batch,
	})
	if err != nil {
		t.Fatal(err)
	}
	var nonce string
	if err := db.db.QueryRow(`SELECT nonce FROM interrupts WHERE id=?`, in.ID).Scan(&nonce); err != nil {
		t.Fatal(err)
	}
	advanced, err := db.AdvanceInterrupt(ctx, AdvanceInterruptCmd{InterruptID: in.ID, ExpectedVersion: 1, ExpectedNonce: nonce, Kind: AdvanceExpiry, NowMS: testNow + 10})
	if err != nil || !advanced {
		t.Fatalf("advance = %v, %v", advanced, err)
	}
	var severity, newNonce, delivery string
	var version, expires, next int64
	if err := db.db.QueryRow(`SELECT severity,nonce,delivery,version,expires_at_ms,next_dispatch_at_ms FROM interrupts WHERE id=?`, in.ID).Scan(&severity, &newNonce, &delivery, &version, &expires, &next); err != nil {
		t.Fatal(err)
	}
	if severity != string(SeverityHigh) || newNonce == nonce || delivery != "immediate" || version != 2 || expires != testNow+20 || next != testNow+10 {
		t.Fatalf("advanced row = %s/%s/%s/%d/%d/%d", severity, newNonce, delivery, version, expires, next)
	}
	if _, err := db.AdvanceInterrupt(ctx, AdvanceInterruptCmd{InterruptID: in.ID, ExpectedVersion: 1, ExpectedNonce: nonce, Kind: AdvanceExpiry, NowMS: testNow + 10}); err != ErrRejectedStale {
		t.Fatalf("stale advance error = %v, want ErrRejectedStale", err)
	}
	assertCount(t, db, "budget_entries", 1)
}

func TestSupervisorInterruptTickDispatches(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	if err := db.SeedProjectForTest(ctx, "cfg", "project", testNow); err != nil {
		t.Fatal(err)
	}
	if err := db.SeedForgeRunForTest(ctx, "run", "project", "cfg", "42", testNow); err != nil {
		t.Fatal(err)
	}
	at := int64(testNow + 1)
	in, err := db.EmitInterrupt(ctx, func() EmitInterruptCmd {
		cmd := t6Command(testNow)
		cmd.BatchAtMS, cmd.Channels = &at, []InterruptChannel{{ID: "ops", Capabilities: []string{"visual"}}}
		return cmd
	}())
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SupervisorInterruptTick(ctx, at); err != nil {
		t.Fatal(err)
	}
	var state string
	var next any
	if err := db.db.QueryRow(`SELECT dispatch_state,next_dispatch_at_ms FROM interrupts WHERE id=?`, in.ID).Scan(&state, &next); err != nil {
		t.Fatal(err)
	}
	if state != "batched" || next != nil {
		t.Fatalf("dispatch state = %q next=%v", state, next)
	}
}

func TestQuotaExhaustionCreatesBatchedInterruptWithoutCharge(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	if err := db.SeedProjectForTest(ctx, "cfg", "project", testNow); err != nil {
		t.Fatal(err)
	}
	if err := db.SeedForgeRunForTest(ctx, "run", "project", "cfg", "42", testNow); err != nil {
		t.Fatal(err)
	}
	cmd := t6Command(testNow)
	cmd.AttentionDailyQuota = map[InterruptSeverity]int{SeverityLow: 0, SeverityNormal: 0, SeverityHigh: 1}
	cmd.DailySummaryAt = "09:00"
	cmd.Channels = []InterruptChannel{{ID: "ops", Capabilities: []string{"visual"}}}
	got, err := db.EmitInterrupt(ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	var kind, charge, state string
	if err := db.db.QueryRow(`SELECT a.kind,COALESCE(a.attention_charge_entry_id,''),i.dispatch_state FROM attention_admissions a JOIN interrupts i ON i.id=a.interrupt_id WHERE a.interrupt_id=?`, got.ID).Scan(&kind, &charge, &state); err != nil {
		t.Fatal(err)
	}
	if kind != "quota_batched" || charge != "" || state != "batched" {
		t.Fatalf("admission=%s charge=%q state=%s", kind, charge, state)
	}
	assertCount(t, db, "budget_entries", 0)
	assertCount(t, db, "attention_batch_members", 1)
}

func TestAdvanceInterruptStartupStallAtLimitHoldsRatherThanAutoRejecting(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	if err := db.SeedProjectForTest(ctx, "cfg", "project", testNow); err != nil {
		t.Fatal(err)
	}
	if err := db.SeedForgeRunForTest(ctx, "run", "project", "cfg", "42", testNow); err != nil {
		t.Fatal(err)
	}
	insertTaskSpec(t, db, "spec", "run", 1)
	insertAttempt(t, db, "run", 1, "spec")
	attempt := 1
	in, err := db.EmitInterrupt(ctx, EmitInterruptCmd{
		RunID: "run", ExpectedRunVersion: 1, AttemptNo: &attempt, Reason: InterruptStartupStall,
		Facts:      map[string]string{"attempt_no": "1", "generation": "1", "diagnostic_cause": "termination_unconfirmed", "isolation_consequence": "worktree held", "recommended_action": "retry", "attempt_diagnostic_ref": "/attempt", "worktree_ref": "/worktree"},
		Generation: InterruptGeneration{AttemptNo: 1, Generation: 1}, GatePhase: GateNone, GuardrailLevel: GuardrailNone,
		ExpiresAfterMS: 10, OnExpire: ExpireEscalate, OnMaxEscalations: ExpireAutoReject, MaxEscalations: 0,
		AttentionDailyQuota: interruptQuota(), Source: SourceRecovery, NowMS: testNow,
	})
	if err == nil || in.ID != "" {
		t.Fatalf("startup_stall auto-reject policy must be rejected, got %#v, %v", in, err)
	}
	// A legitimate frozen policy still maps the exhausted startup_stall to hold.
	in, err = db.EmitInterrupt(ctx, EmitInterruptCmd{
		RunID: "run", ExpectedRunVersion: 1, AttemptNo: &attempt, Reason: InterruptStartupStall,
		Facts:      map[string]string{"attempt_no": "1", "generation": "1", "diagnostic_cause": "termination_unconfirmed", "isolation_consequence": "worktree held", "recommended_action": "retry", "attempt_diagnostic_ref": "/attempt", "worktree_ref": "/worktree"},
		Generation: InterruptGeneration{AttemptNo: 1, Generation: 1}, GatePhase: GateNone, GuardrailLevel: GuardrailNone,
		ExpiresAfterMS: 10, OnExpire: ExpireEscalate, OnMaxEscalations: ExpireHold, MaxEscalations: 0,
		AttentionDailyQuota: interruptQuota(), Source: SourceRecovery, NowMS: testNow,
		Channels: []InterruptChannel{{ID: "text", Capabilities: []string{"text"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var nonce string
	if err := db.db.QueryRow(`SELECT nonce FROM interrupts WHERE id=?`, in.ID).Scan(&nonce); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AdvanceInterrupt(ctx, AdvanceInterruptCmd{InterruptID: in.ID, ExpectedVersion: 1, ExpectedNonce: nonce, Kind: AdvanceExpiry, NowMS: testNow + 10}); err != nil {
		t.Fatal(err)
	}
	var status, held string
	if err := db.db.QueryRow(`SELECT status,held_reason FROM interrupts WHERE id=?`, in.ID).Scan(&status, &held); err != nil {
		t.Fatal(err)
	}
	if status != "open" || held != "max_escalations" {
		t.Fatalf("startup stall = %s/%s", status, held)
	}
}

func TestChannelRendererIncludesCanonicalCommands(t *testing.T) {
	rendered, commands, err := renderChannelInterrupt("标题", "说明", `[{"label":"log","target":"/log"}]`, `[{"id":"hold","label":"暂缓","effect":"等待","risk":"延迟"}]`, "run-1", "nonce-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 1 || commands[0] != "/sift hold run-1 nonce-1 1h" {
		t.Fatalf("commands=%q rendered=%q", commands, rendered)
	}
	if !strings.Contains(rendered, "log: /log") || !strings.Contains(rendered, commands[0]) {
		t.Fatalf("incomplete renderer: %q", rendered)
	}
}
