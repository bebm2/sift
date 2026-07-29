package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// AdvanceKind identifies the only two supervisor-driven Interrupt transitions.
type AdvanceKind string

const (
	AdvanceExpiry   AdvanceKind = "expiry"
	AdvanceDispatch AdvanceKind = "dispatch"
)

// AdvanceInterruptCmd carries the version and nonce observed by a supervisor
// scan. A stale scan is rejected without creating any secondary effect.
type AdvanceInterruptCmd struct {
	InterruptID     string
	ExpectedVersion int64
	ExpectedNonce   string
	Kind            AdvanceKind
	NowMS           int64
}

// AdvanceInterrupt is the sole write port for expiry and dispatch scans.
// It intentionally does not charge attention: escalation reuses the initial
// Interrupt's admission and a stale CAS has no side effects.
func (d *DB) AdvanceInterrupt(ctx context.Context, cmd AdvanceInterruptCmd) (bool, error) {
	if cmd.InterruptID == "" || cmd.ExpectedVersion < 1 || cmd.ExpectedNonce == "" || cmd.NowMS <= 0 || (cmd.Kind != AdvanceExpiry && cmd.Kind != AdvanceDispatch) {
		return false, fmt.Errorf("%w: invalid advance command", ErrInterruptRejected)
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	var status, state, held, nonce, reason, severity, base, onExpire, onMax string
	var version, expiresAt, expiresAfter int64
	var escalation, max int
	var downgraded bool
	err = tx.QueryRowContext(ctx, `SELECT status,dispatch_state,COALESCE(held_reason,''),nonce,reason,severity,base_severity,on_expire,on_max_escalations,version,expires_at_ms,expires_after_ms,escalation_count,max_escalations,suggested_downgrade FROM interrupts WHERE id=?`, cmd.InterruptID).Scan(&status, &state, &held, &nonce, &reason, &severity, &base, &onExpire, &onMax, &version, &expiresAt, &expiresAfter, &escalation, &max, &downgraded)
	if err == sql.ErrNoRows {
		return false, ErrRejectedStale
	}
	if err != nil {
		return false, err
	}
	if status != "open" || version != cmd.ExpectedVersion || nonce != cmd.ExpectedNonce {
		return false, ErrRejectedStale
	}

	if cmd.Kind == AdvanceDispatch {
		if state != "ready" || expiresAt <= cmd.NowMS {
			return false, ErrRejectedStale
		}
		// Channel publication is deliberately owned by the channel worker. This
		// transition consumes the durable due marker exactly once; a later worker
		// cannot turn an old supervisor snapshot into a second delivery.
		res, err := tx.ExecContext(ctx, `UPDATE interrupts SET dispatch_state='batched',next_dispatch_at_ms=NULL,version=version+1,updated_at_ms=? WHERE id=? AND status='open' AND dispatch_state='ready' AND version=? AND nonce=? AND next_dispatch_at_ms<=?`, cmd.NowMS, cmd.InterruptID, cmd.ExpectedVersion, cmd.ExpectedNonce, cmd.NowMS)
		if err != nil {
			return false, err
		}
		return finishAdvance(ctx, tx, res, cmd, "interrupt.dispatched")
	}

	if state == "probe_in_progress" || (state == "held" && held != "manual") || expiresAt > cmd.NowMS {
		return false, ErrRejectedStale
	}
	if onExpire == string(ExpireHold) {
		res, err := tx.ExecContext(ctx, `UPDATE interrupts SET dispatch_state='held',delivery='held',held_reason='expiry',next_dispatch_at_ms=NULL,version=version+1,updated_at_ms=? WHERE id=? AND status='open' AND version=? AND nonce=?`, cmd.NowMS, cmd.InterruptID, cmd.ExpectedVersion, cmd.ExpectedNonce)
		if err != nil {
			return false, err
		}
		return finishAdvance(ctx, tx, res, cmd, "interrupt.expired")
	}
	if onExpire == string(ExpireAutoReject) {
		return d.closeExpiredInterrupt(ctx, tx, cmd)
	}
	if escalation >= max {
		if onMax == string(ExpireAutoReject) && reason != string(InterruptStartupStall) {
			return d.closeExpiredInterrupt(ctx, tx, cmd)
		}
		res, err := tx.ExecContext(ctx, `UPDATE interrupts SET dispatch_state='held',delivery='held',held_reason='max_escalations',next_dispatch_at_ms=NULL,version=version+1,updated_at_ms=? WHERE id=? AND status='open' AND version=? AND nonce=?`, cmd.NowMS, cmd.InterruptID, cmd.ExpectedVersion, cmd.ExpectedNonce)
		if err != nil {
			return false, err
		}
		return finishAdvance(ctx, tx, res, cmd, "interrupt.max_escalations")
	}

	nextSeverity := promoteSeverity(InterruptSeverity(base))
	if downgraded {
		nextSeverity = downgradeInterruptSeverity(nextSeverity)
	}
	nextState, delivery, heldReason := "ready", "batch", ""
	var nextDispatch any
	if nextSeverity == SeverityHigh || nextSeverity == SeverityCritical {
		delivery = "immediate"
		nextDispatch = cmd.NowMS
	} else {
		// The initial batch window is deliberately not reused after expiry. The
		// scheduler has no authority to invent a new availability window.
		nextState, delivery, heldReason, nextDispatch = "held", "held", "batch_after_expiry", nil
	}
	newNonce := newID()
	res, err := tx.ExecContext(ctx, `UPDATE interrupts SET severity=?,nonce=?,nonce_issued_at_ms=?,version=version+1,escalation_count=escalation_count+1,expires_at_ms=?,dispatch_state=?,delivery=?,held_reason=?,next_dispatch_at_ms=?,updated_at_ms=? WHERE id=? AND status='open' AND version=? AND nonce=?`, nextSeverity, newNonce, cmd.NowMS, cmd.NowMS+expiresAfter, nextState, delivery, nullable(heldReason), nextDispatch, cmd.NowMS, cmd.InterruptID, cmd.ExpectedVersion, cmd.ExpectedNonce)
	if err != nil {
		return false, err
	}
	_ = severity // retained in the read set as audit of the CAS snapshot.
	return finishAdvance(ctx, tx, res, cmd, "interrupt.escalated")
}

func (d *DB) closeExpiredInterrupt(ctx context.Context, tx *sql.Tx, cmd AdvanceInterruptCmd) (bool, error) {
	res, err := tx.ExecContext(ctx, `UPDATE interrupts SET status='closed',close_reason='expired_auto_reject',closed_at_ms=?,version=version+1,updated_at_ms=? WHERE id=? AND status='open' AND version=? AND nonce=?`, cmd.NowMS, cmd.NowMS, cmd.InterruptID, cmd.ExpectedVersion, cmd.ExpectedNonce)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		if _, err := tx.ExecContext(ctx, `UPDATE runs SET status='failed',failure_reason='hitl_expired',completed_at_ms=?,version=version+1,updated_at_ms=? WHERE id=(SELECT run_id FROM interrupts WHERE id=?) AND status='waiting_human'`, cmd.NowMS, cmd.NowMS, cmd.InterruptID); err != nil {
			return false, err
		}
	}
	return finishAdvance(ctx, tx, res, cmd, "interrupt.expired_auto_reject")
}

func finishAdvance(ctx context.Context, tx *sql.Tx, res sql.Result, cmd AdvanceInterruptCmd, eventType string) (bool, error) {
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n != 1 {
		return false, ErrRejectedStale
	}
	payload, _ := json.Marshal(map[string]any{"interrupt_id": cmd.InterruptID, "advance": cmd.Kind})
	if _, err := tx.ExecContext(ctx, `INSERT INTO events(id,type,source,payload_schema_version,payload_json,occurred_at_ms,recorded_at_ms) VALUES(?,?,'system',1,?,?,?)`, newID(), eventType, string(payload), cmd.NowMS, cmd.NowMS); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// SupervisorInterruptTick performs the two persisted scans. It never mutates
// Interrupt rows directly; every candidate goes through AdvanceInterrupt.
func (d *DB) SupervisorInterruptTick(ctx context.Context, nowMS int64) error {
	rows, err := d.db.QueryContext(ctx, `SELECT id,version,nonce,'expiry' FROM interrupts WHERE status='open' AND dispatch_state != 'probe_in_progress' AND (dispatch_state != 'held' OR held_reason='manual') AND expires_at_ms<=?
		UNION ALL
		SELECT id,version,nonce,'dispatch' FROM interrupts WHERE status='open' AND dispatch_state='ready' AND next_dispatch_at_ms<=?`, nowMS, nowMS)
	if err != nil {
		return err
	}
	defer rows.Close()
	var cmds []AdvanceInterruptCmd
	for rows.Next() {
		var cmd AdvanceInterruptCmd
		var kind string
		if err := rows.Scan(&cmd.InterruptID, &cmd.ExpectedVersion, &cmd.ExpectedNonce, &kind); err != nil {
			return err
		}
		cmd.Kind, cmd.NowMS = AdvanceKind(kind), nowMS
		cmds = append(cmds, cmd)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, cmd := range cmds {
		if _, err := d.AdvanceInterrupt(ctx, cmd); err != nil && err != ErrRejectedStale {
			return err
		}
	}
	return nil
}
