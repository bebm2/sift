package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// SetInitialTaskSpec is the T2 valid hitl=false commit port (storage.md §11).
// It idempotently inserts the initial Task Spec snapshot, writes the Run
// kind/agent/hitl + current_task_spec pointer and appends an event, in one
// transaction. The Run stays in (or is already in) the queued status — it is
// not exposed as launchable before the assignment is committed (brain.md §8.3).
//
// The full CommitT2Assignment (hitl=true → design_approval Interrupt in the same
// transaction) lands in M3 with the Interrupt emission core; this M1 port
// covers exactly the skeleton chain's hitl=false path.
type SetInitialTaskSpecCmd struct {
	RunID           string
	ExpectedVersion int64
	TaskSpecID      string
	CanonicalJSON   []byte
	ContentDigest   string
	Kind            string // feature|bug|chore|docs|refactor
	AgentID         string
	HITLBeforeStart bool
	SourceEventID   string // optional provenance event
	OccurredAtMS    int64
}

// SetInitialTaskSpec commits the initial Task Spec and Run assignment. It is
// idempotent on (run_id, version=1): re-applying the same snapshot is a no-op.
func (d *DB) SetInitialTaskSpec(ctx context.Context, cmd SetInitialTaskSpecCmd) (Run, error) {
	if cmd.RunID == "" || cmd.ExpectedVersion < 1 {
		return Run{}, errors.New("storage: set initial task spec requires run id and expected version")
	}
	if cmd.TaskSpecID == "" || len(cmd.CanonicalJSON) == 0 || cmd.ContentDigest == "" {
		return Run{}, errors.New("storage: set initial task spec requires snapshot id/json/digest")
	}
	if !json.Valid(cmd.CanonicalJSON) {
		return Run{}, errors.New("storage: set initial task spec canonical json is not valid")
	}
	if cmd.OccurredAtMS <= 0 {
		return Run{}, errors.New("storage: set initial task spec requires occurred_at_ms")
	}

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return Run{}, fmt.Errorf("storage: begin set initial task spec: %w", err)
	}
	defer tx.Rollback()

	var status string
	var version int64
	if err := tx.QueryRowContext(ctx, `SELECT status, version FROM runs WHERE id=?`, cmd.RunID).Scan(&status, &version); err != nil {
		return Run{}, err
	}
	if version != cmd.ExpectedVersion {
		return Run{}, ErrRejectedStale
	}
	// The assignment port only writes the kind/agent; a Run that already left
	// queued (waiting_human/running/...) was committed by a different path and
	// must not be silently re-assigned here.
	if status != string(RunQueued) {
		return Run{}, fmt.Errorf("%w: set initial task spec requires queued, got %s", ErrIllegalTransition, status)
	}

	sourceEvent := cmd.SourceEventID
	if _, err := tx.ExecContext(ctx, `INSERT INTO task_spec_snapshots
		(id, run_id, version, schema_version, canonical_json, content_digest, source_event_id, created_at_ms)
		VALUES (?, ?, 1, 1, ?, ?, ?, ?)
		ON CONFLICT(run_id, version) DO NOTHING`,
		cmd.TaskSpecID, cmd.RunID, string(cmd.CanonicalJSON), cmd.ContentDigest, nullable(sourceEvent), cmd.OccurredAtMS); err != nil {
		return Run{}, fmt.Errorf("storage: insert initial task spec snapshot: %w", err)
	}

	res, err := tx.ExecContext(ctx, `UPDATE runs
		SET kind=?, agent_id=?, hitl_before_start=?, current_task_spec_id=?, version=version+1, updated_at_ms=?
		WHERE id=? AND version=? AND status='queued'`,
		cmd.Kind, cmd.AgentID, boolInt(cmd.HITLBeforeStart), cmd.TaskSpecID, cmd.OccurredAtMS, cmd.RunID, cmd.ExpectedVersion)
	if err != nil {
		return Run{}, fmt.Errorf("storage: assign run: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Run{}, err
	}
	if n != 1 {
		return Run{}, ErrRejectedStale
	}

	payload, _ := json.Marshal(map[string]any{
		"kind":              cmd.Kind,
		"agent":             cmd.AgentID,
		"hitl_before_start": cmd.HITLBeforeStart,
		"task_spec_id":      cmd.TaskSpecID,
	})
	if _, err := tx.ExecContext(ctx, `INSERT INTO events
		(id, run_id, type, source, payload_schema_version, payload_json, occurred_at_ms, recorded_at_ms)
		VALUES (?, ?, 'run.assigned', 'system', 1, ?, ?, ?)`,
		newID(), cmd.RunID, string(payload), cmd.OccurredAtMS, cmd.OccurredAtMS); err != nil {
		return Run{}, fmt.Errorf("storage: insert assignment event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Run{}, fmt.Errorf("storage: commit set initial task spec: %w", err)
	}
	return d.Run(ctx, cmd.RunID)
}
