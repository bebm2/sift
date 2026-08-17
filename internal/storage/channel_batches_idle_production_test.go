package storage

import (
	"context"
	"encoding/json"
	"testing"
)

// This file exercises the production-path for the commander-mode idle
// heartbeat (#1010 NEED-FIX F1): a zero-interrupt full day where the
// supervisor tick is the only driver. The existing
// channel_batches_idle_test.go asserts the sealer branches correctly, but
// its seedIdleDailyBatch helper bypasses the production creation seam by
// hand-inserting the empty collecting batch. That hidden seam is precisely the
// one we are closing here: in production, the daily_summary batch only
// materializes when the first interrupt joins it, so a quiet day has no row
// for PrepareDueAttentionBatches to seal. EnsureIdleDailySummaryBatches, run
// from SupervisorInterruptTick, must close that gap.
//
// The closure criterion from the NEED-FIX closing package:
//
//   - project with Run activity inside IdleRunActivityWindowMS, zero
//     interrupts emitted during the day, one SupervisorInterruptTick past
//     the day's daily_summary_at → outbox contains exactly one idle
//     channel_publish, no fabricated interrupt / batch_member /
//     interrupt_delivery rows;
//   - dormant project (last activity older than the window) → zero
//     channel_publish operations, batch not even pre-created.
//
// Both tests run through SupervisorInterruptTick — the production scheduler
// port — so any future regression in the pre-creation seam fails closed.

// idleChannelConfig returns a closed IdleDailySummaryConfig with a single
// channel. The exact clock (UTC, 09:00) matches the v0 production default
// in docs/specs/config.md §3.9 so the test mirrors operator deployments.
func idleChannelConfig() IdleDailySummaryConfig {
	return IdleDailySummaryConfig{
		Channels:       []InterruptChannel{batchChannel()},
		DayTimezone:    "UTC",
		DailySummaryAt: "09:00",
	}
}

// seedIdleProductionFixture seeds a project + a single Run with the right
// forge target so EnsureIdleDailySummaryBatches has a complete identity to
// reuse. The Run's status is left as "queued" so the test can drive the
// active / dormant branches explicitly.
func seedIdleProductionFixture(t *testing.T, db *DB, runStatus string, runUpdatedAtMS int64) {
	t.Helper()
	ctx := context.Background()
	if err := db.SeedProjectForTest(ctx, "cfg", "project", testNow); err != nil {
		t.Fatal(err)
	}
	if err := db.SeedReverseSyncRunForTest(ctx, "run-prod", "project", "cfg", "issue-1010", "", "queued", runUpdatedAtMS); err != nil {
		t.Fatal(err)
	}
	switch runStatus {
	case "queued", "running", "waiting_human":
		mustExec(t, db, `UPDATE runs SET status=?, completed_at_ms=NULL WHERE id=?`, runStatus, "run-prod")
	case "done":
		// The CHECK constraint requires change_id for "done" status; mirror
		// the existing seedIdleDailyBatch helper.
		mustExec(t, db, `UPDATE runs SET status='done', change_id=?, completed_at_ms=? WHERE id=?`, "change-run-prod", runUpdatedAtMS, "run-prod")
	case "failed":
		mustExec(t, db, `UPDATE runs SET status='failed', failure_reason='expired', completed_at_ms=? WHERE id=?`, runUpdatedAtMS, "run-prod")
	}
}

// TestIdleDailySummaryProductionTickSealsIdleHeartbeat is the central F1
// closing-ruler test. The fleet has one project with a Run that completed
// inside IdleRunActivityWindowMS, zero interrupts are queued, the operator's
// wall clock crosses 09:00 UTC, and the supervisor tick is the only driver.
// The seam must:
//
//  1. pre-create exactly one empty daily_summary collecting batch during the
//     tick (no hand-INSERT in the test);
//
//  2. seal it with a single channel_publish carrying the deterministic idle
//     status_note; the existing prepareAttentionBatch / shouldPublishIdleNoteTx
//     decision tree is reused verbatim;
//
//  3. write zero rows to interrupts / attention_batch_members /
//     interrupt_deliveries (A1 compliance).
//
// The test asserts all three on the live outbox / schema state at the end of
// the tick. It does NOT mutate any helper after setup, so a regression that
// removes the pre-creation seam (the original F1 failure) fails this test.
func TestIdleDailySummaryProductionTickSealsIdleHeartbeat(t *testing.T) {
	db, _ := openTestDB(t)
	db.SetIdleDailySummaryConfig(idleChannelConfig())
	seedIdleProductionFixture(t, db, "done", testNow-int64(2*60*60*1000)) // ~2h ago, well inside 7d

	// No interrupt emit; no hand-INSERTed collecting batch. The seam must
	// build the row inside the tick from the project's most recent Run.
	// Production flow: tick at T1 creates the batch with due=next09:00(>T1);
	// the batch is sealed by the tick that fires AFTER due. We mirror both
	// ticks so the test exercises the full production seam chain rather than
	// relying on a wall-clock artifact.
	tick1 := testNow + int64(60*60*1000) // 1h after testNow; creates the empty batch
	if err := db.SupervisorInterruptTick(context.Background(), tick1); err != nil {
		t.Fatalf("SupervisorInterruptTick (create): %v", err)
	}

	// (1) The seam pre-created exactly one daily_summary collecting batch.
	var batchCount int
	if err := db.db.QueryRow(`SELECT count(*) FROM attention_batches WHERE kind='daily_summary'`).Scan(&batchCount); err != nil {
		t.Fatal(err)
	}
	if batchCount != 1 {
		t.Fatalf("after creation tick: daily_summary batches = %d, want 1 (seam created exactly one empty collector)", batchCount)
	}
	// ... but the tick that created it must NOT yet have sealed it: the
	// batch's due_at_ms is strictly after tick1, so the channel_publish outbox
	// must still be empty.
	var earlyOps int
	if err := db.db.QueryRow(`SELECT count(*) FROM outbox_operations WHERE kind='channel_publish'`).Scan(&earlyOps); err != nil {
		t.Fatal(err)
	}
	if earlyOps != 0 {
		t.Fatalf("after creation tick: channel_publish ops = %d, want 0 (due strictly after tick1)", earlyOps)
	}

	// Drive a second tick past the next 09:00 UTC. 1_700_000_000_000 ms is
	// 2023-11-14 22:13:20 UTC, so the next 09:00 strictly after tick1 lands
	// on 2023-11-15 09:00 UTC; tick2 = 11h after testNow is comfortably past.
	tick2 := testNow + int64(11*60*60*1000)
	if err := db.SupervisorInterruptTick(context.Background(), tick2); err != nil {
		t.Fatalf("SupervisorInterruptTick (seal): %v", err)
	}

	// (2) The sealer published exactly one channel_publish carrying the
	// status_note. We assert it via the outbox so any future regression that
	// silently drops the op fails closed.
	var opCount int
	if err := db.db.QueryRow(`SELECT count(*) FROM outbox_operations WHERE kind='channel_publish'`).Scan(&opCount); err != nil {
		t.Fatal(err)
	}
	if opCount != 1 {
		t.Fatalf("after seal tick: channel_publish ops = %d, want 1 idle heartbeat", opCount)
	}
	var payload string
	var digest string
	if err := db.db.QueryRow(`SELECT payload_json, payload_digest FROM outbox_operations WHERE kind='channel_publish' ORDER BY operation_key LIMIT 1`).Scan(&payload, &digest); err != nil {
		t.Fatal(err)
	}
	if digest != digestJSON([]byte(payload)) {
		t.Fatalf("payload_digest mismatch")
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if got, want := decoded["batch_kind"], "daily_summary"; got != want {
		t.Fatalf("batch_kind = %v, want %s", got, want)
	}
	if got, want := decoded["status_note"], idleRunNoteText; got != want {
		t.Fatalf("status_note = %v, want %s", got, want)
	}
	if got, want := decoded["rendered_text"], idleRunNoteText; got != want {
		t.Fatalf("rendered_text = %v, want %s", got, want)
	}
	if got, want := decoded["project_id"], "project"; got != want {
		t.Fatalf("project_id = %v, want project", got)
	}
	members, ok := decoded["members"].([]any)
	if !ok {
		t.Fatalf("members missing or wrong type: %#v", decoded["members"])
	}
	if len(members) != 0 {
		t.Fatalf("idle members = %d, want 0", len(members))
	}

	// (3) A1 compliance: no fabricated interrupt / batch_member / delivery
	// rows. This is the same property the hand-insert test enforces, but
	// proven through the production path here.
	for _, table := range []string{"interrupts", "attention_batch_members", "interrupt_deliveries"} {
		var n int
		if err := db.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("table %s must remain empty for the idle heartbeat, found %d rows", table, n)
		}
	}
}

// TestIdleDailySummaryDormantProjectStaysSilentOnProductionTick is the
// dormant decay branch on the production path. A Run completed more than
// IdleRunActivityWindowMS ago must not be picked up by the pre-creation seam
// — the WHERE clause filters it out — so neither a batch nor a
// channel_publish is created. This protects against a regression that turns
// the seam into "permanent daily noise".
func TestIdleDailySummaryDormantProjectStaysSilentOnProductionTick(t *testing.T) {
	db, _ := openTestDB(t)
	db.SetIdleDailySummaryConfig(idleChannelConfig())
	dormantStamp := testNow - IdleRunActivityWindowMS - int64(60*60*1000) // ~1h past the window
	seedIdleProductionFixture(t, db, "done", dormantStamp)

	tick1 := testNow + int64(60*60*1000)
	if err := db.SupervisorInterruptTick(context.Background(), tick1); err != nil {
		t.Fatalf("SupervisorInterruptTick first: %v", err)
	}
	tick2 := testNow + int64(11*60*60*1000)
	if err := db.SupervisorInterruptTick(context.Background(), tick2); err != nil {
		t.Fatalf("SupervisorInterruptTick second: %v", err)
	}

	var batchCount, opCount int
	if err := db.db.QueryRow(`SELECT count(*) FROM attention_batches WHERE kind='daily_summary'`).Scan(&batchCount); err != nil {
		t.Fatal(err)
	}
	if batchCount != 0 {
		t.Fatalf("dormant project must not pre-create any batch; got %d", batchCount)
	}
	if err := db.db.QueryRow(`SELECT count(*) FROM outbox_operations WHERE kind='channel_publish'`).Scan(&opCount); err != nil {
		t.Fatal(err)
	}
	if opCount != 0 {
		t.Fatalf("dormant project must not enqueue any channel_publish; got %d", opCount)
	}
}

// TestIdleDailySummaryProductionTickIsIdempotent protects against the seam
// creating duplicate collecting batches across repeated supervisor ticks
// before the due fires. Two consecutive ticks at the same instant must yield
// exactly one batch row — the second tick's INSERT OR IGNORE no-ops because
// the unique index (project_id,kind,channel_id,scope,scope_id,...) already
// pins the identity.
func TestIdleDailySummaryProductionTickIsIdempotent(t *testing.T) {
	db, _ := openTestDB(t)
	db.SetIdleDailySummaryConfig(idleChannelConfig())
	seedIdleProductionFixture(t, db, "done", testNow-int64(60*60*1000))

	tick1 := testNow + int64(60*60*1000)
	if err := db.SupervisorInterruptTick(context.Background(), tick1); err != nil {
		t.Fatalf("SupervisorInterruptTick first: %v", err)
	}
	if err := db.SupervisorInterruptTick(context.Background(), tick1); err != nil {
		t.Fatalf("SupervisorInterruptTick second: %v", err)
	}

	var batchCount int
	if err := db.db.QueryRow(`SELECT count(*) FROM attention_batches WHERE kind='daily_summary'`).Scan(&batchCount); err != nil {
		t.Fatal(err)
	}
	if batchCount != 1 {
		t.Fatalf("duplicate daily_summary batches = %d, want 1", batchCount)
	}
}
