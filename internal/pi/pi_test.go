package pi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeRunner struct {
	path map[string]string
	runs [][]string
}

func (f *fakeRunner) LookPath(name string) (string, error) {
	if p, ok := f.path[name]; ok {
		return p, nil
	}
	return "", os.ErrNotExist
}

func (f *fakeRunner) Run(name string, args ...string) ([]byte, error) {
	f.runs = append(f.runs, append([]string{name}, args...))
	return []byte("ok"), nil
}

func TestEnsureSkillIdempotent(t *testing.T) {
	home := t.TempDir()
	first, err := EnsureSkill(home)
	if err != nil {
		t.Fatalf("EnsureSkill first: %v", err)
	}
	want := filepath.Join(home, SkillDirRel, SkillName, "SKILL.md")
	if first != want {
		t.Fatalf("skill path = %q, want %q", first, want)
	}
	data, err := os.ReadFile(first)
	if err != nil {
		t.Fatalf("read skill: %v", err)
	}
	if !strings.Contains(string(data), "name: sift") {
		t.Fatalf("skill missing frontmatter name")
	}
	if !strings.Contains(string(data), "sift ps") {
		t.Fatalf("skill missing core command reference")
	}
	// Idempotent: second run leaves the file untouched (same mtime/content).
	before, _ := os.Stat(first)
	second, err := EnsureSkill(home)
	if err != nil {
		t.Fatalf("EnsureSkill second: %v", err)
	}
	if second != first {
		t.Fatalf("second path = %q, want %q", second, first)
	}
	after, _ := os.Stat(first)
	if after.ModTime() != before.ModTime() {
		t.Fatalf("idempotent ensure rewrote the file")
	}
}

func TestRunSessionMissingPi(t *testing.T) {
	r := &fakeRunner{path: map[string]string{}}
	err := RunSession(r, "", nil)
	if _, ok := err.(PiMissingError); !ok {
		t.Fatalf("RunSession with missing pi = %v, want PiMissingError", err)
	}
}

func TestContextSnapshotDegrades(t *testing.T) {
	got := ContextSnapshot("/tmp/demo", func() string { return "" })
	if !strings.Contains(got, "当前目录：/tmp/demo") {
		t.Fatalf("snapshot missing cwd: %q", got)
	}
	if !strings.Contains(got, "daemon 不可达") {
		t.Fatalf("snapshot should degrade on empty ps: %q", got)
	}
}

func TestContextSnapshotIncludesPS(t *testing.T) {
	got := ContextSnapshot("/tmp/demo", func() string { return "run-1  running  #42 fix docs\n" })
	if !strings.Contains(got, "run-1") || !strings.Contains(got, "running") {
		t.Fatalf("snapshot should include ps output: %q", got)
	}
}
