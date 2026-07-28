package controlplane

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/miaoxiaoyong/sift/internal/storage"
)

func TestDoctorBaselineChecksConfiguredDependencies(t *testing.T) {
	home := testHome(t)
	bin := filepath.Join(home.Path, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"agent", "gh", "tmux"} {
		writeDoctorExecutable(t, bin, name)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	configYAML := `version: 1
agents:
  - id: agent
    executable: agent
    backend: tmux
projects:
  - id: project
    repo: ` + home.Path + `
    forge:
      kind: github
      project: owner/repo
`
	if err := os.WriteFile(filepath.Join(home.Path, "config.yaml"), []byte(configYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(context.Background(), storage.OpenConfig{Path: filepath.Join(home.Path, "sift.db"), BinaryVersion: Version, Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	result := doctor(context.Background(), true, home)
	if result["exit_code"] != 1 || result["security_posture"] != "unsafe-local" {
		t.Fatalf("doctor result = %#v", result)
	}
	checks := doctorChecks(t, result)
	for _, id := range []string{"runtime", "sqlite", "agent-cli:agent", "forge-cli:project:version", "forge-cli:project:login", "tmux", "permissions:home"} {
		if check := checks[id]; check.Level != "ok" {
			t.Errorf("%s = %#v, want ok", id, check)
		}
	}
	if checks["operator-token-readable-by-agent"].Level != "warning" {
		t.Fatalf("unsafe-local check = %#v", checks["operator-token-readable-by-agent"])
	}
}

func TestDoctorReportsSQLiteAndPermissionErrors(t *testing.T) {
	home := testHome(t)
	if err := os.Chmod(home.Path, 0o755); err != nil {
		t.Fatal(err)
	}
	result := doctor(context.Background(), true, home)
	if result["exit_code"] != 2 {
		t.Fatalf("exit_code = %v, want 2", result["exit_code"])
	}
	checks := doctorChecks(t, result)
	if checks["permissions:home"].Level != "error" {
		t.Fatalf("home permission check = %#v", checks["permissions:home"])
	}
	if checks["sqlite"].Level != "error" {
		t.Fatalf("sqlite check = %#v", checks["sqlite"])
	}
}

func TestDoctorRejectsNonSocketAtSocketPath(t *testing.T) {
	home := testHome(t)
	if err := os.WriteFile(filepath.Join(home.Path, "siftd.sock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	checks := doctorChecks(t, doctor(context.Background(), false, home))
	if checks["permissions:siftd.sock"].Level != "error" {
		t.Fatalf("socket check = %#v", checks["permissions:siftd.sock"])
	}
}

func writeDoctorExecutable(t *testing.T, dir, name string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho 'fixture '+\"$@\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
}

func doctorChecks(t *testing.T, result map[string]any) map[string]doctorCheck {
	t.Helper()
	checks, ok := result["checks"].([]doctorCheck)
	if !ok {
		t.Fatalf("checks type = %T", result["checks"])
	}
	byID := make(map[string]doctorCheck, len(checks))
	for _, check := range checks {
		byID[check.ID] = check
	}
	return byID
}
