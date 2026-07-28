package storage

import (
	"context"
	"database/sql"
	"errors"
)

// ReverseSyncCandidate is the forge identity needed to reconcile one active
// Run. It is deliberately a small read projection; state changes still enter
// through TransitionRun.
type ReverseSyncCandidate struct {
	RunID     string
	Version   int64
	ProjectID string
	IssueID   string
	ChangeID  string
}

// ReverseSyncCandidates returns non-terminal forge Runs for a project. The
// reconciler re-reads forge facts for these rows on every tick, so a closed or
// merged object cannot remain an active local Run after a restart.
func (d *DB) ReverseSyncCandidates(ctx context.Context, projectID string) ([]ReverseSyncCandidate, error) {
	if projectID == "" {
		return nil, errors.New("storage: reverse sync requires project")
	}
	rows, err := d.db.QueryContext(ctx, `SELECT id,version,project_id,issue_id,COALESCE(change_id,'')
		FROM runs WHERE project_id=? AND issue_id IS NOT NULL AND status IN ('queued','running','waiting_human') ORDER BY created_at_ms,id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReverseSyncCandidate
	for rows.Next() {
		var c ReverseSyncCandidate
		if err := rows.Scan(&c.RunID, &c.Version, &c.ProjectID, &c.IssueID, &c.ChangeID); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ReverseSyncIssue applies the Issue fact back to Sift. Issue closure is a
// fact observation and therefore has no actor gate.
func (d *DB) ReverseSyncIssue(ctx context.Context, projectID, issueID string, closed bool, nowMS int64) error {
	if projectID == "" || issueID == "" || nowMS <= 0 {
		return errors.New("storage: invalid reverse sync issue")
	}
	if !closed {
		return nil
	}
	var id string
	var version int64
	err := d.db.QueryRowContext(ctx, `SELECT id,version FROM runs WHERE project_id=? AND issue_id=? AND status IN ('queued','running','waiting_human') ORDER BY created_at_ms DESC LIMIT 1`, projectID, issueID).Scan(&id, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = d.TransitionRun(ctx, id, version, DomainCommand{To: RunFailed, Source: SourceForge, FailureReason: "closed_upstream", OccurredAtMS: nowMS})
	return err
}
