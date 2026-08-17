package storage

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// IdleRunActivityWindowMS is the rolling window during which a project with
// no active Run still warrants a single "daily_summary" status_note line.
// Commander mode (issue #1010) keeps a human subscribed to fleet idle via the
// digest pipeline; the alternative is a fleet that has gone quiet producing
// zero signal, which violates the discipline that "every stop is interrupt-
// ible" is one-sided — the human side loses both "stop" and "still alive"
// signals. 7 days is the v0 boundary: long enough to absorb quiet weekends,
// short enough that truly dormant projects decay back into silent
// cancellation rather than perpetuating a daily no-news row forever.
const IdleRunActivityWindowMS = int64(7 * 24 * 60 * 60 * 1000)

// idleRunNoteText is the deterministic single-line status_note emitted when a
// "daily_summary" batch seals with zero admitted interrupts and the fleet has
// been idle within IdleRunActivityWindowMS. The wording is normative: it is
// the commander-mode "no news is good news, but only when somebody hears it"
// signal. Critical_fuse / interrupt surfaces never use this string.
const idleRunNoteText = "昨日无待办事件；当前无活跃 Run —— 舰队空闲。若尚有未派发工作请开窗喂料"

// EnsureIdleDailySummaryBatches pre-creates empty "daily_summary" collecting
// batches for every project that has touched a Run inside
// IdleRunActivityWindowMS, so a zero-interrupt day still has a batch row that
// PrepareDueAttentionBatches can seal into the commander-mode idle status_note.
// Without this seam the daily_summary batch only materializes when the first
// interrupt joins it (addBatchMemberTx), so a day with zero interrupts is a
// silent miss and the human side loses the "still alive" signal.
//
// The seam is intentionally opt-in: with no IdleDailySummaryConfig installed
// (the default for every existing test) it is a no-op, preserving the legacy
// behavior. Production installs the config once during daemon assembly via
// SetIdleDailySummaryConfig.
//
// Dormant projects (no Run activity inside the window, no rows at all) are
// never pre-created, so a project that has truly gone quiet still decays into
// silent cancellation — the 7d window remains the sole dormancy gate. The
// per-project most recent Run's frozen forge target (forge_kind/host/forge_project_key,
// issue/discussion target) is reused; an empty collector batch for a project
// whose runs reference different targets will pick the single most recent
// row's target, matching the per-project digest surface that
// shouldPublishIdleNoteTx / EnqueueHandle resolve.
func (d *DB) EnsureIdleDailySummaryBatches(ctx context.Context, nowMS int64) error {
	cfg := d.idleDailySummaryConfig()
	if len(cfg.Channels) == 0 || cfg.DailySummaryAt == "" {
		return nil
	}
	due, ok := nextSummary(nowMS, cfg.DayTimezone, cfg.DailySummaryAt)
	if !ok || due <= 0 {
		return fmt.Errorf("storage: invalid idle daily summary clock %q/%q", cfg.DayTimezone, cfg.DailySummaryAt)
	}
	// Active projects (queued/running/waiting_human) still pre-create the
	// empty batch: by the time the due fires the project may have dropped to
	// idle, and shouldPublishIdleNoteTx inside prepareAttentionBatch is the
	// authoritative branch decision. We only skip dormant projects.
	rows, err := d.db.QueryContext(ctx, `SELECT p.id,
		COALESCE((SELECT r.forge_kind FROM runs r WHERE r.project_id=p.id ORDER BY r.updated_at_ms DESC, r.id LIMIT 1), ''),
		COALESCE((SELECT r.forge_host FROM runs r WHERE r.project_id=p.id ORDER BY r.updated_at_ms DESC, r.id LIMIT 1), ''),
		COALESCE((SELECT r.forge_project_key FROM runs r WHERE r.project_id=p.id ORDER BY r.updated_at_ms DESC, r.id LIMIT 1), ''),
		COALESCE((SELECT CASE WHEN r.issue_id IS NOT NULL THEN 'issue' ELSE r.discussion_target_kind END FROM runs r WHERE r.project_id=p.id ORDER BY r.updated_at_ms DESC, r.id LIMIT 1), ''),
		COALESCE((SELECT COALESCE(r.issue_id, r.discussion_target_id) FROM runs r WHERE r.project_id=p.id ORDER BY r.updated_at_ms DESC, r.id LIMIT 1), ''),
		MAX(r.updated_at_ms)
		FROM projects p LEFT JOIN runs r ON r.project_id=p.id
		WHERE p.enabled=1 AND p.health='active'
		GROUP BY p.id
		HAVING MAX(r.updated_at_ms) IS NOT NULL AND MAX(r.updated_at_ms) >= ?`, nowMS-IdleRunActivityWindowMS)
	if err != nil {
		return err
	}
	type pendingProject struct {
		project, forgeKind, host, forgeProject, targetKind, targetID string
	}
	var projects []pendingProject
	for rows.Next() {
		var p pendingProject
		var recent sql.NullInt64
		if err := rows.Scan(&p.project, &p.forgeKind, &p.host, &p.forgeProject, &p.targetKind, &p.targetID, &recent); err != nil {
			rows.Close()
			return err
		}
		if !recent.Valid || recent.Int64 < nowMS-IdleRunActivityWindowMS {
			continue
		}
		if p.forgeKind == "" || p.host == "" || p.forgeProject == "" || p.targetKind == "" || p.targetID == "" {
			// A row in runs without a complete forge target is a test
			// fixture, not a production fleet signal. Skip rather than
			// guess a target identity.
			continue
		}
		projects = append(projects, p)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, p := range projects {
		if err := d.ensureProjectIdleBatches(ctx, p, due, nowMS); err != nil {
			return err
		}
	}
	return nil
}

// ensureProjectIdleBatches pre-creates one empty "daily_summary" collecting
// batch per enabled channel for a single project, unless one already exists
// for (project, channel, due). The batch carries the same channel snapshot
// the dispatcher will later freeze into the batch_deliveries / outbox
// payloads so the heartbeat and a populated daily_summary on a future day
// share an immutable channel projection.
//
// Empty batches that survive PrepareDueAttentionBatches with zero admitted
// members hit the existing idle branch in prepareAttentionBatch; a project
// that grows an active Run between pre-creation and due is the canonical
// reason shouldPublishIdleNoteTx returns false — the seam does not need a
// second activity check here.
func (d *DB) ensureProjectIdleBatches(ctx context.Context, p struct {
	project, forgeKind, host, forgeProject, targetKind, targetID string
}, due, nowMS int64) error {
	cfg := d.idleDailySummaryConfig()
	for _, channel := range cfg.Channels {
		snapshot := channel.snapshot()
		enc := base64.RawURLEncoding.EncodeToString
		batchID := fmt.Sprintf("daily:%s:%s:%d:%s:%s:%s:%s:%s:%s", p.project, cfg.DayTimezone, due, channel.ID, p.forgeKind, enc([]byte(p.host)), enc([]byte(p.forgeProject)), p.targetKind, enc([]byte(p.targetID)))
		deliveryID := batchID + ":publish:1"
		scope := "day"
		scopeID := cfg.DayTimezone + ":" + fmt.Sprint(due)
		// Idempotent: if a batch with this identity already exists (the
		// populated-batch path added it earlier via addBatchMemberTx), the
		// INSERT OR IGNORE is a no-op and we move on. The unique index on
		// (project_id,kind,channel_id,scope,scope_id,forge_*) keeps identity
		// collisions safe across paths.
		if _, err := d.db.ExecContext(ctx, `INSERT OR IGNORE INTO attention_batches
			(id, state, project_id, channel_id, channel_snapshot_json, forge_kind, forge_host, forge_project_key,
			 target_kind, target_id, kind, delivery_id, scope, scope_id, due_at_ms, created_at_ms, updated_at_ms)
			VALUES (?, 'collecting', ?, ?, ?, ?, ?, ?, ?, ?, 'daily_summary', ?, ?, ?, ?, ?, ?)`,
			batchID, p.project, channel.ID, string(snapshot), p.forgeKind, p.host, p.forgeProject, p.targetKind, p.targetID, deliveryID, scope, scopeID, due, nowMS, nowMS); err != nil {
			return fmt.Errorf("storage: ensure idle daily batch: %w", err)
		}
	}
	return nil
}

func (d *DB) PrepareDueAttentionBatches(ctx context.Context, nowMS int64) error {
	rows, err := d.db.QueryContext(ctx, `SELECT id FROM attention_batches WHERE state='collecting' AND due_at_ms<=? ORDER BY due_at_ms,id`, nowMS)
	if err != nil {
		return err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if err := d.prepareAttentionBatch(ctx, id, nowMS); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) prepareAttentionBatch(ctx context.Context, batchID string, nowMS int64) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var kind, project, channelJSON, forgeKind, host, forgeProject, targetKind, targetID, deliveryID, scope, scopeID string
	var due int64
	if err := tx.QueryRowContext(ctx, `SELECT kind,project_id,channel_snapshot_json,forge_kind,forge_host,forge_project_key,target_kind,target_id,delivery_id,scope,scope_id,due_at_ms FROM attention_batches WHERE id=? AND state='collecting'`, batchID).Scan(&kind, &project, &channelJSON, &forgeKind, &host, &forgeProject, &targetKind, &targetID, &deliveryID, &scope, &scopeID, &due); err == sql.ErrNoRows {
		return nil
	} else if err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT m.delivery_id,m.interrupt_id,a.interrupt_version,a.nonce,a.headline,i.brief_markdown,a.reason,a.severity,a.links_json,a.options_json,i.run_id FROM attention_batch_members m JOIN attention_batch_member_authority a ON a.batch_id=m.batch_id AND a.interrupt_id=m.interrupt_id JOIN interrupts i ON i.id=m.interrupt_id WHERE m.batch_id=? AND m.excluded_at_ms IS NULL AND i.status='open' AND i.version=a.interrupt_version AND i.nonce=a.nonce ORDER BY m.interrupt_id`, batchID)
	if err != nil {
		return err
	}
	defer rows.Close()
	members := []map[string]any{}
	texts := []string{}
	for rows.Next() {
		var delivery, id, nonce, headline, brief, reason, severity, links, options, runID string
		var version int
		if err := rows.Scan(&delivery, &id, &version, &nonce, &headline, &brief, &reason, &severity, &links, &options, &runID); err != nil {
			return err
		}
		var l, o any
		if json.Unmarshal([]byte(links), &l) != nil || json.Unmarshal([]byte(options), &o) != nil {
			return fmt.Errorf("storage: corrupt batch member")
		}
		rendered, commandLines, err := renderChannelInterrupt(headline, brief, links, options, runID, nonce)
		if err != nil {
			return err
		}
		members = append(members, map[string]any{"delivery_id": delivery, "interrupt_id": id, "interrupt_version": version, "nonce": nonce, "headline": headline, "reason": reason, "severity": severity, "links": l, "options": o, "command_lines": commandLines})
		texts = append(texts, id+": "+rendered)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(members) == 0 {
		// Commander-mode idle heartbeat (#1010): a "daily_summary" batch that
		// collected zero interrupts is not always a silent cancel — when the
		// project has no active Run AND its most recent Run activity is still
		// inside IdleRunActivityWindowMS, the human side has not yet received
		// any "still alive" signal today. Publish a single deterministic
		// status_note line so the commander session sees "fleet idle". Any
		// active Run OR activity older than the window keeps the original
		// silent cancellation — a truly dormant project should not emit a
		// daily no-news row forever. critical_fuse is intentionally untouched:
		// its empty-membership path is the admission-driven fuse episode, not
		// a digest signal, and it still chains openCriticalSuccessorTx below.
		if kind == "daily_summary" {
			publishIdle, err := d.shouldPublishIdleNoteTx(ctx, tx, project, nowMS)
			if err != nil {
				return err
			}
			if publishIdle {
				return d.sealIdleStatusNoteTx(ctx, tx, batchID, project, channelJSON, forgeKind, host, forgeProject, targetKind, targetID, deliveryID, scope, scopeID, due, nowMS)
			}
		}
		_, err = tx.ExecContext(ctx, `UPDATE attention_batches SET state='cancelled',updated_at_ms=? WHERE id=? AND state='collecting'`, nowMS, batchID)
		if err != nil {
			return err
		}
		if kind == "critical_fuse" {
			if err := openCriticalSuccessorTx(ctx, tx, batchID, nowMS); err != nil {
				return err
			}
		}
		return tx.Commit()
	}
	var channel any
	if json.Unmarshal([]byte(channelJSON), &channel) != nil {
		return fmt.Errorf("storage: corrupt batch channel")
	}
	payloadKind := kind
	if kind == "critical_fuse" {
		payloadKind = "critical_fused"
	}
	payload, err := json.Marshal(map[string]any{"delivery_kind": "attention_batch", "batch_id": batchID, "delivery_id": deliveryID, "batch_kind": payloadKind, "channel": channel, "project_id": project, "forge_alert_target": map[string]any{"forge_kind": forgeKind, "forge_host": host, "forge_project_key": forgeProject, "target_kind": targetKind, "target_id": targetID}, "scope": scope, "scope_id": scopeID, "due_at_ms": due, "members": members, "rendered_text": joinBatchText(texts)})
	if err != nil {
		return err
	}
	key := "attention-batch:" + batchID + ":publish:1"
	if err := insertOperation(ctx, tx, Operation{Key: key, Kind: OperationChannelPublish, Payload: payload}, "", "", nowMS); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO batch_deliveries(batch_id,delivery_id,operation_key,state,created_at_ms) VALUES(?,?,?,'pending',?)`, batchID, deliveryID, key, nowMS); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE attention_batches SET state='sealed',operation_key=?,payload_json=?,payload_digest=?,sealed_at_ms=?,updated_at_ms=? WHERE id=? AND state='collecting'`, key, string(payload), digestJSON(payload), nowMS, nowMS, batchID); err != nil {
		return err
	}
	if kind == "critical_fuse" {
		if err := openCriticalSuccessorTx(ctx, tx, batchID, nowMS); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err == nil {
		d.wakeOutbox()
	}
	return err
}

// shouldPublishIdleNoteTx decides whether a "daily_summary" batch that
// collected zero admitted interrupts should still emit a status_note row.
// It returns true only when (a) the project has no Run in any of the
// commander-mode "active" states AND (b) at least one Run in the project
// recorded activity (runs.updated_at_ms) inside IdleRunActivityWindowMS.
// The window exists so that dormant projects decay back to silent
// cancellation; the activity check exists so the commander session keeps
// hearing from a project that is alive but currently quiet. Both queries
// run inside the caller-supplied transaction so the decision sees the
// same state the sealer commits.
func (d *DB) shouldPublishIdleNoteTx(ctx context.Context, tx *sql.Tx, projectID string, nowMS int64) (bool, error) {
	if projectID == "" {
		return false, nil
	}
	var active int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM runs WHERE project_id=? AND status IN ('queued','running','waiting_human') LIMIT 1`, projectID).Scan(&active); err != nil && err != sql.ErrNoRows {
		return false, err
	} else if err == nil {
		// An active Run means the commander mode already has a fresher signal
		// than this digest would carry; stay silent to avoid duplicate noise.
		return false, nil
	}
	var recent sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT MAX(updated_at_ms) FROM runs WHERE project_id=?`, projectID).Scan(&recent); err != nil {
		return false, err
	}
	if !recent.Valid {
		// No Run has ever touched this project — there is nothing to be idle
		// from. Treat as dormant; cancellation is the correct state.
		return false, nil
	}
	return recent.Int64 >= nowMS-IdleRunActivityWindowMS, nil
}

// sealIdleStatusNoteTx publishes the commander-mode single-line idle signal.
// The payload carries an empty members slice and a non-empty status_note so
// the webhook contract can validate it without admitting a phantom interrupt
// row; the rendered_text equals status_note so existing channel rendering code
// needs no new branch. No batch_member / interrupt / interrupt_delivery rows
// are written — the heartbeat is a digest-level projection, not an interrupt
// surface, and the A1 "no fabricated actor" line is preserved.
func (d *DB) sealIdleStatusNoteTx(ctx context.Context, tx *sql.Tx, batchID, project, channelJSON, forgeKind, host, forgeProject, targetKind, targetID, deliveryID, scope, scopeID string, due, nowMS int64) error {
	var channel any
	if json.Unmarshal([]byte(channelJSON), &channel) != nil {
		return fmt.Errorf("storage: corrupt batch channel")
	}
	payload, err := json.Marshal(map[string]any{
		"delivery_kind":      "attention_batch",
		"batch_id":           batchID,
		"delivery_id":        deliveryID,
		"batch_kind":         "daily_summary",
		"channel":            channel,
		"project_id":         project,
		"forge_alert_target": map[string]any{"forge_kind": forgeKind, "forge_host": host, "forge_project_key": forgeProject, "target_kind": targetKind, "target_id": targetID},
		"scope":              scope,
		"scope_id":           scopeID,
		"due_at_ms":          due,
		"members":            []map[string]any{},
		"status_note":        idleRunNoteText,
		"rendered_text":      idleRunNoteText,
	})
	if err != nil {
		return err
	}
	key := "attention-batch:" + batchID + ":publish:1"
	if err := insertOperation(ctx, tx, Operation{Key: key, Kind: OperationChannelPublish, Payload: payload}, "", "", nowMS); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO batch_deliveries(batch_id,delivery_id,operation_key,state,created_at_ms) VALUES(?,?,?,'pending',?)`, batchID, deliveryID, key, nowMS); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE attention_batches SET state='sealed',operation_key=?,payload_json=?,payload_digest=?,sealed_at_ms=?,updated_at_ms=? WHERE id=? AND state='collecting'`, key, string(payload), digestJSON(payload), nowMS, nowMS, batchID); err != nil {
		return err
	}
	if err := tx.Commit(); err == nil {
		d.wakeOutbox()
	}
	return err
}

// openCriticalSuccessorTx redecides a due fuse episode from the durable
// admitted-evidence window. A successor has a new immutable episode identity;
// the sealed batch is never retimed or reused.
func openCriticalSuccessorTx(ctx context.Context, tx *sql.Tx, batchID string, nowMS int64) error {
	var project, channel, snapshot, forgeKind, host, forgeProject, targetKind, targetID, scope, scopeID string
	var window int64
	var limitTotal, limitRun int
	if err := tx.QueryRowContext(ctx, `SELECT project_id,channel_id,channel_snapshot_json,forge_kind,forge_host,forge_project_key,target_kind,target_id,scope,scope_id,critical_window_ms,critical_total_limit,critical_per_run_limit FROM attention_batches WHERE id=?`, batchID).Scan(&project, &channel, &snapshot, &forgeKind, &host, &forgeProject, &targetKind, &targetID, &scope, &scopeID, &window, &limitTotal, &limitRun); err != nil {
		return err
	}
	where, args := `a.created_at_ms>? AND a.created_at_ms<=?`, []any{nowMS - window, nowMS}
	if scope == "run" {
		where += " AND a.run_id=?"
		args = append(args, scopeID)
	}
	var count, limit int
	query := `SELECT COUNT(*) FROM attention_admissions a WHERE a.kind='critical_admitted' AND ` + where
	if err := tx.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return err
	}
	if scope == "global" {
		limit = limitTotal
	} else {
		limit = limitRun
	}
	if count < limit {
		return nil
	}
	var episode string
	if err := tx.QueryRowContext(ctx, `SELECT a.id FROM attention_admissions a WHERE a.kind='critical_admitted' AND `+where+` ORDER BY a.created_at_ms,a.id LIMIT 1`, args...).Scan(&episode); err != nil {
		return err
	}
	var due int64
	if err := tx.QueryRowContext(ctx, `SELECT MIN(a.created_at_ms)+? FROM attention_admissions a WHERE a.kind='critical_admitted' AND `+where, append([]any{window}, args...)...).Scan(&due); err != nil {
		return err
	}
	enc := base64.RawURLEncoding.EncodeToString
	id := fmt.Sprintf("critical:%s:%s:%s:%s:%s:%s:%s:%s:%s", scope, scopeID, episode, channel, forgeKind, enc([]byte(host)), enc([]byte(forgeProject)), targetKind, enc([]byte(targetID)))
	deliveryID := id + ":publish:1"
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO attention_batches(id,state,project_id,channel_id,channel_snapshot_json,forge_kind,forge_host,forge_project_key,target_kind,target_id,kind,delivery_id,scope,scope_id,episode_admission_id,due_at_ms,critical_window_ms,critical_total_limit,critical_per_run_limit,created_at_ms,updated_at_ms) VALUES(?,'collecting',?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, project, channel, snapshot, forgeKind, host, forgeProject, targetKind, targetID, "critical_fuse", deliveryID, scope, scopeID, episode, due, window, limitTotal, limitRun, nowMS, nowMS); err != nil {
		return err
	}
	var gotProject, gotChannel, gotSnapshot, gotForgeKind, gotHost, gotForgeProject, gotTargetKind, gotTargetID, gotKind, gotDelivery, gotScope, gotScopeID, gotEpisode string
	var gotDue, gotWindow int64
	var gotTotal, gotRun int
	if err := tx.QueryRowContext(ctx, `SELECT project_id,channel_id,channel_snapshot_json,forge_kind,forge_host,forge_project_key,target_kind,target_id,kind,delivery_id,scope,scope_id,episode_admission_id,due_at_ms,critical_window_ms,critical_total_limit,critical_per_run_limit FROM attention_batches WHERE id=?`, id).Scan(&gotProject, &gotChannel, &gotSnapshot, &gotForgeKind, &gotHost, &gotForgeProject, &gotTargetKind, &gotTargetID, &gotKind, &gotDelivery, &gotScope, &gotScopeID, &gotEpisode, &gotDue, &gotWindow, &gotTotal, &gotRun); err != nil {
		return err
	}
	if gotProject != project || gotChannel != channel || gotSnapshot != snapshot || gotForgeKind != forgeKind || gotHost != host || gotForgeProject != forgeProject || gotTargetKind != targetKind || gotTargetID != targetID || gotKind != "critical_fuse" || gotDelivery != deliveryID || gotScope != scope || gotScopeID != scopeID || gotEpisode != episode || gotDue != due || gotWindow != window || gotTotal != limitTotal || gotRun != limitRun {
		return fmt.Errorf("storage: critical successor identity collision")
	}
	return nil
}

func joinBatchText(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += "；"
		}
		out += p
	}
	return out
}

// renderChannelInterrupt is the sole deterministic Channel renderer. Command
// lines always carry the delivery's current Run and nonce; a summary never
// creates a batch-wide command.
func renderChannelInterrupt(headline, brief, linksJSON, optionsJSON, runID, nonce string) (string, []string, error) {
	var links []InterruptLink
	var options []InterruptOption
	if err := json.Unmarshal([]byte(linksJSON), &links); err != nil {
		return "", nil, fmt.Errorf("storage: corrupt interrupt links")
	}
	if err := json.Unmarshal([]byte(optionsJSON), &options); err != nil {
		return "", nil, fmt.Errorf("storage: corrupt interrupt options")
	}
	lines := []string{headline}
	if brief != "" {
		lines = append(lines, brief)
	}
	for _, link := range links {
		lines = append(lines, link.Label+": "+link.Target)
	}
	commands := make([]string, 0, len(options))
	for _, option := range options {
		lines = append(lines, option.Label+"（"+option.ID+"）："+option.Effect+"；风险："+option.Risk)
		command := "/sift " + option.ID + " " + runID + " " + nonce
		switch option.ID {
		case "reject":
			command += " [<reason>]"
		case "hold":
			command += " 1h"
		case "ask":
			command += " <text>"
		}
		commands = append(commands, command)
		lines = append(lines, command)
	}
	return joinBatchText(lines), commands, nil
}
