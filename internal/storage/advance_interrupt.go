package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
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
	var status, state, held, nonce, reason, base, onExpire, onMax, channel, snapshot, zone, summary, delivery string
	var version, expiresAt, expiresAfter, window int64
	var nextDispatch sql.NullInt64
	var escalation, max, total, perRun int
	var downgraded bool
	err = tx.QueryRowContext(ctx, `SELECT status,dispatch_state,COALESCE(held_reason,''),nonce,reason,base_severity,on_expire,on_max_escalations,COALESCE(channel_id,''),COALESCE(channel_snapshot_json,''),delivery,day_timezone,daily_summary_at,version,expires_at_ms,expires_after_ms,escalation_count,max_escalations,suggested_downgrade,critical_window_ms,critical_total_limit,critical_per_run_limit,next_dispatch_at_ms FROM interrupts WHERE id=?`, cmd.InterruptID).Scan(&status, &state, &held, &nonce, &reason, &base, &onExpire, &onMax, &channel, &snapshot, &delivery, &zone, &summary, &version, &expiresAt, &expiresAfter, &escalation, &max, &downgraded, &window, &total, &perRun, &nextDispatch)
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
		res, err := tx.ExecContext(ctx, `UPDATE interrupts SET dispatch_state='batched',next_dispatch_at_ms=NULL,version=version+1,updated_at_ms=? WHERE id=? AND status='open' AND dispatch_state='ready' AND version=? AND nonce=?`, cmd.NowMS, cmd.InterruptID, cmd.ExpectedVersion, cmd.ExpectedNonce)
		if err != nil {
			return false, err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return false, ErrRejectedStale
		}
		if delivery == "immediate" {
			priority := "normal"
			if escalation > 0 {
				priority = "strong"
			}
			if err := enqueueInterruptChannelTx(ctx, tx, cmd.InterruptID, cmd.ExpectedVersion+1, nonce, escalation, priority, cmd.NowMS); err != nil {
				return false, err
			}
		} else if !nextDispatch.Valid {
			return false, fmt.Errorf("%w: batched interrupt lacks frozen summary due", ErrInterruptRejected)
		} else if err := addDailyBatchMemberAtTx(ctx, tx, cmd.InterruptID, cmd.ExpectedVersion+1, nonce, nextDispatch.Int64, cmd.NowMS, channel, snapshot); err != nil {
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
	// Escalation reuses the frozen downgrade decision through the same
	// Severity(...) entry; it is applied once, never re-derived from T6.
	next = Severity(next, downgraded)
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
	var fusedAdmission string
	if next == SeverityCritical {
		admitted, admissionID, scope, err := admitCriticalTx(ctx, tx, cmd.InterruptID, next, cmd.NowMS, "escalation", window, total, perRun)
		if err != nil {
			return false, err
		}
		if !admitted {
			fusedAdmission = admissionID + ":" + scope
			nextState, delivery, heldReason, due = "batched", "batch", "", nil
		}
	}
	res, err := tx.ExecContext(ctx, `UPDATE interrupts SET severity=?,nonce=?,nonce_issued_at_ms=?,version=version+1,escalation_count=escalation_count+1,expires_at_ms=?,dispatch_state=?,delivery=?,held_reason=?,next_dispatch_at_ms=?,updated_at_ms=? WHERE id=? AND status='open' AND version=? AND nonce=?`, next, newNonce, cmd.NowMS, cmd.NowMS+expiresAfter, nextState, delivery, nullable(heldReason), due, cmd.NowMS, cmd.InterruptID, cmd.ExpectedVersion, cmd.ExpectedNonce)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return false, ErrRejectedStale
	}
	if next == SeverityCritical && fusedAdmission != "" {
		if err := addCriticalBatchMemberTx(ctx, tx, cmd.InterruptID, cmd.ExpectedVersion+1, newNonce, fusedAdmission, channel, snapshot, cmd.NowMS); err != nil {
			return false, err
		}
	}
	if err := excludeStaleBatchMembersTx(ctx, tx, cmd.InterruptID, cmd.NowMS); err != nil {
		return false, err
	}
	return finishAdvance(ctx, tx, res, cmd, "interrupt.escalated")
}

func (d *DB) holdAdvance(ctx context.Context, tx *sql.Tx, cmd AdvanceInterruptCmd, reason, event string) (bool, error) {
	res, err := tx.ExecContext(ctx, `UPDATE interrupts SET dispatch_state='held',delivery='held',held_reason=?,next_dispatch_at_ms=NULL,version=version+1,updated_at_ms=? WHERE id=? AND status='open' AND version=? AND nonce=?`, reason, cmd.NowMS, cmd.InterruptID, cmd.ExpectedVersion, cmd.ExpectedNonce)
	if err != nil {
		return false, err
	}
	if err := excludeStaleBatchMembersTx(ctx, tx, cmd.InterruptID, cmd.NowMS); err != nil {
		return false, err
	}
	return finishAdvance(ctx, tx, res, cmd, event)
}

func (d *DB) closeExpiredInterrupt(ctx context.Context, tx *sql.Tx, cmd AdvanceInterruptCmd) (bool, error) {
	var runID, status, reason, binding, bindingDigest string
	var runVersion, bindingSchema int64
	if err := tx.QueryRowContext(ctx, `SELECT r.id,r.status,r.version,i.reason,b.binding_schema_version,b.binding_json,b.binding_digest FROM interrupts i JOIN runs r ON r.id=i.run_id JOIN interrupt_command_effect_bindings b ON b.interrupt_id=i.id WHERE i.id=?`, cmd.InterruptID).Scan(&runID, &status, &runVersion, &reason, &bindingSchema, &binding, &bindingDigest); err != nil {
		return false, err
	}
	armName, err := validateEffectBinding(ctx, tx, cmd.InterruptID, reason, runID, bindingSchema, binding, bindingDigest)
	if err != nil {
		return false, err
	}
	noTransition := armName == "report_quota_failure_review"
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
	if err := excludeStaleBatchMembersTx(ctx, tx, cmd.InterruptID, cmd.NowMS); err != nil {
		return false, err
	}
	return finishAdvance(ctx, tx, res, cmd, "interrupt.expired_auto_reject")
}

func excludeStaleBatchMembersTx(ctx context.Context, tx *sql.Tx, interruptID string, nowMS int64) error {
	_, err := tx.ExecContext(ctx, `UPDATE attention_batch_members AS m
		SET excluded_at_ms=?
		WHERE m.interrupt_id=? AND m.excluded_at_ms IS NULL
			AND EXISTS (SELECT 1 FROM attention_batches b WHERE b.id=m.batch_id AND b.state='collecting')
			AND NOT EXISTS (
				SELECT 1 FROM attention_batch_member_authority a
				JOIN interrupts i ON i.id=m.interrupt_id
				WHERE a.batch_id=m.batch_id AND a.interrupt_id=m.interrupt_id
					AND i.status='open' AND i.version=a.interrupt_version AND i.nonce=a.nonce
			)`, nowMS, interruptID)
	return err
}

// MergeConflictDigest identifies the exact durable conflicting Change observation.
func MergeConflictDigest(changeID, headSHA string) string {
	body, _ := json.Marshal(struct {
		ChangeID     string `json:"change_id"`
		HeadSHA      string `json:"head_sha"`
		Mergeability string `json:"mergeability"`
	}{changeID, headSHA, "conflicting"})
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func validateEffectBinding(ctx context.Context, tx *sql.Tx, interruptID, reason, runID string, schema int64, raw, digest string) (string, error) {
	var binding map[string]json.RawMessage
	if schema != 1 || json.Unmarshal([]byte(raw), &binding) != nil {
		return "", fmt.Errorf("%w: corrupt effect binding", ErrInterruptRejected)
	}
	canonical, err := canonicalJSON(binding)
	if err != nil || string(canonical) != raw {
		return "", fmt.Errorf("%w: non-canonical effect binding", ErrInterruptRejected)
	}
	sum := sha256.Sum256(canonical)
	if digest != hex.EncodeToString(sum[:]) {
		return "", fmt.Errorf("%w: effect binding digest mismatch", ErrInterruptRejected)
	}
	var arm string
	if json.Unmarshal(binding["arm"], &arm) != nil {
		return "", fmt.Errorf("%w: corrupt effect binding", ErrInterruptRejected)
	}
	if rawRun, ok := binding["run_id"]; ok {
		var boundRun string
		if json.Unmarshal(rawRun, &boundRun) != nil || boundRun != runID {
			return "", fmt.Errorf("%w: corrupt effect binding", ErrInterruptRejected)
		}
	} else if arm != "code_review" && arm != "merge_conflict" {
		return "", fmt.Errorf("%w: corrupt effect binding", ErrInterruptRejected)
	}
	fields := map[string][]string{
		"design_approval":             {"arm", "run_id", "task_spec_snapshot_id"},
		"guardrail_violation":         {"arm", "run_id", "head_sha", "rule_id", "matched_paths_digest"},
		"code_review":                 {"arm", "change_id", "head_sha", "review_policy_snapshot_digest"},
		"agent_blocked":               {"arm", "run_id", "attempt_no", "generation", "report_id"},
		"merge_conflict":              {"arm", "change_id", "head_sha", "conflict_digest"},
		"startup_stall":               {"arm", "run_id", "attempt_no", "generation"},
		"failure_review_attempt":      {"arm", "run_id", "attempt_no", "generation", "retry_kind", "change_id", "head_sha", "terminal_attempt_no", "terminal_generation"},
		"report_quota_failure_review": {"arm", "run_id", "daily_bucket_start_ms", "daily_bucket_end_ms", "security_event_id"},
	}
	expected, ok := fields[arm]
	if !ok || len(binding) != len(expected) {
		return "", fmt.Errorf("%w: invalid effect binding arm", ErrInterruptRejected)
	}
	for _, field := range expected {
		if _, ok := binding[field]; !ok {
			return "", fmt.Errorf("%w: incomplete effect binding", ErrInterruptRejected)
		}
	}
	if (arm == "failure_review_attempt" || arm == "report_quota_failure_review") != (reason == string(InterruptFailureReview)) || (arm != "failure_review_attempt" && arm != "report_quota_failure_review" && arm != reason) {
		return "", fmt.Errorf("%w: binding reason mismatch", ErrInterruptRejected)
	}
	text := func(name string) bool { var v string; return json.Unmarshal(binding[name], &v) == nil && v != "" }
	integer := func(name string) bool {
		var v int64
		return json.Unmarshal(binding[name], &v) == nil && v > 0
	}
	if arm != "code_review" && arm != "merge_conflict" && !text("run_id") {
		return "", fmt.Errorf("%w: invalid effect binding field", ErrInterruptRejected)
	}
	switch arm {
	case "design_approval":
		if !text("task_spec_snapshot_id") {
			return "", fmt.Errorf("%w: incomplete effect binding", ErrInterruptRejected)
		}
	case "guardrail_violation":
		if !text("head_sha") || !text("rule_id") || !text("matched_paths_digest") {
			return "", fmt.Errorf("%w: incomplete effect binding", ErrInterruptRejected)
		}
	case "code_review":
		if !text("change_id") || !text("head_sha") || !text("review_policy_snapshot_digest") {
			return "", fmt.Errorf("%w: incomplete effect binding", ErrInterruptRejected)
		}
	case "agent_blocked":
		if !integer("attempt_no") || !integer("generation") || !text("report_id") {
			return "", fmt.Errorf("%w: incomplete effect binding", ErrInterruptRejected)
		}
	case "merge_conflict":
		if !text("change_id") || !text("head_sha") || !text("conflict_digest") {
			return "", fmt.Errorf("%w: incomplete effect binding", ErrInterruptRejected)
		}
	case "startup_stall":
		if !integer("attempt_no") || !integer("generation") {
			return "", fmt.Errorf("%w: incomplete effect binding", ErrInterruptRejected)
		}
	case "report_quota_failure_review":
		if !integer("daily_bucket_start_ms") || !integer("daily_bucket_end_ms") || !text("security_event_id") {
			return "", fmt.Errorf("%w: incomplete effect binding", ErrInterruptRejected)
		}
	case "failure_review_attempt":
		var retry string
		if !integer("attempt_no") || !integer("generation") || json.Unmarshal(binding["retry_kind"], &retry) != nil {
			return "", fmt.Errorf("%w: incomplete effect binding", ErrInterruptRejected)
		}
		var change, head, terminalAttempt, terminalGeneration any
		if json.Unmarshal(binding["change_id"], &change) != nil || json.Unmarshal(binding["head_sha"], &head) != nil || json.Unmarshal(binding["terminal_attempt_no"], &terminalAttempt) != nil || json.Unmarshal(binding["terminal_generation"], &terminalGeneration) != nil {
			return "", fmt.Errorf("%w: incomplete effect binding", ErrInterruptRejected)
		}
		if retry == "gate_recheck" {
			if _, ok := change.(string); !ok {
				return "", fmt.Errorf("%w: invalid effect binding", ErrInterruptRejected)
			}
			if _, ok := head.(string); !ok || terminalAttempt != nil || terminalGeneration != nil {
				return "", fmt.Errorf("%w: invalid effect binding", ErrInterruptRejected)
			}
		} else if retry == "new_attempt" {
			var attempt, generation, terminalAttemptValue, terminalGenerationValue int64
			if change != nil || head != nil || json.Unmarshal(binding["attempt_no"], &attempt) != nil || json.Unmarshal(binding["generation"], &generation) != nil || json.Unmarshal(binding["terminal_attempt_no"], &terminalAttemptValue) != nil || json.Unmarshal(binding["terminal_generation"], &terminalGenerationValue) != nil || attempt != terminalAttemptValue || generation != terminalGenerationValue {
				return "", fmt.Errorf("%w: invalid effect binding", ErrInterruptRejected)
			}
		} else {
			return "", fmt.Errorf("%w: invalid effect binding", ErrInterruptRejected)
		}
	}
	if err := validateEffectBindingReferences(ctx, tx, arm, interruptID, runID, binding); err != nil {
		return "", err
	}
	return arm, nil
}

func validateEffectBindingReferences(ctx context.Context, tx *sql.Tx, arm, interruptID, runID string, binding map[string]json.RawMessage) error {
	textValue := func(name string) string {
		var value string
		_ = json.Unmarshal(binding[name], &value)
		return value
	}
	integerValue := func(name string) int64 {
		var value int64
		_ = json.Unmarshal(binding[name], &value)
		return value
	}
	var exists int
	var err error
	switch arm {
	case "design_approval":
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM task_spec_snapshots WHERE id=? AND run_id=?)`, textValue("task_spec_snapshot_id"), runID).Scan(&exists)
	case "code_review":
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(
			SELECT 1 FROM interrupts i JOIN runs r ON r.id=i.run_id
			JOIN calibration_entries c ON c.id=i.calibration_id
			JOIN gate_evaluations e ON e.id=c.gate_evaluation_id
			JOIN gate_input_snapshots s ON s.id=e.snapshot_id
			WHERE i.id=? AND r.id=? AND r.change_id=? AND r.change_head_sha=?
				AND s.head_sha=? AND s.effective_policy_hash=?)`, interruptID, runID, textValue("change_id"), textValue("head_sha"), textValue("head_sha"), textValue("review_policy_snapshot_digest")).Scan(&exists)
	case "merge_conflict":
		changeID, headSHA := textValue("change_id"), textValue("head_sha")
		if textValue("conflict_digest") != MergeConflictDigest(changeID, headSHA) {
			return fmt.Errorf("%w: invalid merge conflict digest", ErrInterruptRejected)
		}
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(
			SELECT 1 FROM interrupts i JOIN runs r ON r.id=i.run_id
			JOIN calibration_entries c ON c.id=i.calibration_id
			JOIN gate_evaluations e ON e.id=c.gate_evaluation_id
			JOIN gate_input_snapshots s ON s.id=e.snapshot_id
			WHERE i.id=? AND r.id=? AND r.change_id=? AND r.change_head_sha=?
				AND s.head_sha=?
                AND json_extract(s.canonical_json,'$.change.mergeability')='conflicting'
                AND json_extract(e.verdict_json,'$.mergeability')='conflicting')`, interruptID, runID, changeID, headSHA, headSHA).Scan(&exists)
	case "guardrail_violation":
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(
			SELECT 1 FROM interrupts i
			JOIN calibration_entries c ON c.id=i.calibration_id
			JOIN gate_evaluations e ON e.id=c.gate_evaluation_id
			JOIN gate_input_snapshots s ON s.id=e.snapshot_id
			WHERE i.id=? AND e.run_id=? AND s.head_sha=?
				AND json_extract(e.verdict_json,'$.rule_id')=?
				AND json_extract(e.verdict_json,'$.matched_paths_digest')=?)`, interruptID, runID,
			textValue("head_sha"), textValue("rule_id"), textValue("matched_paths_digest")).Scan(&exists)
	case "agent_blocked", "startup_stall", "failure_review_attempt":
		query := `SELECT EXISTS(SELECT 1 FROM attempts a JOIN interrupts i ON i.id=? WHERE a.run_id=? AND a.run_id=i.run_id AND a.attempt_no=? AND a.generation=?)`
		args := []any{interruptID, runID, integerValue("attempt_no"), integerValue("generation")}
		if arm == "failure_review_attempt" {
			var retry string
			_ = json.Unmarshal(binding["retry_kind"], &retry)
			if retry == "new_attempt" {
				query = `SELECT EXISTS(SELECT 1 FROM attempts a JOIN interrupts i ON i.id=? WHERE a.run_id=? AND a.run_id=i.run_id AND a.attempt_no=? AND a.generation=? AND phase='finished' AND ((result_exit_code IS NOT NULL AND result_exit_code<>0) OR result_signal IS NOT NULL))`
			} else if retry == "gate_recheck" {
				query = `SELECT EXISTS(SELECT 1 FROM runs r JOIN interrupts i ON i.id=? WHERE r.id=? AND r.id=i.run_id AND r.change_id=? AND r.change_head_sha=?)`
				args = []any{interruptID, runID, textValue("change_id"), textValue("head_sha")}
			}
		}
		err = tx.QueryRowContext(ctx, query, args...).Scan(&exists)
		if err == nil && exists == 1 && arm == "agent_blocked" {
			var report string
			_ = json.Unmarshal(binding["report_id"], &report)
			if report != "" {
				err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM report_receipts WHERE id=? AND run_id=? AND attempt_no=? AND report_kind='blocker')`, report, runID, integerValue("attempt_no")).Scan(&exists)
			}
		}
	case "report_quota_failure_review":
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM report_quota_exhaustions q JOIN events e ON e.id=q.security_event_id WHERE q.run_id=? AND q.daily_bucket_start_ms=? AND q.daily_bucket_end_ms=? AND q.security_event_id=? AND e.source='system')`, runID, integerValue("daily_bucket_start_ms"), integerValue("daily_bucket_end_ms"), textValue("security_event_id")).Scan(&exists)
	default:
		return nil
	}
	if err != nil || exists != 1 {
		return fmt.Errorf("%w: invalid effect binding reference", ErrInterruptRejected)
	}
	return nil
}

func enqueueInterruptChannelTx(ctx context.Context, tx *sql.Tx, id string, version int64, nonce string, escalation int, priority string, now int64) error {
	var channel, snapshot, headline, brief, links, options, runID, forgeKind, forgeHost, forgeProject, targetKind, targetID string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(i.channel_id,''),COALESCE(i.channel_snapshot_json,''),i.headline,i.brief_markdown,i.links_json,i.options_json,i.run_id,r.forge_kind,r.forge_host,r.forge_project_key,CASE WHEN r.issue_id IS NOT NULL THEN 'issue' ELSE r.discussion_target_kind END,COALESCE(r.issue_id,r.discussion_target_id) FROM interrupts i JOIN runs r ON r.id=i.run_id WHERE i.id=?`, id).Scan(&channel, &snapshot, &headline, &brief, &links, &options, &runID, &forgeKind, &forgeHost, &forgeProject, &targetKind, &targetID); err != nil {
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
	rendered, commandLines, err := renderChannelInterrupt(headline, brief, links, options, runID, nonce)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{"delivery_kind": "interrupt", "delivery_id": deliveryID, "interrupt_id": id, "escalation_no": escalation, "priority": priority, "interrupt_version": version, "nonce": nonce, "channel": ch, "command_lines": commandLines, "rendered_text": rendered})
	if err := insertOperation(ctx, tx, Operation{Key: key, Kind: OperationChannelPublish, Payload: payload, InterruptID: id}, "", "", now); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO interrupt_deliveries(id,delivery_id,interrupt_id,surface,channel_id,channel_snapshot_json,interrupt_version,nonce,escalation_no,priority,operation_key,state,attempt_count,forge_kind,forge_host,forge_project_key,forge_alert_target_kind,forge_alert_target_id,created_at_ms) VALUES(?,?,?,'channel',?,?,?,?,?,?,?,'pending',0,?,?,?,?,?,?)`, newID(), deliveryID, id, channel, snapshot, version, nonce, escalation, priority, key, forgeKind, forgeHost, forgeProject, targetKind, targetID, now)
	return err
}

func admitCriticalTx(ctx context.Context, tx *sql.Tx, id string, finalSeverity InterruptSeverity, now int64, source string, window int64, total, perRun int) (bool, string, string, error) {
	var run, severity, charge, zone string
	if err := tx.QueryRowContext(ctx, `SELECT run_id,severity,COALESCE(charged_budget_entry_id,''),COALESCE(day_timezone,'UTC') FROM interrupts WHERE id=?`, id).Scan(&run, &severity, &charge, &zone); err != nil {
		return false, "", "", err
	}
	key := id + ":critical"
	var kind, existing string
	if err := tx.QueryRowContext(ctx, `SELECT id,kind FROM attention_admissions WHERE admission_key=?`, key).Scan(&existing, &kind); err == nil {
		if kind == "critical_admitted" {
			return true, existing, "", nil
		}
		var scope string
		if err := tx.QueryRowContext(ctx, `SELECT b.scope FROM attention_batch_members m JOIN attention_batches b ON b.id=m.batch_id WHERE m.admission_id=? AND b.kind='critical_fuse' ORDER BY b.created_at_ms LIMIT 1`, existing).Scan(&scope); err != nil {
			return false, existing, "", err
		}
		return false, existing, scope, nil
	} else if err != sql.ErrNoRows {
		return false, "", "", err
	}
	var global, local int
	// The window is (now-window, now], so evidence at the left boundary has
	// expired while same-millisecond committed evidence is counted.
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM attention_admissions WHERE kind='critical_admitted' AND created_at_ms>? AND created_at_ms<=?`, now-window, now).Scan(&global); err != nil {
		return false, "", "", err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM attention_admissions WHERE kind='critical_admitted' AND run_id=? AND created_at_ms>? AND created_at_ms<=?`, run, now-window, now).Scan(&local); err != nil {
		return false, "", "", err
	}
	kind, scope := "critical_admitted", ""
	if global >= total {
		kind, scope = "critical_fused", "global"
	} else if local >= perRun {
		kind, scope = "critical_fused", "run"
	}
	admission := newID()
	_, err := tx.ExecContext(ctx, `INSERT INTO attention_admissions(id,interrupt_id,admission_key,kind,metric_identity,attention_charge_entry_id,severity,day_timezone,run_id,critical_source,created_at_ms) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, admission, id, key, kind, id, nullable(charge), finalSeverity, zone, run, source, now)
	return kind == "critical_admitted", admission, scope, err
}

// NextDailySummaryAt returns the next frozen local summary occurrence.
func NextDailySummaryAt(now int64, zone, clock string) (int64, bool) {
	return nextSummary(now, zone, clock)
}

func nextSummary(now int64, zone, clock string) (int64, bool) {
	loc := time.Local
	var err error
	if zone != "local" {
		loc, err = time.LoadLocation(zone)
	}
	if err != nil {
		return 0, false
	}
	var h, m int
	if _, err = fmt.Sscanf(clock, "%d:%d", &h, &m); err != nil || h > 23 || m > 59 {
		return 0, false
	}
	t := time.UnixMilli(now).In(loc)
	sameDay := time.Date(t.Year(), t.Month(), t.Day(), h, m, 0, 0, loc)
	// time.Date normalizes a nonexistent wall time to one side of a DST gap.
	// Locate the offset transition and return its first valid instant.
	if sameDay.In(loc).Year() == t.Year() && sameDay.In(loc).YearDay() == t.YearDay() && (sameDay.In(loc).Hour() != h || sameDay.In(loc).Minute() != m) {
		for probe := sameDay.Add(4 * time.Hour); probe.After(sameDay); probe = probe.Add(-time.Minute) {
			before := probe.Add(-time.Minute)
			_, beforeOffset := before.In(loc).Zone()
			_, probeOffset := probe.In(loc).Zone()
			if beforeOffset != probeOffset {
				if probe.After(t) {
					return probe.UnixMilli(), true
				}
				break
			}
		}
	}
	candidate := sameDay
	if !candidate.After(t) {
		candidate = time.Date(t.Year(), t.Month(), t.Day()+1, h, m, 0, 0, loc)
	}
	return candidate.UnixMilli(), true
}

func addDailyBatchMemberTx(ctx context.Context, tx *sql.Tx, id string, version int64, nonce string, now int64, channel, snapshot, zone, summary string) error {
	at, ok := nextSummary(now, zone, summary)
	if !ok {
		return fmt.Errorf("%w: invalid frozen summary", ErrInterruptRejected)
	}
	return addDailyBatchMemberAtTx(ctx, tx, id, version, nonce, at, now, channel, snapshot)
}

// addDailyBatchMemberAtTx joins the already-frozen summary occurrence. The
// dispatch path must not recalculate it at the later tick: doing so would move
// a member into the following day's batch when the tick lands exactly at due.
func addDailyBatchMemberAtTx(ctx context.Context, tx *sql.Tx, id string, version int64, nonce string, at, now int64, channel, snapshot string) error {
	if channel == "" || snapshot == "" {
		return fmt.Errorf("%w: batched interrupt lacks channel snapshot", ErrInterruptRejected)
	}
	if at <= 0 {
		return fmt.Errorf("%w: invalid frozen summary", ErrInterruptRejected)
	}
	var admission string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM attention_admissions WHERE admission_key=?`, id+":initial").Scan(&admission); err != nil {
		return err
	}
	return addBatchMemberTx(ctx, tx, "", "daily_summary", id, version, nonce, admission, channel, snapshot, at, now)
}
func addCriticalBatchMemberTx(ctx context.Context, tx *sql.Tx, id string, version int64, nonce, admissionScope, channel, snapshot string, now int64) error {
	if channel == "" || snapshot == "" {
		return fmt.Errorf("%w: fused interrupt lacks channel snapshot", ErrInterruptRejected)
	}
	parts := strings.Split(admissionScope, ":")
	if len(parts) != 2 || (parts[1] != "global" && parts[1] != "run") {
		return fmt.Errorf("%w: invalid critical fuse scope", ErrInterruptRejected)
	}
	admission, scope := parts[0], parts[1]
	var run string
	var window int64
	if err := tx.QueryRowContext(ctx, `SELECT run_id,critical_window_ms FROM interrupts WHERE id=?`, id).Scan(&run, &window); err != nil {
		return err
	}
	scopeID := "global"
	if scope == "run" {
		scopeID = run
	}
	var batch string
	_ = tx.QueryRowContext(ctx, `SELECT b.id FROM attention_batches b JOIN interrupts i ON i.id=? JOIN runs r ON r.id=i.run_id WHERE b.kind='critical_fuse' AND b.scope=? AND b.scope_id=? AND b.channel_id=? AND b.forge_kind=r.forge_kind AND b.forge_host=r.forge_host AND b.forge_project_key=r.forge_project_key AND b.target_kind=CASE WHEN r.issue_id IS NOT NULL THEN 'issue' ELSE r.discussion_target_kind END AND b.target_id=COALESCE(r.issue_id,r.discussion_target_id) AND b.state='collecting' ORDER BY b.created_at_ms LIMIT 1`, id, scope, scopeID, channel).Scan(&batch)
	if batch == "" {
		batch = "critical:" + scope + ":" + scopeID + ":" + admission
	}
	var due int64
	if err := tx.QueryRowContext(ctx, `SELECT MIN(created_at_ms)+? FROM attention_admissions WHERE kind='critical_admitted' AND created_at_ms>? AND created_at_ms<=?`+map[bool]string{true: " AND run_id=?", false: ""}[scope == "run"], append([]any{window, now - window, now}, func() []any {
		if scope == "run" {
			return []any{run}
		}
		return nil
	}()...)...).Scan(&due); err != nil {
		return err
	}
	return addBatchMemberTx(ctx, tx, batch, "critical_fuse", id, version, nonce, admission, channel, snapshot, due, now)
}
func mustBatchZone(ctx context.Context, tx *sql.Tx, interruptID string) string {
	var zone string
	if err := tx.QueryRowContext(ctx, `SELECT day_timezone FROM interrupts WHERE id=?`, interruptID).Scan(&zone); err != nil || zone == "" {
		return "UTC"
	}
	return zone
}

func addBatchMemberTx(ctx context.Context, tx *sql.Tx, batch, kind, id string, version int64, nonce, admission, channel, snapshot string, due, now int64) error {
	var project, forgeKind, host, forgeProject, targetKind, targetID, headline, reason, severity, links, opts string
	var criticalWindow int64
	var criticalTotal, criticalPerRun int
	if err := tx.QueryRowContext(ctx, `SELECT r.project_id,r.forge_kind,r.forge_host,r.forge_project_key,CASE WHEN r.issue_id IS NOT NULL THEN 'issue' ELSE r.discussion_target_kind END,COALESCE(r.issue_id,r.discussion_target_id),i.headline,i.reason,i.severity,i.links_json,i.options_json,i.critical_window_ms,i.critical_total_limit,i.critical_per_run_limit FROM interrupts i JOIN runs r ON r.id=i.run_id WHERE i.id=?`, id).Scan(&project, &forgeKind, &host, &forgeProject, &targetKind, &targetID, &headline, &reason, &severity, &links, &opts, &criticalWindow, &criticalTotal, &criticalPerRun); err != nil {
		return err
	}
	enc := base64.RawURLEncoding.EncodeToString
	if kind == "daily_summary" {
		batch = fmt.Sprintf("daily:%s:%s:%d:%s:%s:%s:%s:%s:%s", project, mustBatchZone(ctx, tx, id), due, channel, forgeKind, enc([]byte(host)), enc([]byte(forgeProject)), targetKind, enc([]byte(targetID)))
	} else {
		if batch == "" {
			batch = "critical:global:global:" + admission
		}
		// Existing collecting batches already contain their complete immutable
		// target identity; append the target suffix only for a new episode.
		if strings.Count(batch, ":") < 8 {
			batch += fmt.Sprintf(":%s:%s:%s:%s:%s", channel, forgeKind, enc([]byte(host)), enc([]byte(forgeProject)), targetKind+":"+enc([]byte(targetID)))
		}
	}
	deliveryID := batch + ":publish:1"
	scope, scopeID := "global", "global"
	if kind == "daily_summary" {
		scope, scopeID = "day", mustBatchZone(ctx, tx, id)+":"+fmt.Sprint(due)
	} else {
		parts := strings.Split(batch, ":")
		if len(parts) >= 4 {
			scope, scopeID = parts[1], parts[2]
		}
	}
	episode := ""
	if kind == "critical_fuse" {
		parts := strings.Split(batch, ":")
		if len(parts) < 4 {
			return fmt.Errorf("%w: invalid critical batch identity", ErrInterruptRejected)
		}
		episode = parts[3]
	}
	batchWindowValue, batchTotalValue, batchPerRunValue := any(criticalWindow), any(criticalTotal), any(criticalPerRun)
	if kind == "daily_summary" {
		// Daily batches do not own critical-fuse limits. Keep these columns NULL
		// for compatibility with databases created before the columns existed.
		batchWindowValue, batchTotalValue, batchPerRunValue = nil, nil, nil
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO attention_batches(id,state,project_id,channel_id,channel_snapshot_json,forge_kind,forge_host,forge_project_key,target_kind,target_id,kind,delivery_id,scope,scope_id,episode_admission_id,due_at_ms,critical_window_ms,critical_total_limit,critical_per_run_limit,created_at_ms,updated_at_ms) VALUES(?,'collecting',?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, batch, project, channel, snapshot, forgeKind, host, forgeProject, targetKind, targetID, kind, deliveryID, scope, scopeID, nullable(episode), due, batchWindowValue, batchTotalValue, batchPerRunValue, now, now); err != nil {
		return fmt.Errorf("create attention batch: %w", err)
	}
	var batchProject, batchChannel, batchSnapshot, batchForgeKind, batchHost, batchForgeProject, batchTargetKind, batchTargetID, batchKind, batchDelivery, batchScope, batchScopeID string
	var batchEpisode sql.NullString
	var batchDue int64
	var batchWindow, batchTotal, batchPerRun sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT project_id,channel_id,channel_snapshot_json,forge_kind,forge_host,forge_project_key,target_kind,target_id,kind,delivery_id,scope,scope_id,episode_admission_id,due_at_ms,critical_window_ms,critical_total_limit,critical_per_run_limit FROM attention_batches WHERE id=?`, batch).Scan(&batchProject, &batchChannel, &batchSnapshot, &batchForgeKind, &batchHost, &batchForgeProject, &batchTargetKind, &batchTargetID, &batchKind, &batchDelivery, &batchScope, &batchScopeID, &batchEpisode, &batchDue, &batchWindow, &batchTotal, &batchPerRun); err != nil {
		return err
	}
	if batchProject != project || batchChannel != channel || batchSnapshot != snapshot || batchForgeKind != forgeKind || batchHost != host || batchForgeProject != forgeProject || batchTargetKind != targetKind || batchTargetID != targetID || batchKind != kind || batchDelivery != deliveryID || batchScope != scope || batchScopeID != scopeID || batchEpisode.String != episode || batchEpisode.Valid != (episode != "") || batchDue != due {
		return fmt.Errorf("%w: attention batch identity collision", ErrInterruptRejected)
	}
	// Critical limits are part of critical-fuse authority only. Legacy daily
	// batches legitimately contain NULL in these columns.
	if kind == "critical_fuse" && (!batchWindow.Valid || !batchTotal.Valid || !batchPerRun.Valid || batchWindow.Int64 != criticalWindow || batchTotal.Int64 != int64(criticalTotal) || batchPerRun.Int64 != int64(criticalPerRun)) {
		return fmt.Errorf("%w: attention batch identity collision", ErrInterruptRejected)
	}
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO attention_batch_members(batch_id,interrupt_id,admission_id,member_key,channel_id,channel_snapshot_json,delivery_id,interrupt_version,nonce,headline,reason,severity,links_json,options_json,joined_at_ms) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, batch, id, admission, batch+":"+id, channel, snapshot, batch+":"+id, version, nonce, headline, reason, severity, links, opts, now)
	if err != nil {
		return fmt.Errorf("add attention batch member: %w", err)
	}
	var gotAdmission, gotKey, gotChannel, gotSnapshot, gotDelivery, gotNonce, gotHeadline, gotReason, gotSeverity, gotLinks, gotOpts string
	var gotVersion int64
	var gotJoined int64
	if err := tx.QueryRowContext(ctx, `SELECT admission_id,member_key,channel_id,channel_snapshot_json,delivery_id,interrupt_version,nonce,headline,reason,severity,links_json,options_json,joined_at_ms FROM attention_batch_members WHERE batch_id=? AND interrupt_id=?`, batch, id).Scan(&gotAdmission, &gotKey, &gotChannel, &gotSnapshot, &gotDelivery, &gotVersion, &gotNonce, &gotHeadline, &gotReason, &gotSeverity, &gotLinks, &gotOpts, &gotJoined); err != nil {
		return err
	}
	if gotAdmission != admission || gotKey != batch+":"+id || gotChannel != channel || gotSnapshot != snapshot || gotDelivery != batch+":"+id || gotHeadline != headline || gotReason != reason || gotLinks != links || gotOpts != opts || gotJoined > now {
		return fmt.Errorf("%w: batch member identity collision", ErrInterruptRejected)
	}
	// The member row is immutable history.  Its separate authority projection is
	// refreshed on every replay so a repeated fuse carries the current nonce.
	_, err = tx.ExecContext(ctx, `INSERT INTO attention_batch_member_authority(batch_id,interrupt_id,interrupt_version,nonce,headline,reason,severity,links_json,options_json,updated_at_ms) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(batch_id,interrupt_id) DO UPDATE SET interrupt_version=excluded.interrupt_version,nonce=excluded.nonce,headline=excluded.headline,reason=excluded.reason,severity=excluded.severity,links_json=excluded.links_json,options_json=excluded.options_json,updated_at_ms=excluded.updated_at_ms WHERE EXISTS (SELECT 1 FROM attention_batches WHERE id=excluded.batch_id AND state='collecting')`, batch, id, version, nonce, headline, reason, severity, links, opts, now)
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
	return d.PrepareDueAttentionBatches(ctx, now)
}
