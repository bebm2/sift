package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/xsift/sift/internal/config"
)

// ActivateConfig atomically materializes a startup config snapshot and its
// project projection. The snapshot is immutable and reused by content hash;
// project rows are the current routing identity used by workers and budgets.
func (d *DB) ActivateConfig(ctx context.Context, snapshot *config.Snapshot, binaryVersion string, nowMS int64) error {
	if d == nil || snapshot == nil || snapshot.Config == nil {
		return errors.New("storage: config activation requires a snapshot")
	}
	if binaryVersion == "" {
		return errors.New("storage: config activation requires a binary version")
	}
	if snapshot.Hash == "" || len(snapshot.CanonicalJSON) == 0 {
		return errors.New("storage: config activation requires a fingerprinted snapshot")
	}

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: begin config activation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var snapshotID string
	err = tx.QueryRowContext(ctx, `SELECT id FROM config_snapshots WHERE config_hash=?`, snapshot.Hash).Scan(&snapshotID)
	if err == sql.ErrNoRows {
		snapshotID = NewID()
		var sourceMTime any
		if !snapshot.Source.MTime.IsZero() {
			sourceMTime = snapshot.Source.MTime.UnixMilli()
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO config_snapshots
			(id, config_hash, schema_version, canonical_json, source_present, source_mtime_ms, loaded_at_ms, binary_version)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, snapshotID, snapshot.Hash, snapshot.Config.Version,
			string(snapshot.CanonicalJSON), boolInt(snapshot.Source.Present), sourceMTime, nowMS, binaryVersion); err != nil {
			return fmt.Errorf("storage: insert config snapshot: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("storage: find config snapshot: %w", err)
	}

	// Projects removed from the file remain for historical foreign keys, but no
	// longer route work. Re-enable and refresh only the projects in this snapshot.
	if _, err = tx.ExecContext(ctx, `UPDATE projects SET enabled=0,updated_at_ms=?`, nowMS); err != nil {
		return fmt.Errorf("storage: disable removed projects: %w", err)
	}
	for _, p := range snapshot.Config.Projects {
		if p.ID == "" {
			return errors.New("storage: config activation encountered project without id")
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO projects
			(id, config_snapshot_id, forge_kind, forge_host, forge_project_key, repo_path,
			 enabled, health, isolation_reason, capabilities_json, capabilities_checked_at_ms,
			 created_at_ms, updated_at_ms)
			VALUES (?, ?, ?, ?, ?, ?, ?, 'active', NULL, '{}', NULL, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
			config_snapshot_id=excluded.config_snapshot_id, forge_kind=excluded.forge_kind,
			forge_host=excluded.forge_host, forge_project_key=excluded.forge_project_key,
			repo_path=excluded.repo_path, enabled=excluded.enabled, health='active',
			isolation_reason=NULL, capabilities_json='{}', capabilities_checked_at_ms=NULL,
			updated_at_ms=excluded.updated_at_ms`, p.ID, snapshotID, string(p.Forge.Kind), p.Forge.Host,
			p.Forge.Project, p.Repo, boolInt(p.Enabled), nowMS, nowMS); err != nil {
			// A UNIQUE(forge_kind,forge_host,forge_project_key) hit means another
			// enabled project already claims this forge identity. Config load is
			// the primary guard (issue #1002), but a pre-existing row from an
			// earlier config must not crash-loop the daemon: skip this project
			// (leave it disabled) and keep activating the rest.
			if isUniqueViolation(err) {
				fmt.Printf("sift daemon: project %s skipped: duplicate forge identity %s/%s (another enabled project claims it)\n", p.ID, p.Forge.Kind, p.Forge.Project)
				continue
			}
			return fmt.Errorf("storage: activate project %s: %w", p.ID, err)
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("storage: commit config activation: %w", err)
	}
	return nil
}

// isUniqueViolation reports whether err is a SQLite UNIQUE constraint failure
// (SQLITE_CONSTRAINT_UNIQUE, extended code 2067) via the modernc driver's
// error string. The daemon uses it to skip — not crash on — a project whose
// forge identity is already claimed by another enabled row (issue #1002).
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "UNIQUE constraint failed") || strings.Contains(s, "(2067)")
}
