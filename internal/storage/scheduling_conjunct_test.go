package storage

import (
	"context"
	"strings"
	"testing"
	"time"
)

// This file closes the WBS §5.2 EmitInterrupt scheduling conjunct (issue #786):
// EmitInterrupt freezes dispatch -> the siftd SupervisorInterruptTick advances
// the due/expired candidate via AdvanceInterrupt -> PrepareDueAttentionBatches
// seals the collecting batch and writes the single channel_publish operation.
//
// Structurally (non-test call sites) there is no second emitter/advancer:
// AdvanceInterrupt and PrepareDueAttentionBatches are each called from exactly
// one production site — SupervisorInterruptTick, the sole supervisor scheduler
// siftd wires — and EmitInterrupt is the sole creation port. Command-driven
// dispatch_state writes (manual hold / retry probe) are HITL transitions, not
// scheduling writes, and are out of scope (#786 non-goals).

// mustSeedProjectRun seeds the minimal project + forge run every scheduling
// test needs. The forge target is the run's forge discussion target.
func mustSeedProjectRun(t *testing.T, ctx context.Context, db *DB, runID string) {
	t.Helper()
	if err := db.SeedProjectForTest(ctx, "cfg", "project", testNow); err != nil {
		t.Fatal(err)
	}
	if err := db.SeedForgeRunForTest(ctx, runID, "project", "cfg", "42", testNow); err != nil {
		t.Fatal(err)
	}
}

// batchChannel is the single compatible visual Channel used by the scheduling
// tests. It carries a deterministic snapshot so the sealed payload is stable.
func batchChannel() InterruptChannel {
	return InterruptChannel{ID: "ops", Type: "webhook", TargetRef: "secret_ref:OPS", Renderer: "plain-v1", Capabilities: []string{"visual"}, Default: true}
}

// readChannelPublish reads the channel_publish operation payload for an
// interrupt (single delivery) so the test can assert it is not a batch arm.
func readChannelPublish(t *testing.T, db *DB, interruptID string) (payload, opKey string, count int) {
	t.Helper()
	rows, err := db.db.Query(`SELECT payload_json,operation_key FROM outbox_operations WHERE kind='channel_publish' AND interrupt_id=? ORDER BY operation_key`, interruptID)
	if err != nil {
		t.Fatalf("read channel_publish: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var p, k string
		if err := rows.Scan(&p, &k); err != nil {
			t.Fatalf("scan channel_publish: %v", err)
		}
		if count == 0 {
			payload, opKey = p, k
		}
		count++
	}
	return payload, opKey, count
}

func totalChannelOps(t *testing.T, db *DB) int {
	t.Helper()
	var n int
	if err := db.db.QueryRow(`SELECT count(*) FROM outbox_operations WHERE kind='channel_publish'`).Scan(&n); err != nil {
		t.Fatalf("count channel_publish: %v", err)
	}
	return n
}

// readMemberAuthority returns the frozen (version, nonce) the collecting batch
// holds for an interrupt. Sealing must mirror the current open Interrupt.
func readMemberAuthority(t *testing.T, db *DB, interruptID string) (version int64, nonce string) {
	t.Helper()
	if err := db.db.QueryRow(`SELECT interrupt_version,nonce FROM attention_batch_member_authority WHERE interrupt_id=?`, interruptID).Scan(&version, &nonce); err != nil {
		t.Fatalf("read member authority: %v", err)
	}
	return version, nonce
}

func readInterruptNonce(t *testing.T, db *DB, id string) string {
	t.Helper()
	var nonce string
	if err := db.db.QueryRow(`SELECT nonce FROM interrupts WHERE id=?`, id).Scan(&nonce); err != nil {
		t.Fatalf("read nonce: %v", err)
	}
	return nonce
}

// TestSchedulingConjunctFallbackBatchReadyToSealedChannelPublish proves the
// full conjunct for the deterministic fallback (Brain absent): EmitInterrupt
// freezes ready/batch with the due clock but writes no batch member and no
// Channel operation; one SupervisorInterruptTick at the frozen due advances
// ready->batched (member + authority frozen at the bumped version/nonce) and
// seals the collecting batch, creating exactly one channel_publish. Only
// AdvanceInterrupt + PrepareDueAttentionBatches run inside the tick.
func TestSchedulingConjunctFallbackBatchReadyToSealedChannelPublish(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	mustSeedProjectRun(t, ctx, db, "run")

	const expiry = int64(72 * 60 * 60 * 1000)
	batchAt := int64(testNow + 60*60*1000) // 1h after emit, strictly before expiry
	cmd := t6Command(testNow)
	cmd.RunID = "run"
	cmd.ExpiresAfterMS, cmd.OnExpire, cmd.OnMaxEscalations, cmd.MaxEscalations = expiry, ExpireHold, ExpireHold, 1
	cmd.BatchAtMS = &batchAt
	cmd.Channels = []InterruptChannel{batchChannel()}
	// No per-call T6 and no production seam installed: admitInterruptT6 uses
	// the deterministic fallback (code_review normal -> batch). This is the
	// defensive scheduling path every reason shares when the Brain is absent.
	in, err := emitTestInterrupt(t, ctx, db, cmd)
	if err != nil {
		t.Fatal(err)
	}

	// (1) Emit freezes dispatch but creates neither a batch member nor a
	// channel_publish: the sole creation write port is EmitInterrupt.
	pre := readAdvanceOutcome(t, db, in.ID)
	if pre.dispatchState != "ready" || pre.delivery != "batch" || pre.nextDispatch.Int64 != batchAt || pre.version != 1 {
		t.Fatalf("emit freeze = %s/%s/%v/v%d, want ready/batch/%v/v1", pre.dispatchState, pre.delivery, pre.nextDispatch, pre.version, batchAt)
	}
	if pre.members != 0 || pre.authority != 0 || pre.channelOps != 0 {
		t.Fatalf("emit created scheduling side effects: members=%d authority=%d channelOps=%d", pre.members, pre.authority, pre.channelOps)
	}

	// (2) The supervisor tick at the frozen due is the sole advance+seal driver.
	if err := db.SupervisorInterruptTick(ctx, batchAt); err != nil {
		t.Fatal(err)
	}

	post := readAdvanceOutcome(t, db, in.ID)
	if post.dispatchState != "batched" || post.delivery != "batch" || post.nextDispatch.Valid || post.version != 2 {
		t.Fatalf("post-tick = %s/%s/%v/v%d, want batched/batch/NULL/v2", post.dispatchState, post.delivery, post.nextDispatch, post.version)
	}
	if post.members != 1 || post.authority != 1 {
		t.Fatalf("post-tick accounting = members=%d authority=%d, want 1/1", post.members, post.authority)
	}
	// The sealed batch's channel_publish is batch-scoped (no interrupt_id), so
	// it is counted across the whole outbox: exactly one attention_batch arm.
	if got := totalChannelOps(t, db); got != 1 {
		t.Fatalf("post-tick channel_publish = %d, want 1 sealed batch op", got)
	}
	if post.admissions != 1 || post.charges != 1 {
		t.Fatalf("post-tick admissions/charges = %d/%d, want 1/1 (no second charge)", post.admissions, post.charges)
	}

	// Dispatch bumps the version but, per §8.2, does not rotate the nonce: only
	// escalation rotates the nonce. The member/authority mirror the bumped
	// version and the unchanged nonce.
	authVersion, authNonce := readMemberAuthority(t, db, in.ID)
	currentNonce := readInterruptNonce(t, db, in.ID)
	if authVersion != 2 || authNonce != currentNonce {
		t.Fatalf("authority = v%d/%s, want v2/current-nonce(%s)", authVersion, authNonce, currentNonce)
	}

	// (3) The sealed batch is a channel_publish attention_batch arm, not a
	// single-interrupt delivery, and carries the frozen batch/delivery IDs.
	var batchState, batchPayload, batchDigest, batchOpKey string
	if err := db.db.QueryRow(`SELECT state,payload_json,payload_digest,operation_key FROM attention_batches WHERE id=(SELECT batch_id FROM attention_batch_members WHERE interrupt_id=?)`, in.ID).Scan(&batchState, &batchPayload, &batchDigest, &batchOpKey); err != nil {
		t.Fatalf("read sealed batch: %v", err)
	}
	if batchState != "sealed" || batchDigest == "" || !strings.HasPrefix(batchOpKey, "attention-batch:") {
		t.Fatalf("sealed batch = %s/%q/%q", batchState, batchDigest, batchOpKey)
	}
	if !strings.Contains(batchPayload, `"delivery_kind":"attention_batch"`) {
		t.Fatalf("sealed payload is not an attention_batch arm: %s", batchPayload)
	}
}

// TestSchedulingConjunctT6AdvisedBatchSealsViaProductionSeal proves the same
// conjunct when a real T6 caller (installed via the production SetInterruptT6
// seam) advises batch. The emit path routes through cmd.T6 =
// d.interruptT6Caller() rather than a per-call override, and an early tick
// before the frozen due must not seal.
func TestSchedulingConjunctT6AdvisedBatchSealsViaProductionSeal(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	mustSeedProjectRun(t, ctx, db, "run")

	// Install the production T6 seam. EmitInterrupt picks this up because the
	// command leaves cmd.T6 nil.
	db.SetInterruptT6(func(_ context.Context, in InterruptT6Input) (InterruptT6Output, error) {
		if in.DefaultChannelID != "ops" || len(in.ChannelCandidates) != 1 || in.ChannelCandidates[0] != "ops" {
			t.Fatalf("T6 input channel set = %q/%v", in.DefaultChannelID, in.ChannelCandidates)
		}
		return InterruptT6Output{Delivery: "batch", ChannelID: "ops"}, nil
	})

	const expiry = int64(48 * 60 * 60 * 1000)
	batchAt := int64(testNow + 30*60*1000)
	cmd := t6Command(testNow)
	cmd.RunID = "run"
	cmd.ExpiresAfterMS, cmd.OnExpire, cmd.OnMaxEscalations, cmd.MaxEscalations = expiry, ExpireHold, ExpireHold, 1
	cmd.BatchAtMS = &batchAt
	cmd.Channels = []InterruptChannel{batchChannel()}
	in, err := emitTestInterrupt(t, ctx, db, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if in.Delivery != "batch" || in.ChannelID != "ops" {
		t.Fatalf("emit accepted T6 advice = %s/%s, want batch/ops", in.Delivery, in.ChannelID)
	}

	// Before the due, an early tick must not seal: the dispatch predicate has
	// not fired and no collecting batch exists yet.
	if err := db.SupervisorInterruptTick(ctx, batchAt-1); err != nil {
		t.Fatal(err)
	}
	if got := readAdvanceOutcome(t, db, in.ID); got.dispatchState != "ready" || got.members != 0 || got.channelOps != 0 {
		t.Fatalf("early tick advanced = %s/members=%d/ops=%d, want ready/0/0", got.dispatchState, got.members, got.channelOps)
	}

	if err := db.SupervisorInterruptTick(ctx, batchAt); err != nil {
		t.Fatal(err)
	}
	post := readAdvanceOutcome(t, db, in.ID)
	if post.dispatchState != "batched" || post.members != 1 || post.authority != 1 {
		t.Fatalf("post-tick = %s/members=%d/authority=%d, want batched/1/1", post.dispatchState, post.members, post.authority)
	}
	if got := totalChannelOps(t, db); got != 1 {
		t.Fatalf("post-tick channel_publish = %d, want 1 sealed batch op", got)
	}
	var t6Payload string
	if err := db.db.QueryRow(`SELECT payload_json FROM outbox_operations WHERE kind='channel_publish'`).Scan(&t6Payload); err != nil {
		t.Fatalf("read sealed payload: %v", err)
	}
	if !strings.Contains(t6Payload, `"delivery_kind":"attention_batch"`) {
		t.Fatalf("sealed payload is not attention_batch: %s", t6Payload)
	}
}

// TestSchedulingConjunctImmediateIsSingleDeliveryAndEscalationIsStrong proves
// the immediate arm: a single channel_publish (delivery_kind=interrupt) is
// created at emit, never an attention_batch, and an escalation reaching
// high/critical produces a strong single redelivery rather than a batch.
func TestSchedulingConjunctImmediateIsSingleDeliveryAndEscalationIsStrong(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	// Seed the project once; both runs live under it (re-seeding collides on
	// the frozen config hash).
	if err := db.SeedProjectForTest(ctx, "cfg", "project", testNow); err != nil {
		t.Fatal(err)
	}
	if err := db.SeedForgeRunForTest(ctx, "run", "project", "cfg", "42", testNow); err != nil {
		t.Fatal(err)
	}
	if err := db.SeedForgeRunForTest(ctx, "run2", "project", "cfg", "43", testNow); err != nil {
		t.Fatal(err)
	}

	const expiry = int64(24 * 60 * 60 * 1000)

	// (a) Initial immediate: a candidate whose T6 advises immediate produces a
	// single channel_publish at emit time. There is no batch member and no
	// attention_batch arm. dispatch_state is batched because the single
	// delivery is already enqueued; the tick never re-touches it.
	immCmd := t6Command(testNow)
	immCmd.RunID = "run"
	immCmd.Generation.ChangeID = "change-imm"
	immCmd.ExpiresAfterMS, immCmd.OnExpire, immCmd.OnMaxEscalations, immCmd.MaxEscalations = expiry, ExpireHold, ExpireHold, 1
	immCmd.Channels = []InterruptChannel{batchChannel()}
	immCmd.T6 = func(context.Context, InterruptT6Input) (InterruptT6Output, error) {
		return InterruptT6Output{Delivery: "immediate", ChannelID: "ops"}, nil
	}
	imm, err := emitTestInterrupt(t, ctx, db, immCmd)
	if err != nil {
		t.Fatal(err)
	}
	immOutcome := readAdvanceOutcome(t, db, imm.ID)
	if immOutcome.dispatchState != "batched" || immOutcome.delivery != "immediate" {
		t.Fatalf("immediate emit = %s/%s, want batched/immediate", immOutcome.dispatchState, immOutcome.delivery)
	}
	payload, opKey, count := readChannelPublish(t, db, imm.ID)
	if count != 1 {
		t.Fatalf("immediate channel_publish count = %d, want 1", count)
	}
	if !strings.Contains(payload, `"delivery_kind":"interrupt"`) || !strings.Contains(payload, `"priority":"normal"`) || !strings.Contains(payload, `"escalation_no":0`) {
		t.Fatalf("immediate payload = %s, want single interrupt/normal/escalation 0", payload)
	}
	if opKey != ChannelPublishOperationKey(imm.ID, 0) {
		t.Fatalf("immediate op key = %q", opKey)
	}
	if immOutcome.members != 0 || immOutcome.authority != 0 {
		t.Fatalf("immediate created batch membership: members=%d authority=%d", immOutcome.members, immOutcome.authority)
	}
	// A tick after emit must not duplicate or convert the single delivery.
	if err := db.SupervisorInterruptTick(ctx, testNow+1); err != nil {
		t.Fatal(err)
	}
	if _, _, after := readChannelPublish(t, db, imm.ID); after != 1 {
		t.Fatalf("post-tick immediate channel_publish = %d, want 1 (no re-touch)", after)
	}

	// (b) Escalation to high yields a strong single redelivery, not a batch. A
	// normal candidate is emitted as batch and escalated on expiry; the
	// resulting immediate dispatch enqueues one strong channel_publish.
	escBatchAt := int64(testNow + 60*60*1000)
	escCmd := t6Command(testNow)
	escCmd.RunID = "run2"
	escCmd.Generation.ChangeID = "change-esc"
	escCmd.ExpiresAfterMS, escCmd.OnExpire, escCmd.OnMaxEscalations, escCmd.MaxEscalations = 2*60*60*1000, ExpireEscalate, ExpireHold, 2
	escCmd.BatchAtMS = &escBatchAt
	escCmd.Channels = []InterruptChannel{batchChannel()}
	escCmd.T6 = func(context.Context, InterruptT6Input) (InterruptT6Output, error) {
		return InterruptT6Output{Delivery: "batch", ChannelID: "ops"}, nil
	}
	esc, err := emitTestInterrupt(t, ctx, db, escCmd)
	if err != nil {
		t.Fatal(err)
	}
	escExpiry := int64(testNow + 2*60*60*1000)
	// Tick 1 at expiry: the expiry predicate fires first (UNION ALL order),
	// escalates normal->high->immediate (ready/immediate/next=now, version+1,
	// rotated nonce). The dispatch row collected for the same Interrupt is
	// stale against the bumped CAS and is swallowed. No batch is sealed.
	if err := db.SupervisorInterruptTick(ctx, escExpiry); err != nil {
		t.Fatal(err)
	}
	mid := readAdvanceOutcome(t, db, esc.ID)
	if mid.delivery != "immediate" || mid.dispatchState != "ready" || mid.version != 2 || mid.escalation != 1 || !mid.nextDispatch.Valid {
		t.Fatalf("post-escalation = %s/%s/v%d/esc=%d/%v, want immediate/ready/v2/esc=1/now", mid.delivery, mid.dispatchState, mid.version, mid.escalation, mid.nextDispatch)
	}
	if mid.channelOps != 0 || mid.members != 0 {
		t.Fatalf("escalation created delivery = ops=%d members=%d, want 0/0", mid.channelOps, mid.members)
	}
	// Tick 2 at the frozen immediate due: the dispatch predicate fires and
	// enqueues exactly one strong single channel_publish.
	if err := db.SupervisorInterruptTick(ctx, escExpiry+1); err != nil {
		t.Fatal(err)
	}
	post := readAdvanceOutcome(t, db, esc.ID)
	if post.dispatchState != "batched" || post.channelOps != 1 || post.members != 0 {
		t.Fatalf("post-redelivery = %s/ops=%d/members=%d, want batched/1/0", post.dispatchState, post.channelOps, post.members)
	}
	escPayload, escOpKey, escCount := readChannelPublish(t, db, esc.ID)
	if escCount != 1 {
		t.Fatalf("escalation channel_publish count = %d, want 1", escCount)
	}
	if !strings.Contains(escPayload, `"delivery_kind":"interrupt"`) || !strings.Contains(escPayload, `"priority":"strong"`) || !strings.Contains(escPayload, `"escalation_no":1`) {
		t.Fatalf("escalation payload = %s, want single interrupt/strong/escalation 1", escPayload)
	}
	if escOpKey != ChannelPublishOperationKey(esc.ID, 1) {
		t.Fatalf("escalation op key = %q", escOpKey)
	}
	if post.admissions != 1 || post.charges != 1 {
		t.Fatalf("escalation admissions/charges = %d/%d, want 1/1 (escalation reuses charge)", post.admissions, post.charges)
	}
}

// TestSchedulingConjunctNextWindowFrozenBeforeExpiryOrHonestFallback proves the
// next_window arm: a delivery may only freeze a window the caller supplied
// deterministically and that is strictly before expiry; without a frozen
// window the model's next_window advice is ignored (it can never guess a time),
// and a window at/after expiry is rejected before it can schedule.
func TestSchedulingConjunctNextWindowFrozenBeforeExpiryOrHonestFallback(t *testing.T) {
	const expiry = int64(48 * 60 * 60 * 1000)
	adviseNextWindow := func() InterruptT6Caller {
		return func(context.Context, InterruptT6Input) (InterruptT6Output, error) {
			return InterruptT6Output{Delivery: "next_window", ChannelID: "ops"}, nil
		}
	}

	t.Run("frozen_window_before_expiry_schedules_and_seals", func(t *testing.T) {
		db, _ := openTestDB(t)
		ctx := context.Background()
		mustSeedProjectRun(t, ctx, db, "run")
		window := int64(testNow + 2*60*60*1000) // strictly before expiry
		cmd := t6Command(testNow)
		cmd.RunID = "run"
		cmd.Generation.ChangeID = "change-nw"
		cmd.ExpiresAfterMS, cmd.OnExpire, cmd.OnMaxEscalations, cmd.MaxEscalations = expiry, ExpireHold, ExpireHold, 1
		cmd.NextWindowAtMS = &window
		cmd.Channels = []InterruptChannel{batchChannel()}
		cmd.T6 = adviseNextWindow()
		in, err := emitTestInterrupt(t, ctx, db, cmd)
		if err != nil {
			t.Fatal(err)
		}
		if in.Delivery != "next_window" || in.NextDispatchAtMS == nil || *in.NextDispatchAtMS != window {
			t.Fatalf("emit next_window freeze = %s/%v, want next_window/%d", in.Delivery, in.NextDispatchAtMS, window)
		}
		// Before the window: no advance, no seal.
		if err := db.SupervisorInterruptTick(ctx, window-1); err != nil {
			t.Fatal(err)
		}
		if got := readAdvanceOutcome(t, db, in.ID); got.dispatchState != "ready" || got.channelOps != 0 || got.members != 0 {
			t.Fatalf("pre-window tick = %s/ops=%d/m=%d, want ready/0/0", got.dispatchState, got.channelOps, got.members)
		}
		// At the frozen window: advance + seal.
		if err := db.SupervisorInterruptTick(ctx, window); err != nil {
			t.Fatal(err)
		}
		post := readAdvanceOutcome(t, db, in.ID)
		if post.dispatchState != "batched" || post.members != 1 {
			t.Fatalf("window tick = %s/m=%d, want batched/1", post.dispatchState, post.members)
		}
		if got := totalChannelOps(t, db); got != 1 {
			t.Fatalf("window tick channel_publish = %d, want 1 sealed batch op", got)
		}
		var nwPayload string
		if err := db.db.QueryRow(`SELECT payload_json FROM outbox_operations WHERE kind='channel_publish'`).Scan(&nwPayload); err != nil {
			t.Fatalf("read window sealed payload: %v", err)
		}
		if !strings.Contains(nwPayload, `"delivery_kind":"attention_batch"`) {
			t.Fatalf("window seal payload = %s, want attention_batch", nwPayload)
		}
	})

	t.Run("no_frozen_window_ignores_model_advice_honest_fallback", func(t *testing.T) {
		db, _ := openTestDB(t)
		ctx := context.Background()
		mustSeedProjectRun(t, ctx, db, "run")
		batchAt := int64(testNow + 60*60*1000)
		cmd := t6Command(testNow)
		cmd.RunID = "run"
		cmd.Generation.ChangeID = "change-nw2"
		cmd.ExpiresAfterMS, cmd.OnExpire, cmd.OnMaxEscalations, cmd.MaxEscalations = expiry, ExpireHold, ExpireHold, 1
		cmd.BatchAtMS = &batchAt
		cmd.Channels = []InterruptChannel{batchChannel()}
		// The model asks for next_window but no caller supplied a window:
		// validT6Advice rejects it and the deterministic fallback wins. The
		// model cannot invent a delivery time.
		cmd.T6 = adviseNextWindow()
		in, err := emitTestInterrupt(t, ctx, db, cmd)
		if err != nil {
			t.Fatal(err)
		}
		if in.Delivery == "next_window" {
			t.Fatalf("emit accepted model-invented next_window: %#v", in)
		}
		if in.Delivery != "batch" {
			t.Fatalf("honest fallback delivery = %s, want batch", in.Delivery)
		}
	})

	t.Run("window_at_or_after_expiry_is_rejected_before_scheduling", func(t *testing.T) {
		db, _ := openTestDB(t)
		ctx := context.Background()
		mustSeedProjectRun(t, ctx, db, "run")
		window := int64(testNow + expiry) // == expiry: not strictly before
		cmd := t6Command(testNow)
		cmd.RunID = "run"
		cmd.Generation.ChangeID = "change-nw3"
		cmd.ExpiresAfterMS, cmd.OnExpire, cmd.OnMaxEscalations, cmd.MaxEscalations = expiry, ExpireHold, ExpireHold, 1
		cmd.NextWindowAtMS = &window
		cmd.Channels = []InterruptChannel{batchChannel()}
		cmd.T6 = adviseNextWindow()
		in, err := emitTestInterrupt(t, ctx, db, cmd)
		if err != nil {
			t.Fatal(err)
		}
		// The invalid window cannot freeze: advice is rejected and the
		// deterministic fallback applies (normal -> batch). The Interrupt is
		// not scheduled past its own expiry.
		if in.Delivery == "next_window" {
			t.Fatalf("emit scheduled next_window at/after expiry: %#v", in)
		}
	})
}

// TestSchedulingConjunctCrossRestartStaleCASAndIdempotentSeal proves the
// restart invariants: after a DB reopen, a stale version/nonce CAS cannot
// re-advance or re-push an Interrupt, and re-running the supervisor tick on an
// already-sealed batch is a no-op (same payload/digest, no second operation).
func TestSchedulingConjunctCrossRestartStaleCASAndIdempotentSeal(t *testing.T) {
	db, path := openTestDB(t)
	ctx := context.Background()
	mustSeedProjectRun(t, ctx, db, "run")

	const expiry = int64(48 * 60 * 60 * 1000)
	batchAt := int64(testNow + 60*60*1000)
	cmd := t6Command(testNow)
	cmd.RunID = "run"
	cmd.Generation.ChangeID = "change-restart"
	cmd.ExpiresAfterMS, cmd.OnExpire, cmd.OnMaxEscalations, cmd.MaxEscalations = expiry, ExpireEscalate, ExpireHold, 1
	cmd.BatchAtMS = &batchAt
	cmd.Channels = []InterruptChannel{batchChannel()}
	cmd.T6 = func(context.Context, InterruptT6Input) (InterruptT6Output, error) {
		return InterruptT6Output{Delivery: "batch", ChannelID: "ops"}, nil
	}
	in, err := emitTestInterrupt(t, ctx, db, cmd)
	if err != nil {
		t.Fatal(err)
	}

	// Capture the pre-tick CAS snapshot an in-flight tick or Channel worker
	// might hold across a restart.
	preTick := readAdvanceOutcome(t, db, in.ID)
	staleVersion, staleNonce := preTick.version, preTick.nonce

	// One tick at the frozen due advances (ready->batched) AND seals the
	// collecting batch in the same call.
	if err := db.SupervisorInterruptTick(ctx, batchAt); err != nil {
		t.Fatal(err)
	}
	var sealedPayload, sealedDigest, sealedOpKey string
	if err := db.db.QueryRow(`SELECT payload_json,payload_digest,operation_key FROM attention_batches WHERE state='sealed'`).Scan(&sealedPayload, &sealedDigest, &sealedOpKey); err != nil {
		t.Fatalf("read sealed batch: %v", err)
	}
	if sealedDigest == "" || !strings.HasPrefix(sealedOpKey, "attention-batch:") {
		t.Fatalf("sealed batch identity = %q/%q", sealedDigest, sealedOpKey)
	}
	opsAfterSeal := totalChannelOps(t, db)
	if opsAfterSeal != 1 {
		t.Fatalf("channel_publish after seal = %d, want 1", opsAfterSeal)
	}

	// Reopen: every in-memory snapshot a caller holds is now potentially stale.
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(ctx, OpenConfig{Path: path, BinaryVersion: "test-binary", Now: time.UnixMilli(testNow)})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// (1) Stale CAS: the pre-tick (version,nonce) can no longer advance. The
	// single-CAS port rejects it and mutates nothing — no re-dispatch, no
	// second seal, no second channel_publish.
	before := readAdvanceOutcome(t, db, in.ID)
	ok, err := db.AdvanceInterrupt(ctx, AdvanceInterruptCmd{InterruptID: in.ID, ExpectedVersion: staleVersion, ExpectedNonce: staleNonce, Kind: AdvanceExpiry, NowMS: batchAt + 1})
	if ok || err != ErrRejectedStale {
		t.Fatalf("stale replay = %v, %v, want false/ErrRejectedStale", ok, err)
	}
	if after := readAdvanceOutcome(t, db, in.ID); after != before {
		t.Fatalf("stale replay mutated state:\n  before=%+v\n  after=%+v", before, after)
	}

	// (2) Idempotent seal: re-running the supervisor tick finds no due
	// collecting batch (already sealed/immutable) and no due ready Interrupt
	// (already batched). No new channel_publish; sealed payload/digest intact.
	if err := db.SupervisorInterruptTick(ctx, batchAt+1); err != nil {
		t.Fatal(err)
	}
	if got := totalChannelOps(t, db); got != opsAfterSeal {
		t.Fatalf("idempotent re-tick changed channel_publish: %d -> %d", opsAfterSeal, got)
	}
	var payloadAgain, digestAgain string
	if err := db.db.QueryRow(`SELECT payload_json,payload_digest FROM attention_batches WHERE operation_key=?`, sealedOpKey).Scan(&payloadAgain, &digestAgain); err != nil {
		t.Fatalf("reread sealed batch: %v", err)
	}
	if payloadAgain != sealedPayload || digestAgain != sealedDigest {
		t.Fatal("idempotent re-seal changed immutable batch payload/digest")
	}
}
