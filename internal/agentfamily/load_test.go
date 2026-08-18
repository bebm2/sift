package agentfamily

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFamilyFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestLoadUserDirMissingDirIsEmpty(t *testing.T) {
	got, err := LoadUserDir(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("LoadUserDir: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("LoadUserDir = %v, want empty", got)
	}
}

func TestLoadUserDirParsesFiles(t *testing.T) {
	dir := t.TempDir()
	writeFamilyFile(t, dir, "acme.yaml", "id: acme\nmatch: [acme]\nrun:\n  args: [\"-p\"]\n")

	got, err := LoadUserDir(dir)
	if err != nil {
		t.Fatalf("LoadUserDir: %v", err)
	}
	if _, ok := got["acme"]; !ok {
		t.Fatalf("LoadUserDir = %v, want acme", got)
	}
}

func TestLoadUserDirRejectsInvalidFile(t *testing.T) {
	dir := t.TempDir()
	writeFamilyFile(t, dir, "bad.yaml", "match: [acme]\nrun:\n  args: [\"-p\"]\n")

	if _, err := LoadUserDir(dir); err == nil {
		t.Fatal("LoadUserDir: want error for missing id, got nil")
	}
}

func TestLoadUserDirRejectsDuplicateIDAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	writeFamilyFile(t, dir, "a.yaml", "id: acme\nmatch: [a]\nrun:\n  args: [\"-p\"]\n")
	writeFamilyFile(t, dir, "b.yaml", "id: acme\nmatch: [b]\nrun:\n  args: [\"-p\"]\n")

	if _, err := LoadUserDir(dir); err == nil {
		t.Fatal("LoadUserDir: want error for duplicate id across files, got nil")
	}
}

func TestLoadUserDirIgnoresNonYAMLFiles(t *testing.T) {
	dir := t.TempDir()
	writeFamilyFile(t, dir, "readme.txt", "not a family")
	writeFamilyFile(t, dir, "acme.yaml", "id: acme\nmatch: [acme]\nrun:\n  args: [\"-p\"]\n")

	got, err := LoadUserDir(dir)
	if err != nil {
		t.Fatalf("LoadUserDir: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("LoadUserDir = %v, want exactly acme", got)
	}
}

func TestLoadEmptyUserDirReturnsBuiltinOnly(t *testing.T) {
	got, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != len(Builtin()) {
		t.Fatalf("Load(\"\") = %d families, want %d (builtin only)", len(got), len(Builtin()))
	}
}

func TestLoadOverridesBuiltinByID(t *testing.T) {
	dir := t.TempDir()
	writeFamilyFile(t, dir, "claude.yaml", "id: claude\nmatch: [claude, claude-custom]\nrun:\n  args: [\"-p\", \"--custom\"]\n")

	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	f, ok := got["claude"]
	if !ok {
		t.Fatal("Load: missing claude after override")
	}
	if len(f.Match) != 2 || f.Match[1] != "claude-custom" {
		t.Errorf("Load: overridden claude.Match = %v", f.Match)
	}
	if len(got) != len(Builtin()) {
		t.Fatalf("Load: got %d families, want %d (override replaces, does not add)", len(got), len(Builtin()))
	}
}

func TestLoadAddsNewFamilyFromUserDir(t *testing.T) {
	dir := t.TempDir()
	writeFamilyFile(t, dir, "acme.yaml", "id: acme\nmatch: [acme]\nrun:\n  args: [\"-p\"]\n")

	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := got["acme"]; !ok {
		t.Fatal("Load: missing new family acme from user dir")
	}
	if len(got) != len(Builtin())+1 {
		t.Fatalf("Load: got %d families, want %d (builtin + acme)", len(got), len(Builtin())+1)
	}
}
