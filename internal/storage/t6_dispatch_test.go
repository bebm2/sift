package storage

import (
	"context"
	"testing"
)

func t6Command(now int64) EmitInterruptCmd {
	return EmitInterruptCmd{
		RunID: "run", ExpectedRunVersion: 1, Reason: InterruptCodeReview,
		Facts:      map[string]string{"change_ref": "https://forge.example/change/1", "head_sha": "abc", "review_requirement": "required", "recommended_action": "approve", "diff_ref": "https://forge.example/change/1/diff"},
		Generation: InterruptGeneration{ChangeID: "change-01", HeadSHA: "0123456789012345678901234567890123456789"},
		GatePhase:  GateNone, GuardrailLevel: GuardrailNone, AttentionDailyQuota: interruptQuota(), DayTimezone: "UTC", Source: SourceSystem, NowMS: now,
	}
}

func TestEmitInterruptAdmitsT6AndPersistsDispatch(t *testing.T) {
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
	cmd.T6 = func(_ context.Context, in InterruptT6Input) (InterruptT6Output, error) {
		if in.DefaultChannelID != "ops" {
			t.Fatalf("default channel = %q", in.DefaultChannelID)
		}
		return InterruptT6Output{ChannelID: "ops", Delivery: "batch", SuggestedDowngrade: true}, nil
	}
	got, err := emitTestInterrupt(t, ctx, db, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if got.Severity != SeverityLow || !got.SuggestedDowngrade || got.ChannelID != "ops" || got.Delivery != "batch" || got.NextDispatchAtMS == nil || *got.NextDispatchAtMS != batchAt {
		t.Fatalf("dispatch = %#v", got)
	}
	var channel, delivery, held string
	var next int64
	var downgraded bool
	if err := db.db.QueryRow(`SELECT channel_id,delivery,COALESCE(held_reason,''),next_dispatch_at_ms,suggested_downgrade FROM interrupts WHERE id=?`, got.ID).Scan(&channel, &delivery, &held, &next, &downgraded); err != nil {
		t.Fatal(err)
	}
	if channel != "ops" || delivery != "batch" || held != "" || next != batchAt || !downgraded {
		t.Fatalf("persisted dispatch = %q/%q/%q/%d/%v", channel, delivery, held, next, downgraded)
	}
	cmd.ExpectedRunVersion = 99
	cmd.T6 = func(context.Context, InterruptT6Input) (InterruptT6Output, error) {
		t.Fatal("T6 called for an existing Interrupt")
		return InterruptT6Output{}, nil
	}
	again, err := emitTestInterrupt(t, ctx, db, cmd)
	if err != nil || again.ID != got.ID || !again.SuggestedDowngrade {
		t.Fatalf("replay = %#v, %v", again, err)
	}
}

func TestEmitInterruptT6InvalidFallsBackAndHighIsImmediate(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	if err := db.SeedProjectForTest(ctx, "cfg", "project", testNow); err != nil {
		t.Fatal(err)
	}
	if err := db.SeedForgeRunForTest(ctx, "run", "project", "cfg", "42", testNow); err != nil {
		t.Fatal(err)
	}
	cmd := t6Command(testNow)
	cmd.GatePhase = GateMerge
	cmd.Channels = []InterruptChannel{{ID: "visual", Capabilities: []string{"visual"}}}
	cmd.T6 = func(context.Context, InterruptT6Input) (InterruptT6Output, error) {
		return InterruptT6Output{ChannelID: "missing", Delivery: "batch"}, nil
	}
	got, err := emitTestInterrupt(t, ctx, db, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if got.Severity != SeverityHigh || got.Delivery != "immediate" || got.NextDispatchAtMS == nil || *got.NextDispatchAtMS != testNow {
		t.Fatalf("high fallback = %#v", got)
	}
}

func TestEmitInterruptHoldsWithoutCompatibleChannelWithoutCallingT6(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	if err := db.SeedProjectForTest(ctx, "cfg", "project", testNow); err != nil {
		t.Fatal(err)
	}
	if err := db.SeedForgeRunForTest(ctx, "run", "project", "cfg", "42", testNow); err != nil {
		t.Fatal(err)
	}
	called := false
	cmd := t6Command(testNow)
	cmd.Channels = []InterruptChannel{{ID: "visual", Capabilities: []string{"visual"}, Isolated: true}}
	cmd.T6 = func(context.Context, InterruptT6Input) (InterruptT6Output, error) {
		called = true
		return InterruptT6Output{}, nil
	}
	got, err := emitTestInterrupt(t, ctx, db, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if called || got.Delivery != "held" || got.HeldReason != "channel_isolated" || got.NextDispatchAtMS != nil {
		t.Fatalf("held dispatch = %#v, called=%v", got, called)
	}
	assertCount(t, db, "outbox_operations", 1) // forge comment remains the first delivery.
}
