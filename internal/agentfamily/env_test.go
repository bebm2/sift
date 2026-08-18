package agentfamily

import (
	"os"
	"path/filepath"
	"testing"
)

func lookupFrom(env map[string]string) Lookup {
	return func(name string) (string, bool) {
		v, ok := env[name]
		return v, ok
	}
}

func TestSnapshotEnvSplitsSecretAndNonSecret(t *testing.T) {
	f := &Family{
		ID: "claude", Match: []string{"claude"},
		Run:    RunSpec{Args: []string{"-p"}},
		Auth:   AuthSpec{Env: []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"}},
		Config: ConfigSpec{Env: []string{"ANTHROPIC_BASE_URL"}},
	}
	env := map[string]string{
		"ANTHROPIC_API_KEY":  "sk-secret",
		"ANTHROPIC_BASE_URL": "https://relay.example.com",
		"UNRELATED":          "ignored",
	}
	snap := SnapshotEnv(f, lookupFrom(env))
	if snap.Secrets["ANTHROPIC_API_KEY"] != "sk-secret" {
		t.Errorf("Secrets[ANTHROPIC_API_KEY] = %q", snap.Secrets["ANTHROPIC_API_KEY"])
	}
	if _, ok := snap.Secrets["ANTHROPIC_AUTH_TOKEN"]; ok {
		t.Error("Secrets contains ANTHROPIC_AUTH_TOKEN, want absent (not set)")
	}
	if snap.NonSecret["ANTHROPIC_BASE_URL"] != "https://relay.example.com" {
		t.Errorf("NonSecret[ANTHROPIC_BASE_URL] = %q", snap.NonSecret["ANTHROPIC_BASE_URL"])
	}
	if _, ok := snap.Secrets["UNRELATED"]; ok {
		t.Error("Secrets contains UNRELATED, want only declared names captured")
	}
}

func TestSnapshotEnvSkipsEmptyValue(t *testing.T) {
	f := &Family{ID: "claude", Match: []string{"claude"}, Run: RunSpec{Args: []string{"-p"}}, Auth: AuthSpec{Env: []string{"ANTHROPIC_API_KEY"}}}
	snap := SnapshotEnv(f, lookupFrom(map[string]string{"ANTHROPIC_API_KEY": ""}))
	if _, ok := snap.Secrets["ANTHROPIC_API_KEY"]; ok {
		t.Error("Secrets contains empty-valued key, want skipped")
	}
}

func TestWriteReadSecretsFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.env")
	in := map[string]string{"ANTHROPIC_API_KEY": "sk-abc", "ANTHROPIC_BASE_URL": "https://x"}
	if err := WriteSecretsFile(path, in); err != nil {
		t.Fatalf("WriteSecretsFile: %v", err)
	}
	out, err := ReadSecretsFile(path)
	if err != nil {
		t.Fatalf("ReadSecretsFile: %v", err)
	}
	if out["ANTHROPIC_API_KEY"] != "sk-abc" || out["ANTHROPIC_BASE_URL"] != "https://x" {
		t.Errorf("ReadSecretsFile = %v", out)
	}
}

func TestWriteSecretsFileMode0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.env")
	if err := WriteSecretsFile(path, map[string]string{"K": "v"}); err != nil {
		t.Fatalf("WriteSecretsFile: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestWriteSecretsFileRejectsNewlineInValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.env")
	err := WriteSecretsFile(path, map[string]string{"K": "line1\nline2"})
	if err == nil {
		t.Fatal("WriteSecretsFile: want error for newline in value, got nil")
	}
}

func TestReadSecretsFileRejectsLineWithoutEquals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.env")
	if err := os.WriteFile(path, []byte("NOEQUALSHERE\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := ReadSecretsFile(path); err == nil {
		t.Fatal("ReadSecretsFile: want error for line without '=', got nil")
	}
}

func TestWriteSecretsFileDeterministicOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.env")
	in := map[string]string{"B": "2", "A": "1", "C": "3"}
	if err := WriteSecretsFile(path, in); err != nil {
		t.Fatalf("WriteSecretsFile: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	want := "A=1\nB=2\nC=3\n"
	if string(data) != want {
		t.Errorf("content = %q, want %q", string(data), want)
	}
}
