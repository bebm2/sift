// Command sift is the thin operator RPC client.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/miaoxiaoyong/sift/internal/config"
	"github.com/miaoxiaoyong/sift/internal/controlplane"
)

func main() {
	os.Exit(run(os.Args, os.Stdout, os.Stderr))
}

// run executes one operator command and returns the process exit status. It is
// split from main so cmd-level tests can assert exit codes without spawning a
// subprocess. config.md §7 mandates that `sift doctor` exits 0/1/2 per the
// doctor result's exit_code; the offline path computes that result locally,
// the online path receives it from the daemon in response.Result.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) < 2 {
		usage(stderr)
		return 2
	}
	home, err := config.ResolveHome()
	if err != nil {
		report(stderr, err)
		return 1
	}
	command := args[1]
	if command == "doctor" && len(args) == 3 && args[2] == "--offline" {
		result := controlplane.OfflineDoctor(home)
		return emitDoctor(stdout, stderr, result)
	}
	method, params, err := request(command, args[2:])
	if err != nil {
		report(stderr, err)
		return 1
	}
	response, err := controlplane.OperatorRequest(home, method, params)
	if err != nil {
		report(stderr, fmt.Errorf("daemon unavailable: %w", err))
		return 1
	}
	if err := printJSON(stdout, response); err != nil {
		report(stderr, err)
		return 1
	}
	if command == "doctor" {
		if !response.OK {
			return 1
		}
		return doctorExitCode(response.Result)
	}
	if !response.OK {
		return 1
	}
	return 0
}

// emitDoctor prints the offline doctor result and maps its exit_code to the
// process exit status (config.md §7).
func emitDoctor(stdout, stderr io.Writer, result map[string]any) int {
	if err := printJSON(stdout, result); err != nil {
		report(stderr, err)
		return 1
	}
	return doctorExitCode(result)
}

// doctorExitCode extracts the process exit status from a doctor result. The
// doctor computes exit_code as 0 (clean), 1 (warning) or 2 (error); this only
// projects it. The offline result carries a Go int, the online result arrives
// from JSON as a float64. A missing or malformed value defaults to 0, matching
// a healthy result that must always set it.
func doctorExitCode(result any) int {
	m, ok := result.(map[string]any)
	if !ok {
		return 0
	}
	switch code := m["exit_code"].(type) {
	case int:
		return code
	case float64:
		return int(code)
	}
	return 0
}

func request(command string, args []string) (string, map[string]any, error) {
	switch command {
	case "ps":
		return "ops.ps", map[string]any{"run_id": nil, "project_id": nil, "status": nil, "limit": 100, "after_run_id": nil}, nil
	case "doctor":
		if len(args) != 0 {
			return "", nil, fmt.Errorf("doctor accepts only --offline")
		}
		return "ops.doctor", map[string]any{}, nil
	case "logs":
		if len(args) != 1 {
			return "", nil, fmt.Errorf("usage: sift logs <run-id>")
		}
		return "ops.logs", map[string]any{"run_id": args[0], "attempt_no": nil, "offset": 0, "limit": 262144}, nil
	case "worktree":
		if len(args) != 1 {
			return "", nil, fmt.Errorf("usage: sift worktree <run-id>")
		}
		return "ops.worktree", map[string]any{"run_id": args[0]}, nil
	case "kill", "retry":
		fs := flag.NewFlagSet(command, flag.ContinueOnError)
		version := fs.Int("expected-version", 0, "expected Run version")
		key := fs.String("request-key", "", "idempotency key")
		if err := fs.Parse(args); err != nil {
			return "", nil, err
		}
		if fs.NArg() != 1 || *version < 1 || *key == "" {
			return "", nil, fmt.Errorf("usage: sift %s <run-id> --expected-version N --request-key KEY", command)
		}
		return "ops." + command, map[string]any{"run_id": fs.Arg(0), "expected_version": *version, "request_key": *key}, nil
	default:
		return "", nil, fmt.Errorf("unknown command %q", command)
	}
}
func printJSON(w io.Writer, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(b))
	return err
}
func report(w io.Writer, err error) { fmt.Fprintln(w, "sift:", err) }
func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: sift ps|logs|worktree|doctor [--offline]|kill|retry")
}
