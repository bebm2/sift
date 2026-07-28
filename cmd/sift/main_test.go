package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/miaoxiaoyong/sift/internal/config"
	"github.com/miaoxiaoyong/sift/internal/controlplane"
	"github.com/miaoxiaoyong/sift/internal/storage"
)

// freshHome returns a 0700 temp dir suitable for use as SIFT_HOME. It creates
// the directory directly in the OS temp root rather than under t.TempDir's
// per-test subdirectory, so the resolved socket path stays within the Unix
// domain socket length limit for the online test.
func freshHome(t *testing.T) string {
	t.Helper()
	home, err := os.MkdirTemp("", "sift")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	if err := os.Chmod(home, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SIFT_HOME", home)
	return home
}

// withDatabase provisions a readable sift.db so the doctor's sqlite and
// permission checks do not report errors, leaving only the unavoidable
// unsafe-local warning.
func withDatabase(t *testing.T, home string) {
	t.Helper()
	db, err := storage.Open(context.Background(), storage.OpenConfig{Path: filepath.Join(home, "sift.db"), BinaryVersion: controlplane.Version, Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestDoctorExitCode extracts the §7 exit status from every shape the doctor
// result can take: a Go int (offline, direct) and a JSON float64 (online, after
// wire decode), plus the degenerate cases that must default to 0.
func TestDoctorExitCode(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result any
		want   int
	}{
		{"offline int clean", map[string]any{"exit_code": int(0)}, 0},
		{"offline int warning", map[string]any{"exit_code": int(1)}, 1},
		{"offline int error", map[string]any{"exit_code": int(2)}, 2},
		{"online float clean", map[string]any{"exit_code": float64(0)}, 0},
		{"online float warning", map[string]any{"exit_code": float64(1)}, 1},
		{"online float error", map[string]any{"exit_code": float64(2)}, 2},
		{"missing exit_code", map[string]any{"checks": nil}, 0},
		{"malformed exit_code", map[string]any{"exit_code": "2"}, 0},
		{"not a map", []any{"checks"}, 0},
		{"nil", nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := doctorExitCode(tc.result); got != tc.want {
				t.Fatalf("doctorExitCode(%v) = %d, want %d", tc.result, got, tc.want)
			}
		})
	}
}

// TestRunDoctorOfflineExitsWithError reproduces the issue #34 baseline: an
// empty SIFT_HOME cannot have a database, so offline doctor must surface the
// sqlite error as a non-zero (2) process exit, not silently exit 0.
func TestRunDoctorOfflineExitsWithError(t *testing.T) {
	freshHome(t) // no sift.db -> sqlite check errors
	var out bytes.Buffer
	code := run([]string{"sift", "doctor", "--offline"}, &out, io.Discard)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2; output:\n%s", code, out.String())
	}
	var result map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["exit_code"] != float64(2) {
		t.Fatalf("doctor exit_code = %v, want 2", result["exit_code"])
	}
	if result["offline"] != true {
		t.Fatalf("doctor offline = %v, want true", result["offline"])
	}
}

// TestRunDoctorOfflineExitsWithWarning verifies the warning-only path (exit 1):
// with a healthy database the only remaining finding is the always-on
// unsafe-local warning.
func TestRunDoctorOfflineExitsWithWarning(t *testing.T) {
	home := freshHome(t)
	withDatabase(t, home)
	code := run([]string{"sift", "doctor", "--offline"}, &bytes.Buffer{}, io.Discard)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}

// TestRunDoctorOnlineExitsWithWarning drives the online path end to end: a live
// daemon returns the doctor result in response.Result, and the process must
// exit with the daemon-computed exit_code (1, unsafe-local warning).
func TestRunDoctorOnlineExitsWithWarning(t *testing.T) {
	home := freshHome(t)
	withDatabase(t, home)
	s, err := controlplane.Start(config.Home{Path: home})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Serve(ctx) }()
	waitSocket(t, filepath.Join(home, "siftd.sock"))

	var out bytes.Buffer
	code := run([]string{"sift", "doctor"}, &out, io.Discard)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; output:\n%s", code, out.String())
	}
	var response map[string]any
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["ok"] != true {
		t.Fatalf("response ok = %v, want true", response["ok"])
	}
	result, _ := response["result"].(map[string]any)
	if result["exit_code"] != float64(1) {
		t.Fatalf("doctor exit_code = %v, want 1", result["exit_code"])
	}
	if result["offline"] != false {
		t.Fatalf("doctor offline = %v, want false", result["offline"])
	}
}

// TestRunDoctorOnlineExitsOneWhenDaemonUnavailable confirms the daemon-missing
// path still surfaces as a non-zero process exit.
func TestRunDoctorOnlineExitsOneWhenDaemonUnavailable(t *testing.T) {
	freshHome(t) // no daemon, no token, no socket
	var stderr bytes.Buffer
	code := run([]string{"sift", "doctor"}, io.Discard, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !bytes.Contains(stderr.Bytes(), []byte("daemon unavailable")) {
		t.Fatalf("stderr = %q, want daemon unavailable message", stderr.String())
	}
}

func waitSocket(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("socket %s not created", path)
}
