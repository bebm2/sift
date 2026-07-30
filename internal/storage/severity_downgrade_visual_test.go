package storage

import (
	"context"
	"sync"
	"testing"
)

// These tests close WBS §5.2's remaining row: "LLM 只能建议 severity 降级；
// min_modality: visual renderer 拒绝语音路径". They prove the three invariants
// without touching the once-charge lifecycle, Command workers or the M5 gate:
//
//  1. BaseSeverity has no LLM/external severity input; EmitInterruptCmd carries
//     no severity field; InterruptT4Output has no severity field; the only way
//     an LLM/T6 suggestion can move severity is the single Severity(...) entry,
//     which lowers at most one level and never upgrades.
//  2. A min_modality=visual candidate that finds no visual-capable channel is
//     held; it is never downgraded onto a voice channel, and severity/T6 cannot
//     change min_modality.
//  3. Concurrent or repeated suggested-downgrade advice converges to a single
//     Interrupt, a single charge and a one-level (non-cumulative) downgrade.

// TestSeverityIsAtMostOneDowngradeAndNeverUpgrades exercises the sole final
// severity entry (interrupt.md §4.2) as a pure function. The LLM cannot set or
// upgrade severity: false returns the promoted base unchanged and true lowers
// at most one level (clamped at low). Non-compounding across replays/concurrency
// is a call-site discipline (apply once to the frozen base), covered by the
// integration tests below.
func TestSeverityIsAtMostOneDowngradeAndNeverUpgrades(t *testing.T) {
	ord := map[InterruptSeverity]int{SeverityLow: 0, SeverityNormal: 1, SeverityHigh: 2, SeverityCritical: 3}
	want := map[InterruptSeverity]InterruptSeverity{
		SeverityCritical: SeverityHigh,
		SeverityHigh:     SeverityNormal,
		SeverityNormal:   SeverityLow,
		SeverityLow:      SeverityLow,
	}
	for _, base := range []InterruptSeverity{SeverityLow, SeverityNormal, SeverityHigh, SeverityCritical} {
		// No advice: the LLM cannot move severity off the promoted base.
		if got := Severity(base, false); got != base {
			t.Errorf("Severity(%s, false) = %s, want %s", base, got, base)
		}
		got := Severity(base, true)
		// The downgrade never upgrades and is at most one level (clamped at low).
		if ord[got] > ord[base] {
			t.Errorf("Severity(%s, true) = %s upgraded severity", base, got)
		}
		if ord[base]-ord[got] > 1 {
			t.Errorf("Severity(%s, true) = %s lowered more than one level", base, got)
		}
		if got != want[base] {
			t.Errorf("Severity(%s, true) = %s, want %s", base, got, want[base])
		}
	}
	// Non-cumulation is a call-site discipline, not function idempotency:
	// the emitter applies Severity(...) once to the frozen base and persists the
	// decision, so replay/escalation reuse it rather than lowering again. That
	// property is covered by the replay and concurrent tests below.
}

// TestEmitInterruptVisualModalityHoldsRatherThanRoutingToVoice proves a visual
// candidate (code_review) never falls onto a voice-only channel: with no
// modality-compatible channel it is held, T6 is not invoked, min_modality stays
// visual and no channel delivery is created.
func TestEmitInterruptVisualModalityHoldsRatherThanRoutingToVoice(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	if err := db.SeedProjectForTest(ctx, "cfg", "project", testNow); err != nil {
		t.Fatal(err)
	}
	if err := db.SeedForgeRunForTest(ctx, "run", "project", "cfg", "42", testNow); err != nil {
		t.Fatal(err)
	}
	t6Called := false
	cmd := t6Command(testNow) // code_review -> min_modality=visual
	cmd.Channels = []InterruptChannel{{ID: "phone", Capabilities: []string{"voice"}, Default: true}}
	cmd.T6 = func(context.Context, InterruptT6Input) (InterruptT6Output, error) {
		t6Called = true
		return InterruptT6Output{}, nil
	}
	got, err := emitTestInterrupt(t, ctx, db, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if t6Called {
		t.Fatal("T6 must not be called when no channel is modality-compatible")
	}
	if got.MinModality != "visual" {
		t.Fatalf("min_modality = %q, want visual (must not downgrade to voice)", got.MinModality)
	}
	if got.Severity != SeverityNormal {
		t.Fatalf("severity = %s, want normal (base; no downgrade advice applied)", got.Severity)
	}
	if got.Delivery != "held" || got.HeldReason != "no_compatible_channel" || got.NextDispatchAtMS != nil {
		t.Fatalf("visual-over-voice dispatch = %#v", got)
	}
	// The forge comment is still the first surface; nothing leaks onto a voice channel.
	assertCount(t, db, "outbox_operations", 1)
	assertCount(t, db, "interrupt_deliveries", 1)
	var channelDeliveries int
	if err := db.db.QueryRow(`SELECT count(*) FROM interrupt_deliveries WHERE surface='channel'`).Scan(&channelDeliveries); err != nil {
		t.Fatal(err)
	}
	if channelDeliveries != 0 {
		t.Fatalf("channel deliveries = %d, want 0 (visual must not land on a voice channel)", channelDeliveries)
	}
}

// TestEmitInterruptT6SuggestedDowngradeNeverChangesModalityToVoice proves the
// one-level severity downgrade and the T4 brief cannot widen a visual
// candidate's modality: even with T4 and a downgrade-suggesting T6 both active,
// the Interrupt stays visual and routes only to a visual-capable channel. T4
// has no severity field, so the final severity comes solely from BaseSeverity
// plus the single Severity(...) downgrade.
func TestEmitInterruptT6SuggestedDowngradeNeverChangesModalityToVoice(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	if err := db.SeedProjectForTest(ctx, "cfg", "project", testNow); err != nil {
		t.Fatal(err)
	}
	if err := db.SeedForgeRunForTest(ctx, "run", "project", "cfg", "42", testNow); err != nil {
		t.Fatal(err)
	}
	t4Called := false
	db.SetInterruptT4(func(_ context.Context, in InterruptT4Input) (InterruptT4Output, error) {
		t4Called = true
		// T4 receives the promoted base severity and the reason's modality; it has
		// no field to return a different severity or modality.
		if in.Severity != SeverityNormal || in.Modality != "visual" {
			t.Fatalf("T4 input = severity %s / modality %q, want normal/visual", in.Severity, in.Modality)
		}
		return InterruptT4Output{Headline: in.Headline, Conclusion: "approve", KeyPoints: []string{"approve"}, Options: []string{"approve", "reject", "hold"}, RecommendedOptionID: "approve"}, nil
	})
	batchAt := int64(testNow + 100)
	cmd := t6Command(testNow)
	cmd.BatchAtMS = &batchAt
	cmd.Channels = []InterruptChannel{{ID: "ops", Capabilities: []string{"visual"}, Default: true}}
	cmd.T6 = func(_ context.Context, in InterruptT6Input) (InterruptT6Output, error) {
		if in.MinModality != "visual" {
			t.Fatalf("T6 input modality = %q, want visual", in.MinModality)
		}
		return InterruptT6Output{ChannelID: "ops", Delivery: "batch", SuggestedDowngrade: true}, nil
	}
	got, err := emitTestInterrupt(t, ctx, db, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if !t4Called {
		t.Fatal("T4 seam not invoked")
	}
	// normal -> low: exactly one level via the T6 suggested_downgrade, the only
	// severity-moving path; T4 never had a severity field to set or upgrade it.
	if got.Severity != SeverityLow || !got.SuggestedDowngrade {
		t.Fatalf("severity = %s, downgraded=%v, want low/true", got.Severity, got.SuggestedDowngrade)
	}
	// Modality is fixed by the reason template and cannot be widened to voice.
	if got.MinModality != "visual" || got.ChannelID != "ops" || got.Delivery != "batch" || got.NextDispatchAtMS == nil || *got.NextDispatchAtMS != batchAt {
		t.Fatalf("visual dispatch = %#v", got)
	}
}

// TestEmitInterruptT6SuggestedDowngradeLowersHighExactlyOneLevel proves the
// downgrade is bounded to one level on a promoted high candidate (code_review at
// merge gate): high -> normal, never to low and never an upgrade, and the
// one-level downgrade is what lets the candidate batch instead of being forced
// to immediate.
func TestEmitInterruptT6SuggestedDowngradeLowersHighExactlyOneLevel(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	if err := db.SeedProjectForTest(ctx, "cfg", "project", testNow); err != nil {
		t.Fatal(err)
	}
	if err := db.SeedForgeRunForTest(ctx, "run", "project", "cfg", "42", testNow); err != nil {
		t.Fatal(err)
	}
	batchAt := int64(testNow + 100)
	cmd := t6Command(testNow)
	cmd.GatePhase = GateMerge // code_review base normal -> high
	cmd.BatchAtMS = &batchAt
	cmd.Channels = []InterruptChannel{{ID: "ops", Capabilities: []string{"visual"}, Default: true}}
	cmd.T6 = func(context.Context, InterruptT6Input) (InterruptT6Output, error) {
		return InterruptT6Output{ChannelID: "ops", Delivery: "batch", SuggestedDowngrade: true}, nil
	}
	got, err := emitTestInterrupt(t, ctx, db, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if got.Severity != SeverityNormal || !got.SuggestedDowngrade || got.Delivery != "batch" || got.NextDispatchAtMS == nil || *got.NextDispatchAtMS != batchAt {
		t.Fatalf("high downgrade dispatch = %#v", got)
	}
}

// TestConcurrentEmitInterruptWithT6SuggestedDowngradeConvergesOneCharge proves
// that concurrent discovery of the same Interrupt, each carrying a
// suggested-downgrade T6 advice, converges to one Interrupt, one charge and a
// single one-level downgrade (startup_stall base high -> normal), with no
// billing bypass or repeated side effect. startup_stall is the standalone
// high-base reason (no Gate binding) used by the existing concurrency vector.
func TestConcurrentEmitInterruptWithT6SuggestedDowngradeConvergesOneCharge(t *testing.T) {
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
	batchAt := int64(testNow + 100)
	cmd := EmitInterruptCmd{RunID: "run", ExpectedRunVersion: 1, AttemptNo: &attempt, Reason: InterruptStartupStall,
		Facts:      map[string]string{"attempt_no": "1", "generation": "1", "diagnostic_cause": "termination_unconfirmed", "isolation_consequence": "worktree 保持隔离", "recommended_action": "retry", "attempt_diagnostic_ref": "/attempt", "worktree_ref": "/worktree"},
		Generation: InterruptGeneration{AttemptNo: 1, Generation: 1}, GatePhase: GateNone, GuardrailLevel: GuardrailNone, AttentionDailyQuota: interruptQuota(), DayTimezone: "UTC", Source: SourceRecovery, NowMS: testNow,
		Channels: []InterruptChannel{{ID: "txt", Capabilities: []string{"text"}, Default: true}}, BatchAtMS: &batchAt,
		T6: func(context.Context, InterruptT6Input) (InterruptT6Output, error) {
			return InterruptT6Output{ChannelID: "txt", Delivery: "batch", SuggestedDowngrade: true}, nil
		},
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := db.EmitInterrupt(ctx, cmd)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent emission = %v", err)
		}
	}
	assertCount(t, db, "interrupts", 1)
	assertCount(t, db, "budget_entries", 1)
	assertCount(t, db, "outbox_operations", 1)
	var severity string
	var downgraded bool
	if err := db.db.QueryRow(`SELECT severity,suggested_downgrade FROM interrupts WHERE generation_key=?`, mustGenerationKey(cmd)).Scan(&severity, &downgraded); err != nil {
		t.Fatal(err)
	}
	// startup_stall base is high; the accepted downgrade lowers it exactly one
	// level to normal, never to low and never re-charged.
	if InterruptSeverity(severity) != SeverityNormal || !downgraded {
		t.Fatalf("concurrent downgrade severity = %s, downgraded=%v, want normal/true", severity, downgraded)
	}
}

// TestEmitInterruptReplayKeepsSingleDowngradeAndSingleCharge proves that a
// repeated emit of the same Interrupt does not recall T6, does not re-charge,
// and does not re-apply the downgrade: the frozen one-level result is returned
// verbatim.
func TestEmitInterruptReplayKeepsSingleDowngradeAndSingleCharge(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	if err := db.SeedProjectForTest(ctx, "cfg", "project", testNow); err != nil {
		t.Fatal(err)
	}
	if err := db.SeedForgeRunForTest(ctx, "run", "project", "cfg", "42", testNow); err != nil {
		t.Fatal(err)
	}
	batchAt := int64(testNow + 100)
	cmd := t6Command(testNow)
	cmd.BatchAtMS = &batchAt
	cmd.Channels = []InterruptChannel{{ID: "ops", Capabilities: []string{"visual"}, Default: true}}
	cmd.T6 = func(context.Context, InterruptT6Input) (InterruptT6Output, error) {
		return InterruptT6Output{ChannelID: "ops", Delivery: "batch", SuggestedDowngrade: true}, nil
	}
	first, err := emitTestInterrupt(t, ctx, db, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if first.Severity != SeverityLow || !first.SuggestedDowngrade {
		t.Fatalf("first severity = %s, downgraded=%v, want low/true", first.Severity, first.SuggestedDowngrade)
	}
	// A replay must not re-invoke T6 nor compound the downgrade or the charge.
	cmd.ExpectedRunVersion = 99
	calls := 0
	cmd.T6 = func(context.Context, InterruptT6Input) (InterruptT6Output, error) {
		calls++
		return InterruptT6Output{ChannelID: "ops", Delivery: "batch", SuggestedDowngrade: true}, nil
	}
	again, err := emitTestInterrupt(t, ctx, db, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 || again.ID != first.ID || again.Severity != SeverityLow || !again.SuggestedDowngrade {
		t.Fatalf("replay = id %q (want %q) severity %s downgraded=%v t6calls=%d", again.ID, first.ID, again.Severity, again.SuggestedDowngrade, calls)
	}
	assertCount(t, db, "interrupts", 1)
	assertCount(t, db, "budget_entries", 1)
	assertCount(t, db, "outbox_operations", 1)
}
