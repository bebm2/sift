package brain

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// FakeProvider is the fake Brain provider of the M1 skeleton chain (WBS M1
// §1.6/§1.7). It honors the same Provider contract as the real subprocess
// CLI and records every request so tests can assert the two physical
// attempts carried byte-identical prompts (brain.md §10).
type FakeProvider struct {
	mu        sync.Mutex
	Responses []FakeResponse
	Requests  [][]byte
}

// FakeResponse scripts one invocation. When RawStdout is set it is used
// verbatim; otherwise an envelope is synthesized from ResultText + usage.
type FakeResponse struct {
	ResultText   string // inner touchpoint JSON; ignored when RawStdout set
	InputTokens  int64
	OutputTokens int64
	RawStdout    []byte // full stdout override (malformed cases)
	Stderr       string
	ExitCode     *int // nil → 0; non-nil → that exit code
	Sleep        time.Duration
	TimedOut     bool // simulate call_timeout firing
	SpawnErr     bool // simulate the process failing to start
}

// FakeEnvelope builds the raw stdout of a well-formed claude-json-v1
// envelope, including unknown diagnostic fields the adapter must tolerate.
func FakeEnvelope(resultText string, inputTokens, outputTokens int64) []byte {
	raw, err := json.Marshal(map[string]any{
		"type":        "result",
		"subtype":     "success",
		"session_id":  "fake-session",
		"result_text": resultText,
		"usage": map[string]any{
			"input_tokens":  inputTokens,
			"output_tokens": outputTokens,
			"cache_read":    3,
		},
	})
	if err != nil {
		panic(fmt.Sprintf("brain: fake envelope: %v", err))
	}
	return raw
}

// ValidT2ResultText returns a legal T2 inner output for the M1 fake chain
// (WBS M1 §1.7: the fake provider must be able to emit a legal T2 output).
func ValidT2ResultText(kind TaskKind, agent string, goals []string, hitl bool) string {
	raw, err := json.Marshal(map[string]any{
		"kind":              string(kind),
		"agent":             agent,
		"hitl_before_start": hitl,
		"goals":             goals,
		"risk_notes":        "",
		"rationale":         "fake provider",
	})
	if err != nil {
		panic(fmt.Sprintf("brain: fake T2 result: %v", err))
	}
	return string(raw)
}

// ValidT1ResultText returns a legal T1 inner output (ready disposition).
func ValidT1ResultText() string {
	return `{"disposition":"ready","questions":[],"possible_duplicate_run_id":null,"rationale":"fake provider"}`
}

// Call serves the next scripted response; when the queue is empty it repeats
// the last one (a retry typically expects a second scripted answer, and an
// empty queue after at least one response is a test bug otherwise).
func (f *FakeProvider) Call(ctx context.Context, req ExecRequest) ExecResult {
	f.mu.Lock()
	f.Requests = append(f.Requests, append([]byte(nil), req.Prompt...))
	if len(f.Responses) == 0 {
		f.mu.Unlock()
		return ExecResult{SpawnErr: fmt.Errorf("brain: fake provider response queue exhausted")}
	}
	resp := f.Responses[0]
	if len(f.Responses) > 1 {
		f.Responses = f.Responses[1:]
	}
	f.mu.Unlock()

	if resp.SpawnErr {
		return ExecResult{SpawnErr: fmt.Errorf("brain: fake spawn failure")}
	}
	if resp.TimedOut {
		return ExecResult{TimedOut: true}
	}
	if resp.Sleep > 0 {
		select {
		case <-ctx.Done():
			return ExecResult{TimedOut: true}
		case <-time.After(resp.Sleep):
		}
	}
	res := ExecResult{StderrSummary: resp.Stderr, ExitCode: resp.ExitCode}
	if resp.ExitCode == nil {
		code := 0
		res.ExitCode = &code
	}
	if resp.RawStdout != nil {
		res.Stdout = resp.RawStdout
	} else {
		res.Stdout = FakeEnvelope(resp.ResultText, resp.InputTokens, resp.OutputTokens)
	}
	return res
}

// LastRequestIdentical reports whether all recorded requests are byte
// identical — the same-prompt retry invariant (brain.md §5).
func (f *FakeProvider) LastRequestIdentical() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := 1; i < len(f.Requests); i++ {
		if !bytes.Equal(f.Requests[0], f.Requests[i]) {
			return false
		}
	}
	return true
}
