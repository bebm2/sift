package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"
)

// advanceOutcome is the per-cell state vector asserted by the AdvanceInterrupt
// outcome matrices (interrupt.md §8.2 point 1–3, §9.2). Nullable string columns
// are read via COALESCE so the empty value denotes NULL; next_dispatch_at_ms is
// the only nullable integer and is compared as a sql.NullInt64.
type advanceOutcome struct {
	status, dispatchState, delivery, severity string
	held, closeReason                         string
	version, escalation, expiresAt            int64
	nonce                                     string
	nextDispatch                              sql.NullInt64
	admissions, charges                       int
	channelOps, members, authority            int
}

func readAdvanceOutcome(t *testing.T, db *DB, id string) advanceOutcome {
	t.Helper()
	var o advanceOutcome
	if err := db.db.QueryRow(`SELECT status,dispatch_state,delivery,severity,COALESCE(held_reason,''),COALESCE(close_reason,''),version,nonce,escalation_count,expires_at_ms,next_dispatch_at_ms FROM interrupts WHERE id=?`, id).Scan(&o.status, &o.dispatchState, &o.delivery, &o.severity, &o.held, &o.closeReason, &o.version, &o.nonce, &o.escalation, &o.expiresAt, &o.nextDispatch); err != nil {
		t.Fatalf("read outcome: %v", err)
	}
	for _, q := range []struct {
		sql string
		dst *int
	}{
		{`SELECT count(*) FROM attention_admissions WHERE interrupt_id=?`, &o.admissions},
		{`SELECT count(*) FROM attention_admissions WHERE interrupt_id=? AND attention_charge_entry_id IS NOT NULL`, &o.charges},
		{`SELECT count(*) FROM outbox_operations WHERE kind='channel_publish' AND interrupt_id=?`, &o.channelOps},
		{`SELECT count(*) FROM attention_batch_members WHERE interrupt_id=?`, &o.members},
		{`SELECT count(*) FROM attention_batch_member_authority WHERE interrupt_id=?`, &o.authority},
	} {
		if err := db.db.QueryRow(q.sql, id).Scan(q.dst); err != nil {
			t.Fatalf("read accounting: %v", err)
		}
	}
	return o
}

// assertAdvanceOutcome compares the full per-cell state vector. prevNonce plus
// nonceRotated express the §8.2 invariant that only escalation rotates the
// nonce: hold/auto-reject bump the version but keep the authority nonce.
func assertAdvanceOutcome(t *testing.T, got, want advanceOutcome, prevNonce string, nonceRotated bool) {
	t.Helper()
	var problems []string
	add := func(label, gotV, wantV string) {
		if gotV != wantV {
			problems = append(problems, fmt.Sprintf("%s=%q want %q", label, gotV, wantV))
		}
	}
	add("status", got.status, want.status)
	add("dispatch_state", got.dispatchState, want.dispatchState)
	add("delivery", got.delivery, want.delivery)
	add("severity", got.severity, want.severity)
	add("held_reason", got.held, want.held)
	add("close_reason", got.closeReason, want.closeReason)
	if got.version != want.version {
		problems = append(problems, fmt.Sprintf("version=%d want %d", got.version, want.version))
	}
	if got.escalation != want.escalation {
		problems = append(problems, fmt.Sprintf("escalation_count=%d want %d", got.escalation, want.escalation))
	}
	if got.expiresAt != want.expiresAt {
		problems = append(problems, fmt.Sprintf("expires_at_ms=%d want %d", got.expiresAt, want.expiresAt))
	}
	if nonceRotated {
		if got.nonce == prevNonce || got.nonce == "" {
			problems = append(problems, fmt.Sprintf("nonce=%q not rotated from %q", got.nonce, prevNonce))
		}
	} else if got.nonce != prevNonce {
		problems = append(problems, fmt.Sprintf("nonce=%q want unchanged %q", got.nonce, prevNonce))
	}
	if got.nextDispatch != want.nextDispatch {
		problems = append(problems, fmt.Sprintf("next_dispatch_at_ms=%v want %v", got.nextDispatch, want.nextDispatch))
	}
	if got.admissions != want.admissions {
		problems = append(problems, fmt.Sprintf("admissions=%d want %d", got.admissions, want.admissions))
	}
	if got.charges != want.charges {
		problems = append(problems, fmt.Sprintf("charges=%d want %d", got.charges, want.charges))
	}
	if got.channelOps != want.channelOps {
		problems = append(problems, fmt.Sprintf("channel_operations=%d want %d", got.channelOps, want.channelOps))
	}
	if got.members != want.members {
		problems = append(problems, fmt.Sprintf("batch_members=%d want %d", got.members, want.members))
	}
	if got.authority != want.authority {
		problems = append(problems, fmt.Sprintf("batch_authority=%d want %d", got.authority, want.authority))
	}
	if len(problems) > 0 {
		t.Fatalf("advance outcome mismatch:\n  %s", strings.Join(problems, "\n  "))
	}
}

// assertStaleReplayRejected re-runs an advance with a stale CAS (the one the
// supervisor just consumed) and asserts the single-CAS port rejects it and
// mutates nothing (interrupt.md §8.2).
func assertStaleReplayRejected(t *testing.T, db *DB, id string, staleVersion int64, staleNonce string, now int64) {
	t.Helper()
	pre := readAdvanceOutcome(t, db, id)
	ok, err := db.AdvanceInterrupt(context.Background(), AdvanceInterruptCmd{InterruptID: id, ExpectedVersion: staleVersion, ExpectedNonce: staleNonce, Kind: AdvanceExpiry, NowMS: now})
	if ok || err != ErrRejectedStale {
		t.Fatalf("stale replay = %v, %v, want false/ErrRejectedStale", ok, err)
	}
	if post := readAdvanceOutcome(t, db, id); post != pre {
		t.Fatalf("stale replay mutated state:\n  pre=%+v\n  post=%+v", pre, post)
	}
}

func mustNextSummary(t *testing.T, now int64, zone, clock string) int64 {
	t.Helper()
	at, ok := NextDailySummaryAt(now, zone, clock)
	if !ok {
		t.Fatalf("next summary at %d (%s/%s)", now, zone, clock)
	}
	return at
}

func TestAdvanceInterruptPostEscalationSummaryExpiryBoundaries(t *testing.T) {
	base := time.UnixMilli(testNow).UTC()
	midnight := time.Date(base.Year(), base.Month(), base.Day()+1, 0, 0, 0, 0, time.UTC).UnixMilli()
	initial := midnight
	for _, tc := range []struct {
		name         string
		expiresAfter int64
		wantState    string
		wantHeld     string
	}{
		{"summary after new expiry", 11 * time.Hour.Milliseconds(), "held", "batch_after_expiry"},
		{"summary at new expiry", 12 * time.Hour.Milliseconds(), "held", "batch_after_expiry"},
		{"summary before new expiry", 13 * time.Hour.Milliseconds(), "ready", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, _ := openTestDB(t)
			ctx := context.Background()
			if err := db.SeedProjectForTest(ctx, "cfg", "project", initial); err != nil {
				t.Fatal(err)
			}
			if err := db.SeedForgeRunForTest(ctx, "run", "project", "cfg", "42", initial); err != nil {
				t.Fatal(err)
			}
			batchAt := initial + 1
			cmd := t6Command(initial)
			cmd.ExpiresAfterMS, cmd.BatchAtMS = tc.expiresAfter, &batchAt
			cmd.OnExpire, cmd.OnMaxEscalations, cmd.MaxEscalations = ExpireEscalate, ExpireHold, 1
			cmd.DailySummaryAt = "00:00"
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
			if ok, err := db.AdvanceInterrupt(ctx, AdvanceInterruptCmd{InterruptID: in.ID, ExpectedVersion: 1, ExpectedNonce: nonce, Kind: AdvanceExpiry, NowMS: initial + tc.expiresAfter}); err != nil || !ok {
				t.Fatalf("advance = %v, %v", ok, err)
			}
			var state, held, newNonce string
			var version, escalation, expiresAt int64
			var due sql.NullInt64
			if err := db.db.QueryRow(`SELECT dispatch_state,COALESCE(held_reason,''),version,nonce,escalation_count,expires_at_ms,next_dispatch_at_ms FROM interrupts WHERE id=?`, in.ID).Scan(&state, &held, &version, &newNonce, &escalation, &expiresAt, &due); err != nil {
				t.Fatal(err)
			}
			if state != tc.wantState || held != tc.wantHeld || version != 2 || newNonce == nonce || escalation != 1 || expiresAt != initial+2*tc.expiresAfter {
				t.Fatalf("post-escalation state=%s/%s version=%d nonce=%q escalation=%d expiry=%d", state, held, version, newNonce, escalation, expiresAt)
			}
			if tc.wantState == "ready" {
				if !due.Valid || due.Int64 != initial+24*time.Hour.Milliseconds() {
					t.Fatalf("summary due=%v, want next midnight", due)
				}
			} else if due.Valid {
				t.Fatalf("held interrupt retained due %d", due.Int64)
			}
			var admissions, charges, operations, members, authority int
			if err := db.db.QueryRow(`SELECT count(*) FROM attention_admissions WHERE interrupt_id=?`, in.ID).Scan(&admissions); err != nil {
				t.Fatal(err)
			}
			if err := db.db.QueryRow(`SELECT count(*) FROM attention_admissions WHERE interrupt_id=? AND attention_charge_entry_id IS NOT NULL`, in.ID).Scan(&charges); err != nil {
				t.Fatal(err)
			}
			if err := db.db.QueryRow(`SELECT count(*) FROM outbox_operations WHERE kind='channel_publish' AND interrupt_id=?`, in.ID).Scan(&operations); err != nil {
				t.Fatal(err)
			}
			if err := db.db.QueryRow(`SELECT count(*) FROM attention_batch_members WHERE interrupt_id=?`, in.ID).Scan(&members); err != nil {
				t.Fatal(err)
			}
			if err := db.db.QueryRow(`SELECT count(*) FROM attention_batch_member_authority WHERE interrupt_id=?`, in.ID).Scan(&authority); err != nil {
				t.Fatal(err)
			}
			if admissions != 1 || charges != 1 || operations != 0 || members != 0 || authority != 0 {
				t.Fatalf("accounting admissions/charges/operations/members/authority=%d/%d/%d/%d/%d", admissions, charges, operations, members, authority)
			}
		})
	}
}

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
	var nonce string
	if err := db.db.QueryRow(`SELECT nonce FROM interrupts WHERE id=?`, in.ID).Scan(&nonce); err != nil {
		t.Fatal(err)
	}
	// Initial frozen dispatch: low (downgraded from normal) batch at the frozen
	// summary due, one charged admission and no member/authority yet.
	assertAdvanceOutcome(t, readAdvanceOutcome(t, db, in.ID), advanceOutcome{
		status: "open", dispatchState: "ready", delivery: "batch", severity: "low",
		version: 1, escalation: 0, expiresAt: testNow + expiry, nextDispatch: sql.NullInt64{Int64: batchAt, Valid: true},
		admissions: 1, charges: 1, channelOps: 0, members: 0, authority: 0,
	}, nonce, false)
	steps := []struct {
		now          int64
		severity     InterruptSeverity
		delivery     string
		escalation   int64
		nonceRotated bool
		due          sql.NullInt64
	}{
		{testNow + expiry, SeverityNormal, "batch", 1, true, sql.NullInt64{Int64: mustNextSummary(t, testNow+expiry, "UTC", "09:00"), Valid: true}},
		{testNow + 2*expiry, SeverityHigh, "immediate", 2, true, sql.NullInt64{Int64: testNow + 2*expiry, Valid: true}},
	}
	var version int64 = 1
	for step, s := range steps {
		if err := db.db.QueryRow(`SELECT version,nonce FROM interrupts WHERE id=?`, in.ID).Scan(&version, &nonce); err != nil {
			t.Fatal(err)
		}
		if ok, err := db.AdvanceInterrupt(ctx, AdvanceInterruptCmd{InterruptID: in.ID, ExpectedVersion: version, ExpectedNonce: nonce, Kind: AdvanceExpiry, NowMS: s.now}); err != nil || !ok {
			t.Fatalf("advance %d = %v, %v", step+1, ok, err)
		}
		// Each escalation reuses the frozen downgrade, rotates nonce/version,
		// advances expiry and recomputes the dispatch due without borrowing a
		// second charge, creating a member or pre-publishing a channel op.
		assertAdvanceOutcome(t, readAdvanceOutcome(t, db, in.ID), advanceOutcome{
			status: "open", dispatchState: "ready", delivery: s.delivery, severity: string(s.severity),
			version: version + 1, escalation: s.escalation, expiresAt: s.now + expiry, nextDispatch: s.due,
			admissions: 1, charges: 1, channelOps: 0, members: 0, authority: 0,
		}, nonce, s.nonceRotated)
		// The CAS the supervisor just consumed is now stale; replaying it must
		// not re-escalate, re-charge or rotate the nonce again.
		assertStaleReplayRejected(t, db, in.ID, version, nonce, s.now+1)
	}
	// Reaching the cap holds without escalating past it or rotating the nonce.
	if err := db.db.QueryRow(`SELECT version,nonce FROM interrupts WHERE id=?`, in.ID).Scan(&version, &nonce); err != nil {
		t.Fatal(err)
	}
	if ok, err := db.AdvanceInterrupt(ctx, AdvanceInterruptCmd{InterruptID: in.ID, ExpectedVersion: version, ExpectedNonce: nonce, Kind: AdvanceExpiry, NowMS: testNow + 3*expiry}); err != nil || !ok {
		t.Fatalf("max advance = %v, %v", ok, err)
	}
	assertAdvanceOutcome(t, readAdvanceOutcome(t, db, in.ID), advanceOutcome{
		status: "open", dispatchState: "held", delivery: "held", severity: "high", held: "max_escalations",
		version: version + 1, escalation: 2, expiresAt: testNow + 3*expiry, nextDispatch: sql.NullInt64{},
		admissions: 1, charges: 1, channelOps: 0, members: 0, authority: 0,
	}, nonce, false)
}

func TestAdvanceInterruptExpiryAndMaxOutcomeMatrix(t *testing.T) {
	cases := []struct {
		name                              string
		onExpire, onMax                   ExpireAction
		max                               int
		wantStatus, wantState             string
		wantHeld, wantClose, wantDelivery string
	}{
		{"expire hold", ExpireHold, ExpireHold, 1, "open", "held", "expiry", "", "held"},
		{"expire auto reject", ExpireAutoReject, ExpireHold, 1, "closed", "batched", "", "expired_auto_reject", "immediate"},
		{"max hold", ExpireEscalate, ExpireHold, 0, "open", "held", "max_escalations", "", "held"},
		{"max auto reject", ExpireEscalate, ExpireAutoReject, 0, "closed", "batched", "", "expired_auto_reject", "immediate"},
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
			// Per-cell full state vector: hold/auto-reject bump the version but
			// never rotate the nonce, escalate nothing, leave expiry frozen and
			// borrow no second charge, member or channel operation.
			assertAdvanceOutcome(t, readAdvanceOutcome(t, db, in.ID), advanceOutcome{
				status: tc.wantStatus, dispatchState: tc.wantState, delivery: tc.wantDelivery, severity: "normal",
				held: tc.wantHeld, closeReason: tc.wantClose,
				version: 2, escalation: 0, expiresAt: testNow + 10, nextDispatch: sql.NullInt64{},
				admissions: 1, charges: 1, channelOps: 1, members: 0, authority: 0,
			}, nonce, false)
			assertStaleReplayRejected(t, db, in.ID, 1, nonce, testNow+20)
		})
	}
}

// TestAdvanceInterruptReasonOutcomeMatrix closes the allowed/prohibited reason
// × on-expire/on-max table (interrupt.md §8.2 point 3). startup_stall forbids
// auto_reject on both clocks at creation and, even at the escalation cap, can
// only end open+held; an allowed reason honours hold/escalate/auto_reject.
func TestAdvanceInterruptReasonOutcomeMatrix(t *testing.T) {
	const expiry = int64(48 * 60 * 60 * 1000)
	cases := []struct {
		name                              string
		reason                            InterruptReason
		onExpire, onMax                   ExpireAction
		max                               int
		rejectEmit                        bool
		wantStatus, wantState             string
		wantHeld, wantClose, wantDelivery string
		wantSeverity                      string
	}{
		{"allowed expire hold", InterruptCodeReview, ExpireHold, ExpireHold, 1, false, "open", "held", "expiry", "", "held", "normal"},
		{"allowed expire auto reject", InterruptCodeReview, ExpireAutoReject, ExpireHold, 1, false, "closed", "batched", "", "expired_auto_reject", "immediate", "normal"},
		{"allowed max hold", InterruptCodeReview, ExpireEscalate, ExpireHold, 0, false, "open", "held", "max_escalations", "", "held", "normal"},
		{"allowed max auto reject", InterruptCodeReview, ExpireEscalate, ExpireAutoReject, 0, false, "closed", "batched", "", "expired_auto_reject", "immediate", "normal"},
		{"startup stall expire hold", InterruptStartupStall, ExpireHold, ExpireHold, 1, false, "open", "held", "expiry", "", "held", "high"},
		{"startup stall max hold", InterruptStartupStall, ExpireEscalate, ExpireHold, 0, false, "open", "held", "max_escalations", "", "held", "high"},
		{"startup stall expire auto reject prohibited", InterruptStartupStall, ExpireAutoReject, ExpireHold, 1, true, "", "", "", "", "", ""},
		{"startup stall max auto reject prohibited", InterruptStartupStall, ExpireEscalate, ExpireAutoReject, 0, true, "", "", "", "", "", ""},
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
			cmd.Reason = tc.reason
			cmd.ExpiresAfterMS, cmd.OnExpire, cmd.OnMaxEscalations, cmd.MaxEscalations = expiry, tc.onExpire, tc.onMax, tc.max
			// One channel covers both modality requirements: code_review is
			// visual and startup_stall is text.
			cmd.Channels = []InterruptChannel{{ID: "ops", Capabilities: []string{"visual", "text"}}}
			if tc.reason == InterruptStartupStall {
				insertTaskSpec(t, db, "spec", "run", 1)
				insertAttempt(t, db, "run", 1, "spec")
				attempt := 1
				cmd.AttemptNo = &attempt
				cmd.Generation = InterruptGeneration{AttemptNo: 1, Generation: 1}
				cmd.Facts = map[string]string{"attempt_no": "1", "generation": "1", "diagnostic_cause": "termination_unconfirmed", "isolation_consequence": "worktree held", "recommended_action": "retry", "attempt_diagnostic_ref": "/attempt", "worktree_ref": "/worktree"}
				cmd.Source = SourceRecovery
			} else {
				cmd.T6 = func(context.Context, InterruptT6Input) (InterruptT6Output, error) {
					return InterruptT6Output{Delivery: "immediate", ChannelID: "ops"}, nil
				}
			}
			in, err := emitTestInterrupt(t, ctx, db, cmd)
			if tc.rejectEmit {
				if err == nil {
					t.Fatalf("emit must reject startup_stall with auto_reject policy")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var nonce string
			if err := db.db.QueryRow(`SELECT nonce FROM interrupts WHERE id=?`, in.ID).Scan(&nonce); err != nil {
				t.Fatal(err)
			}
			if ok, err := db.AdvanceInterrupt(ctx, AdvanceInterruptCmd{InterruptID: in.ID, ExpectedVersion: 1, ExpectedNonce: nonce, Kind: AdvanceExpiry, NowMS: testNow + expiry}); err != nil || !ok {
				t.Fatalf("advance = %v, %v", ok, err)
			}
			// Both reasons emit as immediate (code_review via T6, startup_stall
			// via its High base), so the initial channel operation persists and
			// the outcome never borrows a second charge, member or operation.
			assertAdvanceOutcome(t, readAdvanceOutcome(t, db, in.ID), advanceOutcome{
				status: tc.wantStatus, dispatchState: tc.wantState, delivery: tc.wantDelivery, severity: tc.wantSeverity,
				held: tc.wantHeld, closeReason: tc.wantClose,
				version: 2, escalation: 0, expiresAt: testNow + expiry, nextDispatch: sql.NullInt64{},
				admissions: 1, charges: 1, channelOps: 1, members: 0, authority: 0,
			}, nonce, false)
			assertStaleReplayRejected(t, db, in.ID, 1, nonce, testNow+expiry+1)
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

func TestEmitInterruptSummaryExpiryBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name       string
		delta      int64
		wantState  string
		wantHeld   string
		wantMember int
	}{
		{"before expiry", 99, "ready", "", 0},
		{"at expiry", 100, "held", "batch_after_expiry", 0},
		{"after expiry", 101, "held", "batch_after_expiry", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, _ := openTestDB(t)
			ctx := context.Background()
			if err := db.SeedProjectForTest(ctx, "cfg", "project", testNow); err != nil {
				t.Fatal(err)
			}
			if err := db.SeedForgeRunForTest(ctx, "run", "project", "cfg", "42", testNow); err != nil {
				t.Fatal(err)
			}
			batchAt := testNow + tc.delta
			cmd := t6Command(testNow)
			cmd.ExpiresAfterMS, cmd.BatchAtMS = 100, &batchAt
			cmd.Channels = []InterruptChannel{{ID: "ops", Capabilities: []string{"visual"}}}
			in, err := emitTestInterrupt(t, ctx, db, cmd)
			if err != nil {
				t.Fatal(err)
			}
			var state, held string
			if err := db.db.QueryRow(`SELECT dispatch_state,COALESCE(held_reason,'') FROM interrupts WHERE id=?`, in.ID).Scan(&state, &held); err != nil {
				t.Fatal(err)
			}
			if state != tc.wantState || held != tc.wantHeld {
				t.Fatalf("dispatch = %s/%s, want %s/%s", state, held, tc.wantState, tc.wantHeld)
			}
			var members, authority, operations int
			if err := db.db.QueryRow(`SELECT count(*) FROM attention_batch_members WHERE interrupt_id=?`, in.ID).Scan(&members); err != nil {
				t.Fatal(err)
			}
			if err := db.db.QueryRow(`SELECT count(*) FROM attention_batch_member_authority a JOIN attention_batch_members m ON m.batch_id=a.batch_id AND m.interrupt_id=a.interrupt_id WHERE m.interrupt_id=?`, in.ID).Scan(&authority); err != nil {
				t.Fatal(err)
			}
			if err := db.db.QueryRow(`SELECT count(*) FROM outbox_operations WHERE kind='channel_publish'`).Scan(&operations); err != nil {
				t.Fatal(err)
			}
			if members != tc.wantMember || authority != tc.wantMember || operations != 0 {
				t.Fatalf("member/authority/channel operations = %d/%d/%d", members, authority, operations)
			}
		})
	}
}

func TestAdvanceInterruptExcludesStaleDailyMembersAndCancelsEmptyBatch(t *testing.T) {
	// Commander-mode idle heartbeat (#1010) changes the contract: a daily
	// summary batch that collected zero admitted members is still a digest
	// surface. When the project has no active Run AND at least one Run has
	// touched `updated_at_ms` inside IdleRunActivityWindowMS, the sealer
	// publishes a single status_note line instead of silently cancelling.
	// "Active" runs (status IN queued/running/waiting_human) and runs whose
	// last activity is older than the window keep the original silent
	// cancellation, so the matrix only diverges for cases where Advance
	// drives the run to a terminal state within the window — currently just
	// `close` below. The other cases retain their original expectation
	// because they leave a run in waiting_human, which counts as active.
	for _, tc := range []struct {
		name       string
		onExpire   ExpireAction
		onMax      ExpireAction
		max        int
		wantStatus string
		wantState  string
		wantOps    int
	}{
		{"close", ExpireAutoReject, ExpireHold, 1, "closed", "sealed", 1},
		{"version change", ExpireEscalate, ExpireHold, 1, "open", "cancelled", 0},
		{"expire hold", ExpireHold, ExpireHold, 1, "open", "cancelled", 0},
		{"max hold", ExpireEscalate, ExpireHold, 0, "open", "cancelled", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, path := openTestDB(t)
			ctx := context.Background()
			if err := db.SeedProjectForTest(ctx, "cfg", "project", testNow); err != nil {
				t.Fatal(err)
			}
			if err := db.SeedForgeRunForTest(ctx, "run", "project", "cfg", "42", testNow); err != nil {
				t.Fatal(err)
			}
			const expiry = int64(48 * 60 * 60 * 1000)
			cmd := t6Command(testNow)
			cmd.AttentionDailyQuota = map[InterruptSeverity]int{SeverityLow: 0, SeverityNormal: 0, SeverityHigh: 0}
			cmd.ExpiresAfterMS, cmd.OnExpire, cmd.OnMaxEscalations, cmd.MaxEscalations = expiry, tc.onExpire, tc.onMax, tc.max
			cmd.Channels = []InterruptChannel{{ID: "ops", Capabilities: []string{"visual"}}}
			in, err := emitTestInterrupt(t, ctx, db, cmd)
			if err != nil {
				t.Fatal(err)
			}
			var nonce string
			if err := db.db.QueryRow(`SELECT nonce FROM interrupts WHERE id=?`, in.ID).Scan(&nonce); err != nil {
				t.Fatal(err)
			}
			if ok, err := db.AdvanceInterrupt(ctx, AdvanceInterruptCmd{InterruptID: in.ID, ExpectedVersion: 1, ExpectedNonce: nonce, Kind: AdvanceExpiry, NowMS: testNow + expiry}); err != nil || !ok {
				t.Fatalf("advance = %v, %v", ok, err)
			}
			var status string
			var excluded int
			if err := db.db.QueryRow(`SELECT status FROM interrupts WHERE id=?`, in.ID).Scan(&status); err != nil {
				t.Fatal(err)
			}
			if err := db.db.QueryRow(`SELECT count(*) FROM attention_batch_members WHERE interrupt_id=? AND excluded_at_ms=?`, in.ID, testNow+expiry).Scan(&excluded); err != nil {
				t.Fatal(err)
			}
			if status != tc.wantStatus || excluded != 1 {
				t.Fatalf("status/excluded = %s/%d", status, excluded)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			db, err = Open(ctx, OpenConfig{Path: path, BinaryVersion: "test-binary", Now: time.UnixMilli(testNow)})
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if err := db.PrepareDueAttentionBatches(ctx, testNow+expiry); err != nil {
				t.Fatal(err)
			}
			var state string
			var operations int
			if err := db.db.QueryRow(`SELECT state FROM attention_batches`).Scan(&state); err != nil {
				t.Fatal(err)
			}
			if err := db.db.QueryRow(`SELECT count(*) FROM outbox_operations WHERE kind='channel_publish'`).Scan(&operations); err != nil {
				t.Fatal(err)
			}
			if state != tc.wantState || operations != tc.wantOps {
				t.Fatalf("batch state/channel operations = %s/%d", state, operations)
			}
		})
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
