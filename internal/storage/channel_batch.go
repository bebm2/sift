package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// PrepareDueAttentionBatches is the sole batch sealing port. It freezes the
// surviving member snapshots and the channel operation in one transaction.
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
	rows, err := tx.QueryContext(ctx, `SELECT m.delivery_id,m.interrupt_id,m.interrupt_version,m.nonce,m.headline,m.reason,m.severity,m.links_json,m.options_json FROM attention_batch_members m JOIN interrupts i ON i.id=m.interrupt_id WHERE m.batch_id=? AND m.excluded_at_ms IS NULL AND i.status='open' AND i.version=m.interrupt_version AND i.nonce=m.nonce ORDER BY m.interrupt_id`, batchID)
	if err != nil {
		return err
	}
	defer rows.Close()
	members := []map[string]any{}
	texts := []string{}
	for rows.Next() {
		var delivery, id, nonce, headline, reason, severity, links, options string
		var version int
		if err := rows.Scan(&delivery, &id, &version, &nonce, &headline, &reason, &severity, &links, &options); err != nil {
			return err
		}
		var l, o any
		if json.Unmarshal([]byte(links), &l) != nil || json.Unmarshal([]byte(options), &o) != nil {
			return fmt.Errorf("storage: corrupt batch member")
		}
		members = append(members, map[string]any{"delivery_id": delivery, "interrupt_id": id, "interrupt_version": version, "nonce": nonce, "headline": headline, "reason": reason, "severity": severity, "links": l, "options": o, "command_lines": []string{}})
		texts = append(texts, id+": "+headline)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(members) == 0 {
		_, err = tx.ExecContext(ctx, `UPDATE attention_batches SET state='cancelled',updated_at_ms=? WHERE id=? AND state='collecting'`, nowMS, batchID)
		if err != nil {
			return err
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
	if err = tx.Commit(); err == nil {
		d.wakeOutbox()
	}
	return err
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
