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
	// Frozen fuse values are supplied by production callers; defaults preserve
	// compatibility with older supervisor callers and tests.
	CriticalWindowMS    int64
	CriticalTotalLimit  int
	CriticalPerRunLimit int
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
		res, err := tx.ExecContext(ctx, `UPDATE interrupts SET dispatch_state='batched',next_dispatch_at_ms=NULL,version=version+1,updated_at_ms=? WHERE id=? AND status='open' AND dispatch_state='ready' AND version=? AND nonce=? AND next_dispatch_at_ms<=?`, cmd.NowMS, cmd.InterruptID, cmd.ExpectedVersion, cmd.ExpectedNonce, cmd.NowMS)
		if err != nil {
			return false, err
		}
		if n, _ := res.RowsAffected(); n == 1 {
			if err := enqueueInterruptChannelTx(ctx, tx, cmd.InterruptID, cmd.ExpectedVersion, cmd.ExpectedNonce, escalation, "normal", cmd.NowMS); err != nil {
				return false, err
			}
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

	// base_severity is the domain severity before escalation. Recompute from
	// it, rather than repeatedly promoting the already-promoted snapshot.
	nextSeverity := InterruptSeverity(base)
	for i := 0; i <= escalation; i++ {
		nextSeverity = promoteSeverity(nextSeverity)
	}
	if downgraded {
		nextSeverity = downgradeInterruptSeverity(nextSeverity)
	}
	if nextSeverity == SeverityCritical {
		admitted, err := admitCriticalTx(ctx, tx, cmd.InterruptID, cmd.NowMS, "escalation", cmd)
		if err != nil {
			return false, err
		}
		if !admitted {
			res, err := tx.ExecContext(ctx, `UPDATE interrupts SET dispatch_state='held',delivery='held',held_reason='critical_fuse',next_dispatch_at_ms=NULL,version=version+1,updated_at_ms=? WHERE id=? AND status='open' AND version=? AND nonce=?`, cmd.NowMS, cmd.InterruptID, cmd.NowMS, cmd.InterruptID, cmd.ExpectedVersion, cmd.ExpectedNonce)
			if err != nil {
				return false, err
			}
			return finishAdvance(ctx, tx, res, cmd, "interrupt.critical_fused")
		}
	}
	nextState, delivery, heldReason := "ready", "batch", ""
	var nextDispatch any
	if nextSeverity == SeverityHigh || nextSeverity == SeverityCritical {
		delivery = "immediate"
		nextDispatch = cmd.NowMS
	} else {
		// Reuse only the frozen summary instant; never invent a new window.
		nextState, delivery, heldReason, nextDispatch = "held", "held", "batch_after_expiry", nil
	}
	newNonce := newID()
	res, err := tx.ExecContext(ctx, `UPDATE interrupts SET severity=?,nonce=?,nonce_issued_at_ms=?,version=version+1,escalation_count=escalation_count+1,expires_at_ms=?,dispatch_state=?,delivery=?,held_reason=?,next_dispatch_at_ms=?,updated_at_ms=? WHERE id=? AND status='open' AND version=? AND nonce=?`, nextSeverity, newNonce, cmd.NowMS, cmd.NowMS+expiresAfter, nextState, delivery, nullable(heldReason), nextDispatch, cmd.NowMS, cmd.InterruptID, cmd.ExpectedVersion, cmd.ExpectedNonce)
	if err != nil {
		return false, err
	}
	_ = severity
	if n, _ := res.RowsAffected(); n == 1 && delivery == "immediate" {
		if err := enqueueInterruptChannelTx(ctx, tx, cmd.InterruptID, cmd.ExpectedVersion+1, newNonce, escalation+1, "strong", cmd.NowMS); err != nil {
			return false, err
		}
	}
	return finishAdvance(ctx, tx, res, cmd, "interrupt.escalated")
}

func (d *DB) closeExpiredInterrupt(ctx context.Context, tx *sql.Tx, cmd AdvanceInterruptCmd) (bool, error) {
	var runID, status string
	var runVersion int64
	if err := tx.QueryRowContext(ctx, `SELECT r.id,r.status,r.version FROM interrupts i JOIN runs r ON r.id=i.run_id WHERE i.id=?`, cmd.InterruptID).Scan(&runID, &status, &runVersion); err != nil {
		return false, err
	}
	if RunStatus(status) != RunWaitingHuman {
		return false, ErrRejectedStale
	}
	if err := d.transition(ctx, tx, runID, runVersion, DomainCommand{To: RunFailed, Source: SourceSystem, Actor: "advance_interrupt", FailureReason: "hitl_expired", OccurredAtMS: cmd.NowMS}); err != nil {
		return false, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE interrupts SET status='closed',close_reason='expired_auto_reject',closed_at_ms=?,version=version+1,updated_at_ms=? WHERE id=? AND status='open' AND version=? AND nonce=?`, cmd.NowMS, cmd.NowMS, cmd.InterruptID, cmd.ExpectedVersion, cmd.ExpectedNonce)
	if err != nil {
		return false, err
	}
	return finishAdvance(ctx, tx, res, cmd, "interrupt.expired_auto_reject")
}

func enqueueInterruptChannelTx(ctx context.Context, tx *sql.Tx, interruptID string, version int64, nonce string, escalation int, priority string, nowMS int64) error {
	var channel, modality, headline, brief string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(channel_id,''),min_modality,headline,brief_markdown FROM interrupts WHERE id=?`, interruptID).Scan(&channel, &modality, &headline, &brief); err != nil {
		return err
	}
	if channel == "" {
		return nil
	}
	deliveryID := fmt.Sprintf("interrupt:%s:%d:%s", interruptID, escalation, channel)
	key := ChannelPublishOperationKey(interruptID, escalation)
	payload, _ := json.Marshal(map[string]any{"delivery_kind": "interrupt", "delivery_id": deliveryID, "interrupt_id": interruptID, "escalation_no": escalation, "priority": priority, "interrupt_version": version, "nonce": nonce, "channel": map[string]any{"id": channel, "type": "webhook", "target_ref": "secret_ref:" + channel, "renderer": "plain-v1", "capabilities": []string{modality}}, "rendered_text": headline + "\n\n" + brief})
	if err := insertOperation(ctx, tx, Operation{Key: key, Kind: OperationChannelPublish, Payload: payload, InterruptID: interruptID}, "", "", nowMS); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO interrupt_deliveries(id,delivery_id,interrupt_id,surface,channel_id,channel_snapshot_json,interrupt_version,nonce,escalation_no,priority,operation_key,state,attempt_count,created_at_ms) VALUES(?,?,?,'channel',?,?,?,?,?,?,?,'pending',0,?)`, newID(), deliveryID, interruptID, channel, string(mustJSON(map[string]any{"id": channel})), version, nonce, escalation, priority, key, nowMS)
	return err
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

func admitCriticalTx(ctx context.Context, tx *sql.Tx, interruptID string, nowMS int64, source string, cmd AdvanceInterruptCmd) (bool, error) {
	var runID string
	if err := tx.QueryRowContext(ctx, `SELECT run_id FROM interrupts WHERE id=?`, interruptID).Scan(&runID); err != nil {
		return false, err
	}
	key := interruptID + ":critical"
	var existing string
	if err := tx.QueryRowContext(ctx, `SELECT kind FROM attention_admissions WHERE admission_key=?`, key).Scan(&existing); err == nil {
		return existing == "critical_admitted", nil
	} else if err != sql.ErrNoRows {
		return false, err
	}
	// A zero limit is used by old callers that have no frozen fuse snapshot;
	// production callers pass the limits in the command extension.
	if cmd.CriticalTotalLimit <= 0 {
		cmd.CriticalTotalLimit = 5
	}
	if cmd.CriticalWindowMS <= 0 {
		cmd.CriticalWindowMS = 15 * 60 * 1000
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM attention_admissions WHERE created_at_ms>? AND kind='critical_admitted'`, nowMS-cmd.CriticalWindowMS).Scan(&count); err != nil {
		return false, err
	}
	kind := "critical_admitted"
	if count >= cmd.CriticalTotalLimit {
		kind = "critical_fused"
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO attention_admissions(id,interrupt_id,admission_key,kind,metric_identity,run_id,critical_source,created_at_ms) VALUES(?,?,?,?,?,?,?,?)`, newID(), interruptID, key, kind, interruptID, runID, source, nowMS)
	return err == nil && kind == "critical_admitted", err
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
