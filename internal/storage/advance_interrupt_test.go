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
	insertTaskSpec(t, db, "spec", "run", 1)
	insertAttempt(t, db, "run", 1, "spec")
	mustExec(t, db, `INSERT INTO events(id,run_id,attempt_no,project_id,type,source,payload_schema_version,payload_json,occurred_at_ms,recorded_at_ms) VALUES ('report-event','run',1,'project','report','agent',1,'{}',?,?)`, testNow, testNow)
	mustExec(t, db, `INSERT INTO report_receipts(id,run_id,attempt_no,report_key,report_kind,payload_digest,event_id,received_at_ms) VALUES ('report-1','run',1,'report-1','blocker','digest','report-event',?)`, testNow)
	batch := int64(testNow + 2)
	attempt := 1
	in, err := db.EmitInterrupt(ctx, EmitInterruptCmd{
		RunID: "run", ExpectedRunVersion: 1, AttemptNo: &attempt, Reason: InterruptAgentBlocked,
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

func TestAdvanceInterruptRepeatedCriticalFuseSealsCurrentAuthority(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	if err := db.SeedProjectForTest(ctx, "cfg", "project", testNow); err != nil {
		t.Fatal(err)
	}
	for _, run := range []string{"run", "run-2"} {
		if err := db.SeedForgeRunForTest(ctx, run, "project", "cfg", map[string]string{"run": "42", "run-2": "43"}[run], testNow); err != nil {
			t.Fatal(err)
		}
	}
	emit := func(run string, now int64) Interrupt {
		cmd := t6Command(now)
		cmd.RunID = run
		cmd.Generation.ChangeID = "change-" + run
		cmd.ExpiresAfterMS, cmd.OnExpire, cmd.OnMaxEscalations, cmd.MaxEscalations = 10, ExpireEscalate, ExpireHold, 3
		cmd.CriticalTotalLimit, cmd.CriticalPerRunLimit = 1, 10
		batchAt := now + 1
		cmd.BatchAtMS = &batchAt
		cmd.Channels = []InterruptChannel{{ID: "ops", Capabilities: []string{"visual"}, Default: true}}
		in, err := emitTestInterrupt(t, ctx, db, cmd)
		if err != nil {
			t.Fatal(err)
		}
		return in
	}
	advance := func(id string, version int64, nonce string, now int64) (int64, string) {
		ok, err := db.AdvanceInterrupt(ctx, AdvanceInterruptCmd{InterruptID: id, ExpectedVersion: version, ExpectedNonce: nonce, Kind: AdvanceExpiry, NowMS: now})
		if err != nil || !ok {
			var gotVersion, expires int64
			var gotNonce, state string
			_ = db.db.QueryRow(`SELECT version,nonce,expires_at_ms,dispatch_state FROM interrupts WHERE id=?`, id).Scan(&gotVersion, &gotNonce, &expires, &state)
			t.Fatalf("advance %s = %v, %v (got version=%d nonce=%s expires=%d state=%s)", id, ok, err, gotVersion, gotNonce, expires, state)
		}
		var gotVersion int64
		var gotNonce string
		if err := db.db.QueryRow(`SELECT version,nonce FROM interrupts WHERE id=?`, id).Scan(&gotVersion, &gotNonce); err != nil {
			t.Fatal(err)
		}
		return gotVersion, gotNonce
	}

	// The first Interrupt occupies the sole admitted-critical slot.
	admitted := emit("run", testNow)
	var nonce string
	if err := db.db.QueryRow(`SELECT nonce FROM interrupts WHERE id=?`, admitted.ID).Scan(&nonce); err != nil {
		t.Fatal(err)
	}
	version, nonce := advance(admitted.ID, 1, nonce, testNow+10)
	_, _ = advance(admitted.ID, version, nonce, testNow+20) // admitted critical

	// The second one fuses twice. The second fuse must refresh only the
	// collecting authority, so sealing uses its newest nonce/version.
	fused := emit("run-2", testNow+100)
	if err := db.db.QueryRow(`SELECT nonce FROM interrupts WHERE id=?`, fused.ID).Scan(&nonce); err != nil {
		t.Fatal(err)
	}
	version, nonce = advance(fused.ID, 1, nonce, testNow+110)       // normal → high
	version, nonce = advance(fused.ID, version, nonce, testNow+120) // high → fused critical
	version, nonce = advance(fused.ID, version, nonce, testNow+130) // repeated fused critical
	var batch, authorityNonce string
	var authorityVersion int64
	if err := db.db.QueryRow(`SELECT batch_id FROM attention_batch_members WHERE interrupt_id=?`, fused.ID).Scan(&batch); err != nil {
		t.Fatal(err)
	}
	if err := db.db.QueryRow(`SELECT interrupt_version,nonce FROM attention_batch_member_authority WHERE batch_id=? AND interrupt_id=?`, batch, fused.ID).Scan(&authorityVersion, &authorityNonce); err != nil {
		t.Fatal(err)
	}
	if authorityVersion != version || authorityNonce != nonce {
		t.Fatalf("authority = %d/%s, want %d/%s", authorityVersion, authorityNonce, version, nonce)
	}
	if err := db.PrepareDueAttentionBatches(ctx, testNow+1_000_000); err != nil {
		t.Fatal(err)
	}
	var payload string
	if err := db.db.QueryRow(`SELECT payload_json FROM attention_batches WHERE id=?`, batch).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(payload, `"interrupt_version":4`) || !strings.Contains(payload, `"nonce":"`+nonce+`"`) {
		t.Fatalf("sealed payload did not use current authority: %s", payload)
	}
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
	cmd := t6Command(testNow)
	cmd.BatchAtMS, cmd.Channels = &at, []InterruptChannel{{ID: "ops", Capabilities: []string{"visual"}}}
	in, err := emitTestInterrupt(t, ctx, db, cmd)
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
	got, err := emitTestInterrupt(t, ctx, db, cmd)
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

func TestNextDailySummaryAtSkipsTheCurrentOccurrence(t *testing.T) {
	const at = int64(1785286800000)
	got, ok := NextDailySummaryAt(at, "Asia/Shanghai", "09:00")
	if !ok || got <= at {
		t.Fatalf("next summary at instant = %d, %v", got, ok)
	}
	oneMSLater, ok := NextDailySummaryAt(at+1, "Asia/Shanghai", "09:00")
	if !ok || oneMSLater != got {
		t.Fatalf("next summary after instant = %d, %v; want %d", oneMSLater, ok, got)
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
