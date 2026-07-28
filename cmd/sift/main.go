// Command sift is the thin operator RPC client.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/miaoxiaoyong/sift/internal/config"
	"github.com/miaoxiaoyong/sift/internal/controlplane"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	home, err := config.ResolveHome()
	if err != nil {
		fatal(err)
	}
	command := os.Args[1]
	if command == "doctor" && len(os.Args) == 3 && os.Args[2] == "--offline" {
		print(controlplane.OfflineDoctor(home))
		return
	}
	method, params, err := request(command, os.Args[2:])
	if err != nil {
		fatal(err)
	}
	response, err := controlplane.OperatorRequest(home, method, params)
	if err != nil {
		fatal(fmt.Errorf("daemon unavailable: %w", err))
	}
	print(response)
	if !response.OK {
		os.Exit(1)
	}
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
func print(v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fatal(err)
	}
	fmt.Println(string(b))
}
func fatal(err error) { fmt.Fprintln(os.Stderr, "sift:", err); os.Exit(1) }
func usage() {
	fmt.Fprintln(os.Stderr, "usage: sift ps|logs|worktree|doctor [--offline]|kill|retry")
	os.Exit(2)
}
