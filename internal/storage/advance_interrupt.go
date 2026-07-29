package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

type AdvanceKind string

const (
	AdvanceExpiry   AdvanceKind = "expiry"
	AdvanceDispatch AdvanceKind = "dispatch"
)

type AdvanceInterruptCmd struct {
	InterruptID     string
	ExpectedVersion int64
	ExpectedNonce   string
	Kind            AdvanceKind
	NowMS           int64
}

func (d *DB) AdvanceInterrupt(ctx context.Context, cmd AdvanceInterruptCmd) (bool, error) {
	if cmd.InterruptID == "" || cmd.ExpectedVersion < 1 || cmd.ExpectedNonce == "" || cmd.NowMS <= 0 || (cmd.Kind != AdvanceExpiry && cmd.Kind != AdvanceDispatch) {
		return false, fmt.Errorf("%w: invalid advance command", ErrInterruptRejected)
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var status, state, held, nonce, reason, base, onExpire, onMax, channel, snapshot, zone, summary string
	var version, expiresAt, expiresAfter, window int64
	var escalation, max, total, perRun int
	var downgraded bool
	err = tx.QueryRowContext(ctx, `SELECT status,dispatch_state,COALESCE(held_reason,''),nonce,reason,base_severity,on_expire,on_max_escalations,COALESCE(channel_id,''),COALESCE(channel_snapshot_json,''),day_timezone,daily_summary_at,version,expires_at_ms,expires_after_ms,escalation_count,max_escalations,suggested_downgrade,critical_window_ms,critical_total_limit,critical_per_run_limit FROM interrupts WHERE id=?`, cmd.InterruptID).Scan(&status, &state, &held, &nonce, &reason, &base, &onExpire, &onMax, &channel, &snapshot, &zone, &summary, &version, &expiresAt, &expiresAfter, &escalation, &max, &downgraded, &window, &total, &perRun)
	if err == sql.ErrNoRows || status != "open" || version != cmd.ExpectedVersion || nonce != cmd.ExpectedNonce {
		return false, ErrRejectedStale
	}
	if err != nil {
		return false, err
	}
	if cmd.Kind == AdvanceDispatch {
		if state != "ready" || expiresAt <= cmd.NowMS {
			return false, ErrRejectedStale
		}
		if err := addDailyBatchMemberTx(ctx, tx, cmd.InterruptID, cmd.ExpectedVersion, cmd.ExpectedNonce, cmd.NowMS, channel, snapshot, zone, summary); err != nil {
			return false, err
		}
		res, err := tx.ExecContext(ctx, `UPDATE interrupts SET dispatch_state='batched',next_dispatch_at_ms=NULL,version=version+1,updated_at_ms=? WHERE id=? AND status='open' AND dispatch_state='ready' AND version=? AND nonce=?`, cmd.NowMS, cmd.InterruptID, cmd.ExpectedVersion, cmd.ExpectedNonce)
		if err != nil {
			return false, err
		}
		return finishAdvance(ctx, tx, res, cmd, "interrupt.dispatched")
	}
	if state == "probe_in_progress" || (state == "held" && held != "manual") || expiresAt > cmd.NowMS {
		return false, ErrRejectedStale
	}
	if onExpire == string(ExpireHold) {
		return d.holdAdvance(ctx, tx, cmd, "expiry", "interrupt.expired")
	}
	if onExpire == string(ExpireAutoReject) {
		return d.closeExpiredInterrupt(ctx, tx, cmd)
	}
	if escalation >= max {
		if onMax == string(ExpireAutoReject) && reason != string(InterruptStartupStall) {
			return d.closeExpiredInterrupt(ctx, tx, cmd)
		}
		return d.holdAdvance(ctx, tx, cmd, "max_escalations", "interrupt.max_escalations")
	}
	next := InterruptSeverity(base)
	for i := 0; i <= escalation; i++ {
		next = promoteSeverity(next)
	}
	if downgraded {
		next = downgradeInterruptSeverity(next)
	}
	newNonce := newID()
	nextState, delivery, heldReason := "ready", "batch", ""
	var due any
	if next == SeverityHigh || next == SeverityCritical {
		delivery = "immediate"
		due = cmd.NowMS
	} else if at, ok := nextSummary(cmd.NowMS, zone, summary); ok && at < cmd.NowMS+expiresAfter {
		due = at
	} else {
		nextState, delivery, heldReason = "held", "held", "batch_after_expiry"
	}
	if next == SeverityCritical {
		admitted, admissionID, err := admitCriticalTx(ctx, tx, cmd.InterruptID, cmd.NowMS, "escalation", window, total, perRun)
		if err != nil {
			return false, err
		}
		if !admitted {
			if err := addCriticalBatchMemberTx(ctx, tx, cmd.InterruptID, cmd.ExpectedVersion+1, newNonce, admissionID, channel, snapshot, cmd.NowMS); err != nil {
				return false, err
			}
			nextState, delivery, heldReason, due = "batched", "batch", "", nil
		}
	}
	res, err := tx.ExecContext(ctx, `UPDATE interrupts SET severity=?,nonce=?,nonce_issued_at_ms=?,version=version+1,escalation_count=escalation_count+1,expires_at_ms=?,dispatch_state=?,delivery=?,held_reason=?,next_dispatch_at_ms=?,updated_at_ms=? WHERE id=? AND status='open' AND version=? AND nonce=?`, next, newNonce, cmd.NowMS, cmd.NowMS+expiresAfter, nextState, delivery, nullable(heldReason), due, cmd.NowMS, cmd.InterruptID, cmd.ExpectedVersion, cmd.ExpectedNonce)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 1 && delivery == "immediate" {
		if err := enqueueInterruptChannelTx(ctx, tx, cmd.InterruptID, cmd.ExpectedVersion+1, newNonce, escalation+1, "strong", cmd.NowMS); err != nil {
			return false, err
		}
	}
	return finishAdvance(ctx, tx, res, cmd, "interrupt.escalated")
}

func (d *DB) holdAdvance(ctx context.Context, tx *sql.Tx, cmd AdvanceInterruptCmd, reason, event string) (bool, error) {
	res, err := tx.ExecContext(ctx, `UPDATE interrupts SET dispatch_state='held',delivery='held',held_reason=?,next_dispatch_at_ms=NULL,version=version+1,updated_at_ms=? WHERE id=? AND status='open' AND version=? AND nonce=?`, reason, cmd.NowMS, cmd.InterruptID, cmd.ExpectedVersion, cmd.ExpectedNonce)
	if err != nil {
		return false, err
	}
	return finishAdvance(ctx, tx, res, cmd, event)
}

func (d *DB) closeExpiredInterrupt(ctx context.Context, tx *sql.Tx, cmd AdvanceInterruptCmd) (bool, error) {
	var runID, status, binding string
	var runVersion int64
	if err := tx.QueryRowContext(ctx, `SELECT r.id,r.status,r.version,b.binding_json FROM interrupts i JOIN runs r ON r.id=i.run_id JOIN interrupt_command_effect_bindings b ON b.interrupt_id=i.id WHERE i.id=?`, cmd.InterruptID).Scan(&runID, &status, &runVersion, &binding); err != nil {
		return false, err
	}
	var arm struct {
		Arm          string `json:"arm"`
		NoTransition bool   `json:"no_transition"`
	}
	if json.Unmarshal([]byte(binding), &arm) != nil {
		return false, fmt.Errorf("%w: corrupt effect binding", ErrInterruptRejected)
	}
	// Older rows used a boolean; new rows use the closed tagged union.
	noTransition := arm.NoTransition || arm.Arm == "report_quota_failure_review"
	if !noTransition {
		if RunStatus(status) != RunWaitingHuman {
			return false, ErrRejectedStale
		}
		if err := d.transition(ctx, tx, runID, runVersion, DomainCommand{To: RunFailed, Source: SourceSystem, Actor: "advance_interrupt", FailureReason: "hitl_expired", OccurredAtMS: cmd.NowMS}); err != nil {
			return false, err
		}
	}
	res, err := tx.ExecContext(ctx, `UPDATE interrupts SET status='closed',close_reason='expired_auto_reject',closed_at_ms=?,version=version+1,updated_at_ms=? WHERE id=? AND status='open' AND version=? AND nonce=?`, cmd.NowMS, cmd.NowMS, cmd.InterruptID, cmd.ExpectedVersion, cmd.ExpectedNonce)
	if err != nil {
		return false, err
	}
	return finishAdvance(ctx, tx, res, cmd, "interrupt.expired_auto_reject")
}

func enqueueInterruptChannelTx(ctx context.Context, tx *sql.Tx, id string, version int64, nonce string, escalation int, priority string, now int64) error {
	var channel, snapshot, headline, brief string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(channel_id,''),COALESCE(channel_snapshot_json,''),headline,brief_markdown FROM interrupts WHERE id=?`, id).Scan(&channel, &snapshot, &headline, &brief); err != nil {
		return err
	}
	if channel == "" || snapshot == "" {
		return nil
	}
	deliveryID := fmt.Sprintf("interrupt:%s:%d:%s", id, escalation, channel)
	key := ChannelPublishOperationKey(id, escalation)
	var ch any
	if err := json.Unmarshal([]byte(snapshot), &ch); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{"delivery_kind": "interrupt", "delivery_id": deliveryID, "interrupt_id": id, "escalation_no": escalation, "priority": priority, "interrupt_version": version, "nonce": nonce, "channel": ch, "rendered_text": headline + "\n\n" + brief})
	if err := insertOperation(ctx, tx, Operation{Key: key, Kind: OperationChannelPublish, Payload: payload, InterruptID: id}, "", "", now); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO interrupt_deliveries(id,delivery_id,interrupt_id,surface,channel_id,channel_snapshot_json,interrupt_version,nonce,escalation_no,priority,operation_key,state,attempt_count,created_at_ms) VALUES(?,?,?,'channel',?,?,?,?,?,?,?,'pending',0,?)`, newID(), deliveryID, id, channel, snapshot, version, nonce, escalation, priority, key, now)
	return err
}

func admitCriticalTx(ctx context.Context, tx *sql.Tx, id string, now int64, source string, window int64, total, perRun int) (bool, string, error) {
	var run string
	if err := tx.QueryRowContext(ctx, `SELECT run_id FROM interrupts WHERE id=?`, id).Scan(&run); err != nil {
		return false, "", err
	}
	key := id + ":critical"
	var kind, existing string
	if err := tx.QueryRowContext(ctx, `SELECT id,kind FROM attention_admissions WHERE admission_key=?`, key).Scan(&existing, &kind); err == nil {
		return kind == "critical_admitted", existing, nil
	} else if err != sql.ErrNoRows {
		return false, "", err
	}
	var global, local int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM attention_admissions WHERE kind='critical_admitted' AND created_at_ms>=? AND created_at_ms<?`, now-window, now).Scan(&global); err != nil {
		return false, "", err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM attention_admissions WHERE kind='critical_admitted' AND run_id=? AND created_at_ms>=? AND created_at_ms<?`, run, now-window, now).Scan(&local); err != nil {
		return false, "", err
	}
	kind = "critical_admitted"
	if global >= total || local >= perRun {
		kind = "critical_fused"
	}
	admission := newID()
	_, err := tx.ExecContext(ctx, `INSERT INTO attention_admissions(id,interrupt_id,admission_key,kind,metric_identity,run_id,critical_source,created_at_ms) VALUES(?,?,?,?,?,?,?,?)`, admission, id, key, kind, id, run, source, now)
	return kind == "critical_admitted", admission, err
}

func nextSummary(now int64, zone, clock string) (int64, bool) {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return 0, false
	}
	var h, m int
	if _, err = fmt.Sscanf(clock, "%d:%d", &h, &m); err != nil || h > 23 || m > 59 {
		return 0, false
	}
	t := time.UnixMilli(now).In(loc)
	candidate := time.Date(t.Year(), t.Month(), t.Day(), h, m, 0, 0, loc)
	if !candidate.After(t) {
		candidate = time.Date(t.Year(), t.Month(), t.Day()+1, h, m, 0, 0, loc)
	}
	return candidate.UnixMilli(), true
}

func addDailyBatchMemberTx(ctx context.Context, tx *sql.Tx, id string, version int64, nonce string, now int64, channel, snapshot, zone, summary string) error {
	if channel == "" || snapshot == "" {
		return fmt.Errorf("%w: batched interrupt lacks channel snapshot", ErrInterruptRejected)
	}
	at, ok := nextSummary(now-1, zone, summary)
	if !ok {
		return fmt.Errorf("%w: invalid frozen summary", ErrInterruptRejected)
	}
	var admission string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM attention_admissions WHERE admission_key=?`, id+":initial").Scan(&admission); err != nil {
		return err
	}
	return addBatchMemberTx(ctx, tx, "daily:"+channel+":"+fmt.Sprint(at), "daily_summary", id, version, nonce, admission, channel, snapshot, at, now)
}
func addCriticalBatchMemberTx(ctx context.Context, tx *sql.Tx, id string, version int64, nonce, admission, channel, snapshot string, now int64) error {
	if channel == "" || snapshot == "" {
		return fmt.Errorf("%w: fused interrupt lacks channel snapshot", ErrInterruptRejected)
	}
	return addBatchMemberTx(ctx, tx, "critical:"+admission+":"+channel, "critical_fuse", id, version, nonce, admission, channel, snapshot, now, now)
}
func addBatchMemberTx(ctx context.Context, tx *sql.Tx, batch, kind, id string, version int64, nonce, admission, channel, snapshot string, due, now int64) error {
	var project, forgeKind, host, forgeProject, targetKind, targetID, headline, reason, severity, links, opts string
	if err := tx.QueryRowContext(ctx, `SELECT r.project_id,r.forge_kind,r.forge_host,r.forge_project_key,CASE WHEN r.issue_id IS NOT NULL THEN 'issue' ELSE r.discussion_target_kind END,COALESCE(r.issue_id,r.discussion_target_id),i.headline,i.reason,i.severity,i.links_json,i.options_json FROM interrupts i JOIN runs r ON r.id=i.run_id WHERE i.id=?`, id).Scan(&project, &forgeKind, &host, &forgeProject, &targetKind, &targetID, &headline, &reason, &severity, &links, &opts); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO attention_batches(id,state,project_id,channel_id,channel_snapshot_json,forge_kind,forge_host,forge_project_key,target_kind,target_id,operation_key,updated_at_ms) VALUES(?,'collecting',?,?,?,?,?,?,?,?,NULL,?)`, batch, project, channel, snapshot, forgeKind, host, forgeProject, targetKind, targetID, now); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO attention_batch_members(batch_id,interrupt_id,admission_id,member_key,channel_id,channel_snapshot_json,delivery_id,interrupt_version,nonce,headline,reason,severity,links_json,options_json,joined_at_ms) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, batch, id, admission, batch+":"+id, channel, snapshot, batch+":"+id, version, nonce, headline, reason, severity, links, opts, now)
	return err
}

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
	return nil
}
