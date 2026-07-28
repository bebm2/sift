package storage

import (
	"context"
	"errors"
	"testing"
)

func TestCreateForgeRunIdempotentAndTimestamps(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	insertConfigSnapshot(t, db, "cfg1")
	insertProject(t, db, "p1", "cfg1")

	const trig = 1_699_000_000_000
	first, err := db.CreateForgeRun(ctx, CreateForgeRunCmd{
		RunID: "r1", ProjectID: "p1", ConfigSnapshotID: "cfg1",
		ForgeKind: "github", ForgeHost: "github.com", ForgeProjectKey: "org/repo-p1",
		IssueID: "42", IssueURL: "u", IssueAuthor: "alice",
		TriggerLabelEventID: "42:sift", TriggerActor: "alice",
		TriggerObservedAtMS: trig, CreatedAtMS: testNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != RunQueued || first.Version != 1 {
		t.Fatalf("first = %+v", first)
	}
	// Re-applying the same intake is idempotent: same Run, no new events.
	second, err := db.CreateForgeRun(ctx, CreateForgeRunCmd{
		RunID:     "r1-x", // different requested id
		ProjectID: "p1", ConfigSnapshotID: "cfg1",
		ForgeKind: "github", ForgeHost: "github.com", ForgeProjectKey: "org/repo-p1",
		IssueID: "42", TriggerLabelEventID: "42:sift", TriggerActor: "alice",
		TriggerObservedAtMS: trig, CreatedAtMS: testNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID {
		t.Fatalf("idempotent run id = %s, want %s", second.ID, first.ID)
	}
	var events, receipts int
	mustExecScan(t, db, `SELECT COUNT(*) FROM events WHERE run_id='r1' AND type='intake.trigger_observed'`, &events)
	if events != 1 {
		t.Fatalf("trigger_observed events = %d, want 1 (idempotent)", events)
	}
	mustExecScan(t, db, `SELECT COUNT(*) FROM forge_event_receipts WHERE project_id='p1' AND forge_event_id='42:sift'`, &receipts)
	if receipts != 1 {
		t.Fatalf("receipts = %d, want 1", receipts)
	}
	// The trigger-observed event carries the trusted actor and the P50 anchor.
	ev, ok, err := db.FirstEventOfType(ctx, "r1", "intake.trigger_observed")
	if err != nil || !ok || ev.OccurredAtMS != trig || ev.Actor != "alice" {
		t.Fatalf("trigger event = %+v ok=%v err=%v", ev, ok, err)
	}
}

func TestCreateForgeRunRejectsIncomplete(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	cases := []CreateForgeRunCmd{
		{ProjectID: "p", ConfigSnapshotID: "c", ForgeKind: "github", ForgeHost: "h", ForgeProjectKey: "k", IssueID: "1", TriggerLabelEventID: "e", TriggerActor: "a", TriggerObservedAtMS: 1, CreatedAtMS: 1},
		{RunID: "r", ConfigSnapshotID: "c", ForgeKind: "github", ForgeHost: "h", ForgeProjectKey: "k", IssueID: "1", TriggerLabelEventID: "e", TriggerActor: "a", TriggerObservedAtMS: 1, CreatedAtMS: 1},
		{RunID: "r", ProjectID: "p", ConfigSnapshotID: "c", ForgeKind: "bogus", ForgeHost: "h", ForgeProjectKey: "k", IssueID: "1", TriggerLabelEventID: "e", TriggerActor: "a", TriggerObservedAtMS: 1, CreatedAtMS: 1},
		{RunID: "r", ProjectID: "p", ConfigSnapshotID: "c", ForgeKind: "github", ForgeHost: "h", ForgeProjectKey: "k", IssueID: "1", TriggerLabelEventID: "e", TriggerObservedAtMS: 0, CreatedAtMS: 1},
	}
	for i, cmd := range cases {
		if _, err := db.CreateForgeRun(ctx, cmd); err == nil {
			t.Fatalf("case %d: expected error", i)
		}
	}
}

func TestSetInitialTaskSpecAssignsRun(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	insertConfigSnapshot(t, db, "cfg1")
	insertProject(t, db, "p1", "cfg1")
	mustExec(t, db, `INSERT INTO runs
		(id, source_kind, project_id, config_snapshot_id, forge_kind, forge_host, forge_project_key,
		 issue_id, status, max_attempts, created_at_ms, updated_at_ms)
		VALUES ('r1','forge','p1','cfg1','github','github.com','org/repo-p1','42','queued',3,?,?)`, testNow, testNow)

	r, err := db.SetInitialTaskSpec(ctx, SetInitialTaskSpecCmd{
		RunID: "r1", ExpectedVersion: 1, TaskSpecID: "spec1",
		CanonicalJSON: []byte(`{"goals":["x"]}`), ContentDigest: "d1",
		Kind: "feature", AgentID: "fake-agent", OccurredAtMS: testNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Kind != "feature" || r.AgentID != "fake-agent" || r.Version != 2 {
		t.Fatalf("assigned run = %+v", r)
	}
	// Re-applying the same snapshot version is stale (version moved to 2).
	if _, err := db.SetInitialTaskSpec(ctx, SetInitialTaskSpecCmd{
		RunID: "r1", ExpectedVersion: 1, TaskSpecID: "spec1",
		CanonicalJSON: []byte(`{"goals":["x"]}`), ContentDigest: "d1",
		Kind: "feature", AgentID: "fake-agent", OccurredAtMS: testNow,
	}); !errors.Is(err, ErrRejectedStale) {
		t.Fatalf("re-apply err = %v, want ErrRejectedStale", err)
	}
	// Assigning a Run that left queued is illegal.
	mustExec(t, db, `UPDATE runs SET status='running', version=3 WHERE id='r1'`)
	if _, err := db.SetInitialTaskSpec(ctx, SetInitialTaskSpecCmd{
		RunID: "r1", ExpectedVersion: 3, TaskSpecID: "spec2",
		CanonicalJSON: []byte(`{"goals":["x"]}`), ContentDigest: "d2",
		Kind: "bug", AgentID: "a", OccurredAtMS: testNow,
	}); !errors.Is(err, ErrIllegalTransition) {
		t.Fatalf("assign running err = %v, want ErrIllegalTransition", err)
	}
}

func TestAppendEventAndCountOutbox(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	insertConfigSnapshot(t, db, "cfg1")
	insertProject(t, db, "p1", "cfg1")
	insertManualRun(t, db, "r1", "p1", "cfg1")

	if _, err := db.AppendEvent(ctx, EventCmd{
		RunID: "r1", Type: "attempt.completed", Source: SourceAgent, Actor: "a",
		PayloadJSON: []byte(`{}`), OccurredAtMS: testNow, RecordedAtMS: testNow,
	}); err != nil {
		t.Fatal(err)
	}
	// Idempotency key makes a duplicate a no-op.
	if _, err := db.AppendEvent(ctx, EventCmd{
		RunID: "r1", Type: "attempt.completed", Source: SourceAgent,
		PayloadJSON: []byte(`{}`), IdempotencyKey: "dup",
		OccurredAtMS: testNow, RecordedAtMS: testNow,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AppendEvent(ctx, EventCmd{
		RunID: "r1", Type: "attempt.completed", Source: SourceAgent,
		PayloadJSON: []byte(`{}`), IdempotencyKey: "dup",
		OccurredAtMS: testNow, RecordedAtMS: testNow,
	}); err != nil {
		t.Fatalf("duplicate idempotency key must be no-op, got %v", err)
	}
	// Invalid source / payload / timestamps rejected.
	if _, err := db.AppendEvent(ctx, EventCmd{RunID: "r1", Type: "x", Source: "bogus", PayloadJSON: []byte(`{}`), OccurredAtMS: 1, RecordedAtMS: 1}); err == nil {
		t.Fatal("invalid source must error")
	}
	if _, err := db.AppendEvent(ctx, EventCmd{RunID: "r1", Type: "x", Source: SourceSystem, PayloadJSON: []byte(`not json`), OccurredAtMS: 1, RecordedAtMS: 1}); err == nil {
		t.Fatal("invalid payload must error")
	}
	n, err := db.CountOperationsByKind(ctx, OperationCreateChange)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("create_change ops = %d, want 0", n)
	}
}

// mustExecScan reads a single scalar via the pool for test assertions.
func mustExecScan(t *testing.T, db *DB, query string, dst *int) {
	t.Helper()
	if err := db.db.QueryRowContext(context.Background(), query).Scan(dst); err != nil {
		t.Fatalf("scan: %v\nquery: %s", err, query)
	}
}
