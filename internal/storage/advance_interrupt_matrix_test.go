package storage

import (
	"context"
	"fmt"
	"testing"
	"time"
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

func TestAdvanceInterruptExpiryAndMaxOutcomeMatrix(t *testing.T) {
	cases := []struct {
		name                 string
		onExpire, onMax      ExpireAction
		max                  int
		wantStatus, wantHeld string
	}{
		{"expire hold", ExpireHold, ExpireHold, 1, "open", "expiry"},
		{"expire auto reject", ExpireAutoReject, ExpireHold, 1, "closed", ""},
		{"max hold", ExpireEscalate, ExpireHold, 0, "open", "max_escalations"},
		{"max auto reject", ExpireEscalate, ExpireAutoReject, 0, "closed", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, _ := openTestDB(t)
			ctx := context.Background()
			if err := db.SeedProjectForTest(ctx, "cfg", "project", testNow); err != nil {
				t.Fatal(err)
			}
			if err := db.SeedForgeRunForTest(ctx, "run", "project", "cfg", "42", testNow); err != nil {
				t.Fatal(err)
			}
			cmd := t6Command(testNow)
			cmd.ExpiresAfterMS, cmd.OnExpire, cmd.OnMaxEscalations, cmd.MaxEscalations = 10, tc.onExpire, tc.onMax, tc.max
			cmd.Channels = []InterruptChannel{{ID: "ops", Capabilities: []string{"visual"}}}
			cmd.T6 = func(context.Context, InterruptT6Input) (InterruptT6Output, error) {
				return InterruptT6Output{Delivery: "immediate", ChannelID: "ops"}, nil
			}
			in, err := emitTestInterrupt(t, ctx, db, cmd)
			if err != nil {
				t.Fatal(err)
			}
			var nonce string
			if err := db.db.QueryRow(`SELECT nonce FROM interrupts WHERE id=?`, in.ID).Scan(&nonce); err != nil {
				t.Fatal(err)
			}
			if ok, err := db.AdvanceInterrupt(ctx, AdvanceInterruptCmd{InterruptID: in.ID, ExpectedVersion: 1, ExpectedNonce: nonce, Kind: AdvanceExpiry, NowMS: testNow + 10}); err != nil || !ok {
				t.Fatalf("advance = %v, %v", ok, err)
			}
			var status, held, closeReason string
			if err := db.db.QueryRow(`SELECT status,COALESCE(held_reason,''),COALESCE(close_reason,'') FROM interrupts WHERE id=?`, in.ID).Scan(&status, &held, &closeReason); err != nil {
				t.Fatal(err)
			}
			if status != tc.wantStatus || held != tc.wantHeld {
				t.Fatalf("interrupt = %s/%s, want %s/%s", status, held, tc.wantStatus, tc.wantHeld)
			}
			if status == "closed" && closeReason != "expired_auto_reject" {
				t.Fatalf("close reason = %q", closeReason)
			}
		})
	}
}

func TestAdvanceInterruptDispatchUsesFrozenSummaryDue(t *testing.T) {
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
	cmd.ExpiresAfterMS, cmd.OnExpire, cmd.OnMaxEscalations, cmd.MaxEscalations = 72*60*60*1000, ExpireEscalate, ExpireHold, 1
	cmd.BatchAtMS = &batchAt
	cmd.Channels = []InterruptChannel{{ID: "ops", Capabilities: []string{"visual"}}}
	cmd.T6 = func(context.Context, InterruptT6Input) (InterruptT6Output, error) {
		return InterruptT6Output{Delivery: "batch", ChannelID: "ops", SuggestedDowngrade: true}, nil
	}
	in, err := emitTestInterrupt(t, ctx, db, cmd)
	if err != nil {
		t.Fatal(err)
	}
	var nonce string
	if err := db.db.QueryRow(`SELECT nonce FROM interrupts WHERE id=?`, in.ID).Scan(&nonce); err != nil {
		t.Fatal(err)
	}
	if ok, err := db.AdvanceInterrupt(ctx, AdvanceInterruptCmd{InterruptID: in.ID, ExpectedVersion: 1, ExpectedNonce: nonce, Kind: AdvanceExpiry, NowMS: testNow + 72*60*60*1000}); err != nil || !ok {
		t.Fatalf("expiry advance = %v, %v", ok, err)
	}
	var due int64
	if err := db.db.QueryRow(`SELECT next_dispatch_at_ms FROM interrupts WHERE id=?`, in.ID).Scan(&due); err != nil {
		t.Fatal(err)
	}
	if due <= testNow+72*60*60*1000 {
		t.Fatalf("frozen summary due = %d, want after expiry", due)
	}
	if err := db.SupervisorInterruptTick(ctx, due); err != nil {
		t.Fatal(err)
	}
	var batch, memberNonce string
	var batchDue, memberVersion int64
	if err := db.db.QueryRow(`SELECT m.batch_id,b.due_at_ms,m.nonce,m.interrupt_version FROM attention_batch_members m JOIN attention_batches b ON b.id=m.batch_id WHERE m.interrupt_id=?`, in.ID).Scan(&batch, &batchDue, &memberNonce, &memberVersion); err != nil {
		t.Fatal(err)
	}
	if batchDue != due || batch != "daily:project:UTC:"+fmt.Sprint(due)+":ops:github:Z2l0aHViLmNvbQ:b3JnL3JlcG8tcHJvamVjdA:issue:NDI" || memberVersion != 3 || memberNonce == nonce {
		t.Fatalf("daily member = %s/%d/%s/%d, want frozen due %d", batch, batchDue, memberNonce, memberVersion, due)
	}
	var authorityVersion int64
	var authorityNonce string
	if err := db.db.QueryRow(`SELECT interrupt_version,nonce FROM attention_batch_member_authority WHERE batch_id=? AND interrupt_id=?`, batch, in.ID).Scan(&authorityVersion, &authorityNonce); err != nil {
		t.Fatal(err)
	}
	if authorityVersion != memberVersion || authorityNonce != memberNonce {
		t.Fatalf("authority = %d/%s, member = %d/%s", authorityVersion, authorityNonce, memberVersion, memberNonce)
	}
}

func TestAdvanceInterruptRestartRejectsOldTickAndCreatesStrongEscalationDelivery(t *testing.T) {
	db, path := openTestDB(t)
	ctx := context.Background()
	if err := db.SeedProjectForTest(ctx, "cfg", "project", testNow); err != nil {
		t.Fatal(err)
	}
	if err := db.SeedForgeRunForTest(ctx, "run", "project", "cfg", "42", testNow); err != nil {
		t.Fatal(err)
	}
	cmd := t6Command(testNow)
	cmd.ExpiresAfterMS, cmd.OnExpire, cmd.OnMaxEscalations, cmd.MaxEscalations = 10, ExpireEscalate, ExpireHold, 1
	cmd.Channels = []InterruptChannel{{ID: "ops", Capabilities: []string{"visual"}}}
	cmd.T6 = func(context.Context, InterruptT6Input) (InterruptT6Output, error) {
		return InterruptT6Output{Delivery: "immediate", ChannelID: "ops"}, nil
	}
	in, err := emitTestInterrupt(t, ctx, db, cmd)
	if err != nil {
		t.Fatal(err)
	}
	var oldNonce string
	if err := db.db.QueryRow(`SELECT nonce FROM interrupts WHERE id=?`, in.ID).Scan(&oldNonce); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(ctx, OpenConfig{Path: path, BinaryVersion: "test-binary", Now: time.UnixMilli(testNow)})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.SupervisorInterruptTick(ctx, testNow); err != nil {
		t.Fatal(err)
	}
	if err := db.SupervisorInterruptTick(ctx, testNow+10); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AdvanceInterrupt(ctx, AdvanceInterruptCmd{InterruptID: in.ID, ExpectedVersion: 1, ExpectedNonce: oldNonce, Kind: AdvanceExpiry, NowMS: testNow + 10}); err != ErrRejectedStale {
		t.Fatalf("old tick = %v, want stale", err)
	}
	if err := db.SupervisorInterruptTick(ctx, testNow+10); err != nil {
		t.Fatal(err)
	}
	var priority string
	if err := db.db.QueryRow(`SELECT priority FROM interrupt_deliveries WHERE interrupt_id=? ORDER BY escalation_no DESC`, in.ID).Scan(&priority); err != nil {
		t.Fatal(err)
	}
	if priority != "strong" {
		t.Fatalf("escalation delivery priority = %q, want strong", priority)
	}
}
