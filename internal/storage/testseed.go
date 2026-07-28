package storage

import (
	"context"
	"fmt"
)

// Cross-package integration-test seeds. These are NOT domain write ports
// (§11): they exist so tests in other packages (brain shell, control plane)
// can satisfy foreign keys without raw SQL access. Production code must not
// call them.

// SeedProjectForTest inserts a config snapshot and project with minimal
// valid rows.
func (d *DB) SeedProjectForTest(ctx context.Context, cfgID, projectID string, nowMS int64) error {
	if _, err := d.db.ExecContext(ctx, `INSERT INTO config_snapshots
		(id, config_hash, schema_version, canonical_json, source_present, source_mtime_ms, loaded_at_ms, binary_version)
		VALUES (?, ?, 1, '{}', 1, NULL, ?, 'test-binary')`, cfgID, "hash-"+cfgID, nowMS); err != nil {
		return fmt.Errorf("storage: seed config snapshot: %w", err)
	}
	if _, err := d.db.ExecContext(ctx, `INSERT INTO projects
		(id, config_snapshot_id, forge_kind, forge_host, forge_project_key, repo_path,
		 enabled, health, isolation_reason, capabilities_json, capabilities_checked_at_ms,
		 created_at_ms, updated_at_ms)
		VALUES (?, ?, 'github', 'github.com', ?, ?, 1, 'active', NULL, '{}', NULL, ?, ?)`,
		projectID, cfgID, "org/repo-"+projectID, "/repo/"+projectID, nowMS, nowMS); err != nil {
		return fmt.Errorf("storage: seed project: %w", err)
	}
	return nil
}

// SeedForgeRunForTest inserts a forge-sourced queued run with minimal valid
// fields.
func (d *DB) SeedForgeRunForTest(ctx context.Context, runID, projectID, cfgID, issueID string, nowMS int64) error {
	if _, err := d.db.ExecContext(ctx, `INSERT INTO runs
		(id, source_kind, project_id, config_snapshot_id, forge_kind, forge_host, forge_project_key,
		 issue_id, status, max_attempts, created_at_ms, updated_at_ms)
		VALUES (?, 'forge', ?, ?, 'github', 'github.com', ?, ?, 'queued', 3, ?, ?)`,
		runID, projectID, cfgID, "org/repo-"+projectID, issueID, nowMS, nowMS); err != nil {
		return fmt.Errorf("storage: seed run: %w", err)
	}
	return nil
}
