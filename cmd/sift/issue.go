// `sift issue` (issue #963 v1): the read-only semantic entry. Two paths:
//
//   - Fast path (`sift issue` with no arguments): a deterministic listing of
//     every enabled project's open issues through the existing forge read
//     surface. Zero model calls, zero tokens.
//   - Slow path (`sift issue "自然语言问题"`): the CLI gathers a read-only
//     evidence pack itself (open issues per project, plus full issue + comment
//     detail for every #N the question mentions), then makes one headless pi
//     call with the pack piped through stdin and the tool allowlist pinned to
//     `read` — the model can read the evidence and nothing else: no bash, no
//     write path, no forge capability of its own. Issue facts reach the model
//     only through what this command gathered (the architectural hallucination
//     guard: what was not fetched does not exist for the answer).
//
// Degradation stays honest: pi missing falls back to the deterministic listing
// plus install guidance (exit 0); forge failures surface per project instead
// of aborting the whole answer.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/xsift/sift/internal/config"
	"github.com/xsift/sift/internal/forge"
)

// openIssueMaxPages bounds the API page walk of the deterministic listing:
// three 100-item pages per project is far beyond any interactive question,
// and the alternative (walking every page of a monorepo tracker) is a
// user-facing stall.
const openIssueMaxPages = 3

// Evidence pack caps: they bound the prompt size and the fetch cost. Truncated
// sections say so in the pack itself, so the model can answer honestly
// ("列表被截断") instead of assuming completeness.
const (
	evidenceMaxIssues    = 30 // per project
	evidenceBodyRunes    = 1500
	evidenceMaxComments  = 50 // per referenced issue
	evidenceCommentRunes = 800
)

// issuePiTimeout bounds the whole headless pi call. It is deliberately
// generous (3 min): the model reads a large evidence pack.
const issuePiTimeout = 3 * time.Minute

// issueForge is the forge read surface this command uses — exactly the
// existing reads (issue #963: zero new forge capability, and writes are not
// even on the interface).
type issueForge interface {
	ListOpenIssues(ctx context.Context, p forge.ProjectRef, maxPages int) ([]forge.Issue, bool, error)
	GetIssue(ctx context.Context, p forge.ProjectRef, id string) (forge.Issue, error)
	ListIssueComments(ctx context.Context, p forge.ProjectRef, id string, since forge.Cursor) ([]forge.Comment, forge.Cursor, error)
}

// newIssueForge builds the forge adapter for one bound project. The adapter is
// budget-free: the actor is the operator at the terminal, the same as running
// gh/glab by hand — daemon intake budgets do not apply to interactive reads.
var newIssueForge = func(ref config.ForgeRef) issueForge {
	kind, cli := forge.KindGitHub, "gh"
	if ref.Kind == config.ForgeKindGitLab {
		kind, cli = forge.KindGitLab, "glab"
	}
	if ref.CLI != "" {
		cli = ref.CLI
	}
	return forge.NewAdapter(kind, cli, forge.ExecRunner)
}

// headlessPi runs one non-interactive pi invocation. Tests substitute fakes;
// production pins --tools read so the model provably cannot reach a write path
// (acceptance #4: the read-only guarantee is in the code, not the prompt).
type headlessPi interface {
	LookPath(name string) (string, error)
	Run(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, args ...string) error
}

type osHeadlessPi struct{}

func (osHeadlessPi) LookPath(name string) (string, error) { return exec.LookPath(name) }
func (osHeadlessPi) Run(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, args ...string) error {
	cmd := exec.CommandContext(ctx, "pi", args...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

var issuePi headlessPi = osHeadlessPi{}

// runIssue dispatches `sift issue`: `new` enters the drafting session (only
// as a bare `new` or followed by --flags, so an unquoted question starting
// with the word "new" still takes the Q&A path); no arguments take the
// deterministic fast path; everything else joined by spaces is the question.
func runIssue(args []string, home config.Home, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "new" && (len(args) == 1 || strings.HasPrefix(args[1], "-")) {
		return runIssueNew(args[1:], home, stdin, stdout, stderr)
	}
	// --all opts out of the cwd-project scoping everywhere (issue feedback:
	// hexark 目录不应查其他项目，但偶尔仍要全局视图)。
	all := false
	if len(args) > 0 && args[0] == "--all" {
		all = true
		args = args[1:]
	}
	if all {
		if len(args) == 0 {
			return listOpenIssuesAll(home, stdout, stderr)
		}
		return answerIssueQuestionAll(strings.Join(args, " "), home, stdout, stderr)
	}
	if len(args) == 0 {
		return listOpenIssues(home, stdout, stderr)
	}
	return answerIssueQuestion(strings.Join(args, " "), home, stdout, stderr)
}

// loadIssueProjects loads the config snapshot and returns the enabled
// projects. An absent/empty registry is reported to w with the init hint and
// ok=false; a broken config is a hard error distinguished by the bool pair.
func loadIssueProjects(home config.Home, w io.Writer) (projects []config.Project, ok bool, err error) {
	snap, e := config.Load(home, time.Now())
	if e != nil {
		return nil, false, e
	}
	for _, p := range snap.Config.Projects {
		if p.Enabled {
			projects = append(projects, p)
		}
	}
	if len(projects) == 0 {
		fmt.Fprintln(w, "还没有绑定的项目：先运行 `sift init` 或 `sift project add` 绑定一个仓库。")
		return nil, false, nil
	}
	return projects, true, nil
}

// cwdEnabledProject returns the enabled project whose repo contains the
// current working directory, if there is exactly one such project. Commands
// scoped by cwd (issue list/question) default to it: running inside a
// registered repo should not fan out to every other project's forge (issue
// feedback: hexark 目录下查 gh 项目噪音)。
func cwdEnabledProject(projects []config.Project) (config.Project, bool) {
	cwd, err := os.Getwd()
	if err != nil {
		return config.Project{}, false
	}
	if cwd, err = filepath.EvalSymlinks(cwd); err != nil {
		return config.Project{}, false
	}
	var match config.Project
	found := false
	for _, p := range projects {
		if p.Repo == "" {
			continue
		}
		repo := p.Repo
		if resolved, err := filepath.EvalSymlinks(p.Repo); err == nil {
			repo = resolved
		}
		if cwd == repo || strings.HasPrefix(cwd, repo+string(os.PathSeparator)) {
			if found { // nested registrations: ambiguous, keep the full listing
				return config.Project{}, false
			}
			match, found = p, true
		}
	}
	return match, found
}

// listOpenIssues is the fast path: cwd-scoped when the working directory sits
// inside exactly one enabled project's repo (the common case — you are in the
// repo you care about), all projects otherwise.
func listOpenIssues(home config.Home, stdout, stderr io.Writer) int {
	return listOpenIssuesScoped(home, stdout, stderr, false)
}

// listOpenIssuesAll is the --all variant: every enabled project, no cwd scope.
func listOpenIssuesAll(home config.Home, stdout, stderr io.Writer) int {
	return listOpenIssuesScoped(home, stdout, stderr, true)
}

func listOpenIssuesScoped(home config.Home, stdout, stderr io.Writer, all bool) int {
	projects, ok, err := loadIssueProjects(home, stdout)
	if err != nil {
		report(stderr, fmt.Errorf("读取配置失败：%w", err))
		return 1
	}
	if !ok {
		return 0
	}
	if !all {
		if p, inRepo := cwdEnabledProject(projects); inRepo {
			// Scoped listing: one line says which project and that --all widens.
			fmt.Fprintf(stdout, "当前目录项目 %s（sift issue --all 查看全部项目）\n\n", p.ID)
			projects = []config.Project{p}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	exit := 0
	for _, p := range projects {
		ref := forge.ProjectRef{Kind: forge.Kind(p.Forge.Kind), Host: p.Forge.Host, ProjectKey: p.Forge.Project}
		issues, truncated, e := newIssueForge(p.Forge).ListOpenIssues(ctx, ref, openIssueMaxPages)
		if e != nil {
			fmt.Fprintf(stderr, "✗ %s（%s）：%v\n", p.ID, p.Forge.Project, e)
			exit = 1
			continue
		}
		renderOpenIssues(stdout, p, issues, truncated)
	}
	return exit
}

// renderOpenIssues prints one project's open issues, newest update first.
func renderOpenIssues(w io.Writer, p config.Project, issues []forge.Issue, truncated bool) {
	fmt.Fprintf(w, "▸ %s（%s，%d 个 open issue）\n", p.ID, p.Forge.Project, len(issues))
	if len(issues) == 0 {
		fmt.Fprintln(w, "  （无）")
		return
	}
	for _, i := range issues {
		line := fmt.Sprintf("  #%-5s %s", i.ID, i.Title)
		if len(i.Labels) > 0 {
			line += "  [" + strings.Join(i.Labels, ",") + "]"
		}
		fmt.Fprintln(w, line)
	}
	if truncated {
		fmt.Fprintf(w, "  （已达分页上限，仅显示前 %d 个）\n", openIssueMaxPages*100)
	}
}

// answerIssueQuestion is the slow path: gather evidence deterministically,
// then one headless pi call with the pack on stdin and tools pinned to read.
func answerIssueQuestion(question string, home config.Home, stdout, stderr io.Writer) int {
	return answerIssueQuestionScoped(question, home, stdout, stderr, false)
}

func answerIssueQuestionAll(question string, home config.Home, stdout, stderr io.Writer) int {
	return answerIssueQuestionScoped(question, home, stdout, stderr, true)
}

func answerIssueQuestionScoped(question string, home config.Home, stdout, stderr io.Writer, all bool) int {
	projects, ok, err := loadIssueProjects(home, stdout)
	if err != nil {
		report(stderr, fmt.Errorf("读取配置失败：%w", err))
		return 1
	}
	if !ok {
		return 0
	}
	if !all {
		if p, inRepo := cwdEnabledProject(projects); inRepo {
			fmt.Fprintf(stdout, "✓ 当前目录项目 %s，仅针对它取证（--all 查全部）\n", p.ID)
			projects = []config.Project{p}
		}
	}
	if _, e := issuePi.LookPath("pi"); e != nil {
		fmt.Fprintln(stderr, "⚠ 未检测到 pi，本次降级为确定性列表；装好 pi 后再用自然语言提问。")
		fmt.Fprintln(stderr, piInstallManual())
		return listOpenIssues(home, stdout, stderr)
	}
	ctx, cancel := context.WithTimeout(context.Background(), issuePiTimeout)
	defer cancel()
	pack := gatherIssueEvidence(ctx, projects, question, stderr)
	fmt.Fprintln(stdout, "✓ 正在调用 pi（只读取证，最长 3 分钟）…")
	prompt := "你是 sift 的 issue 分析助手。用户问题：\n" + question + "\n\n" +
		"stdin 是本次从 forge 只读接口取证的 issue 快照。回答规则：\n" +
		"- 只能基于快照中的事实回答；快照里没有的明说「取不到」，绝不编造编号、标题或结论。\n" +
		"- 快照标注了截断的地方，回答要承认只看到了截断内的内容。\n" +
		"- 引用 issue 时给出 #编号 与标题；中文、简洁、分点。"
	if e := issuePi.Run(ctx, strings.NewReader(pack), stdout, stderr, "--tools", "read", "-p", prompt); e != nil {
		if ctx.Err() != nil {
			report(stderr, fmt.Errorf("pi 调用超时（%s）：可缩小问题范围后重试", issuePiTimeout))
		} else {
			report(stderr, fmt.Errorf("pi 调用失败：%w", e))
		}
		return 1
	}
	return 0
}

// issueRefPattern finds the #N issue references in a question (#963: "#42 的
// 讨论核心分歧是什么？"). Plain numbers without # are deliberately not
// matched — too ambiguous against quantities in natural language.
var issueRefPattern = regexp.MustCompile(`#(\d{1,6})`)

// gatherIssueEvidence builds the evidence pack: open issues per project, plus
// full detail (issue + comments) for every #N the question references. All
// fetch failures are noted inside the pack, never silently dropped — the
// model must see what could not be fetched.
func gatherIssueEvidence(ctx context.Context, projects []config.Project, question string, stderr io.Writer) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Sift issue 取证快照（只读）\n生成时间：%s\n项目数：%d\n\n", time.Now().Format("2006-01-02 15:04"), len(projects))
	refs := issueRefPattern.FindAllStringSubmatch(question, -1)
	var refIDs []string
	seen := map[string]bool{}
	for _, m := range refs {
		if !seen[m[1]] {
			seen[m[1]] = true
			refIDs = append(refIDs, m[1])
		}
	}
	for _, p := range projects {
		ref := forge.ProjectRef{Kind: forge.Kind(p.Forge.Kind), Host: p.Forge.Host, ProjectKey: p.Forge.Project}
		f := newIssueForge(p.Forge)
		fmt.Fprintf(&b, "## 项目 %s（%s，%s）\n", p.ID, p.Forge.Kind, p.Forge.Project)
		issues, truncated, e := f.ListOpenIssues(ctx, ref, openIssueMaxPages)
		if e != nil {
			fmt.Fprintf(&b, "取证失败：%v（本项目的 issue 不可用，回答时明说取不到）\n\n", e)
			fmt.Fprintf(stderr, "⚠ %s：%v\n", p.ID, e)
			continue
		}
		shown := issues
		listTruncated := false
		if len(shown) > evidenceMaxIssues {
			shown = shown[:evidenceMaxIssues]
			listTruncated = true
		}
		fmt.Fprintf(&b, "open issue 共 %d 个%s：\n\n", len(issues), orderNote(truncated))
		for _, i := range shown {
			writeEvidenceIssue(&b, i, evidenceBodyRunes)
		}
		if listTruncated {
			fmt.Fprintf(&b, "（列表超长，仅取证前 %d 个）\n", evidenceMaxIssues)
		}
		// Referenced #N that the open listing already covers still get the
		// comment thread; ones outside it (closed, other state) are fetched
		// whole so "closed 的 #7 为什么失败" is answerable too.
		for _, id := range refIDs {
			writeEvidenceRefDetail(&b, ctx, f, ref, id)
		}
		fmt.Fprintln(&b)
	}
	return b.String()
}

// orderNote renders the truncation caveats of the listing itself.
func orderNote(pageTruncated bool) string {
	if pageTruncated {
		return "（按更新时间倒序；已达分页上限，列表不完整）"
	}
	return "（按更新时间倒序）"
}

// writeEvidenceIssue writes one issue block with a body truncation marker.
func writeEvidenceIssue(b *strings.Builder, i forge.Issue, bodyRunes int) {
	fmt.Fprintf(b, "### #%s %s\n", i.ID, i.Title)
	fmt.Fprintf(b, "- 状态：%s；作者：%s；labels：%s\n", i.State, i.Author, strings.Join(i.Labels, ","))
	fmt.Fprintf(b, "- 链接：%s\n", i.URL)
	body := truncateRunes(i.Body, bodyRunes)
	if strings.TrimSpace(body) != "" {
		if len([]rune(i.Body)) > bodyRunes {
			fmt.Fprintf(b, "正文（截断至 %d 字）：%s\n", bodyRunes, body)
		} else {
			fmt.Fprintf(b, "正文：%s\n", body)
		}
	} else {
		fmt.Fprintln(b, "正文：（空）")
	}
}

// writeEvidenceRefDetail appends the full issue + comment thread for one #N
// reference. A miss (not found / no permission) is recorded in the pack so the
// model can say "取不到" instead of guessing from the listing alone.
func writeEvidenceRefDetail(b *strings.Builder, ctx context.Context, f issueForge, ref forge.ProjectRef, id string) {
	issue, e := f.GetIssue(ctx, ref, id)
	if e != nil {
		fmt.Fprintf(b, "\n#### #%s 详情\n取不到：%v\n", id, e)
		return
	}
	fmt.Fprintf(b, "\n#### #%s 详情（含评论）\n", id)
	writeEvidenceIssue(b, issue, evidenceBodyRunes*2)
	comments, _, ce := f.ListIssueComments(ctx, ref, id, "")
	if ce != nil {
		fmt.Fprintf(b, "评论取不到：%v\n", ce)
		return
	}
	if len(comments) == 0 {
		fmt.Fprintln(b, "评论：（无）")
		return
	}
	shown := comments
	comTruncated := false
	if len(shown) > evidenceMaxComments {
		shown = shown[:evidenceMaxComments]
		comTruncated = true
	}
	for _, c := range shown {
		fmt.Fprintf(b, "- %s：%s\n", c.Author, truncateRunes(c.Body, evidenceCommentRunes))
	}
	if comTruncated {
		fmt.Fprintf(b, "（仅前 %d 条评论）\n", evidenceMaxComments)
	}
}

// truncateRunes cuts s to at most n runes on a rune boundary.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
