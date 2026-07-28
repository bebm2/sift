package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveHomeEnvWins(t *testing.T) {
	t.Setenv("SIFT_HOME", "/opt/sift")
	home, err := ResolveHomeWith(func() (string, error) { return "/should/not/use", nil })
	if err != nil {
		t.Fatal(err)
	}
	if home.Path != "/opt/sift" {
		t.Fatalf("Path = %q, want /opt/sift", home.Path)
	}
}

func TestResolveHomeEnvMustBeAbsolute(t *testing.T) {
	t.Setenv("SIFT_HOME", "relative/path")
	if _, err := ResolveHomeWith(func() (string, error) { return "/h", nil }); err == nil {
		t.Fatal("relative SIFT_HOME must be rejected")
	}
}

func TestResolveHomeDefaultUserDir(t *testing.T) {
	t.Setenv("SIFT_HOME", "")
	home, err := ResolveHomeWith(func() (string, error) { return "/users/me", nil })
	if err != nil {
		t.Fatal(err)
	}
	if home.Path != "/users/me/.sift" {
		t.Fatalf("Path = %q, want /users/me/.sift", home.Path)
	}
}

func TestResolveHomeUserDirUnobtainable(t *testing.T) {
	t.Setenv("SIFT_HOME", "")
	if _, err := ResolveHomeWith(func() (string, error) { return "", os.ErrNotExist }); err == nil {
		t.Fatal("unobtainable home must error")
	}
}

func TestEnsureHomeLayoutCreatesDir(t *testing.T) {
	root := t.TempDir()
	home := Home{Path: filepath.Join(root, "nested", "sift")}
	if err := EnsureHomeLayout(home); err != nil {
		t.Fatalf("create: %v", err)
	}
	info, err := os.Stat(home.Path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o077 != 0 {
		t.Fatalf("created home mode too open: %s", info.Mode())
	}
}

func TestEnsureHomeLayoutRejectsWideDir(t *testing.T) {
	home := Home{Path: t.TempDir()}
	if err := os.Chmod(home.Path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := EnsureHomeLayout(home); err == nil {
		t.Fatal("group-readable home must be rejected")
	}
}

func TestEnsureHomeLayoutRejectsNonDir(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "notadir")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureHomeLayout(Home{Path: file}); err == nil {
		t.Fatal("non-directory home must be rejected")
	}
}

func TestEnsureHomeLayoutRejectsWideConfig(t *testing.T) {
	home := tempHome(t)
	cfg := ConfigPath(home)
	if err := os.WriteFile(cfg, []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureHomeLayout(home); err == nil {
		t.Fatal("group-readable config.yaml must be rejected")
	}
}

func TestEnsureHomeLayoutAcceptsOwnerOnlyConfig(t *testing.T) {
	home := tempHome(t)
	writeConfig(t, home, "version: 1\n")
	if err := EnsureHomeLayout(home); err != nil {
		t.Fatalf("owner-only config must be accepted: %v", err)
	}
}
