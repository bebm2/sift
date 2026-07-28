package storage

import (
	"context"
	"database/sql"
	"fmt"
)

// Event is the read model of one events row. It is the authoritative timeline
// of a Run: append-only, ordered by seq, carrying the timestamps the M1
// skeleton P50 measurement (PRD §10.2 trigger→started) is computed from.
type Event struct {
	Seq          int64
	ID           string
	RunID        string
	AttemptNo    *int
	ProjectID    string
	Type         string
	Source       string
	Actor        string
	PayloadJSON  []byte
	OccurredAtMS int64
	RecordedAtMS int64
}

// RunEvents returns the events of one Run in seq order (storage.md §7.1
// append-only stream). It is the read port the M1 skeleton uses to locate the
// trigger-observed and agent-started events for the P50 computation.
func (d *DB) RunEvents(ctx context.Context, runID string) ([]Event, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT seq, id, COALESCE(run_id,''), attempt_no, COALESCE(project_id,''),
		type, source, actor, payload_json, occurred_at_ms, recorded_at_ms
		FROM events WHERE run_id=? ORDER BY seq`, runID)
	if err != nil {
		return nil, fmt.Errorf("storage: run events: %w", err)
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		var actor sql.NullString
		var attemptNo sql.NullInt64
		if err := rows.Scan(&e.Seq, &e.ID, &e.RunID, &attemptNo, &e.ProjectID, &e.Type, &e.Source,
			&actor, &e.PayloadJSON, &e.OccurredAtMS, &e.RecordedAtMS); err != nil {
			return nil, err
		}
		if attemptNo.Valid {
			v := int(attemptNo.Int64)
			e.AttemptNo = &v
		}
		e.Actor = actor.String
		out = append(out, e)
	}
	return out, rows.Err()
}

// FirstEventOfType returns the first event of one type for a Run, or false. The
// skeleton uses it to resolve the P50 anchors deterministically.
func (d *DB) FirstEventOfType(ctx context.Context, runID, eventType string) (Event, bool, error) {
	events, err := d.RunEvents(ctx, runID)
	if err != nil {
		return Event{}, false, err
	}
	for _, e := range events {
		if e.Type == eventType {
			return e, true, nil
		}
	}
	return Event{}, false, nil
}
