package wrapper

import (
	"encoding/json"
	"os"
	osexec "os/exec"
	"path/filepath"
	"testing"

	"github.com/xsift/sift/internal/controlplane"
	runtimepkg "github.com/xsift/sift/internal/runtime"
)

// TestProductionWrapperInjectsFamilySecretsIntoAgentEnv is the last-mile E2E
// check for issue #1024: internal/launchworker resolves a family's captured
// auth/config env into BootstrapAgent.LaunchEnv (see
// internal/launchworker/agentfamily_test.go for that step), and this test
// proves the real production sift-agent-wrapper binary, exec'ing a real OS
// process through the unmodified DirectLauncher, actually hands those
// values to the Agent's environment — not just to an in-memory struct.
//
// runtime.DirectLauncher.Start replaces the wrapper's own environment
// entirely (cmd.Env = ["SIFT_RUN_DIR=...", launch.Env...]), so this also
// confirms the credential-free sandbox model still holds: the Agent sees
// exactly SIFT_RUN_DIR plus whatever the frozen launch_env carries, nothing
// inherited from the wrapper's ambient environment.
func TestProductionWrapperInjectsFamilySecretsIntoAgentEnv(t *testing.T) {
	wrapperPath := buildWrapper(t)
	root := shortTempDir(t)
	runDir := filepath.Join(root, "runs", "run-1", "attempts", "1")
	if err := os.MkdirAll(runDir, 0700); err != nil {
		t.Fatal(err)
	}
	script := `printf '%s|%s' "$ANTHROPIC_API_KEY" "$ANTHROPIC_BASE_URL" > "$SIFT_RUN_DIR/captured-env"`
	bootstrapData, err := json.Marshal(runtimepkg.Bootstrap{
		SchemaVersion: 2, ProtocolMajor: controlplane.ProtocolMajor, ProtocolMinor: controlplane.ProtocolMinor,
		DaemonVersion: controlplane.Version, WrapperVersion: controlplane.Version,
		RunID: "run-1", AttemptNo: 1, Generation: 1, DispatchID: "dispatch",
		BootstrapNonce: "aaaaaaaaaaaaaaaa", RunToken: "bbbbbbbbbbbbbbbb",
		RunDir: runDir, WorktreePath: t.TempDir(),
		Agent: runtimepkg.BootstrapAgent{
			ID: "claude-code", Executable: "/bin/sh", Args: []string{"-c", script}, TaskTransport: "stdin",
			LaunchEnv: map[string]string{
				"ANTHROPIC_API_KEY":  "sk-e2e-test",
				"ANTHROPIC_BASE_URL": "https://relay.example.com",
			},
		},
		TaskSpecSnapshotID: "task-1", TaskSpec: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	bootstrap := filepath.Join(runDir, "bootstrap.json")
	if err := os.WriteFile(bootstrap, bootstrapData, 0600); err != nil {
		t.Fatal(err)
	}
	server := newWrapperServer(t, root, "")
	defer server.Close()

	out, err := osexec.Command(wrapperPath, bootstrap).CombinedOutput()
	if err != nil {
		t.Fatalf("wrapper failed: %v\n%s", err, out)
	}
	got := readFile(t, filepath.Join(runDir, "captured-env"))
	want := "sk-e2e-test|https://relay.example.com"
	if got != want {
		t.Fatalf("agent process env = %q, want %q", got, want)
	}
}
