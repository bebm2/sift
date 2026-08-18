package agentfamily

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveArgsNoFamilyIsNoop(t *testing.T) {
	got, err := ResolveArgs(Builtin(), LaunchOverrides{}, []string{"-p"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "-p" {
		t.Fatalf("got = %v", got)
	}
}

func TestResolveArgsUnknownFamilyFailsClosed(t *testing.T) {
	if _, err := ResolveArgs(Builtin(), LaunchOverrides{FamilyID: "ghost"}, []string{"-p"}); err == nil {
		t.Fatal("expected error for unknown family")
	}
}

func TestResolveArgsAppendsModelAndThinkingInFixedOrder(t *testing.T) {
	got, err := ResolveArgs(Builtin(), LaunchOverrides{FamilyID: "pi", Model: "opus", Thinking: "high"}, []string{"-p"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-p", "--model", "opus", "--thinking", "high"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got = %v, want %v", got, want)
	}
}

func TestResolveArgsUnsupportedOverrideFailsClosed(t *testing.T) {
	if _, err := ResolveArgs(Builtin(), LaunchOverrides{FamilyID: "claude", Thinking: "high"}, []string{"-p"}); err == nil {
		t.Fatal("expected error: claude family does not declare a thinking flag")
	}
}

func TestResolveArgsDoesNotMutateBaseArgs(t *testing.T) {
	base := []string{"-p"}
	if _, err := ResolveArgs(Builtin(), LaunchOverrides{FamilyID: "pi", Model: "opus"}, base); err != nil {
		t.Fatal(err)
	}
	if len(base) != 1 || base[0] != "-p" {
		t.Fatalf("base mutated: %v", base)
	}
}

func TestResolveLaunchEnvNoSecretsDirReturnsBase(t *testing.T) {
	base := map[string]string{"HOME": "/h"}
	got, err := ResolveLaunchEnv(nil, "", "agent", base)
	if err != nil {
		t.Fatal(err)
	}
	if got["HOME"] != "/h" || len(got) != 1 {
		t.Fatalf("got = %v", got)
	}
}

func TestResolveLaunchEnvMissingFileReturnsBaseUnchanged(t *testing.T) {
	dir := t.TempDir()
	base := map[string]string{"HOME": "/h"}
	got, err := ResolveLaunchEnv(nil, dir, "agent", base)
	if err != nil {
		t.Fatal(err)
	}
	if got["HOME"] != "/h" || len(got) != 1 {
		t.Fatalf("got = %v", got)
	}
}

func TestResolveLaunchEnvMergesSecretsOverBase(t *testing.T) {
	dir := t.TempDir()
	if err := WriteSecretsFile(filepath.Join(dir, "agent.env"), map[string]string{"ANTHROPIC_API_KEY": "sk-test"}); err != nil {
		t.Fatal(err)
	}
	base := map[string]string{"HOME": "/h"}
	got, err := ResolveLaunchEnv(nil, dir, "agent", base)
	if err != nil {
		t.Fatal(err)
	}
	if got["HOME"] != "/h" || got["ANTHROPIC_API_KEY"] != "sk-test" || len(got) != 2 {
		t.Fatalf("got = %v", got)
	}
	if base["ANTHROPIC_API_KEY"] != "" || len(base) != 1 {
		t.Fatalf("base mutated: %v", base)
	}
}

func TestResolveLaunchEnvPropagatesMalformedFileError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.env")
	if err := os.WriteFile(path, []byte("malformed-line-without-equals\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveLaunchEnv(nil, dir, "agent", nil); err == nil {
		t.Fatal("expected malformed secrets file to error")
	}
}

func writeClaudeSettings(t *testing.T, home, envJSON string) {
	t.Helper()
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"env":`+envJSON+`}`), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestResolveLaunchEnvLiveConfigOverridesSnapshot(t *testing.T) {
	home := t.TempDir()
	writeClaudeSettings(t, home, `{"ANTHROPIC_API_KEY":"sk-live","ANTHROPIC_BASE_URL":"https://relay.example.com"}`)
	secretsDir := t.TempDir()
	if err := WriteSecretsFile(filepath.Join(secretsDir, "agent.env"), map[string]string{
		"ANTHROPIC_API_KEY": "sk-stale",
		"ANTHROPIC_MODEL":   "opus",
	}); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveLaunchEnv(Builtin()["claude"], secretsDir, "agent", map[string]string{"HOME": home})
	if err != nil {
		t.Fatal(err)
	}
	if got["ANTHROPIC_API_KEY"] != "sk-live" {
		t.Fatalf("live settings.json must win over snapshot, got %q", got["ANTHROPIC_API_KEY"])
	}
	if got["ANTHROPIC_BASE_URL"] != "https://relay.example.com" {
		t.Fatalf("live BASE_URL = %q", got["ANTHROPIC_BASE_URL"])
	}
	if got["ANTHROPIC_MODEL"] != "opus" {
		t.Fatalf("names absent from settings.json must keep the snapshot, got %q", got["ANTHROPIC_MODEL"])
	}
	if got["HOME"] != home {
		t.Fatalf("HOME = %q", got["HOME"])
	}
}

func TestResolveLaunchEnvLiveConfigIgnoresUndeclaredKeys(t *testing.T) {
	home := t.TempDir()
	writeClaudeSettings(t, home, `{"ANTHROPIC_API_KEY":"sk-live","EDITOR":"vim"}`)
	got, err := ResolveLaunchEnv(Builtin()["claude"], "", "agent", map[string]string{"HOME": home})
	if err != nil {
		t.Fatal(err)
	}
	if got["ANTHROPIC_API_KEY"] != "sk-live" {
		t.Fatalf("declared key missing: %v", got)
	}
	if _, ok := got["EDITOR"]; ok {
		t.Fatalf("undeclared settings.json env key must not be injected: %v", got)
	}
}

func TestResolveLaunchEnvLiveConfigMissingFileKeepsSnapshot(t *testing.T) {
	home := t.TempDir()
	secretsDir := t.TempDir()
	if err := WriteSecretsFile(filepath.Join(secretsDir, "agent.env"), map[string]string{"ANTHROPIC_API_KEY": "sk-snap"}); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveLaunchEnv(Builtin()["claude"], secretsDir, "agent", map[string]string{"HOME": home})
	if err != nil {
		t.Fatal(err)
	}
	if got["ANTHROPIC_API_KEY"] != "sk-snap" {
		t.Fatalf("got = %v", got)
	}
}

func TestResolveLaunchEnvSkipsNonJSONConfigFiles(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("model = \"gpt-5\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveLaunchEnv(Builtin()["codex"], "", "agent", map[string]string{"HOME": home})
	if err != nil {
		t.Fatalf("toml config file must be skipped, not fail-closed: %v", err)
	}
	if got["HOME"] != home || len(got) != 1 {
		t.Fatalf("got = %v", got)
	}
}

func TestResolveLaunchEnvLiveConfigSkipsTildeWithoutHome(t *testing.T) {
	got, err := ResolveLaunchEnv(Builtin()["claude"], "", "agent", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got = %v", got)
	}
}

func TestResolveLaunchEnvLiveConfigMalformedJSONFailsClosed(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveLaunchEnv(Builtin()["claude"], "", "agent", map[string]string{"HOME": home}); err == nil {
		t.Fatal("expected malformed settings.json to fail closed")
	}
}
