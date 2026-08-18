package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xsift/sift/internal/agentfamily"
	"github.com/xsift/sift/internal/config"
)

func TestLoadSetupFamiliesDefaultsToBuiltin(t *testing.T) {
	home := config.Home{Path: t.TempDir()}
	families, err := loadSetupFamilies(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(families) != len(agentfamily.Builtin()) {
		t.Fatalf("loadSetupFamilies = %d families, want %d builtin", len(families), len(agentfamily.Builtin()))
	}
}

func TestLoadSetupFamiliesOverlaysUserDir(t *testing.T) {
	home := config.Home{Path: t.TempDir()}
	if err := os.MkdirAll(agentFamiliesDir(home), 0o700); err != nil {
		t.Fatal(err)
	}
	custom := "id: acme\nmatch: [acme]\nrun:\n  args: [\"-p\"]\n"
	if err := os.WriteFile(filepath.Join(agentFamiliesDir(home), "acme.yaml"), []byte(custom), 0o600); err != nil {
		t.Fatal(err)
	}
	families, err := loadSetupFamilies(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := families["acme"]; !ok {
		t.Fatalf("loadSetupFamilies missing user-defined acme family: %v", families)
	}
}

func TestWriteAgentSecretsCapturesPresentEnv(t *testing.T) {
	home := config.Home{Path: t.TempDir()}
	claude := agentfamily.Builtin()["claude"]
	lookup := func(name string) (string, bool) {
		if name == "ANTHROPIC_API_KEY" {
			return "sk-test", true
		}
		return "", false
	}
	if err := writeAgentSecrets(home, "claude-code", claude, lookup); err != nil {
		t.Fatal(err)
	}
	got, err := agentfamily.ReadSecretsFile(agentSecretsPath(home, "claude-code"))
	if err != nil {
		t.Fatal(err)
	}
	if got["ANTHROPIC_API_KEY"] != "sk-test" {
		t.Fatalf("secrets = %v", got)
	}
	info, err := os.Stat(agentSecretsPath(home, "claude-code"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestWriteAgentSecretsRemovesStaleFileWhenNothingPresent(t *testing.T) {
	home := config.Home{Path: t.TempDir()}
	claude := agentfamily.Builtin()["claude"]
	present := func(name string) (string, bool) { return "sk-test", true }
	if err := writeAgentSecrets(home, "claude-code", claude, present); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(agentSecretsPath(home, "claude-code")); err != nil {
		t.Fatalf("expected secrets file to exist before removal check: %v", err)
	}

	absent := func(string) (string, bool) { return "", false }
	if err := writeAgentSecrets(home, "claude-code", claude, absent); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(agentSecretsPath(home, "claude-code")); !os.IsNotExist(err) {
		t.Fatalf("expected stale secrets file removed, stat err = %v", err)
	}
}

func TestSyncAgentSecretsSkipsEntriesWithoutFamily(t *testing.T) {
	home := config.Home{Path: t.TempDir()}
	doc := map[string]any{"agents": []any{
		map[string]any{"id": "a", "executable": "e"},
	}}
	present := func(name string) (string, bool) { return "sk-test", true }
	if err := syncAgentSecrets(home, doc, agentfamily.Builtin(), present); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(agentSecretsPath(home, "a")); !os.IsNotExist(err) {
		t.Fatalf("expected no secrets file for agent without family, stat err = %v", err)
	}
}

func TestSyncAgentSecretsWritesForMatchedFamily(t *testing.T) {
	home := config.Home{Path: t.TempDir()}
	doc := map[string]any{"agents": []any{
		map[string]any{"id": "claude-code", "executable": "/usr/local/bin/claude", "family": "claude"},
	}}
	lookup := func(name string) (string, bool) {
		if name == "ANTHROPIC_BASE_URL" {
			return "https://relay.example.com", true
		}
		return "", false
	}
	if err := syncAgentSecrets(home, doc, agentfamily.Builtin(), lookup); err != nil {
		t.Fatal(err)
	}
	got, err := agentfamily.ReadSecretsFile(agentSecretsPath(home, "claude-code"))
	if err != nil {
		t.Fatal(err)
	}
	if got["ANTHROPIC_BASE_URL"] != "https://relay.example.com" {
		t.Fatalf("secrets = %v", got)
	}
}

// TestInitCapturesRelayEnvIntoSecretsFileNotConfig is the issue #1024
// end-to-end acceptance: a relay ANTHROPIC_BASE_URL/ANTHROPIC_API_KEY set in
// the init shell must reach a 0600 secrets file, and config.yaml must not
// contain the value.
func TestInitCapturesRelayEnvIntoSecretsFileNotConfig(t *testing.T) {
	home := initTestRepo(t)
	gitOnlyPATH(t)
	replaceSetupCmd(t, &fakeCommand{})

	fake := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANTHROPIC_API_KEY", "sk-relay-test")
	t.Setenv("ANTHROPIC_BASE_URL", "https://relay.example.com")

	var out bytes.Buffer
	code := runWithInput([]string{"sift", "init", "--offline", "--agent", fake}, strings.NewReader(""), &out, io.Discard)
	if code != 0 {
		t.Fatalf("init = %d: %s", code, out.String())
	}

	raw, err := os.ReadFile(config.ConfigPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "sk-relay-test") {
		t.Fatalf("config.yaml must never contain the captured secret: %s", raw)
	}
	if !strings.Contains(string(raw), "family: claude") {
		t.Fatalf("config.yaml missing family: claude: %s", raw)
	}

	secrets, err := agentfamily.ReadSecretsFile(agentSecretsPath(home, "claude"))
	if err != nil {
		t.Fatalf("ReadSecretsFile: %v", err)
	}
	if secrets["ANTHROPIC_API_KEY"] != "sk-relay-test" {
		t.Fatalf("secrets[ANTHROPIC_API_KEY] = %q", secrets["ANTHROPIC_API_KEY"])
	}
	if secrets["ANTHROPIC_BASE_URL"] != "https://relay.example.com" {
		t.Fatalf("secrets[ANTHROPIC_BASE_URL] = %q", secrets["ANTHROPIC_BASE_URL"])
	}
	info, err := os.Stat(agentSecretsPath(home, "claude"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("secrets file mode = %v, want 0600", info.Mode().Perm())
	}
}
