package main

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/xsift/sift/internal/config"
	"github.com/xsift/sift/internal/hosting"
)

func managedConfigApplyFixture(t *testing.T) (string, hosting.Spec, net.Listener) {
	t.Helper()
	home := freshHome(t)
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("managed hosting is not available on this platform")
	}
	installReleaseLayout(t, home)
	// Issue #980 P0: hosting.NewSpec resolves UnitPath against the real OS
	// user home/config dir (launchd ~/Library/LaunchAgents, systemd
	// ~/.config/systemd/user). Isolating $HOME (and clearing XDG_CONFIG_HOME so
	// systemd resolution falls back to $HOME/.config) before resolving means the
	// unit write below can only ever land inside this test-owned root, never on
	// the real user's launchd plist. Production unit resolution is untouched.
	unitHome := filepath.Join(t.TempDir(), "home")
	t.Setenv("HOME", unitHome)
	t.Setenv("XDG_CONFIG_HOME", "")
	spec, err := hosting.NewSpec(home)
	if err != nil {
		t.Fatal(err)
	}
	// Fail-closed: if UnitPath ever escapes the test-owned HOME (HOME leaked
	// from the host, or resolution stopped honoring it), fail before writing a
	// byte to what would be a real unit file.
	if !filepath.IsAbs(spec.UnitPath) || !pathWithin(unitHome, spec.UnitPath) {
		t.Fatalf("UnitPath %q is not under the test-owned HOME %q; refusing to touch a real unit", spec.UnitPath, unitHome)
	}
	if err := os.MkdirAll(filepath.Dir(spec.UnitPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spec.UnitPath, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", filepath.Join(home, "siftd.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return home, spec, listener
}

// pathWithin reports whether p is under root, as a strict descendant (or the
// root itself), using lexical containment. It rejects sibling, parent and
// absolute-escape paths so a malformed resolved unit path can never pass the
// fail-closed guard.
func pathWithin(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func installFakeHostingTool(t *testing.T, exitCode int) {
	t.Helper()
	bin := t.TempDir()
	tool := "systemctl"
	if runtime.GOOS == "darwin" {
		tool = "launchctl"
	}
	if err := os.WriteFile(filepath.Join(bin, tool), []byte("#!/bin/sh\nexit "+string(rune('0'+exitCode))+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
}

func TestAnnounceConfigAppliedManagedRestartSucceeds(t *testing.T) {
	home, _, _ := managedConfigApplyFixture(t)
	installFakeHostingTool(t, 0)
	var stdout, stderr bytes.Buffer
	announceConfigApplied(mustHome(t, home), &stdout, &stderr)
	if !strings.Contains(stdout.String(), "已自动重启 daemon 并生效") {
		t.Fatalf("stdout = %q, want successful restart", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestAnnounceConfigAppliedManagedRestartFailureWarns(t *testing.T) {
	home, _, _ := managedConfigApplyFixture(t)
	installFakeHostingTool(t, 1)
	var stdout, stderr bytes.Buffer
	announceConfigApplied(mustHome(t, home), &stdout, &stderr)
	if strings.Contains(stdout.String(), "已自动重启 daemon 并生效") {
		t.Fatalf("stdout falsely reported success: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "自动重启后台服务失败") {
		t.Fatalf("stderr = %q, want restart warning", stderr.String())
	}
}

func TestAnnounceConfigAppliedNoBackendGivesActionableHint(t *testing.T) {
	home, _, _ := managedConfigApplyFixture(t)
	// Keep PATH empty so hosting.Exec returns ErrNoBackend instead of running a
	// real supervisor. Restart must then be observable as a failure to callers.
	t.Setenv("PATH", t.TempDir())
	var stdout, stderr bytes.Buffer
	code := runService([]string{"restart"}, mustHome(t, home), &stdout, &stderr)
	if code == 0 {
		t.Fatal("restart without a hosting backend returned success")
	}
	combined := stdout.String() + stderr.String()
	if strings.Contains(combined, "已自动重启 daemon 并生效") {
		t.Fatalf("no-backend restart falsely reported success: %q", combined)
	}
	if !strings.Contains(combined, "sift daemon") || !strings.Contains(combined, "前台") {
		t.Fatalf("no-backend output lacks actionable foreground hint: %q", combined)
	}
}

// TestManagedConfigApplyFixtureDoesNotTouchOuterUnit is the issue #980 P0
// regression at the fixture level. The pre-fix fixture resolved UnitPath
// against the real OS home and then wrote "fixture" over it; this test seeds a
// sentinel at the outer HOME's unit location (the path the broken code would
// have hit) and runs the very fixture the target tests share. It must remain
// byte-identical afterwards — no real-user unit is ever written or deleted.
func TestManagedConfigApplyFixtureDoesNotTouchOuterUnit(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("managed hosting is not available on this platform")
	}
	// Simulate the caller's real HOME with a sentinel unit already in place,
	// exactly where hosting resolves it against the OS home/config dir.
	outer := filepath.Join(t.TempDir(), "outer-home")
	t.Setenv("HOME", outer)
	t.Setenv("XDG_CONFIG_HOME", "")
	var outerUnit string
	switch runtime.GOOS {
	case "darwin":
		outerUnit = filepath.Join(outer, "Library", "LaunchAgents", hosting.Label+".plist")
	default:
		outerUnit = filepath.Join(outer, ".config", "systemd", "user", hosting.ServiceName+".service")
	}
	if err := os.MkdirAll(filepath.Dir(outerUnit), 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := []byte("issue-980 sentinel " + time.Now().String() + "\n")
	if err := os.WriteFile(outerUnit, sentinel, 0o600); err != nil {
		t.Fatal(err)
	}

	// Run the target fixture. It re-isolates HOME to its own test-owned root,
	// so its unit write must land there, never in outer.
	_, spec, _ := managedConfigApplyFixture(t)

	// Fail-closed: the fixture's own UnitPath must never resolve under the
	// caller's HOME — if it did, the pre-fix code would have written to outerUnit.
	if !filepath.IsAbs(spec.UnitPath) || pathWithin(outer, spec.UnitPath) {
		t.Fatalf("fixture UnitPath %q resolved under the caller HOME %q", spec.UnitPath, outer)
	}
	after, err := os.ReadFile(outerUnit)
	if err != nil {
		t.Fatalf("outer unit vanished: %v", err)
	}
	if !bytes.Equal(sentinel, after) {
		t.Fatalf("outer unit was modified by the fixture: before=%q after=%q", sentinel, after)
	}
}

func mustHome(t *testing.T, path string) (home config.Home) {
	t.Helper()
	resolved, err := config.ResolveHome()
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Path != path {
		t.Fatalf("resolved home = %q, want %q", resolved.Path, path)
	}
	return resolved
}
