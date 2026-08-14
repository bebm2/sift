package main

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xsift/sift/internal/config"
)

func TestInitFlagsWriteMergeAndBackup(t *testing.T) {
	_ = freshHome(t)
	home, err := config.ResolveHome()
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(t.TempDir(), "demo")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", repo, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	if out, err := exec.Command("git", "-C", repo, "remote", "add", "origin", "git@github.com:owner/repo.git").CombinedOutput(); err != nil {
		t.Fatalf("git remote: %v: %s", err, out)
	}
	agent := filepath.Join(t.TempDir(), "fake-agent")
	if err := os.WriteFile(agent, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	args := []string{"sift", "init", "--offline", "--agent", agent, "--project", repo, "--forge", "github", "--operator", "alice"}
	var out bytes.Buffer
	if code := runWithInput(args, strings.NewReader(""), &out, io.Discard); code != 0 {
		t.Fatalf("init = %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "已写入") || !strings.Contains(out.String(), "sift daemon") {
		t.Fatalf("output = %q", out.String())
	}
	snap, err := config.Load(home, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Config.Agents) != 1 || snap.Config.Agents[0].Executable != agent || strings.Join(snap.Config.Agents[0].Args, ",") != "" {
		t.Fatalf("agents = %#v", snap.Config.Agents)
	}
	if len(snap.Config.Projects) != 1 || snap.Config.Projects[0].Repo != repo || snap.Config.Projects[0].Forge.Project != "owner/repo" {
		t.Fatalf("projects = %#v", snap.Config.Projects)
	}
	if got := snap.Config.Operators.GitHub; len(got) != 1 || got[0] != "alice" {
		t.Fatalf("operators = %#v", got)
	}
	if info, err := os.Stat(config.ConfigPath(home)); err != nil || info.Mode().Perm() != config.ConfigFileMode {
		t.Fatalf("config mode = %v, %v", info, err)
	}

	out.Reset()
	if code := runWithInput(args, strings.NewReader(""), &out, io.Discard); code != 0 {
		t.Fatalf("second init = %d: %s", code, out.String())
	}
	snap, err = config.Load(home, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Config.Agents) != 1 || len(snap.Config.Projects) != 1 {
		t.Fatalf("rerun was not idempotent: %#v", snap.Config)
	}
	if info, err := os.Stat(config.ConfigPath(home) + ".bak"); err != nil || info.Mode().Perm() != config.ConfigFileMode {
		t.Fatalf("backup mode = %v, %v", info, err)
	}
}

func TestWriteSetupDocumentRejectsInvalidEditWithoutReplacingConfig(t *testing.T) {
	_ = freshHome(t)
	home, err := config.ResolveHome()
	if err != nil {
		t.Fatal(err)
	}
	valid := []byte("version: 1\noperators:\n  github: [alice]\n")
	if err := os.WriteFile(config.ConfigPath(home), valid, config.ConfigFileMode); err != nil {
		t.Fatal(err)
	}
	invalid := map[string]any{
		"version": 1,
		"projects": []any{map[string]any{
			"id": "demo", "repo": "/tmp/demo", "forge": map[string]any{"kind": "github", "project": "owner/repo"}, "agents": []any{"missing"},
		}},
	}
	if err := writeSetupDocument(home, invalid, true); err == nil {
		t.Fatal("invalid edit was written")
	}
	got, err := os.ReadFile(config.ConfigPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, valid) {
		t.Fatalf("config changed after invalid edit: %q", got)
	}
}

func TestForgeLoginFromStatus(t *testing.T) {
	for _, tt := range []struct {
		name, status, want string
	}{
		{"github", "github.com\n  ✓ Logged in to github.com account miaoxiaoyong (keyring)\n", "miaoxiaoyong"},
		{"gitlab", "Logged in to gitlab.hexinfo.cn as hex.miao\n", "hex.miao"},
		{"missing", "not logged in", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := forgeLoginFromStatus(tt.status); got != tt.want {
				t.Fatalf("forgeLoginFromStatus(%q) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestAgentArgsDefaultsAndOverride(t *testing.T) {
	doc := map[string]any{"version": 1}
	addAgent(doc, "claude", nil)
	addAgent(doc, "codex", nil)
	custom := []string{"--custom", "value"}
	addAgent(doc, "custom=custom-agent", &custom)

	agents := list(doc, "agents")
	if got := agents[0].(map[string]any)["args"]; !equalStrings(got, []string{"-p"}) {
		t.Fatalf("claude args = %#v", got)
	}
	if got := agents[1].(map[string]any)["args"]; !equalStrings(got, []string{"exec", "-"}) {
		t.Fatalf("codex args = %#v", got)
	}
	if got := agents[2].(map[string]any)["args"]; !equalStrings(got, custom) {
		t.Fatalf("custom args = %#v", got)
	}
}

func TestProjectAddDoesNotChangeOperators(t *testing.T) {
	_ = freshHome(t)
	home, err := config.ResolveHome()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.ConfigPath(home), []byte("version: 1\noperators:\n  github: [alice]\n"), config.ConfigFileMode); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(t.TempDir(), "demo")
	if out, err := exec.Command("git", "init", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	if out, err := exec.Command("git", "-C", repo, "remote", "add", "origin", "git@github.com:owner/repo.git").CombinedOutput(); err != nil {
		t.Fatalf("git remote: %v: %s", err, out)
	}
	var out bytes.Buffer
	if code := runWithInput([]string{"sift", "project", "add", "--offline", "--project", repo, "--forge", "github"}, strings.NewReader(""), &out, io.Discard); code != 0 {
		t.Fatalf("project add = %d: %s", code, out.String())
	}
	snap, err := config.Load(home, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got := snap.Config.Operators.GitHub; len(got) != 1 || got[0] != "alice" {
		t.Fatalf("operators changed: %#v", got)
	}
	if len(snap.Config.Projects) != 1 {
		t.Fatalf("projects = %#v", snap.Config.Projects)
	}
}

func equalStrings(value any, want []string) bool {
	got, ok := value.([]any)
	if !ok || len(got) != len(want) {
		return false
	}
	for i, arg := range got {
		if arg != want[i] {
			return false
		}
	}
	return true
}

func TestDetectForgeKind(t *testing.T) {
	for _, tt := range []struct {
		host, want string
	}{
		{"github.com", "github"},
		{"gitlab.com", "gitlab"},
		{"gitlab.hexinfo.cn", "gitlab"},
		{"gitlab.example.com", "gitlab"},
		{"github.enterprise.com", "github"},
		{"bitbucket.org", ""},
		{"", ""},
	} {
		if got := detectForgeKind(tt.host); got != tt.want {
			t.Fatalf("detectForgeKind(%q) = %q, want %q", tt.host, got, tt.want)
		}
	}
}

func TestRemoteHostProject(t *testing.T) {
	for _, tt := range []struct {
		url, host, project string
	}{
		{"git@github.com:owner/repo.git", "github.com", "owner/repo"},
		{"https://github.com/owner/repo", "github.com", "owner/repo"},
		{"ssh://git@gitlab.hexinfo.cn/group/proj.git", "gitlab.hexinfo.cn", "group/proj"},
		{"git://github.com/a/b.git", "github.com", "a/b"},
		{"https://github.com/owner/repo.git/", "github.com", "owner/repo"},
		{"not a url", "", ""},
		{"", "", ""},
	} {
		host, project := remoteHostProject(tt.url)
		if host != tt.host || project != tt.project {
			t.Fatalf("remoteHostProject(%q) = (%q,%q), want (%q,%q)", tt.url, host, project, tt.host, tt.project)
		}
	}
}

// TestDetectAgentsVersionsAndCharacteristics pins issue #930: auto-detect
// probes versions via --version and every detected row carries the built-in
// characteristic profile (Chinese tags).
func TestDetectAgentsVersionsAndCharacteristics(t *testing.T) {
	bin := t.TempDir()
	for name, body := range map[string]string{
		"claude": "#!/bin/sh\nprintf 'Claude Code version 2.1.0\\n'\n",
		"codex":  "#!/bin/sh\nprintf 'codex-cli 0.145.0\\n'\n",
		"pi":     "#!/bin/sh\nprintf '0.84.1\\n'\n",
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+filepath.Dir(gitPath))

	found := detectAgents()
	if len(found) != 3 {
		t.Fatalf("detectAgents = %#v, want claude/codex/pi", found)
	}
	if got := found[0]; got.name != "claude" || got.version != "2.1.0" {
		t.Fatalf("found[0] = %#v, want claude 2.1.0", got)
	}
	if got := found[1]; got.name != "codex" || got.version != "0.145.0" {
		t.Fatalf("found[1] = %#v, want codex 0.145.0", got)
	}
	row := formatDetectedAgent(found[0])
	for _, want := range []string{"claude (2.1.0)", "编码·推理·长上下文", "200K", "中", "Anthropic"} {
		if !strings.Contains(row, want) {
			t.Fatalf("formatDetectedAgent(claude) = %q, missing %q", row, want)
		}
	}
}

// TestInteractiveInitCharacteristicsDisplay is the wizard integration test for
// issue #930: the numbered rows show executable (version) plus the built-in
// characteristic labels in Chinese, and the default all-selection still writes
// every detected agent to config.
func TestInteractiveInitCharacteristicsDisplay(t *testing.T) {
	_ = freshHome(t)
	home, err := config.ResolveHome()
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(t.TempDir(), "demo")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", repo, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	if out, err := exec.Command("git", "-C", repo, "remote", "add", "origin", "git@github.com:owner/repo.git").CombinedOutput(); err != nil {
		t.Fatalf("git remote: %v: %s", err, out)
	}
	t.Chdir(repo)
	repo, err = filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	for name, body := range map[string]string{
		"claude": "#!/bin/sh\nprintf 'Claude Code version 2.0.0\\n'\n",
		"codex":  "#!/bin/sh\nprintf 'codex-cli 0.5.0\\n'\n",
		"pi":     "#!/bin/sh\nprintf '0.9.9\\n'\n",
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+filepath.Dir(gitPath))

	var out bytes.Buffer
	// Answers: gh install=n ; glab install=n ; agents=all ; operator=Enter (skip).
	if code := runWithInput([]string{"sift", "init"}, strings.NewReader("n\nn\nall\n\n"), &out, io.Discard); code != 0 {
		t.Fatalf("init = %d: %s", code, out.String())
	}
	for _, want := range []string{
		"1. claude (2.0.0) — 编码·推理·长上下文 · 200K · 中 · 中 · Anthropic",
		"2. codex (0.5.0) — 编码·审查 · 200K · 中 · 快 · OpenAI",
		"3. pi (0.9.9) — 编码·规划·审查 · 200K · 高 · 中 · pi 编码代理",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("wizard output missing %q:\n%s", want, out.String())
		}
	}
	snap, err := config.Load(home, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Config.Agents) != 3 {
		t.Fatalf("agents = %#v, want all 3 detected agents", snap.Config.Agents)
	}
}

// TestNonInteractiveAgentAddReportsVersion pins issue #930: the non-interactive
// path writes the probed version into the output (without putting it in config).
func TestNonInteractiveAgentAddReportsVersion(t *testing.T) {
	_ = freshHome(t)
	home, err := config.ResolveHome()
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	agent := filepath.Join(bin, "claude")
	if err := os.WriteFile(agent, []byte("#!/bin/sh\nprintf 'Claude Code version 3.1.4\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+filepath.Dir(gitPath))

	var out bytes.Buffer
	if code := runWithInput([]string{"sift", "agent", "add", "--offline", "--agent", "claude"}, strings.NewReader(""), &out, io.Discard); code != 0 {
		t.Fatalf("agent add = %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "Agent claude（claude 3.1.4）") {
		t.Fatalf("output does not report the probed version: %q", out.String())
	}
	snap, err := config.Load(home, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Config.Agents) != 1 {
		t.Fatalf("agents = %#v", snap.Config.Agents)
	}
}

func TestSelectAgents(t *testing.T) {
	found := []string{"claude", "codex", "pi"}
	for _, tt := range []struct {
		picked, want string
	}{
		{"", "claude,codex,pi"},    // Enter = all selected
		{"all", "claude,codex,pi"}, // explicit all
		{"ALL", "claude,codex,pi"},
		{"1,3", "claude,pi"},
		{"3,1", "pi,claude"},
		{"2", "codex"},
		{" 1, 3 ", "claude,pi"},
		{"0", ""},
		{"none", ""},
		{"1,9", "claude"},          // out-of-range entries are dropped
		{"claude,pi", "claude,pi"}, // legacy names pass through
	} {
		if got := selectAgents(tt.picked, found); got != tt.want {
			t.Fatalf("selectAgents(%q) = %q, want %q", tt.picked, got, tt.want)
		}
	}
}

func TestParseOperatorSpec(t *testing.T) {
	github, gitlab := "github:alice", "gitlab:bob"
	specs, err := parseOperatorSpec(github+","+gitlab, "github")
	if err != nil {
		t.Fatal(err)
	}
	if !equalStringSlice(specs["github"], []string{"alice"}) || !equalStringSlice(specs["gitlab"], []string{"bob"}) {
		t.Fatalf("parseOperatorSpec = %#v", specs)
	}
	// Plain names attach to the project kind default.
	specs, err = parseOperatorSpec("carol, github:dan", "gitlab")
	if err != nil {
		t.Fatal(err)
	}
	if !equalStringSlice(specs["gitlab"], []string{"carol"}) || !equalStringSlice(specs["github"], []string{"dan"}) {
		t.Fatalf("parseOperatorSpec plain = %#v", specs)
	}
	if _, err := parseOperatorSpec("bogus:user", "github"); err == nil {
		t.Fatal("unrecognized kind prefix was accepted")
	}
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestProjectAddForgeOverridePersistsDetectedHost(t *testing.T) {
	_ = freshHome(t)
	home, err := config.ResolveHome()
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(t.TempDir(), "demo")
	if out, err := exec.Command("git", "init", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	// Host maps to no known forge: --forge overrides the kind, but the
	// probed host must still be persisted instead of being dropped.
	if out, err := exec.Command("git", "-C", repo, "remote", "add", "origin", "git@git.corp.example:group/proj.git").CombinedOutput(); err != nil {
		t.Fatalf("git remote: %v: %s", err, out)
	}
	var out bytes.Buffer
	if code := runWithInput([]string{"sift", "project", "add", "--offline", "--project", repo, "--forge", "gitlab"}, strings.NewReader(""), &out, io.Discard); code != 0 {
		t.Fatalf("project add = %d: %s", code, out.String())
	}
	snap, err := config.Load(home, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Config.Projects) != 1 {
		t.Fatalf("projects = %#v", snap.Config.Projects)
	}
	p := snap.Config.Projects[0].Forge
	if p.Kind != config.ForgeKindGitLab || p.Project != "group/proj" || p.Host != "git.corp.example" {
		t.Fatalf("forge ref = %#v, want gitlab/group/proj@git.corp.example", p)
	}
}

func TestInteractiveProjectAddAskOncePersistsHost(t *testing.T) {
	_ = freshHome(t)
	home, err := config.ResolveHome()
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(t.TempDir(), "demo")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", repo, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	// Undetectable host: the one-time prompt answers gitlab, and the host
	// must survive into forge.host (issue #929 review F1).
	if out, err := exec.Command("git", "-C", repo, "remote", "add", "origin", "git@git.corp.example:group/proj.git").CombinedOutput(); err != nil {
		t.Fatalf("git remote: %v: %s", err, out)
	}
	t.Chdir(repo)
	repo, err = filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if code := runWithInput([]string{"sift", "project", "add"}, strings.NewReader("gitlab\n"), &out, io.Discard); code != 0 {
		t.Fatalf("project add = %d: %s", code, out.String())
	}
	snap, err := config.Load(home, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Config.Projects) != 1 {
		t.Fatalf("projects = %#v", snap.Config.Projects)
	}
	p := snap.Config.Projects[0].Forge
	if p.Kind != config.ForgeKindGitLab || p.Project != "group/proj" || p.Host != "git.corp.example" {
		t.Fatalf("forge ref = %#v, want gitlab/group/proj@git.corp.example", p)
	}
}

func TestInitFlagsOperatorLoginFallback(t *testing.T) {
	_ = freshHome(t)
	home, err := config.ResolveHome()
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(t.TempDir(), "demo")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", repo, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	if out, err := exec.Command("git", "-C", repo, "remote", "add", "origin", "git@github.com:owner/repo.git").CombinedOutput(); err != nil {
		t.Fatalf("git remote: %v: %s", err, out)
	}
	agent := filepath.Join(t.TempDir(), "fake-agent")
	if err := os.WriteFile(agent, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	// Both detected identities are used directly, even though the project is
	// GitHub-bound. Init must not ask the user to confirm either login.
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte("#!/bin/sh\nprintf 'github.com\\n  ✓ Logged in to github.com account probe-user (keyring)\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "glab"), []byte("#!/bin/sh\nprintf 'Logged in to gitlab.com as gitlab-user\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+filepath.Dir(gitPath))
	var out bytes.Buffer
	if code := runWithInput([]string{"sift", "init", "--agent", agent, "--project", repo}, strings.NewReader(""), &out, io.Discard); code != 0 {
		t.Fatalf("init = %d: %s", code, out.String())
	}
	snap, err := config.Load(home, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got := snap.Config.Operators.GitHub; len(got) != 1 || got[0] != "probe-user" {
		t.Fatalf("operators.github = %#v, want [probe-user]", got)
	}
	if got := snap.Config.Operators.GitLab; len(got) != 1 || got[0] != "gitlab-user" {
		t.Fatalf("operators.gitlab = %#v, want [gitlab-user]", got)
	}
	if strings.Contains(out.String(), "操作员用户名") || !strings.Contains(out.String(), "✓ operator: probe-user") || !strings.Contains(out.String(), "✓ operator: gitlab-user") {
		t.Fatalf("detected operators were not used directly: %q", out.String())
	}
	if len(snap.Config.Projects) != 1 || snap.Config.Projects[0].Forge.Host != "github.com" {
		t.Fatalf("projects = %#v (default github.com host stays omitted)", snap.Config.Projects)
	}
}

func TestInteractiveInitProbeLoginUsesOperatorWithoutPrompt(t *testing.T) {
	_ = freshHome(t)
	home, err := config.ResolveHome()
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(t.TempDir(), "demo")
	if out, err := exec.Command("git", "init", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	if out, err := exec.Command("git", "-C", repo, "remote", "add", "origin", "git@github.com:owner/repo.git").CombinedOutput(); err != nil {
		t.Fatalf("git remote: %v: %s", err, out)
	}
	t.Chdir(repo)
	agentBin := t.TempDir()
	for _, name := range []string{"claude", "gh"} {
		body := "#!/bin/sh\n"
		if name == "claude" {
			body += "printf 'Claude Code version 2.0.0\\n'\n"
		} else {
			body += "printf 'github.com\\n  ✓ Logged in to github.com account probe-user (keyring)\\n'\n"
		}
		if err := os.WriteFile(filepath.Join(agentBin, name), []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", agentBin+string(os.PathListSeparator)+filepath.Dir(gitPath))

	var out bytes.Buffer
	// The only answers are the glab install decline (gh is already logged in)
	// and agent selection. A successful gh probe must consume no operator
	// answer and must be written directly to the allowlist.
	if code := runWithInput([]string{"sift", "init"}, strings.NewReader("n\nall\n"), &out, io.Discard); code != 0 {
		t.Fatalf("init = %d: %s", code, out.String())
	}
	if strings.Contains(out.String(), "操作员用户名") || !strings.Contains(out.String(), "✓ operator: probe-user") {
		t.Fatalf("probe login was not used without prompting: %q", out.String())
	}
	snap, err := config.Load(home, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got := snap.Config.Operators.GitHub; len(got) != 1 || got[0] != "probe-user" {
		t.Fatalf("operators.github = %#v, want [probe-user]", got)
	}
}

func TestProjectAddSelfHostedGitlabHostPersisted(t *testing.T) {
	_ = freshHome(t)
	home, err := config.ResolveHome()
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(t.TempDir(), "demo")
	if out, err := exec.Command("git", "init", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	if out, err := exec.Command("git", "-C", repo, "remote", "add", "origin", "ssh://git@gitlab.hexinfo.cn/group/proj.git").CombinedOutput(); err != nil {
		t.Fatalf("git remote: %v: %s", err, out)
	}
	var out bytes.Buffer
	if code := runWithInput([]string{"sift", "project", "add", "--offline", "--project", repo}, strings.NewReader(""), &out, io.Discard); code != 0 {
		t.Fatalf("project add = %d: %s", code, out.String())
	}
	snap, err := config.Load(home, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Config.Projects) != 1 {
		t.Fatalf("projects = %#v", snap.Config.Projects)
	}
	p := snap.Config.Projects[0]
	if p.Forge.Kind != config.ForgeKindGitLab || p.Forge.Project != "group/proj" || p.Forge.Host != "gitlab.hexinfo.cn" {
		t.Fatalf("forge ref = %#v, want gitlab/group/proj@gitlab.hexinfo.cn", p.Forge)
	}
}

func TestInteractiveProjectAddCwdAutoDetect(t *testing.T) {
	_ = freshHome(t)
	home, err := config.ResolveHome()
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(t.TempDir(), "demo")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", repo, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	if out, err := exec.Command("git", "-C", repo, "remote", "add", "origin", "git@github.com:owner/repo.git").CombinedOutput(); err != nil {
		t.Fatalf("git remote: %v: %s", err, out)
	}
	t.Chdir(repo)
	repo, err = filepath.EvalSymlinks(repo) // git canonicalizes /var→/private/var on macOS
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	// No --project, no --forge: cwd project, forge auto-detected from origin.
	if code := runWithInput([]string{"sift", "project", "add"}, strings.NewReader("\n"), &out, io.Discard); code != 0 {
		t.Fatalf("project add = %d: %s", code, out.String())
	}
	if strings.Contains(out.String(), "Forge 类型") {
		t.Fatalf("project add asked a forge question: %q", out.String())
	}
	snap, err := config.Load(home, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Config.Projects) != 1 || snap.Config.Projects[0].Repo != repo {
		t.Fatalf("projects = %#v", snap.Config.Projects)
	}
	if p := snap.Config.Projects[0].Forge; p.Kind != config.ForgeKindGitHub || p.Project != "owner/repo" {
		t.Fatalf("forge ref = %#v, want github/owner/repo", p)
	}
}

func TestInteractiveInitNumberedAgentSelection(t *testing.T) {
	_ = freshHome(t)
	home, err := config.ResolveHome()
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(t.TempDir(), "demo")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", repo, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	if out, err := exec.Command("git", "-C", repo, "remote", "add", "origin", "git@github.com:owner/repo.git").CombinedOutput(); err != nil {
		t.Fatalf("git remote: %v: %s", err, out)
	}
	t.Chdir(repo)
	// Deterministic agent discovery: only fake claude/codex/pi plus git are on
	// PATH, so gh/glab probes find no login and selection is stable.
	bin := t.TempDir()
	for _, name := range []string{"claude", "codex", "pi"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+filepath.Dir(gitPath))
	var out bytes.Buffer
	// Answers: gh install=n ; glab install=n ; agents=1,3 ; project=Enter (cwd) ;
	// operator=Enter (skip).
	if code := runWithInput([]string{"sift", "init"}, strings.NewReader("n\nn\n1,3\n\n"), &out, io.Discard); code != 0 {
		t.Fatalf("init = %d: %s", code, out.String())
	}
	if strings.Contains(out.String(), "Forge 类型") {
		t.Fatalf("init asked a forge question: %q", out.String())
	}
	snap, err := config.Load(home, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Config.Agents) != 2 {
		t.Fatalf("agents = %#v, want the 1,3 subset", snap.Config.Agents)
	}
	if got := snap.Config.Agents[0]; got.ID != "claude" || got.Executable != "claude" {
		t.Fatalf("agent[0] = %#v", got)
	}
	if got := snap.Config.Agents[1]; got.ID != "pi" || got.Executable != "pi" {
		t.Fatalf("agent[1] = %#v", got)
	}
	if len(snap.Config.Projects) != 1 || snap.Config.Projects[0].Forge.Kind != config.ForgeKindGitHub {
		t.Fatalf("projects = %#v", snap.Config.Projects)
	}
}

func TestProjectAddNonRepoErrors(t *testing.T) {
	_ = freshHome(t)
	dir := t.TempDir()
	t.Chdir(dir)
	if repo := detectedRepo(); repo != "" {
		t.Skipf("test temp dir is inside a git worktree: %s", repo)
	}
	var out, errb bytes.Buffer
	if code := runWithInput([]string{"sift", "project", "add", "--offline"}, strings.NewReader(""), &out, &errb); code != 1 {
		t.Fatalf("project add offline outside a repo = %d, want 1: stdout=%q", code, out.String())
	}
	if !strings.Contains(errb.String(), "cd 到项目目录") {
		t.Fatalf("error is not actionable: %q", errb.String())
	}
}

func TestSetupAddAndDaemonAwareHint(t *testing.T) {
	_ = freshHome(t)
	home, err := config.ResolveHome()
	if err != nil {
		t.Fatal(err)
	}
	agent := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(agent, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if code := runWithInput([]string{"sift", "agent", "add", "--offline", "--agent", agent}, strings.NewReader(""), &out, io.Discard); code != 0 {
		t.Fatalf("agent add = %d: %s", code, out.String())
	}
	snap, err := config.Load(home, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got := snap.Config.Agents[0].Args; strings.Join(got, ",") != "-p" {
		t.Fatalf("default args = %#v", got)
	}
	codex := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(codex, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if code := runWithInput([]string{"sift", "agent", "add", "--offline", "--agent", codex, "--agent-args=--custom,value"}, strings.NewReader(""), &out, io.Discard); code != 0 {
		t.Fatalf("agent add override = %d: %s", code, out.String())
	}
	snap, err = config.Load(home, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got := snap.Config.Agents[1].Args; strings.Join(got, ",") != "--custom,value" {
		t.Fatalf("override args = %#v", got)
	}
	addr := net.UnixAddr{Name: filepath.Join(home.Path, "siftd.sock"), Net: "unix"}
	listener, err := net.ListenUnix("unix", &addr)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	out.Reset()
	if code := runWithInput([]string{"sift", "agent", "add", "--offline", "--agent", agent}, strings.NewReader(""), &out, io.Discard); code != 0 {
		t.Fatalf("agent add = %d: %s", code, out.String())
	}
	if strings.Contains(out.String(), "sift service reload") || !strings.Contains(out.String(), "前台运行") {
		t.Fatalf("daemon-aware output = %q", out.String())
	}
}

// ---- issue #960: init dependency guidance ----------------------------------

// fakeCommand is a test double for setupCmd: CI never really installs packages
// or runs official auth flows (issue #960 acceptance 5). lookup defaults to
// false, output to an error, run records invocations and returns nil.
type fakeCommand struct {
	found    map[string]bool
	outputFn func(name string, args ...string) (string, error)
	runFn    func(name string, args ...string) error
	runs     [][]string
}

func (f *fakeCommand) lookup(name string) bool { return f.found[name] }
func (f *fakeCommand) output(name string, args ...string) (string, error) {
	if f.outputFn != nil {
		return f.outputFn(name, args...)
	}
	return "", errors.New("fake command: not found")
}
func (f *fakeCommand) run(name string, args ...string) error {
	f.runs = append(f.runs, append([]string{name}, args...))
	if f.runFn != nil {
		return f.runFn(name, args...)
	}
	return nil
}

func replaceSetupCmd(t *testing.T, f *fakeCommand) {
	t.Helper()
	prev := setupCmd
	setupCmd = f
	t.Cleanup(func() { setupCmd = prev })
}

// initTestRepo creates a git repo with a GitHub origin and chdirs into it.
func initTestRepo(t *testing.T) config.Home {
	t.Helper()
	_ = freshHome(t)
	home, err := config.ResolveHome()
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(t.TempDir(), "demo")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", repo, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	if out, err := exec.Command("git", "-C", repo, "remote", "add", "origin", "git@github.com:owner/repo.git").CombinedOutput(); err != nil {
		t.Fatalf("git remote: %v: %s", err, out)
	}
	t.Chdir(repo)
	return home
}

// gitOnlyPATH narrows PATH to the directory holding git so no forge CLI, npm or
// coding agent is visible (deterministic probes regardless of the host).
func gitOnlyPATH(t *testing.T) {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(gitPath))
}

// TestInitForgeMissingGuidesInstall pins acceptance 1 (missing → install
// prompt) and acceptance 2 (declining install still completes init with exit
// 0 and the config written).
func TestInitForgeMissingGuidesInstall(t *testing.T) {
	home := initTestRepo(t)
	gitOnlyPATH(t)

	var out bytes.Buffer
	// Answers: gh install=n ; glab install=n ; pi=n ; agent fallback=Enter ;
	// operator=Enter (EOF).
	if code := runWithInput([]string{"sift", "init"}, strings.NewReader("n\nn\nn\n\n"), &out, io.Discard); code != 0 {
		t.Fatalf("init = %d: %s", code, out.String())
	}
	for _, want := range []string{
		"未检测到 GitHub CLI（gh）", "是否现在安装 gh",
		"未检测到 GitLab CLI（glab）", "是否现在安装 glab",
		"手动安装 pi",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("guidance output missing %q:\n%s", want, out.String())
		}
	}
	snap, err := config.Load(home, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Config.Projects) != 1 || len(snap.Config.Agents) != 0 {
		t.Fatalf("decline must still complete project binding without agents: %#v", snap.Config)
	}
}

// TestInitForgeNotLoggedGuidesLogin pins acceptance 1 (installed-not-logged →
// login question): declining falls back to the manual operator question, and a
// successful official auth login records the operator without asking.
func TestInitForgeNotLoggedGuidesLogin(t *testing.T) {
	t.Run("login success records operator silently", func(t *testing.T) {
		home := initTestRepo(t)
		gitOnlyPATH(t)
		calls := 0
		fake := &fakeCommand{found: map[string]bool{"gh": true, "glab": true}}
		fake.outputFn = func(name string, args ...string) (string, error) {
			if name == "gh" {
				calls++
				if calls == 1 {
					return "", errors.New("gh not logged in")
				}
				return "github.com\n  ✓ Logged in to github.com account gh-user (keyring)\n", nil
			}
			return "", errors.New("fake command: not found")
		}
		replaceSetupCmd(t, fake)

		var out bytes.Buffer
		// Answers: gh login=y ; glab login=n ; pi=n ; agent fallback=Enter ;
		// operator=Enter (EOF).
		if code := runWithInput([]string{"sift", "init"}, strings.NewReader("y\nn\nn\n\n"), &out, io.Discard); code != 0 {
			t.Fatalf("init = %d: %s", code, out.String())
		}
		for _, want := range []string{"检测到 gh 未登录", "是否现在运行官方 gh auth login", "✓ 已检测到 GitHub 登录：gh-user", "✓ operator: gh-user"} {
			if !strings.Contains(out.String(), want) {
				t.Fatalf("output missing %q:\n%s", want, out.String())
			}
		}
		if got := fake.runs; len(got) != 1 || strings.Join(got[0], " ") != "gh auth login" {
			t.Fatalf("official auth login was not passed through: %#v", got)
		}
		snap, err := config.Load(home, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if got := snap.Config.Operators.GitHub; len(got) != 1 || got[0] != "gh-user" {
			t.Fatalf("operators.github = %#v, want [gh-user]", got)
		}
		if strings.Contains(out.String(), "操作员用户名") {
			t.Fatalf("logged operator must not be asked to confirm: %q", out.String())
		}
	})

	t.Run("decline falls back to manual operator question", func(t *testing.T) {
		home := initTestRepo(t)
		gitOnlyPATH(t)
		fake := &fakeCommand{found: map[string]bool{"gh": true}}
		fake.outputFn = func(name string, args ...string) (string, error) {
			if name == "gh" {
				return "", errors.New("gh not logged in")
			}
			return "", errors.New("fake command: not found")
		}
		replaceSetupCmd(t, fake)

		var out bytes.Buffer
		// Answers: gh login=n ; glab install=n ; pi=n ; agent fallback=Enter ;
		// operator=alice.
		if code := runWithInput([]string{"sift", "init"}, strings.NewReader("n\nn\nn\n\nalice\n"), &out, io.Discard); code != 0 {
			t.Fatalf("init = %d: %s", code, out.String())
		}
		if !strings.Contains(out.String(), "是否现在运行官方 gh auth login") {
			t.Fatalf("decline should keep the login question: %q", out.String())
		}
		snap, err := config.Load(home, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if got := snap.Config.Operators.GitHub; len(got) != 1 || got[0] != "alice" {
			t.Fatalf("operators.github = %#v, want [alice]", got)
		}
	})
}

// TestInitForgeInstallFailureDegrades pins acceptance 2: a failed install
// command degrades to the official manual path and init still completes with
// exit code 0 and the config written.
func TestInitForgeInstallFailureDegrades(t *testing.T) {
	home := initTestRepo(t)
	gitOnlyPATH(t)
	fake := &fakeCommand{found: map[string]bool{"brew": true, "npm": true}}
	fake.runFn = func(name string, args ...string) error {
		if name == "brew" {
			return errors.New("permission denied")
		}
		return nil
	}
	replaceSetupCmd(t, fake)

	var out bytes.Buffer
	// Answers: gh install=y ; glab install=n ; pi=n ; agent fallback=Enter ;
	// operator=Enter (EOF).
	if code := runWithInput([]string{"sift", "init"}, strings.NewReader("y\nn\nn\n\n"), &out, io.Discard); code != 0 {
		t.Fatalf("init = %d: %s", code, out.String())
	}
	for _, want := range []string{"自动安装 gh 失败", "cli.github.com"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("degradation output missing %q:\n%s", want, out.String())
		}
	}
	if _, err := os.Stat(config.ConfigPath(home)); err != nil {
		t.Fatalf("config was not written after failed install: %v", err)
	}
}

// TestInitPiBootstrapInstallsAndRegisters pins acceptance 3: an empty agent
// scan offers pi first; confirming installs via npm, verifies pi and writes
// config agents[pi] (default -p args, reusing addAgent).
func TestInitPiBootstrapInstallsAndRegisters(t *testing.T) {
	home := initTestRepo(t)
	gitOnlyPATH(t)
	fake := &fakeCommand{found: map[string]bool{"npm": true}}
	fake.outputFn = func(name string, args ...string) (string, error) {
		if name == "pi" {
			return "pi 0.9.9\n", nil
		}
		return "", errors.New("fake command: not found")
	}
	replaceSetupCmd(t, fake)

	var out bytes.Buffer
	// Answers: gh install=n ; glab install=n ; pi=Enter (yes) ; operator=Enter (EOF).
	if code := runWithInput([]string{"sift", "init"}, strings.NewReader("n\nn\n\n"), &out, io.Discard); code != 0 {
		t.Fatalf("init = %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "推荐安装 pi（开源，多模型，支持订阅/API Key）") {
		t.Fatalf("pi guidance missing: %q", out.String())
	}
	wantRun := []string{"npm", "install", "-g", "--ignore-scripts", "@earendil-works/pi-coding-agent"}
	if len(fake.runs) != 1 || strings.Join(fake.runs[0], " ") != strings.Join(wantRun, " ") {
		t.Fatalf("npm install = %#v, want %v", fake.runs, wantRun)
	}
	snap, err := config.Load(home, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Config.Agents) != 1 || snap.Config.Agents[0].ID != "pi" || snap.Config.Agents[0].Executable != "pi" {
		t.Fatalf("agents = %#v, want pi registered", snap.Config.Agents)
	}
	if got := snap.Config.Agents[0].Args; strings.Join(got, ",") != "-p" {
		t.Fatalf("pi default args = %#v, want [-p]", got)
	}
}

// TestInitPiBootstrapDeclinedPrintsGuidance pins acceptance 3: declining the pi
// offer prints the manual path and does not block the wizard.
func TestInitPiBootstrapDeclinedPrintsGuidance(t *testing.T) {
	home := initTestRepo(t)
	gitOnlyPATH(t)

	var out bytes.Buffer
	// Answers: gh install=n ; glab install=n ; pi=n ; agent fallback=Enter ;
	// operator=Enter (EOF).
	if code := runWithInput([]string{"sift", "init"}, strings.NewReader("n\nn\nn\n\n"), &out, io.Discard); code != 0 {
		t.Fatalf("init = %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "curl -fsSL https://pi.dev/install.sh | sh") {
		t.Fatalf("declined pi install must print the manual path: %q", out.String())
	}
	snap, err := config.Load(home, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Config.Agents) != 0 {
		t.Fatalf("agents = %#v, want none", snap.Config.Agents)
	}
}

// TestInitPiBootstrapNoNpmDegrades pins acceptance 3: without npm the pi offer
// degrades to the official script guidance and does not block.
func TestInitPiBootstrapNoNpmDegrades(t *testing.T) {
	home := initTestRepo(t)
	gitOnlyPATH(t)
	fake := &fakeCommand{} // npm not on PATH
	replaceSetupCmd(t, fake)

	var out bytes.Buffer
	// Answers: gh install=n ; glab install=n ; pi=y ; agent fallback=Enter ;
	// operator=Enter (EOF).
	if code := runWithInput([]string{"sift", "init"}, strings.NewReader("n\nn\ny\n\n"), &out, io.Discard); code != 0 {
		t.Fatalf("init = %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "未检测到 npm") || !strings.Contains(out.String(), "https://pi.dev/install.sh") {
		t.Fatalf("no-npm degradation missing: %q", out.String())
	}
	snap, err := config.Load(home, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Config.Agents) != 0 {
		t.Fatalf("agents = %#v, want none", snap.Config.Agents)
	}
}

// TestInitNonInteractivePathsSkipGuidance pins acceptance 4: --offline probes
// nothing, and the flags-all-given path keeps only the graded report — no
// install/login/pi prompts either way.
func TestInitNonInteractivePathsSkipGuidance(t *testing.T) {
	t.Run("offline", func(t *testing.T) {
		home := initTestRepo(t)
		gitOnlyPATH(t)
		var out bytes.Buffer
		if code := runWithInput([]string{"sift", "init", "--offline"}, strings.NewReader(""), &out, io.Discard); code != 0 {
			t.Fatalf("init offline = %d: %s", code, out.String())
		}
		for _, forbidden := range []string{"是否现在安装", "是否现在运行官方", "推荐安装 pi", "未检测到 GitHub CLI", "已检测到 GitHub 登录"} {
			if strings.Contains(out.String(), forbidden) {
				t.Fatalf("offline init must skip all guidance/report, got %q in %q", forbidden, out.String())
			}
		}
		if _, err := os.Stat(config.ConfigPath(home)); err != nil {
			t.Fatalf("config was not written: %v", err)
		}
	})

	t.Run("flags given", func(t *testing.T) {
		home := initTestRepo(t)
		gitOnlyPATH(t)
		agent := filepath.Join(t.TempDir(), "fake-agent")
		if err := os.WriteFile(agent, []byte("#!/bin/sh\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		// --agent/--project given: non-interactive. The graded report still
		// shows the missing CLI, but no guidance prompt may appear.
		if code := runWithInput([]string{"sift", "init", "--agent", agent, "--project", "."}, strings.NewReader(""), &out, io.Discard); code != 0 {
			t.Fatalf("init flags = %d: %s", code, out.String())
		}
		if !strings.Contains(out.String(), "未检测到 GitHub CLI（gh）") {
			t.Fatalf("graded report missing for flags path: %q", out.String())
		}
		for _, forbidden := range []string{"是否现在安装", "是否现在运行官方", "推荐安装 pi"} {
			if strings.Contains(out.String(), forbidden) {
				t.Fatalf("flags path must not prompt, got %q in %q", forbidden, out.String())
			}
		}
		snap, err := config.Load(home, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if len(snap.Config.Agents) != 1 {
			t.Fatalf("agents = %#v", snap.Config.Agents)
		}
	})
}

// TestProbeForgeLoginThreeStates pins acceptance 1 at the unit level: the probe
// distinguishes missing, installed-not-logged and logged via the injected
// runner.
func TestProbeForgeLoginThreeStates(t *testing.T) {
	fake := &fakeCommand{found: map[string]bool{"gh": true}}
	fake.outputFn = func(name string, args ...string) (string, error) {
		if name == "gh" && len(args) == 2 && args[0] == "auth" && args[1] == "status" {
			return "github.com\n  ✓ Logged in to github.com account alice (keyring)\n", nil
		}
		return "", errors.New("fake command: not found")
	}
	replaceSetupCmd(t, fake)

	if got := probeForgeLogin("github"); !got.installed || got.login != "alice" {
		t.Fatalf("logged probe = %#v", got)
	}
	fake.found["gh"] = false
	if got := probeForgeLogin("github"); got.installed || got.login != "" {
		t.Fatalf("missing probe = %#v", got)
	}
	fake.found["gh"] = true
	fake.outputFn = func(string, ...string) (string, error) { return "", errors.New("not logged in") }
	if got := probeForgeLogin("github"); !got.installed || got.login != "" {
		t.Fatalf("installed-not-logged probe = %#v", got)
	}
}

// TestReportForgeLoginsGraded pins the three-state report wording: missing and
// installed-not-logged are distinguishable, logged shows the identity.
func TestReportForgeLoginsGraded(t *testing.T) {
	var out bytes.Buffer
	reportForgeLogins(&out, forgeLogins{
		github: forgeProbe{installed: true, login: "alice"},
		gitlab: forgeProbe{installed: true},
	})
	for _, want := range []string{"已检测到 GitHub 登录：alice", "检测到 glab 未登录；请运行 glab auth login。"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("graded report missing %q: %q", want, out.String())
		}
	}
	out.Reset()
	reportForgeLogins(&out, forgeLogins{})
	for _, want := range []string{"未检测到 GitHub CLI（gh）", "未检测到 GitLab CLI（glab）"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("graded report missing %q: %q", want, out.String())
		}
	}
}

// TestAskYes pins the confirm-first semantics: Enter and y/yes/是 confirm,
// anything else (n/no, unexpected text) declines — installs are never silent
// (issue #960 §2 红线).
func TestAskYes(t *testing.T) {
	for _, tt := range []struct {
		answer, name string
		want         bool
	}{
		{"\n", "enter", true},
		{"y\n", "y", true},
		{"yes\n", "yes", true},
		{"是\n", "是", true},
		{"n\n", "n", false},
		{"no\n", "no", false},
		{"garbage\n", "unexpected", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			if got := askYes(bufio.NewReader(strings.NewReader(tt.answer)), &out, "测试问题"); got != tt.want {
				t.Fatalf("askYes(%q) = %v, want %v", tt.answer, got, tt.want)
			}
		})
	}
}

// TestPiAuthLikely pins the weak login signal: the auth file or a common API
// key env var counts as possibly logged in, otherwise guidance is shown.
func TestPiAuthLikely(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, k := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "OPENROUTER_API_KEY", "GEMINI_API_KEY"} {
		t.Setenv(k, "")
	}
	if piAuthLikely() {
		t.Fatal("empty env + no auth file must not count as logged in")
	}
	if err := os.MkdirAll(filepath.Join(home, ".pi", "agent"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".pi", "agent", "auth.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !piAuthLikely() {
		t.Fatal("auth.json must count as possibly logged in")
	}
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	if !piAuthLikely() {
		t.Fatal("ANTHROPIC_API_KEY must count as possibly logged in")
	}
}
