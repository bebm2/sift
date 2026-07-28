package brain

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"regexp"
	"sync"
	"time"
)

// Provider subprocess protocol (brain.md §4): prompt/input on stdin, no
// shell, no input on argv, empty temp working directory, minimal environment
// allowlist, config call_timeout, stdout capped at max_raw_output_bytes+1.

// ExecRequest is one physical provider invocation.
type ExecRequest struct {
	Prompt         []byte
	Timeout        time.Duration
	MaxOutputBytes int64
}

// ExecResult is the raw outcome of one physical invocation. The shell maps
// it onto brain_attempts facts; nothing here is retried or interpreted.
type ExecResult struct {
	Stdout          []byte // first MaxOutputBytes bytes when truncated
	StdoutTruncated bool
	StderrSummary   string // credential-redacted, bounded
	StderrTruncated bool
	ExitCode        *int  // nil when the process never exited normally
	TimedOut        bool  // config call_timeout fired
	SpawnErr        error // process could not be started
}

// Provider is the physical invocation port. SubprocessProvider runs the real
// CLI; FakeProvider serves the M1 skeleton chain and shell tests.
type Provider interface {
	Call(ctx context.Context, req ExecRequest) ExecResult
}

// DefaultEnvAllowlist is the minimal environment the CLI needs (brain.md §4:
// never inject operator/run/wrapper credentials).
var DefaultEnvAllowlist = []string{
	"HOME", "PATH", "USER", "LOGNAME", "SHELL", "LANG", "LC_ALL", "LC_CTYPE", "TMPDIR", "TERM", "XDG_CONFIG_HOME", "XDG_CACHE_HOME",
}

const (
	// stderrSummaryLimit is the V0 stderr bound (brain.md §4.2).
	stderrSummaryLimit = 4096
	// stderrReadCap bounds how much stderr we bother reading at all.
	stderrReadCap = 1 << 20
)

// credentialPatterns are removed from stderr before it is summarized
// (brain.md §4.2). The summary never enters prompts, events or the outbox.
var credentialPatterns = []*regexp.Regexp{
	regexp.MustCompile(`ghp_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`gh[ousr]_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}`),
	regexp.MustCompile(`glpat-[A-Za-z0-9_\-]{10,}`),
	regexp.MustCompile(`sk-[A-Za-z0-9_\-]{16,}`),
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	regexp.MustCompile(`xox[baprs]-[A-Za-z0-9\-]{10,}`),
	regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9_\-\.]{8,}`),
}

// SubprocessProvider invokes the configured agent CLI as a child process.
type SubprocessProvider struct {
	Executable string
	Args       []string
	// EnvAllowlist names the inherited variables; nil uses
	// DefaultEnvAllowlist. SIFT_* variables are never inherited.
	EnvAllowlist []string
	// NewWorkDir creates the empty per-call working directory; nil uses
	// os.MkdirTemp. Tests install a fixed directory to assert cwd isolation.
	NewWorkDir func() (string, error)
}

// Call runs one physical invocation.
func (p SubprocessProvider) Call(ctx context.Context, req ExecRequest) ExecResult {
	workDir, err := p.workDir()
	if err != nil {
		return ExecResult{SpawnErr: err}
	}
	defer os.RemoveAll(workDir)

	ctx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, p.Executable, p.Args...)
	cmd.Dir = workDir
	cmd.Env = p.env()
	cmd.Stdin = bytes.NewReader(req.Prompt)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return ExecResult{SpawnErr: err}
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return ExecResult{SpawnErr: err}
	}
	if err := cmd.Start(); err != nil {
		return ExecResult{SpawnErr: err}
	}

	var wg sync.WaitGroup
	var outBuf, errBuf []byte
	wg.Add(2)
	go func() {
		defer wg.Done()
		// Read at most MaxOutputBytes+1: crossing the cap terminates the
		// process (brain.md §4.2).
		outBuf, _ = io.ReadAll(io.LimitReader(stdout, req.MaxOutputBytes+1))
		if int64(len(outBuf)) > req.MaxOutputBytes {
			_ = cmd.Process.Kill()
		}
	}()
	go func() {
		defer wg.Done()
		errBuf, _ = io.ReadAll(io.LimitReader(stderr, stderrReadCap+1))
	}()
	wg.Wait()
	waitErr := cmd.Wait()

	res := ExecResult{}
	if int64(len(outBuf)) > req.MaxOutputBytes {
		res.StdoutTruncated = true
		res.Stdout = outBuf[:req.MaxOutputBytes]
	} else {
		res.Stdout = outBuf
	}
	res.StderrSummary, res.StderrTruncated = summarizeStderr(errBuf)
	if ctx.Err() == context.DeadlineExceeded {
		res.TimedOut = true
		return res
	}
	var exitErr *exec.ExitError
	switch {
	case waitErr == nil:
		code := 0
		res.ExitCode = &code
	case errors.As(waitErr, &exitErr):
		code := exitErr.ExitCode()
		res.ExitCode = &code
	default:
		// Killed by us (oversize) or by the timeout context: no exit fact.
	}
	return res
}

func (p SubprocessProvider) workDir() (string, error) {
	if p.NewWorkDir != nil {
		return p.NewWorkDir()
	}
	return os.MkdirTemp("", "sift-brain-*")
}

func (p SubprocessProvider) env() []string {
	allow := p.EnvAllowlist
	if allow == nil {
		allow = DefaultEnvAllowlist
	}
	allowed := map[string]bool{}
	for _, k := range allow {
		allowed[k] = true
	}
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := bytes.Cut([]byte(kv), []byte{'='})
		if allowed[string(k)] {
			env = append(env, kv)
		}
	}
	return env
}

// summarizeStderr redacts credential patterns first, then bounds the summary
// to stderrSummaryLimit bytes (brain.md §4.2).
func summarizeStderr(raw []byte) (string, bool) {
	redacted := raw
	for _, re := range credentialPatterns {
		redacted = re.ReplaceAll(redacted, []byte("[redacted]"))
	}
	if len(redacted) > stderrSummaryLimit {
		return string(redacted[:stderrSummaryLimit]), true
	}
	return string(redacted), false
}
