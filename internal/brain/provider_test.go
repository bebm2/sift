package brain

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Real-CLI shell tests (brain.md §10.1/§10.9): the fixture CLI is this test
// binary re-executed as TestHelperProcess, driven by a JSON fixture file.
// No live model is involved.

// cliFixture scripts the helper process. Behaviors are consumed by
// invocation index persisted in a state file, so a retry sequence
// (invalid → valid) can be scripted deterministically.
type cliFixture struct {
	Behaviors []cliBehavior `json:"behaviors"`
}

type cliBehavior struct {
	Stdout      string `json:"stdout"`
	Stderr      string `json:"stderr"`
	ExitCode    int    `json:"exit_code"`
	SleepMS     int    `json:"sleep_ms"`
	StdoutBytes int    `json:"stdout_bytes"` // emit this many 'x' bytes instead of Stdout
}

func writeFixture(t *testing.T, behaviors ...cliBehavior) (fixturePath, statePath, captureDir string) {
	t.Helper()
	dir := t.TempDir()
	fx, err := json.Marshal(cliFixture{Behaviors: behaviors})
	if err != nil {
		t.Fatal(err)
	}
	fixturePath = filepath.Join(dir, "fixture.json")
	if err := os.WriteFile(fixturePath, fx, 0o600); err != nil {
		t.Fatal(err)
	}
	statePath = filepath.Join(dir, "state")
	captureDir = filepath.Join(dir, "capture")
	if err := os.Mkdir(captureDir, 0o700); err != nil {
		t.Fatal(err)
	}
	return fixturePath, statePath, captureDir
}

// fixtureProvider returns a SubprocessProvider invoking this test binary as
// the fake CLI, plus the directory where the helper captures stdin/cwd.
func fixtureProvider(t *testing.T, fixturePath, statePath, captureDir string) SubprocessProvider {
	t.Helper()
	return SubprocessProvider{
		Executable: os.Args[0],
		Args:       []string{"-test.run=^TestHelperProcess$", "--"},
		EnvAllowlist: []string{
			"SIFT_FAKE_CLI", "SIFT_FAKE_CLI_FIXTURE", "SIFT_FAKE_CLI_STATE", "SIFT_FAKE_CLI_CAPTURE",
		},
	}
}

// helperEnv returns the environment the helper needs; the parent test
// process sets these via t.Setenv so the allowlisted variables exist to be
// inherited.
func setHelperEnv(t *testing.T, fixturePath, statePath, captureDir string) {
	t.Helper()
	t.Setenv("SIFT_FAKE_CLI", "1")
	t.Setenv("SIFT_FAKE_CLI_FIXTURE", fixturePath)
	t.Setenv("SIFT_FAKE_CLI_STATE", statePath)
	t.Setenv("SIFT_FAKE_CLI_CAPTURE", captureDir)
}

// TestHelperProcess is the fake provider CLI. It reads the prompt from
// stdin (capturing it for assertions), records its working directory, then
// performs the scripted behavior for this invocation index.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("SIFT_FAKE_CLI") != "1" {
		return
	}
	fixtureData, err := os.ReadFile(os.Getenv("SIFT_FAKE_CLI_FIXTURE"))
	if err != nil {
		os.Exit(90)
	}
	var fx cliFixture
	if err := json.Unmarshal(fixtureData, &fx); err != nil || len(fx.Behaviors) == 0 {
		os.Exit(91)
	}
	statePath := os.Getenv("SIFT_FAKE_CLI_STATE")
	idx := 0
	if raw, err := os.ReadFile(statePath); err == nil {
		_ = json.Unmarshal(raw, &idx)
	}
	_ = os.WriteFile(statePath, []byte(jsonNumber(idx+1)), 0o600)
	if idx >= len(fx.Behaviors) {
		idx = len(fx.Behaviors) - 1
	}
	b := fx.Behaviors[idx]

	stdin, _ := io.ReadAll(os.Stdin)
	capture := os.Getenv("SIFT_FAKE_CLI_CAPTURE")
	if capture != "" {
		_ = os.WriteFile(filepath.Join(capture, "stdin."+jsonNumber(idx)), stdin, 0o600)
		wd, _ := os.Getwd()
		entries, _ := os.ReadDir(wd)
		_ = os.WriteFile(filepath.Join(capture, "cwd."+jsonNumber(idx)),
			[]byte(fmt.Sprintf("%s\n%d", wd, len(entries))), 0o600)
	}

	if b.SleepMS > 0 {
		time.Sleep(time.Duration(b.SleepMS) * time.Millisecond)
	}
	if b.StdoutBytes > 0 {
		_, _ = os.Stdout.Write(make([]byte, b.StdoutBytes))
	} else {
		_, _ = os.Stdout.WriteString(b.Stdout)
	}
	_, _ = os.Stderr.WriteString(b.Stderr)
	os.Exit(b.ExitCode)
}

func jsonNumber(n int) string {
	raw, _ := json.Marshal(n)
	return string(raw)
}

func readStdinCapture(t *testing.T, captureDir string, idx int) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(captureDir, "stdin."+jsonNumber(idx)))
	if err != nil {
		t.Fatalf("read stdin capture: %v", err)
	}
	return raw
}

func TestSubprocessProviderValid(t *testing.T) {
	fx, st, cap := writeFixture(t, cliBehavior{Stdout: string(FakeEnvelope(`{"ok":true}`, 3, 2))})
	setHelperEnv(t, fx, st, cap)
	p := fixtureProvider(t, fx, st, cap)

	prompt := []byte("the prompt bytes")
	res := p.Call(context.Background(), ExecRequest{Prompt: prompt, Timeout: 10 * time.Second, MaxOutputBytes: 1 << 20})
	if res.SpawnErr != nil || res.TimedOut || res.StdoutTruncated {
		t.Fatalf("res = %+v", res)
	}
	if res.ExitCode == nil || *res.ExitCode != 0 {
		t.Fatalf("exit = %v", res.ExitCode)
	}
	text, in, out, err := ParseEnvelope(res.Stdout)
	if err != nil || string(text) != `{"ok":true}` || in != 3 || out != 2 {
		t.Fatalf("envelope = %q %d %d %v", text, in, out, err)
	}
	if got := readStdinCapture(t, cap, 0); string(got) != string(prompt) {
		t.Fatalf("stdin = %q, want exact prompt bytes", got)
	}
	// cwd was an empty temp directory, not the repo.
	cwdRec, err := os.ReadFile(filepath.Join(cap, "cwd.0"))
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.SplitN(string(cwdRec), "\n", 2)
	if len(parts) != 2 || parts[1] != "0" {
		t.Fatalf("work dir not empty at exec time: %q", cwdRec)
	}
	if !strings.Contains(parts[0], "sift-brain-") {
		t.Fatalf("work dir = %q, want sift-brain-* temp dir", parts[0])
	}
}

func TestSubprocessProviderSequence(t *testing.T) {
	// invalid → valid across two invocations of the same fixture (§10.1).
	fx, st, cap := writeFixture(t,
		cliBehavior{Stdout: string(FakeEnvelope(`{"ok":`, 1, 1))}, // malformed inner
		cliBehavior{Stdout: string(FakeEnvelope(`{"ok":true}`, 2, 2))},
	)
	setHelperEnv(t, fx, st, cap)
	p := fixtureProvider(t, fx, st, cap)

	first := p.Call(context.Background(), ExecRequest{Prompt: []byte("p"), Timeout: 10 * time.Second, MaxOutputBytes: 1 << 20})
	second := p.Call(context.Background(), ExecRequest{Prompt: []byte("p"), Timeout: 10 * time.Second, MaxOutputBytes: 1 << 20})
	if string(first.Stdout) == string(second.Stdout) {
		t.Fatal("sequence did not advance")
	}
	if _, _, _, err := ParseEnvelope(second.Stdout); err != nil {
		t.Fatalf("second: %v", err)
	}
}

func TestSubprocessProviderFailures(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		fx, st, cap := writeFixture(t, cliBehavior{SleepMS: 500, Stdout: "{}"})
		setHelperEnv(t, fx, st, cap)
		p := fixtureProvider(t, fx, st, cap)
		res := p.Call(context.Background(), ExecRequest{Prompt: []byte("p"), Timeout: 50 * time.Millisecond, MaxOutputBytes: 1 << 20})
		if !res.TimedOut {
			t.Fatalf("res = %+v", res)
		}
	})
	t.Run("nonzero_exit", func(t *testing.T) {
		fx, st, cap := writeFixture(t, cliBehavior{Stdout: "oops", ExitCode: 3})
		setHelperEnv(t, fx, st, cap)
		p := fixtureProvider(t, fx, st, cap)
		res := p.Call(context.Background(), ExecRequest{Prompt: []byte("p"), Timeout: 10 * time.Second, MaxOutputBytes: 1 << 20})
		if res.ExitCode == nil || *res.ExitCode != 3 {
			t.Fatalf("exit = %v", res.ExitCode)
		}
	})
	t.Run("oversize", func(t *testing.T) {
		fx, st, cap := writeFixture(t, cliBehavior{StdoutBytes: 10 << 20})
		setHelperEnv(t, fx, st, cap)
		p := fixtureProvider(t, fx, st, cap)
		res := p.Call(context.Background(), ExecRequest{Prompt: []byte("p"), Timeout: 10 * time.Second, MaxOutputBytes: 4096})
		if !res.StdoutTruncated || int64(len(res.Stdout)) != 4096 {
			t.Fatalf("res = truncated %v len %d", res.StdoutTruncated, len(res.Stdout))
		}
	})
	t.Run("usage_missing", func(t *testing.T) {
		fx, st, cap := writeFixture(t, cliBehavior{Stdout: `{"result_text":"{}"}`})
		setHelperEnv(t, fx, st, cap)
		p := fixtureProvider(t, fx, st, cap)
		res := p.Call(context.Background(), ExecRequest{Prompt: []byte("p"), Timeout: 10 * time.Second, MaxOutputBytes: 1 << 20})
		if _, _, _, err := ParseEnvelope(res.Stdout); err == nil {
			t.Fatal("usage missing must fail")
		}
	})
	t.Run("usage_invalid", func(t *testing.T) {
		fx, st, cap := writeFixture(t, cliBehavior{Stdout: `{"result_text":"{}","usage":{"input_tokens":-3,"output_tokens":1}}`})
		setHelperEnv(t, fx, st, cap)
		p := fixtureProvider(t, fx, st, cap)
		res := p.Call(context.Background(), ExecRequest{Prompt: []byte("p"), Timeout: 10 * time.Second, MaxOutputBytes: 1 << 20})
		if _, _, _, err := ParseEnvelope(res.Stdout); err == nil {
			t.Fatal("usage invalid must fail")
		}
	})
	t.Run("spawn_failed", func(t *testing.T) {
		p := SubprocessProvider{Executable: filepath.Join(t.TempDir(), "no-such-cli")}
		res := p.Call(context.Background(), ExecRequest{Prompt: []byte("p"), Timeout: time.Second, MaxOutputBytes: 4096})
		if res.SpawnErr == nil {
			t.Fatal("spawn must fail")
		}
	})
	t.Run("stderr_redaction", func(t *testing.T) {
		secret := "ghp_0123456789abcdefghijKL"
		fx, st, cap := writeFixture(t, cliBehavior{Stderr: "failed with token " + secret + " and bearer abcdefgh123"})
		setHelperEnv(t, fx, st, cap)
		p := fixtureProvider(t, fx, st, cap)
		res := p.Call(context.Background(), ExecRequest{Prompt: []byte("p"), Timeout: 10 * time.Second, MaxOutputBytes: 1 << 20})
		if strings.Contains(res.StderrSummary, secret) || strings.Contains(res.StderrSummary, "abcdefgh123") {
			t.Fatalf("credentials leaked: %q", res.StderrSummary)
		}
		if !strings.Contains(res.StderrSummary, "[redacted]") {
			t.Fatalf("summary = %q", res.StderrSummary)
		}
	})
	t.Run("stderr_truncation", func(t *testing.T) {
		fx, st, cap := writeFixture(t, cliBehavior{Stderr: strings.Repeat("e", stderrSummaryLimit+100)})
		setHelperEnv(t, fx, st, cap)
		p := fixtureProvider(t, fx, st, cap)
		res := p.Call(context.Background(), ExecRequest{Prompt: []byte("p"), Timeout: 10 * time.Second, MaxOutputBytes: 1 << 20})
		if !res.StderrTruncated || len(res.StderrSummary) != stderrSummaryLimit {
			t.Fatalf("stderr = %d truncated %v", len(res.StderrSummary), res.StderrTruncated)
		}
	})
	t.Run("env_scrubbed", func(t *testing.T) {
		// SIFT_* credentials are never inherited even when allowlisted
		// variables exist in the parent environment.
		t.Setenv("SIFT_OPERATOR_TOKEN", "super-secret")
		fx, st, cap := writeFixture(t, cliBehavior{Stdout: "{}"})
		setHelperEnv(t, fx, st, cap)
		p := fixtureProvider(t, fx, st, cap)
		env := p.env()
		for _, kv := range env {
			if strings.HasPrefix(kv, "SIFT_OPERATOR_TOKEN=") {
				t.Fatalf("credential leaked into child env")
			}
		}
	})
}
