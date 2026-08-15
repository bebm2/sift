package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xsift/sift/internal/config"
	"github.com/xsift/sift/internal/forge"
)

// turnPi is the multi-turn headless-pi double: every Run call records the
// invocation, replies with the next canned reply, and simulates pi's session
// save so the host's --session continuation can be asserted.
type turnPi struct {
	replies []string
	calls   []struct {
		args  []string
		stdin string
	}
}

func (p *turnPi) LookPath(string) (string, error) { return "/usr/bin/pi", nil }

func (p *turnPi) Run(_ context.Context, stdin io.Reader, stdout, stderr io.Writer, args ...string) error {
	in, _ := io.ReadAll(stdin)
	p.calls = append(p.calls, struct {
		args  []string
		stdin string
	}{args, string(in)})
	// Simulate the session file pi saves under --session-dir.
	for i, a := range args {
		if a == "--session-dir" && i+1 < len(args) {
			_ = os.WriteFile(filepath.Join(args[i+1], "session.jsonl"), []byte("{}"), 0o600)
		}
	}
	reply := ""
	if len(p.calls) <= len(p.replies) {
		reply = p.replies[len(p.calls)-1]
	}
	fmt.Fprint(stdout, reply)
	return nil
}

func (p *turnPi) promptOf(n int) string {
	for i, a := range p.calls[n].args {
		if a == "-p" && i+1 < len(p.calls[n].args) {
			return p.calls[n].args[i+1]
		}
	}
	return ""
}

// fakeWriter records register-gate writes.
type fakeWriter struct {
	calls []struct {
		title  string
		body   string
		labels []string
	}
	err error
}

func (w *fakeWriter) CreateIssue(_ context.Context, _ forge.ProjectRef, title, body string, labels []string) (forge.Issue, error) {
	w.calls = append(w.calls, struct {
		title  string
		body   string
		labels []string
	}{title, body, labels})
	if w.err != nil {
		return forge.Issue{}, w.err
	}
	return forge.Issue{ID: "42", Title: title, URL: "https://x/42", State: forge.IssueOpen}, nil
}

// swapIssueNew installs the drafting-session seams for one test.
func swapIssueNew(t *testing.T, pi *turnPi, w *fakeWriter, f *fakeIssueForge) {
	t.Helper()
	oldPi, oldW, oldF := issuePi, newIssueWriter, newIssueForge
	issuePi = pi
	if w != nil {
		newIssueWriter = func(config.ForgeRef) issueWriter { return w }
	}
	if f != nil {
		newIssueForge = func(config.ForgeRef) issueForge { return f }
	}
	t.Cleanup(func() { issuePi, newIssueWriter, newIssueForge = oldPi, oldW, oldF })
}

const draftFenceReply = "好的，草稿如下：\n\n```issue\n给 sift issue 增加批量关闭能力\n\n## 背景\n管理 20+ open issue 时逐个关很繁琐。\n\n## 验收\n- `sift issue close 1,2,3` 幂等\n```\n有其他想补充的吗？"

func TestIssueNewRegistersAfterDiscussion(t *testing.T) {
	home := testHome(t)
	issueTestProject(t)
	pi := &turnPi{replies: []string{draftFenceReply, "已按你的补充更新草稿：\n\n```issue\n给 sift issue 增加批量关闭能力\n\n## 背景\n管理 20+ open issue 时逐个关很繁琐。\n\n## 验收\n- `sift issue close 1,2,3` 幂等\n- 支持 --dry-run\n```\n还有别的吗？"}}
	w := &fakeWriter{}
	swapIssueNew(t, pi, w, &fakeIssueForge{})

	var out, errB strings.Builder
	code := runIssueNew(nil, home, strings.NewReader("我想批量关闭 issue\n再补一条验收：要支持 --dry-run\n好，登记\ny\nn\n"), &out, &errB)
	if code != 0 {
		t.Fatalf("exit=%d out=%s err=%s", code, out.String(), errB.String())
	}
	if len(pi.calls) != 2 {
		t.Fatalf("pi turns=%d, want 2", len(pi.calls))
	}
	// Turn 1: read-only tools, private session dir, preamble + evidence on stdin.
	if pi.calls[0].args[0] != "--tools" || pi.calls[0].args[1] != "read" {
		t.Fatalf("turn1 args=%v", pi.calls[0].args)
	}
	if !strings.Contains(pi.promptOf(0), "起草助手") || !strings.Contains(pi.promptOf(0), "批量关闭") {
		t.Fatalf("turn1 prompt lacks preamble/question: %s", pi.promptOf(0))
	}
	// Turn 2: continues the saved session.
	if !strings.Contains(strings.Join(pi.calls[1].args, " "), "--session ") {
		t.Fatalf("turn2 args=%v, want --session continuation", pi.calls[1].args)
	}
	if !strings.Contains(pi.promptOf(1), "--dry-run") {
		t.Fatalf("turn2 prompt lacks follow-up: %s", pi.promptOf(1))
	}
	// Register gate: one write, no labels (trigger declined).
	if len(w.calls) != 1 {
		t.Fatalf("CreateIssue calls=%d", len(w.calls))
	}
	c := w.calls[0]
	if c.title != "给 sift issue 增加批量关闭能力" || !strings.Contains(c.body, "幂等") || !strings.Contains(c.body, "--dry-run") {
		t.Fatalf("created=%+v", c)
	}
	if len(c.labels) != 0 {
		t.Fatalf("labels=%v, want none (no implicit trigger)", c.labels)
	}
	if !strings.Contains(out.String(), "已登记 #42") || !strings.Contains(out.String(), "gh issue edit 42 --add-label") {
		t.Fatalf("output lacks URL/hint:\n%s", out.String())
	}
}

func TestIssueNewTriggerLabelOptIn(t *testing.T) {
	home := testHome(t)
	issueTestProject(t)
	pi := &turnPi{replies: []string{draftFenceReply}}
	w := &fakeWriter{}
	swapIssueNew(t, pi, w, &fakeIssueForge{})
	var out, errB strings.Builder
	code := runIssueNew(nil, home, strings.NewReader("讨论\n登记\ny\ny\n"), &out, &errB)
	if code != 0 || len(w.calls) != 1 {
		t.Fatalf("exit=%d calls=%d err=%s", code, len(w.calls), errB.String())
	}
	if len(w.calls[0].labels) != 1 || w.calls[0].labels[0] == "" {
		t.Fatalf("labels=%v, want the trigger label", w.calls[0].labels)
	}
	if strings.Contains(out.String(), "未打触发标签") {
		t.Fatalf("trigger hint shown despite opt-in:\n%s", out.String())
	}
}

func TestIssueNewRegisterWithoutDraftRendersThenCancels(t *testing.T) {
	home := testHome(t)
	issueTestProject(t)
	pi := &turnPi{replies: []string{"我先问两个问题…", draftFenceReply}}
	w := &fakeWriter{}
	swapIssueNew(t, pi, w, &fakeIssueForge{})
	var out, errB strings.Builder
	// 登记 before any fence → host asks the agent to render; then 放弃 at the
	// gate returns to the discussion; q exits.
	code := runIssueNew(nil, home, strings.NewReader("讨论\n登记\nn\nq\n"), &out, &errB)
	if code != 0 {
		t.Fatalf("exit=%d err=%s", code, errB.String())
	}
	if len(w.calls) != 0 {
		t.Fatalf("CreateIssue must not run after 放弃, calls=%d", len(w.calls))
	}
	if !strings.Contains(out.String(), "已取消登记") {
		t.Fatalf("output lacks cancel:\n%s", out.String())
	}
}

func TestIssueNewDedupeWarnsAndRequiresSecondConfirm(t *testing.T) {
	home := testHome(t)
	issueTestProject(t)
	pi := &turnPi{replies: []string{draftFenceReply}}
	w := &fakeWriter{}
	f := &fakeIssueForge{issues: []forge.Issue{
		{ID: "5", Title: "给 sift issue 增加批量关闭能力", State: forge.IssueOpen, Author: "a", URL: "https://x/5"},
	}}
	swapIssueNew(t, pi, w, f)
	var out, errB strings.Builder
	// 登记 → y → dedupe warning → decline (n) → back to discussion → q.
	code := runIssueNew(nil, home, strings.NewReader("讨论\n登记\ny\nn\nq\n"), &out, &errB)
	if code != 0 || len(w.calls) != 0 {
		t.Fatalf("exit=%d calls=%d", code, len(w.calls))
	}
	if !strings.Contains(out.String(), "已有同题 open issue") {
		t.Fatalf("dedupe warning missing:\n%s", out.String())
	}
}

func TestIssueNewRefFetchIsDeterministic(t *testing.T) {
	home := testHome(t)
	issueTestProject(t)
	pi := &turnPi{replies: []string{"收到，纳入讨论"}}
	w := &fakeWriter{}
	f := &fakeIssueForge{
		get:      map[string]forge.Issue{"7": {ID: "7", Title: "旧讨论", State: forge.IssueOpen, Author: "a", URL: "u", Body: "旧正文"}},
		comments: map[string][]forge.Comment{"7": {{Author: "b", Body: "旧评论"}}},
	}
	swapIssueNew(t, pi, w, f)
	var out, errB strings.Builder
	code := runIssueNew(nil, home, strings.NewReader("#7\n接着这个讨论\nq\n"), &out, &errB)
	if code != 0 {
		t.Fatalf("exit=%d err=%s", code, errB.String())
	}
	// #N costs no model call; the fetch lands in stdout for the human…
	if len(pi.calls) != 1 {
		t.Fatalf("pi turns=%d, want 1 (#7 is host-side)", len(pi.calls))
	}
	if !strings.Contains(out.String(), "旧讨论") || !strings.Contains(out.String(), "旧评论") {
		t.Fatalf("human fetch output missing:\n%s", out.String())
	}
	// …and is injected into the next model turn.
	if !strings.Contains(pi.promptOf(0), "补充取证 #7") || !strings.Contains(pi.promptOf(0), "旧正文") {
		t.Fatalf("context injection missing: %s", pi.promptOf(0))
	}
}

func TestIssueNewPiMissingExitsWithGuidance(t *testing.T) {
	home := testHome(t)
	issueTestProject(t)
	old := issuePi
	issuePi = missingPi{}
	t.Cleanup(func() { issuePi = old })
	var out, errB strings.Builder
	code := runIssueNew(nil, home, strings.NewReader("讨论\n"), &out, &errB)
	if code != 1 {
		t.Fatalf("exit=%d, want 1", code)
	}
	if !strings.Contains(errB.String(), "未检测到 pi") || !strings.Contains(errB.String(), "pi") {
		t.Fatalf("stderr lacks guidance:\n%s", errB.String())
	}
}

type missingPi struct{}

func (missingPi) LookPath(string) (string, error) { return "", errors.New("not found") }
func (missingPi) Run(context.Context, io.Reader, io.Writer, io.Writer, ...string) error {
	t := "must not be called"
	_ = t
	return errors.New("must not be called")
}

func TestIssueNewAmbiguousProjectRequiresFlag(t *testing.T) {
	home := testHome(t)
	repo1 := filepath.Join(t.TempDir(), "one")
	repo2 := filepath.Join(t.TempDir(), "two")
	addTestProject(t, repo1, "git@github.com:owner/one.git")
	addTestProject(t, repo2, "git@github.com:owner/two.git")
	var out, errB strings.Builder
	code := runIssueNew(nil, home, strings.NewReader("讨论\n"), &out, &errB)
	if code != 1 || !strings.Contains(errB.String(), "--project") {
		t.Fatalf("exit=%d err=%s", code, errB.String())
	}
	// Explicit id resolves.
	pi := &turnPi{replies: []string{"ok"}}
	swapIssueNew(t, pi, &fakeWriter{}, &fakeIssueForge{})
	code = runIssueNew([]string{"--project", "two"}, home, strings.NewReader("讨论\nq\n"), &out, &errB)
	if code != 0 || len(pi.calls) != 1 {
		t.Fatalf("--project exit=%d turns=%d err=%s", code, len(pi.calls), errB.String())
	}
}

func TestIssueNewRegisterFailureKeepsSession(t *testing.T) {
	home := testHome(t)
	issueTestProject(t)
	pi := &turnPi{replies: []string{draftFenceReply}}
	w := &fakeWriter{err: errors.New("401 unauthorized")}
	swapIssueNew(t, pi, w, &fakeIssueForge{})
	var out, errB strings.Builder
	code := runIssueNew(nil, home, strings.NewReader("讨论\n登记\ny\nn\n"), &out, &errB)
	if code != 2 {
		t.Fatalf("exit=%d, want 2", code)
	}
	if !strings.Contains(errB.String(), "登记失败") || !strings.Contains(errB.String(), "401") {
		t.Fatalf("stderr lacks failure:\n%s", errB.String())
	}
}

func TestParseIssueFence(t *testing.T) {
	out := "讨论…\n```issue\n标题A\n正文A1\n```\n中间议论\n```issue\n标题B\n正文B1\n正文B2\n```\n结尾"
	title, body, ok := parseIssueFence(out)
	if !ok || title != "标题B" || body != "正文B1\n正文B2" {
		t.Fatalf("got (%q,%q,%v), want last fence", title, body, ok)
	}
	if _, _, ok := parseIssueFence("```issue\n没有闭合"); ok {
		t.Fatalf("unclosed fence accepted")
	}
	if _, _, ok := parseIssueFence("没有围栏"); ok {
		t.Fatalf("plain text accepted")
	}
}

func TestIssueNewDispatchGuard(t *testing.T) {
	// `sift issue new features planned` (unquoted question) must NOT enter
	// the drafting session: it stays a Q&A question. With an empty registry
	// the Q&A path prints the bind hint and exits 0 — proof of dispatch.
	testHome(t)
	var out, errB strings.Builder
	code := runWithInput([]string{"sift", "issue", "new", "features", "planned"}, strings.NewReader(""), &out, &errB)
	if code != 0 {
		t.Fatalf("exit=%d err=%s", code, errB.String())
	}
	if strings.Contains(out.String(), "起草会话") {
		t.Fatalf("unquoted question wrongly entered the session:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "还没有绑定的项目") {
		t.Fatalf("expected Q&A-path hint:\n%s", out.String())
	}
}

// testHome wraps freshHome for readability in this file.
func testHome(t *testing.T) config.Home {
	t.Helper()
	_ = freshHome(t)
	home, err := config.ResolveHome()
	if err != nil {
		t.Fatal(err)
	}
	return home
}

func TestIsRegisterCommand(t *testing.T) {
	yes := []string{"登记", "/register", "好，登记", "可以登记", "那就登记", "登记吧", "登记了", "好的，登记！"}
	for _, s := range yes {
		if !isRegisterCommand(s) {
			t.Fatalf("isRegisterCommand(%q)=false, want true", s)
		}
	}
	no := []string{"帮我想想怎么写登记流程", "登记一个新功能的讨论", "q", "什么是登记", "先别登记，再讨论一下这个问题怎么拆"}
	for _, s := range no {
		if isRegisterCommand(s) {
			t.Fatalf("isRegisterCommand(%q)=true, want false", s)
		}
	}
}
