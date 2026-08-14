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

	"github.com/xsift/sift/internal/pi"
)

// runPi resolves the user home, installs the Sift skill and launches the pi
// TUI. A missing pi degrades to the same manual install guidance the init
// wizard uses (issue #960), never blocking.
//
// stdin is handed to pi verbatim: a TUI needs a real terminal for its tty
// detection, and piping a context snapshot through io.MultiReader breaks it
// (issue: sift pi 卡住不进交互 — the session started but never rendered).
// Context injection is not needed: the Sift skill already teaches the agent
// to gather state itself via read-only commands (sift ps/timeline/logs).
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

	if _, err := exec.LookPath("pi"); err != nil {
		fmt.Fprintln(stderr, piInstallManual())
		return 1
	}
	if err := pi.RunSession(pi.OSRunner{}); err != nil {
		fmt.Fprintln(stderr, "✗ pi 会话结束：", err)
		return 1
	}
	return 0
}
