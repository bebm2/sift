package storage

import (
	"context"
	"database/sql"
	"encoding/json"
)

func finishAdvance(ctx context.Context, tx *sql.Tx, res sql.Result, cmd AdvanceInterruptCmd, event string) (bool, error) {
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n != 1 {
		return false, ErrRejectedStale
	}
	payload, _ := json.Marshal(map[string]any{"interrupt_id": cmd.InterruptID, "advance": cmd.Kind})
	if _, err := tx.ExecContext(ctx, `INSERT INTO events(id,type,source,payload_schema_version,payload_json,occurred_at_ms,recorded_at_ms) VALUES(?,?,'system',1,?,?,?)`, newID(), event, string(payload), cmd.NowMS, cmd.NowMS); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (d *DB) SupervisorInterruptTick(ctx context.Context, now int64) error {
	rows, err := d.db.QueryContext(ctx, `SELECT id,version,nonce,'expiry' FROM interrupts WHERE status='open' AND dispatch_state!='probe_in_progress' AND (dispatch_state!='held' OR held_reason='manual') AND expires_at_ms<=? UNION ALL SELECT id,version,nonce,'dispatch' FROM interrupts WHERE status='open' AND dispatch_state='ready' AND next_dispatch_at_ms<=?`, now, now)
	if err != nil {
		return err
	}
	defer rows.Close()
	var cmds []AdvanceInterruptCmd
	for rows.Next() {
		var c AdvanceInterruptCmd
		var kind string
		if err := rows.Scan(&c.InterruptID, &c.ExpectedVersion, &c.ExpectedNonce, &kind); err != nil {
			return err
		}
		c.Kind, c.NowMS = AdvanceKind(kind), now
		cmds = append(cmds, c)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, c := range cmds {
		if _, err := d.AdvanceInterrupt(ctx, c); err != nil && err != ErrRejectedStale {
			return err
		}
	}
	// Commander-mode idle heartbeat seam (#1010, NEED-FIX F1): pre-create
	// empty daily_summary collecting batches for projects with Run activity
	// inside IdleRunActivityWindowMS so a zero-interrupt day still has a
	// batch row that PrepareDueAttentionBatches can seal into the single
	// status_note line. The seam is opt-in: without
	// SetIdleDailySummaryConfig (the default for tests) this is a no-op and
	// the legacy "batch created when first interrupt joins" behavior is
	// preserved verbatim. Runs before PrepareDueAttentionBatches so any new
	// collecting row is selected by the same SELECT state='collecting'
	// AND due_at_ms<=now that picks up populated batches.
	if err := d.EnsureIdleDailySummaryBatches(ctx, now); err != nil {
		return err
	}
	return d.PrepareDueAttentionBatches(ctx, now)
}
