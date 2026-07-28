package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/miaoxiaoyong/sift/internal/forge"
)

// UpdateProjectAutoMergeCapability records the startup CAS-capability proof.
// An absent or malformed projection is intentionally interpreted as disabled
// by AutoMergeEnabled, so restarts cannot recover an optimistic default.
func (d *DB) UpdateProjectAutoMergeCapability(ctx context.Context, projectID string, enabled bool, evidence string, nowMS int64) error {
	if projectID == "" || nowMS <= 0 {
		return errors.New("storage: auto-merge capability requires project and timestamp")
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw string
	if err := tx.QueryRowContext(ctx, `SELECT capabilities_json FROM projects WHERE id=?`, projectID).Scan(&raw); err != nil {
		return err
	}
	capabilities := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &capabilities); err != nil {
		return fmt.Errorf("storage: invalid project capabilities: %w", err)
	}
	capabilities["auto_merge"] = enabled
	encoded, err := json.Marshal(capabilities)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE projects SET capabilities_json=?,capabilities_checked_at_ms=?,updated_at_ms=? WHERE id=?`, string(encoded), nowMS, nowMS, projectID); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{"auto_merge": enabled, "evidence": evidence})
	if _, err := tx.ExecContext(ctx, `INSERT INTO events(id,project_id,type,source,payload_schema_version,payload_json,occurred_at_ms,recorded_at_ms) VALUES(?,?, 'project.capability_checked','system',1,?,?,?)`, newID(), projectID, string(payload), nowMS, nowMS); err != nil {
		return err
	}
	return tx.Commit()
}

// AutoMergeEnabled implements forge.AutoMergeCapabilityReader using the
// durable project projection. Missing rows, malformed values, and absent keys
// are all unavailable rather than optimistic defaults.
func (d *DB) AutoMergeEnabled(ctx context.Context, ref forge.ProjectRef) (bool, error) {
	var raw string
	err := d.db.QueryRowContext(ctx, `SELECT capabilities_json FROM projects WHERE forge_kind=? AND forge_host=? AND forge_project_key=? AND enabled=1`, string(ref.Kind), ref.Host, ref.ProjectKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var capabilities map[string]any
	if err := json.Unmarshal([]byte(raw), &capabilities); err != nil {
		return false, nil
	}
	enabled, ok := capabilities["auto_merge"].(bool)
	return ok && enabled, nil
}

var _ forge.AutoMergeCapabilityReader = (*DB)(nil)
