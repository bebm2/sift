package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/xsift/sift/internal/config"
)

func TestActivateConfigProjectsOnEmptyDB(t *testing.T) {
	ctx := context.Background()
	now := time.UnixMilli(1234)
	db, err := Open(ctx, OpenConfig{Path: filepath.Join(t.TempDir(), "sift.db"), BinaryVersion: "test", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	snapshot := &config.Snapshot{
		Config: &config.Config{Version: 1, Projects: []config.Project{
			{ID: "enabled", Repo: "/repo/enabled", Enabled: true, Forge: config.ForgeRef{Kind: config.ForgeKindGitHub, Host: "github.com", Project: "org/repo"}},
			{ID: "disabled", Repo: "/repo/disabled", Enabled: false, Forge: config.ForgeRef{Kind: config.ForgeKindGitLab, Host: "gitlab.com", Project: "org/repo"}},
		}},
		Hash:          "hash-activation",
		CanonicalJSON: []byte(`{"version":1}`),
	}
	if err := db.ActivateConfig(ctx, snapshot, "test", now.UnixMilli()); err != nil {
		t.Fatal(err)
	}

	var snapshots, projects, enabled int
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM config_snapshots`).Scan(&snapshots); err != nil {
		t.Fatal(err)
	}
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM projects`).Scan(&projects); err != nil {
		t.Fatal(err)
	}
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM projects WHERE enabled=1`).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if snapshots != 1 || projects != 2 || enabled != 1 {
		t.Fatalf("activation counts: snapshots=%d projects=%d enabled=%d", snapshots, projects, enabled)
	}

	// Re-activating the same fingerprint is idempotent for the immutable
	// snapshot and refreshes the current project projection in one transaction.
	if err := db.ActivateConfig(ctx, snapshot, "test", now.Add(time.Second).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM config_snapshots`).Scan(&snapshots); err != nil {
		t.Fatal(err)
	}
	if snapshots != 1 {
		t.Fatalf("same config hash created %d snapshots, want 1", snapshots)
	}
}

// TestActivateConfigDuplicateForgeIdentitySkips pins the issue #1002 daemon
// guard: a snapshot whose enabled projects duplicate a forge identity already
// claimed by a pre-existing row must skip the duplicate (log + continue), not
// fail activation and crash-loop the daemon. Config load rejects this shape
// for new configs; this covers rows left by earlier configs.
func TestActivateConfigDuplicateForgeIdentitySkips(t *testing.T) {
	ctx := context.Background()
	now := time.UnixMilli(1234)
	db, err := Open(ctx, OpenConfig{Path: filepath.Join(t.TempDir(), "sift.db"), BinaryVersion: "test", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mk := func(hash string, ids ...string) *config.Snapshot {
		projects := make([]config.Project, 0, len(ids))
		for _, id := range ids {
			projects = append(projects, config.Project{ID: id, Repo: "/repo/" + id, Enabled: true,
				Forge: config.ForgeRef{Kind: config.ForgeKindGitHub, Host: "github.com", Project: "owner/demo"}})
		}
		return &config.Snapshot{Config: &config.Config{Version: 1, Projects: projects}, Hash: hash, CanonicalJSON: []byte(`{"version":1}`)}
	}

	// Seed: one claims the identity.
	if err := db.ActivateConfig(ctx, mk("seed", "one"), "test", now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	// Second snapshot adds two@same identity: activation must survive.
	if err := db.ActivateConfig(ctx, mk("dup", "one", "two"), "test", now.Add(time.Second).UnixMilli()); err != nil {
		t.Fatalf("duplicate identity must not fail activation: %v", err)
	}
	var enabled int
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM projects WHERE enabled=1`).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if enabled != 1 {
		t.Fatalf("enabled projects = %d, want 1 (only the first claimant)", enabled)
	}
}
