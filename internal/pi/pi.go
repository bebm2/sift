// Package pi implements the `sift pi` command: it ensures the Sift skill is
// installed where the pi agent discovers skills, snapshots the current Sift
// state as context, and starts an interactive pi session with that context
// injected. The skill is the usability layer; security stays in the sift CLI
// itself (approve nonces, gate fail-closed, auto_merge off) — the worst a
// capable agent can do equals a fast human user (issue #962).
package pi

import (
	"embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

//go:embed skill/SKILL.md
var skillFS embed.FS

// SkillSourceName is the embedded skill file path inside the binary.
const SkillSourceName = "skill/SKILL.md"

// SkillDirRel is the pi user skills directory relative to the user's home.
// pi discovers `~/.pi/agent/skills/<name>/SKILL.md` automatically.
const SkillDirRel = ".pi/agent/skills"

// SkillName is the skill directory name under the pi skills root.
const SkillName = "sift"

// Runner abstracts process execution so tests never run real installs or
// spawn an interactive TUI.
type Runner interface {
	LookPath(name string) (string, error)
	Run(name string, args ...string) ([]byte, error)
}

// OSRunner executes through os/exec.
type OSRunner struct{}

func (OSRunner) LookPath(name string) (string, error) { return exec.LookPath(name) }
func (OSRunner) Run(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	return cmd.CombinedOutput()
}

// EnsureSkill installs the embedded Sift skill into the user's pi skills
// directory, creating parent directories as needed. It is idempotent: a
// byte-identical existing file is left untouched; a different file is
// overwritten so the skill stays in sync with the sift release. Returns the
// written path.
func EnsureSkill(userHome string) (string, error) {
	skillDir := filepath.Join(userHome, SkillDirRel, SkillName)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return "", fmt.Errorf("pi: create skill directory: %w", err)
	}
	data, err := skillFS.ReadFile(SkillSourceName)
	if err != nil {
		return "", fmt.Errorf("pi: read embedded skill: %w", err)
	}
	target := filepath.Join(skillDir, "SKILL.md")
	existing, err := os.ReadFile(target)
	if err == nil && string(existing) == string(data) {
		return target, nil
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		return "", fmt.Errorf("pi: write skill: %w", err)
	}
	return target, nil
}

// PiMissingError reports that the pi executable is not on PATH.
type PiMissingError struct{}

func (PiMissingError) Error() string {
	return "pi 未安装：请先安装 pi（curl -fsSL https://pi.dev/install.sh | sh，或 npm install -g --ignore-scripts @earendil-works/pi-coding-agent），然后重试 `sift pi`"
}

// RunSession launches an interactive pi session with the Sift skill available
// and the current Sift state injected as context. It is intended for a real
// terminal: it hands stdin/stdout/stderr to the child process. The pi
// executable is resolved from PATH; a missing pi returns PiMissingError so
// the caller can route to the #960 install guidance.
func RunSession(r Runner, skillPath string, ctx io.Reader) error {
	if _, err := r.LookPath("pi"); err != nil {
		return PiMissingError{}
	}
	cmd := exec.Command("pi")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if ctx != nil {
		cmd.Stdin = io.MultiReader(ctx, os.Stdin)
	}
	return cmd.Run()
}

// ContextSnapshot renders the context injected into the pi session: the
// current directory and the abbreviated `sift ps` state when the daemon is
// reachable. It degrades to a short notice when the daemon is unavailable;
// the snapshot is a prompt hint, never an authorization input.
func ContextSnapshot(cwd string, psFn func() string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Sift context snapshot\n\n")
	fmt.Fprintf(&b, "- 当前目录：%s\n", cwd)
	if psFn != nil {
		if out := psFn(); out != "" {
			fmt.Fprintf(&b, "- 当前 runs（sift ps）：\n```\n%s\n```\n", strings.TrimSpace(out))
		} else {
			fmt.Fprintln(&b, "- 当前 runs：daemon 不可达或无输出，运行 `sift ps` 确认。")
		}
	}
	b.WriteString("\n请基于以上快照与 Sift skill 回答用户的问题；取不到的事实不要猜测。\n")
	return b.String()
}
