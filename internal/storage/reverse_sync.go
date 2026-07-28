package storage

import (
	"context"
	"database/sql"
	"errors"
)

// ReverseSyncIssue applies Forge-owned Issue facts back to Sift. Forge close
// is authoritative and may fail a queued/running/waiting Run; an open Issue
// never reopens a terminal Run. The transition port supplies the CAS and audit
// event, keeping this direction separate from label projection writes.
func (d *DB) ReverseSyncIssue(ctx context.Context, projectID, issueID string, closed bool, nowMS int64) error {
	if projectID == "" || issueID == "" || nowMS <= 0 {
		return errors.New("storage: invalid reverse sync issue")
	}
	var id string
	var version int64
	var status string
	err := d.db.QueryRowContext(ctx, `SELECT id,version,status FROM runs WHERE project_id=? AND issue_id=? ORDER BY created_at_ms DESC LIMIT 1`, projectID, issueID).Scan(&id, &version, &status)
	if errors.Is(err, sql.ErrNoRows) || !closed {
		return nil
	}
	if err != nil {
		return err
	}
	switch RunStatus(status) {
	case RunQueued, RunRunning, RunWaitingHuman:
		_, err = d.TransitionRun(ctx, id, version, DomainCommand{To: RunFailed, Source: SourceForge, FailureReason: "closed_upstream", OccurredAtMS: nowMS})
	}
	return err
}
