// runPi implements `sift pi`: an interactive pi session with the Sift skill
// installed and the current Sift state injected as context. The skill is the
// usability layer; every sift gate (approve nonce, policy fail-closed,
// auto_merge default off) lives in the CLI itself, so handing the CLI to a
// capable agent is no more dangerous than a fast human user (issue #962).
// The skill file is embedded in the binary and written to the user's pi
// skills directory idempotently, keeping it in sync with the sift release.
package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/xsift/sift/internal/pi"
)

// runPi resolves the user home, installs the Sift skill, builds the context
// snapshot and launches the pi TUI. A missing pi degrades to the same manual
// install guidance the init wizard uses (issue #960), never blocking.
func runPi(stdout, stderr io.Writer) int {
	userHome, err := os.UserHomeDir()
	if err != nil {
		report(stderr, err)
		return 1
	}
	skillPath, err := pi.EnsureSkill(userHome)
	if err != nil {
		report(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "✓ Sift skill 就绪：%s\n", skillPath)

	runner := pi.OSRunner{}
	if _, err := runner.LookPath("pi"); err != nil {
		fmt.Fprintln(stderr, piInstallManual())
		return 1
	}

	cwd, _ := os.Getwd()
	snapshot := pi.ContextSnapshot(cwd, func() string { return psSnapshot() })
	if err := pi.RunSession(runner, skillPath, strings.NewReader(snapshot)); err != nil {
		fmt.Fprintln(stderr, "✗ pi 会话结束：", err)
		return 1
	}
	return 0
}

// psSnapshot returns a compact `sift ps` view for the context snapshot. It
// degrades to "" when the daemon is unreachable so the session still starts;
// the snapshot is a prompt hint, never an authorization input.
func psSnapshot() string {
	out, err := exec.Command("sift", "ps").CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
