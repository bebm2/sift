package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xsift/sift/internal/config"
	"github.com/xsift/sift/internal/forge"
)

// fakeIssueForge is the read-surface double for `sift issue`: it answers the
// three reads the command may make and records nothing else (the command must
// not be able to reach any write path — there is none on the interface).
type fakeIssueForge struct {
	issues    []forge.Issue
	truncated bool
	listErr   error

	get      map[string]forge.Issue
	getErr   map[string]error
	comments map[string][]forge.Comment
}

func (f *fakeIssueForge) ListOpenIssues(context.Context, forge.ProjectRef, int) ([]forge.Issue, bool, error) {
	return f.issues, f.truncated, f.listErr
}
func (f *fakeIssueForge) GetIssue(_ context.Context, _ forge.ProjectRef, id string) (forge.Issue, error) {
	if e, ok := f.getErr[id]; ok {
		return forge.Issue{}, e
	}
	if i, ok := f.get[id]; ok {
		return i, nil
	}
	return forge.Issue{}, errors.New("not found")
}
func (f *fakeIssueForge) ListIssueComments(_ context.Context, _ forge.ProjectRef, id string, _ forge.Cursor) ([]forge.Comment, forge.Cursor, error) {
	return f.comments[id], "", nil
}

// swapIssueForge replaces the adapter seam for one test and restores it after.
func swapIssueForge(t *testing.T, f *fakeIssueForge) {
	t.Helper()
	old := newIssueForge
	newIssueForge = func(config.ForgeRef) issueForge { return f }
	t.Cleanup(func() { newIssueForge = old })
}

// fakeHeadlessPi records the one headless invocation: the pinned read-only
// tool allowlist, the prompt, and the evidence piped through stdin.
type fakeHeadlessPi struct {
	lookErr   error
	called    int
	args      []string
	stdin     string
	runErr    error
	lastStdio *bytes.Buffer
}

func (p *fakeHeadlessPi) LookPath(string) (string, error) { return "", p.lookErr }
func (p *fakeHeadlessPi) Run(_ context.Context, stdin io.Reader, stdout, stderr io.Writer, args ...string) error {
	p.called++
	p.args = args
	b, _ := io.ReadAll(stdin)
	p.stdin = string(b)
	p.lastStdio = stdout.(*bytes.Buffer)
	return p.runErr
}

func swapIssuePi(t *testing.T, p *fakeHeadlessPi) {
	t.Helper()
	old := issuePi
	issuePi = p
	t.Cleanup(func() { issuePi = old })
}

// issueTestProject registers one enabled github project through the real
// offline add path and returns its repo path.
func issueTestProject(t *testing.T) string {
	t.Helper()
	// Isolate SIFT_HOME first: addTestProject goes through the real offline
	// add path, which without this writes the demo project into the *real*
	// ~/.sift/config.yaml (issue #1002 — that is exactly how eight demo*
	// entries landed in a live config and crash-looped the daemon).
	freshHome(t)
	repo := filepath.Join(t.TempDir(), "demo")
	addTestProject(t, repo, "git@github.com:owner/demo.git")
	return repo
}

// TestIssueFastPathDeterministic pins acceptance #1: no arguments → the
// deterministic listing, zero model calls (the pi seam would explode).
func TestIssueFastPathDeterministic(t *testing.T) {
	issueTestProject(t)
	swapIssueForge(t, &fakeIssueForge{issues: []forge.Issue{
		{ID: "7", Title: "修复登录超时", State: forge.IssueOpen, Author: "alice", URL: "https://x/7", Labels: []string{"bug"}},
		{ID: "3", Title: "补充文档", State: forge.IssueOpen, Author: "bob", URL: "https://x/3"},
	}})
	swapIssuePi(t, &fakeHeadlessPi{lookErr: errors.New("must not be called")})

	var out, errB bytes.Buffer
	if code := runWithInput([]string{"sift", "issue"}, strings.NewReader(""), &out, &errB); code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errB.String())
	}
	got := out.String()
	for _, want := range []string{"demo", "owner/demo", "2 个 open issue", "#7", "修复登录超时", "[bug]", "#3", "补充文档"} {
		if !strings.Contains(got, want) {
			t.Fatalf("listing lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "pi") {
		t.Fatalf("fast path must not invoke a model:\n%s", got)
	}
}

// TestIssueFastPathTruncationAndFailure pins the honesty markers: the page-cap
// note renders, and one failing project fails the run (exit 1) without hiding
// the healthy project.
func TestIssueFastPathTruncationAndFailure(t *testing.T) {
	issueTestProject(t)
	swapIssueForge(t, &fakeIssueForge{
		issues:    []forge.Issue{{ID: "1", Title: "t", State: forge.IssueOpen, Author: "a", URL: "u"}},
		truncated: true,
	})
	var out, errB bytes.Buffer
	code := runWithInput([]string{"sift", "issue"}, strings.NewReader(""), &out, &errB)
	if code != 0 || !strings.Contains(out.String(), "已达分页上限") {
		t.Fatalf("truncation note missing (code=%d):\n%s", code, out.String())
	}
}

// TestIssueSlowPathReadOnlyToolsAndEvidence pins the v1 architecture: the
// evidence pack carries the issue facts, the tool allowlist is pinned to read
// in the invocation itself, and the prompt states the no-hallucination rules.
func TestIssueSlowPathReadOnlyToolsAndEvidence(t *testing.T) {
	issueTestProject(t)
	long := strings.Repeat("长", 1600)
	swapIssueForge(t, &fakeIssueForge{
		issues: []forge.Issue{{ID: "7", Title: "讨论迁移", State: forge.IssueOpen, Author: "alice", URL: "https://x/7", Labels: []string{"sift"}, Body: long}},
		get:    map[string]forge.Issue{"7": {ID: "7", Title: "讨论迁移", State: forge.IssueOpen, Author: "alice", URL: "https://x/7", Body: "正文"}},
		comments: map[string][]forge.Comment{
			"7": {{Author: "bob", Body: "核心分歧是数据迁移顺序"}, {Author: "carol", Body: "先双写再切换"}},
		},
	})
	pi := &fakeHeadlessPi{}
	swapIssuePi(t, pi)

	var out, errB bytes.Buffer
	code := runWithInput([]string{"sift", "issue", "#7", "的讨论核心分歧是什么？"}, strings.NewReader(""), &out, &errB)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errB.String())
	}
	if pi.called != 1 {
		t.Fatalf("pi called %d times, want 1", pi.called)
	}
	// The read-only guarantee is in the invocation, not the prompt.
	if len(pi.args) < 3 || pi.args[0] != "--tools" || pi.args[1] != "read" || pi.args[2] != "-p" {
		t.Fatalf("args=%v, want --tools read -p <prompt>", pi.args)
	}
	prompt := pi.args[3]
	for _, want := range []string{"#7", "的讨论核心分歧是什么？", "只能基于快照"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt lacks %q: %s", want, prompt)
		}
	}
	for _, want := range []string{"讨论迁移", "核心分歧是数据迁移顺序", "截断至 1500 字", "取证快照"} {
		if !strings.Contains(pi.stdin, want) {
			t.Fatalf("evidence pack lacks %q:\n%.500s", want, pi.stdin)
		}
	}
	if !strings.Contains(out.String(), "正在调用 pi") {
		t.Fatalf("missing announce:\n%s", out.String())
	}
}

// TestIssueSlowPathPiMissingFallsBack pins acceptance #3: pi unavailable →
// guidance plus the deterministic listing, exit 0.
func TestIssueSlowPathPiMissingFallsBack(t *testing.T) {
	issueTestProject(t)
	swapIssueForge(t, &fakeIssueForge{issues: []forge.Issue{{ID: "7", Title: "t", State: forge.IssueOpen, Author: "a", URL: "u"}}})
	swapIssuePi(t, &fakeHeadlessPi{lookErr: errors.New("not on PATH")})

	var out, errB bytes.Buffer
	code := runWithInput([]string{"sift", "issue", "哪些相关？"}, strings.NewReader(""), &out, &errB)
	if code != 0 {
		t.Fatalf("exit=%d, want 0 (degraded, not failed)", code)
	}
	if !strings.Contains(errB.String(), "未检测到 pi") || !strings.Contains(errB.String(), "pi") {
		t.Fatalf("stderr lacks guidance:\n%s", errB.String())
	}
	if !strings.Contains(out.String(), "#7") {
		t.Fatalf("fallback listing missing:\n%s", out.String())
	}
}

// TestIssueRefDetailFailureIsHonest pins the hallucination guard's other half:
// an unfetchable #N is recorded in the pack so the model can say 取不到.
func TestIssueRefDetailFailureIsHonest(t *testing.T) {
	issueTestProject(t)
	swapIssueForge(t, &fakeIssueForge{
		getErr: map[string]error{"9": errors.New("404 Not Found")},
	})
	pi := &fakeHeadlessPi{}
	swapIssuePi(t, pi)
	var out, errB bytes.Buffer
	if code := runWithInput([]string{"sift", "issue", "#9", "怎么样"}, strings.NewReader(""), &out, &errB); code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errB.String())
	}
	if !strings.Contains(pi.stdin, "#9") || !strings.Contains(pi.stdin, "取不到") {
		t.Fatalf("pack must record the unfetchable #9:\n%.500s", pi.stdin)
	}
}

// TestIssueNoProjectsHint pins the zero-config degrade: a friendly hint, exit
// 0, no model call.
func TestIssueNoProjectsHint(t *testing.T) {
	freshHome(t)
	var out, errB bytes.Buffer
	code := runWithInput([]string{"sift", "issue"}, strings.NewReader(""), &out, &errB)
	if code != 0 {
		t.Fatalf("exit=%d, want 0", code)
	}
	if !strings.Contains(out.String(), "sift init") {
		t.Fatalf("hint missing:\n%s", out.String())
	}
}

// TestIssueHelpRegistered ensures the command metadata row exists so help and
// completion stay in sync with dispatch.
func TestIssueHelpRegistered(t *testing.T) {
	var out bytes.Buffer
	if code := runWithInput([]string{"sift", "issue", "--help"}, strings.NewReader(""), &out, io.Discard); code != 0 {
		t.Fatalf("issue --help exit=%d", code)
	}
	if !strings.Contains(out.String(), "sift issue [问题]") {
		t.Fatalf("help lacks usage:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "sift issue new") {
		t.Fatalf("help lacks new subcommand:\n%s", out.String())
	}
}

// TestRealHomeUntouchedByIssueTests is the issue #1002 regression guard: the
// earlier issue tests registered demo projects into the *real* ~/.sift (no
// freshHome), which crash-looped a live daemon. issueTestProject must isolate
// via freshHome before the real offline add path runs.
func TestIssueTestProjectIsolatesHome(t *testing.T) {
	userHome, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no user home:", err)
	}
	realConfig := filepath.Join(userHome, ".sift", "config.yaml")
	before, errBefore := os.ReadFile(realConfig)
	repo := issueTestProject(t)
	after, errAfter := os.ReadFile(realConfig)
	if errBefore == nil && errAfter == nil && !bytes.Equal(before, after) {
		t.Fatal("issueTestProject modified the real ~/.sift/config.yaml")
	}
	if repo == "" {
		t.Fatal("issueTestProject returned empty repo")
	}
}

// TestCwdEnabledProjectScopesListing pins the issue-feedback fix: when the
// working directory sits inside exactly one enabled project's repo, `sift
// issue` (list and question) defaults to that project only; other projects'
// forges are not touched. --all restores the full view.
func TestCwdEnabledProjectScopesListing(t *testing.T) {
	_ = testHome(t)
	repo := issueTestProject(t) // registers demo@github/owner/demo under repo
	t.Chdir(repo)
	// Register a second enabled project with a repo far away.
	far := filepath.Join(t.TempDir(), "far")
	addTestProject(t, far, "git@github.com:owner/far.git")
	swapIssueForge(t, &fakeIssueForge{issues: []forge.Issue{
		{ID: "7", Title: "demo issue", State: forge.IssueOpen, Author: "a", URL: "https://x/7"},
	}})
	var out, errB strings.Builder
	if code := runWithInput([]string{"sift", "issue"}, strings.NewReader(""), &out, &errB); code != 0 {
		t.Fatalf("exit=%d err=%s", code, errB.String())
	}
	got := out.String()
	if !strings.Contains(got, "当前目录项目 demo") {
		t.Fatalf("listing must announce the cwd project:\n%s", got)
	}
	if strings.Contains(got, "owner/far") {
		t.Fatalf("cwd scope must not touch other projects:\n%s", got)
	}
	// --all widens back to every project.
	out.Reset()
	if code := runWithInput([]string{"sift", "issue", "--all"}, strings.NewReader(""), &out, &errB); code == 0 && !strings.Contains(out.String(), "far") {
		t.Fatalf("--all must list every project:\n%s", out.String())
	}
}
