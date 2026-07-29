package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// ensureChannelSchema is kept separate from the historical M1 migration so
// opening databases created by older binaries remains safe. The tables are
// additive projections and are deliberately not used as a second domain write
// port.
func ensureChannelSchema(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS attention_batches (
 id TEXT NOT NULL PRIMARY KEY, state TEXT NOT NULL CHECK(state IN ('collecting','sealed','delivered','failed','cancelled')),
 project_id TEXT NOT NULL, channel_id TEXT NOT NULL, channel_snapshot_json TEXT NOT NULL,
 forge_kind TEXT NOT NULL, forge_host TEXT NOT NULL, forge_project_key TEXT NOT NULL,
 target_kind TEXT NOT NULL, target_id TEXT NOT NULL, operation_key TEXT, updated_at_ms INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS batch_deliveries (
 batch_id TEXT NOT NULL PRIMARY KEY, delivery_id TEXT NOT NULL UNIQUE,
 operation_key TEXT NOT NULL UNIQUE, state TEXT NOT NULL CHECK(state IN ('pending','delivered','failed')),
 attempt_count INTEGER NOT NULL DEFAULT 0, remote_ref TEXT, last_error TEXT,
 created_at_ms INTEGER NOT NULL, delivered_at_ms INTEGER);
CREATE TABLE IF NOT EXISTS attention_admissions (
 id TEXT NOT NULL PRIMARY KEY, interrupt_id TEXT NOT NULL REFERENCES interrupts(id),
 admission_key TEXT NOT NULL UNIQUE, kind TEXT NOT NULL CHECK(kind IN ('critical_admitted','critical_fused')),
 metric_identity TEXT NOT NULL, run_id TEXT NOT NULL REFERENCES runs(id),
 critical_source TEXT NOT NULL CHECK(critical_source IN ('initial','escalation')), created_at_ms INTEGER NOT NULL);
CREATE UNIQUE INDEX IF NOT EXISTS attention_admissions_interrupt_critical ON attention_admissions(interrupt_id);
CREATE TABLE IF NOT EXISTS channel_failure_episodes (
 subject_id TEXT NOT NULL, generation INTEGER NOT NULL CHECK(generation=1),
 consecutive_failures INTEGER NOT NULL CHECK(consecutive_failures>=0),
 state TEXT NOT NULL CHECK(state IN ('open','alerted','ended_delivered','ended_failed')),
 last_error_class TEXT, alert_operation_key TEXT UNIQUE, created_at_ms INTEGER NOT NULL,
 updated_at_ms INTEGER NOT NULL, ended_at_ms INTEGER, PRIMARY KEY(subject_id,generation));`)
	return err
}

type channelPayload struct {
	DeliveryKind     string            `json:"delivery_kind"`
	DeliveryID       string            `json:"delivery_id"`
	InterruptID      string            `json:"interrupt_id"`
	InterruptVersion int               `json:"interrupt_version"`
	Nonce            string            `json:"nonce"`
	EscalationNo     int               `json:"escalation_no"`
	BatchID          string            `json:"batch_id"`
	Channel          json.RawMessage   `json:"channel"`
	ProjectID        string            `json:"project_id"`
	ForgeAlertTarget *forgeAlertTarget `json:"forge_alert_target"`
}
type forgeAlertTarget struct {
	ForgeKind       string `json:"forge_kind"`
	ForgeHost       string `json:"forge_host"`
	ForgeProjectKey string `json:"forge_project_key"`
	TargetKind      string `json:"target_kind"`
	TargetID        string `json:"target_id"`
}

func channelSubject(p channelPayload) string { return p.DeliveryID }

// ChannelDiagnostics reads the durable Channel projections used by operator
// views. It intentionally does not infer state from in-memory workers.
func (d *DB) ChannelDiagnostics(ctx context.Context) ([]map[string]any, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT delivery_id,operation_key,state,attempt_count,COALESCE(last_error,''),created_at_ms FROM interrupt_deliveries WHERE surface='channel' ORDER BY created_at_ms,delivery_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, key, state, last string
		var attempts, created int64
		if err := rows.Scan(&id, &key, &state, &attempts, &last, &created); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"delivery_id": id, "operation_key": key, "state": state, "attempt_count": attempts, "last_error": last, "created_at_ms": created})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func applyChannelOutcomeTx(ctx context.Context, tx *sql.Tx, claim ClaimedOperation, outcome CompleteOutcome, _ bool) error {
	var p channelPayload
	if err := json.Unmarshal(claim.Payload, &p); err != nil || p.DeliveryID == "" {
		return fmt.Errorf("storage: invalid channel payload")
	}
	subject := channelSubject(p)
	batch := p.DeliveryKind == "attention_batch"
	var remote struct {
		RemoteRef string `json:"remote_ref"`
	}
	_ = json.Unmarshal(outcome.Evidence, &remote)
	var res sql.Result
	var err error
	if batch {
		res, err = tx.ExecContext(ctx, `UPDATE batch_deliveries SET attempt_count=attempt_count+1, state=?, remote_ref=CASE WHEN ?='delivered' THEN ? ELSE remote_ref END, last_error=?, delivered_at_ms=CASE WHEN ?='delivered' THEN ? ELSE delivered_at_ms END WHERE delivery_id=? AND operation_key=?`, channelDeliveryState(outcome), channelDeliveryState(outcome), nullable(remote.RemoteRef), nullable(outcome.ErrorSummary), channelDeliveryState(outcome), outcome.NowMS, subject, claim.Key)
	} else {
		res, err = tx.ExecContext(ctx, `UPDATE interrupt_deliveries SET attempt_count=attempt_count+1, state=?, remote_ref=CASE WHEN ?='delivered' THEN ? ELSE remote_ref END, last_error=?, delivered_at_ms=CASE WHEN ?='delivered' THEN ? ELSE delivered_at_ms END WHERE delivery_id=? AND operation_key=?`, channelDeliveryState(outcome), channelDeliveryState(outcome), nullable(remote.RemoteRef), nullable(outcome.ErrorSummary), channelDeliveryState(outcome), outcome.NowMS, subject, claim.Key)
	}
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		if err != nil {
			return err
		}
		return fmt.Errorf("storage: missing channel delivery projection")
	}
	if batch && outcome.State != OperationSucceeded {
		state := "pending"
		if outcome.State != OperationRetryable {
			state = "failed"
		}
		if _, err := tx.ExecContext(ctx, `UPDATE attention_batches SET state=?,updated_at_ms=? WHERE id=? AND state NOT IN ('delivered','failed','cancelled')`, state, outcome.NowMS, p.BatchID); err != nil {
			return err
		}
	}
	var old int
	var oldAlert sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT consecutive_failures,alert_operation_key FROM channel_failure_episodes WHERE subject_id=? AND generation=1`, subject).Scan(&old, &oldAlert)
	if err == sql.ErrNoRows {
		if _, err = tx.ExecContext(ctx, `INSERT INTO channel_failure_episodes(subject_id,generation,consecutive_failures,state,last_error_class,created_at_ms,updated_at_ms) VALUES(?,1,0,'open',NULL,?,?)`, subject, outcome.NowMS, outcome.NowMS); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if outcome.State == OperationSucceeded {
		if batch {
			_, err = tx.ExecContext(ctx, `UPDATE attention_batches SET state='delivered',updated_at_ms=? WHERE id=? AND state NOT IN ('delivered','failed','cancelled')`, outcome.NowMS, p.BatchID)
			if err != nil {
				return err
			}
			// Delivery evidence is a Ledger fact for every frozen member. The
			// deterministic id makes completion/replay idempotent.
			var members []struct{ ID, RunID string }
			rows, qerr := tx.QueryContext(ctx, `SELECT json_extract(value,'$.interrupt_id'), i.run_id FROM outbox_operations o, json_each(json_extract(o.payload_json,'$.members')) j JOIN interrupts i ON i.id=json_extract(j.value,'$.interrupt_id') WHERE o.id=?`, claim.ID)
			if qerr == nil {
				for rows.Next() {
					var m struct{ ID, RunID string }
					if rows.Scan(&m.ID, &m.RunID) == nil {
						members = append(members, m)
					}
				}
				rows.Close()
			}
			for _, m := range members {
				_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO ledger_entries(id,run_id,interrupt_id,entry_kind,features_schema_version,features_json,created_at_ms) VALUES(?,?,?,'attention_delivery',1,?,?)`, "channel_delivery:"+subject+":"+m.ID, m.RunID, m.ID, `{"surface":"channel","delivery_state":"delivered"}`, outcome.NowMS)
				if err != nil {
					return err
				}
			}
		}
		_, err = tx.ExecContext(ctx, `UPDATE channel_failure_episodes SET consecutive_failures=0,state='ended_delivered',last_error_class=NULL,updated_at_ms=?,ended_at_ms=? WHERE subject_id=? AND generation=1 AND state NOT LIKE 'ended_%'`, outcome.NowMS, outcome.NowMS, subject)
		return err
	}
	count := old + 1
	threshold := outcome.ChannelFailureAlertAfter
	if threshold <= 0 {
		threshold = 3
	}
	terminal := outcome.State != OperationRetryable
	state := "open"
	if terminal {
		state = "ended_failed"
	} else if oldAlert.Valid {
		state = "alerted"
	} else if count >= threshold {
		state = "alerted"
	}
	if _, err = tx.ExecContext(ctx, `UPDATE channel_failure_episodes SET consecutive_failures=?,state=?,last_error_class=?,updated_at_ms=?,ended_at_ms=CASE WHEN ? THEN ? ELSE ended_at_ms END WHERE subject_id=? AND generation=1 AND state NOT LIKE 'ended_%'`, count, state, nullable(string(outcome.ErrorClass)), outcome.NowMS, terminal, outcome.NowMS, subject); err != nil {
		return err
	}
	if old < threshold && count >= threshold && !oldAlert.Valid {
		target := p.ForgeAlertTarget
		if target == nil && !batch {
			target = &forgeAlertTarget{}
			err := tx.QueryRowContext(ctx, `SELECT r.forge_kind,r.forge_host,r.forge_project_key,CASE WHEN r.issue_id IS NOT NULL THEN 'issue' ELSE r.discussion_target_kind END,COALESCE(r.issue_id,r.discussion_target_id) FROM interrupts i JOIN runs r ON r.id=i.run_id WHERE i.id=?`, p.InterruptID).Scan(&target.ForgeKind, &target.ForgeHost, &target.ForgeProjectKey, &target.TargetKind, &target.TargetID)
			if err != nil || target.ForgeKind == "" || target.ForgeHost == "" || target.ForgeProjectKey == "" || target.TargetKind == "" || target.TargetID == "" {
				return fmt.Errorf("storage: missing frozen channel alert target")
			}
		}
		if target == nil {
			return fmt.Errorf("storage: missing batch channel alert target")
		}
		key := AlertOperationKey("channel_failure", subject, 1)
		markdown := fmt.Sprintf("[sift alert:channel_failure:%s:1]\nChannel operation: %s\nEpisode generation: 1\nConsecutive failures: %d\nLatest error class: %s\nStatus: generated_not_delivered\nDiagnostics: sift ps; sift doctor", subject, claim.Key, count, outcome.ErrorClass)
		payload, _ := json.Marshal(map[string]any{"forge_kind": target.ForgeKind, "forge_host": target.ForgeHost, "forge_project_key": target.ForgeProjectKey, "target_kind": target.TargetKind, "target_id": target.TargetID, "purpose": "channel_failure", "markdown": markdown})
		if err := insertOperation(ctx, tx, Operation{Key: key, Kind: OperationForgeAlert, Payload: payload}, "", "", outcome.NowMS); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE channel_failure_episodes SET alert_operation_key=? WHERE subject_id=? AND generation=1`, key, subject); err != nil {
			return err
		}
	}
	return nil
}

func channelDeliveryState(o CompleteOutcome) string {
	if o.State == OperationSucceeded {
		return "delivered"
	}
	if o.State == OperationRetryable {
		return "pending"
	}
	return "failed"
}

// EnqueueChannelPublish creates the durable delivery projection and immutable
// operation together. Callers pass already sealed channel_publish bytes.
func (d *DB) EnqueueChannelPublish(ctx context.Context, op Operation, deliveryID string, nowMS int64) error {
	if op.Kind != OperationChannelPublish || deliveryID == "" {
		return fmt.Errorf("storage: invalid channel publish")
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := insertOperation(ctx, tx, op, op.RunID, "", nowMS); err != nil {
		return err
	}
	var p channelPayload
	if err := json.Unmarshal(op.Payload, &p); err != nil {
		return err
	}
	if p.DeliveryID != deliveryID {
		return fmt.Errorf("storage: channel delivery identity mismatch")
	}
	if p.DeliveryKind == "attention_batch" {
		if p.BatchID == "" || deliveryID != p.BatchID+":publish:1" || p.ProjectID == "" || p.ForgeAlertTarget == nil {
			return fmt.Errorf("storage: invalid batch identity")
		}
		var channel struct{ ID, Type, TargetRef, Renderer string }
		if json.Unmarshal(p.Channel, &channel) != nil || channel.ID == "" || channel.Type != "webhook" || channel.TargetRef == "" || channel.Renderer != "plain-v1" {
			return fmt.Errorf("storage: invalid batch channel snapshot")
		}
		if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO attention_batches(id,state,project_id,channel_id,channel_snapshot_json,forge_kind,forge_host,forge_project_key,target_kind,target_id,operation_key,updated_at_ms) VALUES(?,'sealed',?,?,?,?,?,?,?,?,?,?)`, p.BatchID, p.ProjectID, channel.ID, string(p.Channel), p.ForgeAlertTarget.ForgeKind, p.ForgeAlertTarget.ForgeHost, p.ForgeAlertTarget.ForgeProjectKey, p.ForgeAlertTarget.TargetKind, p.ForgeAlertTarget.TargetID, op.Key, nowMS); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO batch_deliveries(batch_id,delivery_id,operation_key,state,created_at_ms) VALUES(?,?,?,'pending',?)`, p.BatchID, deliveryID, op.Key, nowMS)
	} else {
		if p.InterruptID == "" || p.InterruptVersion < 1 || p.Nonce == "" {
			return fmt.Errorf("storage: invalid interrupt channel delivery")
		}
		var channel struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(p.Channel, &channel) != nil || channel.ID == "" {
			return fmt.Errorf("storage: invalid channel snapshot")
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO interrupt_deliveries(id,delivery_id,interrupt_id,surface,channel_id,channel_snapshot_json,interrupt_version,nonce,escalation_no,priority,operation_key,state,attempt_count,created_at_ms) VALUES(?,?,?,'channel',?,?,?,?,?,'normal',?,'pending',0,?)`, newID(), deliveryID, p.InterruptID, channel.ID, string(p.Channel), p.InterruptVersion, p.Nonce, p.EscalationNo, op.Key, nowMS)
	}
	if err != nil {
		return err
	}
	if err = tx.Commit(); err == nil {
		d.wakeOutbox()
	}
	return err
}
