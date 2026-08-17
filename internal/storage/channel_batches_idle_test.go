package storage

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// The commander-mode idle heartbeat (#1010) is a digest-level projection:
// when a "daily_summary" batch collects zero admitted interrupts and the
// project has no active Run AND recent activity inside
// IdleRunActivityWindowMS, the sealer publishes a single status_note line
// instead of silently cancelling. This file drives the three branches:
//   * active Run present         → still cancelled silently
//   * no active Run + recent     → publishes status_note, no interrupt rows
//   * no active Run + dormant    → still cancelled silently (dormant decay)
//
// The status_note payload is itself exercised structurally so that any
// future divergence (e.g. a fabricated interrupt row, a member leaked into
// the payload, a wrong batch_kind) fails closed.

func seedIdleDailyBatch(t *testing.T, db *DB, projectID, cfgID, runID string, runStatus string, runUpdatedAtMS int64) string {
	t.Helper()
	ctx := context.Background()
	if err := db.SeedProjectForTest(ctx, cfgID, projectID, testNow); err != nil {
		t.Fatal(err)
	}
	if runID != "" {
		// Insert as "queued" so the schema's CHECK constraints accept the
		// row without completed_at_ms, then UPDATE to the desired terminal
		// (or kept) status with completed_at_ms / change_id set in the same
		// statement. This sidesteps having to thread extra columns through
		// the cross-package test seeds.
		if err := db.SeedReverseSyncRunForTest(ctx, runID, projectID, cfgID, "issue-"+runID, "", "queued", runUpdatedAtMS); err != nil {
			t.Fatal(err)
		}
		switch runStatus {
		case "queued", "running", "waiting_human":
			mustExec(t, db, `UPDATE runs SET status=?, completed_at_ms=NULL WHERE id=?`, runStatus, runID)
		case "failed":
			mustExec(t, db, `UPDATE runs SET status='failed', failure_reason='expired', completed_at_ms=? WHERE id=?`, runUpdatedAtMS, runID)
		case "done":
			mustExec(t, db, `UPDATE runs SET status='done', change_id=?, completed_at_ms=? WHERE id=?`, "change-"+runID, runUpdatedAtMS, runID)
		}
	}
	const batchID = "daily:idle:project-x:UTC:1700000000000:ops-slack:github:Z2l0aHViLmNvbQ:b3duZXIvcmVwby1wcm9qZWN0LXg:issue:NDI"
	const deliveryID = batchID + ":publish:1"
	// due_at_ms = testNow so PrepareDueAttentionBatches picks the batch up
	// on the very next tick; scope_id encodes the same instant for fixture
	// traceability.
	mustExec(t, db, `INSERT INTO attention_batches
		(id, state, project_id, channel_id, channel_snapshot_json, forge_kind, forge_host, forge_project_key,
		 target_kind, target_id, kind, delivery_id, scope, scope_id, due_at_ms, updated_at_ms, created_at_ms)
		VALUES (?, 'collecting', ?, 'ops-slack', ?, 'github', 'github.com', 'org/repo-project-x',
		        'issue', '42', 'daily_summary', ?, 'day', 'UTC:1700000000000', ?, ?, ?)`,
		batchID, projectID, `{"capabilities":["text"],"id":"ops-slack","renderer":"plain-v1","target_ref":"secret_ref:OPS","type":"webhook"}`,
		deliveryID, testNow, testNow, testNow)
	return batchID
}

// TestDailySummaryIdleProjectSealsStatusNote is the central commander-mode
// positive case: no Run is active, the project touched a Run at testNow (well
// inside IdleRunActivityWindowMS = 7d), and the batch has zero admitted
// interrupts. The sealer must seal a payload carrying status_note + an empty
// members slice, schedule a single channel_publish outbox operation, and
// must not write any interrupt/batch_member/interrupt_delivery rows.
func TestDailySummaryIdleProjectSealsStatusNote(t *testing.T) {
	db, _ := openTestDB(t)
	const projectID = "project-x"
	const cfgID = "cfg-x"
	const runID = "run-idle"
	const batchID = "daily:idle:project-x:UTC:1700000000000:ops-slack:github:Z2l0aHViLmNvbQ:b3duZXIvcmVwby1wcm9qZWN0LXg:issue:NDI"
	seedIdleDailyBatch(t, db, projectID, cfgID, runID, "done", testNow)

	if err := db.PrepareDueAttentionBatches(context.Background(), testNow); err != nil {
		t.Fatal(err)
	}

	var state, payload, digest string
	if err := db.db.QueryRow(`SELECT state, payload_json, payload_digest FROM attention_batches WHERE id=?`, batchID).Scan(&state, &payload, &digest); err != nil {
		t.Fatal(err)
	}
	if state != "sealed" {
		t.Fatalf("idle batch state = %s, want sealed", state)
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
		t.Fatalf("rendered_text = %v, want %s (status_note passthrough)", got, want)
	}
	members, ok := decoded["members"].([]any)
	if !ok {
		t.Fatalf("members missing or wrong type: %#v", decoded["members"])
	}
	if len(members) != 0 {
		t.Fatalf("idle members = %d, want 0", len(members))
	}
	if digest != digestJSON([]byte(payload)) {
		t.Fatalf("payload_digest mismatch")
	}

	var ops int
	if err := db.db.QueryRow(`SELECT count(*) FROM outbox_operations WHERE kind='channel_publish' AND operation_key=?`,
		"attention-batch:"+batchID+":publish:1").Scan(&ops); err != nil {
		t.Fatal(err)
	}
	if ops != 1 {
		t.Fatalf("idle channel_publish ops = %d, want 1", ops)
	}

	// A1 compliance: the heartbeat must never materialize as an interrupt row.
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

// TestDailySummaryIdleProjectWithActiveRunStillCancels protects the active-
// run branch. A queued Run inside the window means the commander session
// already has a fresher signal than this digest; publishing a status_note
// would duplicate noise, so the sealer must still cancel silently.
func TestDailySummaryIdleProjectWithActiveRunStillCancels(t *testing.T) {
	db, _ := openTestDB(t)
	const projectID = "project-x"
	const cfgID = "cfg-x"
	const runID = "run-active"
	const batchID = "daily:idle:project-x:UTC:1700000000000:ops-slack:github:Z2l0aHViLmNvbQ:b3duZXIvcmVwby1wcm9qZWN0LXg:issue:NDI"
	seedIdleDailyBatch(t, db, projectID, cfgID, runID, "queued", testNow)

	if err := db.PrepareDueAttentionBatches(context.Background(), testNow); err != nil {
		t.Fatal(err)
	}
	var state string
	if err := db.db.QueryRow(`SELECT state FROM attention_batches WHERE id=?`, batchID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "cancelled" {
		t.Fatalf("active-run batch state = %s, want cancelled", state)
	}
	var ops int
	if err := db.db.QueryRow(`SELECT count(*) FROM outbox_operations WHERE kind='channel_publish'`).Scan(&ops); err != nil {
		t.Fatal(err)
	}
	if ops != 0 {
		t.Fatalf("active-run should not enqueue channel_publish; got %d", ops)
	}
}

// TestDailySummaryIdleDormantProjectCancelsSilently protects the dormant
// decay branch. A Run whose last updated_at_ms is older than
// IdleRunActivityWindowMS must not generate a perpetual daily no-news row:
// the sealer cancels silently. The test seeds the Run at the same instant
// as the batch but back-dates updated_at_ms so the activity check fails.
func TestDailySummaryIdleDormantProjectCancelsSilently(t *testing.T) {
	db, _ := openTestDB(t)
	const projectID = "project-x"
	const cfgID = "cfg-x"
	const runID = "run-dormant"
	const batchID = "daily:idle:project-x:UTC:1700000000000:ops-slack:github:Z2l0aHViLmNvbQ:b3duZXIvcmVwby1wcm9qZWN0LXg:issue:NDI"
	dormantStamp := testNow - IdleRunActivityWindowMS - int64(60*60*1000) // ~1 hour past the 7d window
	seedIdleDailyBatch(t, db, projectID, cfgID, runID, "done", dormantStamp)

	if err := db.PrepareDueAttentionBatches(context.Background(), testNow); err != nil {
		t.Fatal(err)
	}
	var state string
	if err := db.db.QueryRow(`SELECT state FROM attention_batches WHERE id=?`, batchID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "cancelled" {
		t.Fatalf("dormant batch state = %s, want cancelled", state)
	}
	var ops int
	if err := db.db.QueryRow(`SELECT count(*) FROM outbox_operations WHERE kind='channel_publish'`).Scan(&ops); err != nil {
		t.Fatal(err)
	}
	if ops != 0 {
		t.Fatalf("dormant project should not enqueue; got %d", ops)
	}
}

// TestDailySummaryIdleNoRunsEverStillCancels protects the no-history branch:
// if a project has zero rows in `runs` at all, recent.Valid is false and the
// seeder cancels — there is nothing for a heartbeat to point at.
func TestDailySummaryIdleNoRunsEverStillCancels(t *testing.T) {
	db, _ := openTestDB(t)
	const projectID = "project-x"
	const cfgID = "cfg-x"
	const batchID = "daily:idle:project-x:UTC:1700000000000:ops-slack:github:Z2l0aHViLmNvbQ:b3duZXIvcmVwby1wcm9qZWN0LXg:issue:NDI"
	// No runID → no runs row at all.
	seedIdleDailyBatch(t, db, projectID, cfgID, "", "", 0)

	if err := db.PrepareDueAttentionBatches(context.Background(), testNow); err != nil {
		t.Fatal(err)
	}
	var state string
	if err := db.db.QueryRow(`SELECT state FROM attention_batches WHERE id=?`, batchID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "cancelled" {
		t.Fatalf("no-history batch state = %s, want cancelled", state)
	}
}

// TestDailySummaryIdleStatusNoteTextIsStable locks the deterministic text:
// the commander session parses the note by exact match, so any rewrite of
// the wording would silently break the dispatcher. The string is normative
// per issue #1010 §1.
func TestDailySummaryIdleStatusNoteTextIsStable(t *testing.T) {
	want := "昨日无待办事件；当前无活跃 Run —— 舰队空闲。若尚有未派发工作请开窗喂料"
	if idleRunNoteText != want {
		t.Fatalf("idle note = %q, want %q", idleRunNoteText, want)
	}
	if strings.Contains(idleRunNoteText, "\n") {
		t.Fatalf("idle note must be a single line")
	}
}