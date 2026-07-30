package storage

import (
	"context"
	"database/sql"
	"errors"
)

// GateCandidate is the immutable Run identity plus the latest execution refs
// needed to freeze a Gate input after create_change has converged.
type GateCandidate struct {
	RunID, ProjectID, TaskKind, ChangeID, BaseRef, HeadRef string
	Version                                                int64
	AttemptNo, Generation                                  int
}

// GateReevaluationSource resolves the frozen Run identity (project, task
// kind, change, branch refs) for a gate_re_evaluation worker. It mirrors the
// GateCandidate projection but for a single run, so the worker can route to
// the matching Forge adapter and Gate reconciler without reconstructing these
// refs from mutable state. A run missing its change or branch refs is not a
// valid re-evaluation source.
func (d *DB) GateReevaluationSource(ctx context.Context, runID string) (GateCandidate, error) {
	if runID == "" {
		return GateCandidate{}, errors.New("storage: run id is required")
	}
	var c GateCandidate
	var changeID sql.NullString
	err := d.db.QueryRowContext(ctx, `SELECT r.id,r.project_id,COALESCE(r.kind,''),r.change_id,r.version,
		COALESCE((SELECT a.base_ref FROM attempts a WHERE a.run_id=r.id ORDER BY a.attempt_no DESC LIMIT 1),''),
		COALESCE((SELECT a.branch_name FROM attempts a WHERE a.run_id=r.id ORDER BY a.attempt_no DESC LIMIT 1),''),
		COALESCE((SELECT a.attempt_no FROM attempts a WHERE a.run_id=r.id ORDER BY a.attempt_no DESC LIMIT 1),0),
		COALESCE((SELECT a.generation FROM attempts a WHERE a.run_id=r.id ORDER BY a.attempt_no DESC LIMIT 1),0)
		FROM runs r WHERE r.id=?`, runID).Scan(&c.RunID, &c.ProjectID, &c.TaskKind, &changeID, &c.Version, &c.BaseRef, &c.HeadRef, &c.AttemptNo, &c.Generation)
	if err != nil {
		return GateCandidate{}, err
	}
	c.ChangeID = changeID.String
	return c, nil
}

// FreezeGateChangeHead records the exact Change head that Gate is about to
// evaluate. It returns the current Run version, advancing it only on head drift.
func (d *DB) FreezeGateChangeHead(ctx context.Context, runID, changeID, headSHA string, expectedVersion, nowMS int64) (int64, error) {
	if runID == "" || changeID == "" || headSHA == "" || expectedVersion < 1 || nowMS < 1 {
		return 0, errors.New("storage: invalid gate change identity")
	}
	result, err := d.db.ExecContext(ctx, `UPDATE runs SET change_head_sha=?,version=version+1,updated_at_ms=? WHERE id=? AND change_id=? AND version=? AND (change_head_sha IS NULL OR change_head_sha<>?)`, headSHA, nowMS, runID, changeID, expectedVersion, headSHA)
	if err != nil {
		return 0, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if changed == 1 {
		return expectedVersion + 1, nil
	}
	var version int64
	var currentChange, currentHead string
	if err := d.db.QueryRowContext(ctx, `SELECT version,change_id,COALESCE(change_head_sha,'') FROM runs WHERE id=?`, runID).Scan(&version, &currentChange, &currentHead); err != nil {
		return 0, err
	}
	if version != expectedVersion || currentChange != changeID || currentHead != headSHA {
		return 0, ErrRejectedStale
	}
	return version, nil
}

func (d *DB) GateCandidates(ctx context.Context, projectID string) ([]GateCandidate, error) {
	if projectID == "" {
		return nil, errors.New("storage: gate candidates require project")
	}
	rows, err := d.db.QueryContext(ctx, `SELECT r.id,r.project_id,COALESCE(r.kind,''),r.change_id,r.version,
		COALESCE((SELECT a.base_ref FROM attempts a WHERE a.run_id=r.id ORDER BY a.attempt_no DESC LIMIT 1),''),
		COALESCE((SELECT a.branch_name FROM attempts a WHERE a.run_id=r.id ORDER BY a.attempt_no DESC LIMIT 1),''),
		COALESCE((SELECT a.attempt_no FROM attempts a WHERE a.run_id=r.id ORDER BY a.attempt_no DESC LIMIT 1),0),
		COALESCE((SELECT a.generation FROM attempts a WHERE a.run_id=r.id ORDER BY a.attempt_no DESC LIMIT 1),0)
		FROM runs r WHERE r.project_id=? AND r.change_id IS NOT NULL
		AND r.status IN ('queued','running','waiting_human') ORDER BY r.updated_at_ms,r.id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GateCandidate
	for rows.Next() {
		var c GateCandidate
		if err := rows.Scan(&c.RunID, &c.ProjectID, &c.TaskKind, &c.ChangeID, &c.Version, &c.BaseRef, &c.HeadRef, &c.AttemptNo, &c.Generation); err != nil {
			return nil, err
		}
		if c.TaskKind == "" || c.BaseRef == "" || c.HeadRef == "" {
			continue
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
