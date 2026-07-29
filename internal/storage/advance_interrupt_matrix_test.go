package storage

import (
	"context"
	"testing"
)

func TestAdvanceInterruptEscalationCountsReuseDowngrade(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	if err := db.SeedProjectForTest(ctx, "cfg", "project", testNow); err != nil {
		t.Fatal(err)
	}
	if err := db.SeedForgeRunForTest(ctx, "run", "project", "cfg", "42", testNow); err != nil {
		t.Fatal(err)
	}
	batchAt := int64(testNow + 1)
	cmd := t6Command(testNow)
	const expiry = int64(48 * 60 * 60 * 1000)
	cmd.ExpiresAfterMS, cmd.OnExpire, cmd.OnMaxEscalations, cmd.MaxEscalations = expiry, ExpireEscalate, ExpireHold, 2
	cmd.BatchAtMS = &batchAt
	cmd.Channels = []InterruptChannel{{ID: "ops", Capabilities: []string{"visual"}, Default: true}}
	cmd.T6 = func(context.Context, InterruptT6Input) (InterruptT6Output, error) {
		return InterruptT6Output{SuggestedDowngrade: true, Delivery: "batch", ChannelID: "ops"}, nil
	}
	in, err := emitTestInterrupt(t, ctx, db, cmd)
	if err != nil {
		t.Fatal(err)
	}
	want := []InterruptSeverity{SeverityNormal, SeverityHigh}
	for step, severity := range want {
		var version int64
		var nonce string
		if err := db.db.QueryRow(`SELECT version,nonce FROM interrupts WHERE id=?`, in.ID).Scan(&version, &nonce); err != nil {
			t.Fatal(err)
		}
		if ok, err := db.AdvanceInterrupt(ctx, AdvanceInterruptCmd{InterruptID: in.ID, ExpectedVersion: version, ExpectedNonce: nonce, Kind: AdvanceExpiry, NowMS: testNow + int64(step+1)*expiry}); err != nil || !ok {
			t.Fatalf("advance %d = %v, %v", step+1, ok, err)
		}
		var got string
		if err := db.db.QueryRow(`SELECT severity FROM interrupts WHERE id=?`, in.ID).Scan(&got); err != nil || InterruptSeverity(got) != severity {
			t.Fatalf("severity after %d = %q, %v; want %q", step+1, got, err, severity)
		}
	}
	var version int64
	var nonce string
	if err := db.db.QueryRow(`SELECT version,nonce FROM interrupts WHERE id=?`, in.ID).Scan(&version, &nonce); err != nil {
		t.Fatal(err)
	}
	if ok, err := db.AdvanceInterrupt(ctx, AdvanceInterruptCmd{InterruptID: in.ID, ExpectedVersion: version, ExpectedNonce: nonce, Kind: AdvanceExpiry, NowMS: testNow + 3*expiry}); err != nil || !ok {
		t.Fatalf("max advance = %v, %v", ok, err)
	}
	var state, held string
	if err := db.db.QueryRow(`SELECT dispatch_state,held_reason FROM interrupts WHERE id=?`, in.ID).Scan(&state, &held); err != nil || state != "held" || held != "max_escalations" {
		t.Fatalf("max result = %s/%s, %v", state, held, err)
	}
}
