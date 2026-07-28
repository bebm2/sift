package config

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// This file implements the two-level startup probe framework of config.md §5:
//
//   - Process-level probes (§5.1): any failure refuses daemon startup. They
//     cover SIFT_HOME/permissions, config decode/normalize/fingerprint, agent
//     executables, the brain executable and tmux when used. (SQLite, sockets,
//     the single-instance lock and forge CLIs are wired by later slices.)
//   - Project-level probes (§5.2): a failure isolates only that project,
//     writes its health projection, warns once and lets healthy projects keep
//     scheduling.
//
// Probes are values the daemon composes into its ordered startup list; this
// package provides the runner and the config-owned probes.

// Probe is one process-level check. Run must be safe to call with a cancelled
// context.
type Probe struct {
	Name string
	Run  func(ctx context.Context) error
}

// Outcome is the recorded result of one Probe.
type Outcome struct {
	Name string
	Err  error
}

// RunProcessProbes runs every probe and returns all outcomes. A non-nil error
// in any outcome means startup must refuse (config.md §5.1).
func RunProcessProbes(ctx context.Context, probes []Probe) []Outcome {
	out := make([]Outcome, 0, len(probes))
	for _, p := range probes {
		err := p.Run(ctx)
		out = append(out, Outcome{Name: p.Name, Err: err})
		if ctx.Err() != nil {
			break
		}
	}
	return out
}

// AnyFailed reports whether any outcome carries an error, i.e. whether startup
// must refuse.
func AnyFailed(outcomes []Outcome) bool {
	for _, o := range outcomes {
		if o.Err != nil {
			return true
		}
	}
	return false
}

// Diagnostics collects resolved absolute executable paths from startup probes
// (config.md §5.1.7). The running launcher and process-identity records use
// these resolved paths; the original configured values still enter the config
// hash, so PATH drift after startup cannot silently change which binary runs.
type Diagnostics struct {
	// AgentPaths maps agent id → resolved absolute executable path.
	AgentPaths map[string]string
	// BrainPath is the resolved brain executable, empty in deterministic mode.
	BrainPath string
	// TmuxPath is the resolved tmux binary when a backend requires it.
	TmuxPath string
}

// NewDiagnostics returns a zeroed Diagnostics with initialized maps.
func NewDiagnostics() *Diagnostics {
	return &Diagnostics{AgentPaths: map[string]string{}}
}

// AgentExecutableProbe builds the §5.1.4 probe: every defined agent's
// executable must resolve on PATH. Even agents not yet referenced by a project
// are probed — agent definitions are startup-sensitive closed config.
func AgentExecutableProbe(cfg *Config, diag *Diagnostics) Probe {
	return Probe{
		Name: "agent-executables",
		Run: func(ctx context.Context) error {
			for _, a := range cfg.Agents {
				p, err := exec.LookPath(a.Executable)
				if err != nil {
					return fmt.Errorf("agent %q: executable %q not found on PATH: %w", a.ID, a.Executable, err)
				}
				diag.AgentPaths[a.ID] = p
			}
			return nil
		},
	}
}

// BrainExecutableProbe builds the §5.1.5 probe. A null/empty executable is
// deterministic mode and is not probed (config.md §3.4).
func BrainExecutableProbe(cfg *Config, diag *Diagnostics) Probe {
	return Probe{
		Name: "brain-executable",
		Run: func(ctx context.Context) error {
			if cfg.Brain.Executable == "" {
				return nil
			}
			p, err := exec.LookPath(cfg.Brain.Executable)
			if err != nil {
				return fmt.Errorf("brain: executable %q not found on PATH: %w", cfg.Brain.Executable, err)
			}
			diag.BrainPath = p
			return nil
		},
	}
}

// TmuxProbe builds the §5.1.6 probe: tmux is resolved only when some agent or
// the runtime backend selects the tmux backend.
func TmuxProbe(cfg *Config, diag *Diagnostics) Probe {
	return Probe{
		Name: "tmux",
		Run: func(ctx context.Context) error {
			if !usesTmux(cfg) {
				return nil
			}
			p, err := exec.LookPath("tmux")
			if err != nil {
				return fmt.Errorf("tmux backend selected but tmux not found on PATH: %w", err)
			}
			diag.TmuxPath = p
			return nil
		},
	}
}

func usesTmux(cfg *Config) bool {
	if cfg.Runtime.Backend == BackendTmux {
		return true
	}
	for _, a := range cfg.Agents {
		if a.Backend == BackendTmux {
			return true
		}
	}
	return false
}

// checkRepoDir is the §5.2.1 repo skeleton check: the path must exist and be a
// directory. Normalize already enforced absoluteness; git integrity and
// base-branch checks arrive with the Forge/runtime slices.
func checkRepoDir(repo string) error {
	info, err := os.Stat(repo)
	if err != nil {
		return fmt.Errorf("repo %s: %w", repo, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("repo %s: not a directory", repo)
	}
	return nil
}

// ProjectProbe is one project-level check (config.md §5.2).
type ProjectProbe struct {
	Name string
	Run  func(ctx context.Context, p Project) error
}

// ProjectOutcome is the recorded result for one project.
type ProjectOutcome struct {
	ProjectID string
	Name      string
	Err       error
}

// RunProjectProbes runs each probe across each enabled project independently.
// A failure is recorded for that project only; other projects are unaffected
// (§5.2). The caller turns a failing outcome into a one-time warning and a
// project health projection entry.
func RunProjectProbes(ctx context.Context, cfg *Config, probes []ProjectProbe) []ProjectOutcome {
	out := make([]ProjectOutcome, 0)
	for _, p := range cfg.Projects {
		if !p.Enabled {
			continue
		}
		for _, pr := range probes {
			err := pr.Run(ctx, p)
			out = append(out, ProjectOutcome{ProjectID: p.ID, Name: pr.Name, Err: err})
			if ctx.Err() != nil {
				return out
			}
		}
	}
	return out
}

// FailedProjects returns the set of project ids with at least one failing
// probe, for the one-time warning deduplication.
func FailedProjects(outcomes []ProjectOutcome) []string {
	seen := map[string]bool{}
	var failed []string
	for _, o := range outcomes {
		if o.Err != nil && !seen[o.ProjectID] {
			seen[o.ProjectID] = true
			failed = append(failed, o.ProjectID)
		}
	}
	return failed
}

// ProjectRepoProbe builds the §5.2.1 project probe skeleton: the repo path must
// be absolute and exist as a directory. Git-repository integrity, base-branch
// readability, policy schema and forge auth land in later slices.
func ProjectRepoProbe() ProjectProbe {
	return ProjectProbe{
		Name: "repo",
		Run: func(_ context.Context, p Project) error {
			return checkRepoDir(p.Repo)
		},
	}
}

// ProjectAgentsProbe builds the §5.2.4 project probe: each candidate agent id
// must resolve against the effective config. Normalize already enforces this
// for configured lists; the probe is the runtime re-check.
func ProjectAgentsProbe(cfg *Config) ProjectProbe {
	known := make(map[string]bool, len(cfg.Agents))
	for _, a := range cfg.Agents {
		known[a.ID] = true
	}
	return ProjectProbe{
		Name: "agents",
		Run: func(_ context.Context, p Project) error {
			for _, id := range p.Agents {
				if !known[id] {
					return fmt.Errorf("project %q references unknown agent %q", p.ID, id)
				}
			}
			return nil
		},
	}
}
