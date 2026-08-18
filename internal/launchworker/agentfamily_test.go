package launchworker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xsift/sift/internal/agentfamily"
	"github.com/xsift/sift/internal/config"
)

// Issue #1024: an agent referencing a family resolves its model/thinking
// overrides into extra argv and its captured secrets file into launch_env
// before the bootstrap the wrapper execs is ever written.

func TestRunOnceResolvesFamilyOverridesIntoBootstrap(t *testing.T) {
	root, db, boot, now := qualificationLaunchFixture(t)
	agentPath := filepath.Join(root, "agent")
	if err := os.WriteFile(agentPath, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	secretsDir := t.TempDir()
	if err := agentfamily.WriteSecretsFile(agentfamily.SecretsFilePath(secretsDir, "agent"), map[string]string{"ANTHROPIC_API_KEY": "sk-test"}); err != nil {
		t.Fatal(err)
	}
	host := &recordingBackend{}
	worker := &Worker{
		DB: db, BootID: boot, WorkerID: "family", Root: root, Lease: time.Minute,
		Now:      func() time.Time { return now.Add(2 * time.Millisecond) },
		Backends: BackendRouter{config.BackendProcess: host},
		Agents: []config.Agent{{
			ID: "agent", Executable: agentPath, Args: []string{"-p"}, TaskTransport: config.TaskTransportStdin,
			Family: "claude", Model: "opus",
		}},
		Families:   agentfamily.Builtin(),
		SecretsDir: secretsDir,
	}
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(host.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(host.calls))
	}
	var bootstrap struct {
		Agent struct {
			Args      []string          `json:"args"`
			LaunchEnv map[string]string `json:"launch_env"`
		} `json:"agent"`
	}
	if err := json.Unmarshal(host.calls[0].contents, &bootstrap); err != nil {
		t.Fatal(err)
	}
	wantArgs := []string{"-p", "--model", "opus"}
	if len(bootstrap.Agent.Args) != len(wantArgs) {
		t.Fatalf("args = %v, want %v", bootstrap.Agent.Args, wantArgs)
	}
	for i, a := range wantArgs {
		if bootstrap.Agent.Args[i] != a {
			t.Fatalf("args = %v, want %v", bootstrap.Agent.Args, wantArgs)
		}
	}
	if bootstrap.Agent.LaunchEnv["ANTHROPIC_API_KEY"] != "sk-test" {
		t.Fatalf("launch_env = %v", bootstrap.Agent.LaunchEnv)
	}
}

func TestRunOnceLiveConfigOverridesFrozenSecrets(t *testing.T) {
	root, db, boot, now := qualificationLaunchFixture(t)
	agentPath := filepath.Join(root, "agent")
	if err := os.WriteFile(agentPath, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	settings := []byte(`{"env":{"ANTHROPIC_API_KEY":"sk-live","ANTHROPIC_BASE_URL":"https://relay.example.com"}}`)
	if err := os.WriteFile(filepath.Join(home, ".claude", "settings.json"), settings, 0o600); err != nil {
		t.Fatal(err)
	}
	secretsDir := t.TempDir()
	if err := agentfamily.WriteSecretsFile(agentfamily.SecretsFilePath(secretsDir, "agent"), map[string]string{"ANTHROPIC_API_KEY": "sk-stale"}); err != nil {
		t.Fatal(err)
	}
	host := &recordingBackend{}
	worker := &Worker{
		DB: db, BootID: boot, WorkerID: "family-live", Root: root, Lease: time.Minute,
		Now:      func() time.Time { return now.Add(2 * time.Millisecond) },
		Backends: BackendRouter{config.BackendProcess: host},
		Agents: []config.Agent{{
			ID: "agent", Executable: agentPath, TaskTransport: config.TaskTransportStdin,
			Family: "claude", LaunchEnv: map[string]string{"HOME": home},
		}},
		Families:   agentfamily.Builtin(),
		SecretsDir: secretsDir,
	}
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(host.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(host.calls))
	}
	var bootstrap struct {
		Agent struct {
			LaunchEnv map[string]string `json:"launch_env"`
		} `json:"agent"`
	}
	if err := json.Unmarshal(host.calls[0].contents, &bootstrap); err != nil {
		t.Fatal(err)
	}
	if bootstrap.Agent.LaunchEnv["ANTHROPIC_API_KEY"] != "sk-live" {
		t.Fatalf("launch_env = %v, want live settings.json to win", bootstrap.Agent.LaunchEnv)
	}
	if bootstrap.Agent.LaunchEnv["ANTHROPIC_BASE_URL"] != "https://relay.example.com" {
		t.Fatalf("launch_env = %v", bootstrap.Agent.LaunchEnv)
	}
}

func TestRunOnceUnknownFamilyFailsClosed(t *testing.T) {
	root, db, boot, now := qualificationLaunchFixture(t)
	agentPath := filepath.Join(root, "agent")
	if err := os.WriteFile(agentPath, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	host := &recordingBackend{}
	worker := &Worker{
		DB: db, BootID: boot, WorkerID: "family-ghost", Root: root, Lease: time.Minute,
		Now:      func() time.Time { return now.Add(2 * time.Millisecond) },
		Backends: BackendRouter{config.BackendProcess: host},
		Agents: []config.Agent{{
			ID: "agent", Executable: agentPath, TaskTransport: config.TaskTransportStdin, Family: "ghost",
		}},
		Families: agentfamily.Builtin(),
	}
	if err := worker.RunOnce(context.Background()); err == nil {
		t.Fatal("expected fail-closed error for unresolvable family")
	}
	if len(host.calls) != 0 {
		t.Fatalf("backend spawned despite unresolvable family: %#v", host.calls)
	}
}
