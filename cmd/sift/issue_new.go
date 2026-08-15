// `sift issue new` (issue #999): a discussion-first drafting session with a
// deterministic register gate. The pain point it serves: the human has the
// judgment but not the patience for long-form writing; the agent has the
// patience but must not have the pen on forge.
//
// Shape:
//
//	human (short lines) ⇄ pi turns (--tools read,grep,find,ls — pi's
//	documented read-only set; open-issue evidence pack on turn 1; session
//	continued across turns via --session-dir/--session)
//	  host commands: #N = fetch that issue+comments (no model call),
//	  草稿 = show current draft, 登记 = register gate, q = quit
//	登记 → host (not the agent): parse the latest ```issue fence → show the
//	  full draft → y / e($EDITOR) / 放弃 → title dedupe warning → CreateIssue
//	  (no labels by default) → explicit y/N prompt for the trigger label.
//
// Red lines: the agent never holds a write tool and never touches the forge
// write path — registration is plain Go executed after a human confirmation
// the agent cannot produce; the trigger label is never attached implicitly.
package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/xsift/sift/internal/config"
	"github.com/xsift/sift/internal/forge"
)

// issueWriter is the sole forge write surface of the register gate.
type issueWriter interface {
	CreateIssue(ctx context.Context, p forge.ProjectRef, title, body string, labels []string) (forge.Issue, error)
}

// newIssueWriter builds the write adapter for one bound project. Like the
// read seam it is budget-free: the actor is the human confirming the draft.
var newIssueWriter = func(ref config.ForgeRef) issueWriter {
	kind, cli := forgeCLINames(ref)
	return forge.NewAdapter(kind, cli, forge.ExecRunner)
}

// forgeCLINames resolves the adapter kind and CLI executable for a forge
// binding (shared by the read and write seams).
func forgeCLINames(ref config.ForgeRef) (forge.Kind, string) {
	kind, cli := forge.KindGitHub, "gh"
	if ref.Kind == config.ForgeKindGitLab {
		kind, cli = forge.KindGitLab, "glab"
	}
	if ref.CLI != "" {
		cli = ref.CLI
	}
	return kind, cli
}

// issueSession drives one drafting session: the pi conversation state and the
// draft extracted from it.
type issueSession struct {
	dir         string
	sessionFile string // set after the first turn resolves the saved session
	draftPath   string
	turns       int
}

// issueTurnTimeout bounds one drafting turn. It is deliberately generous
// (5 min): the agent reads both the evidence pack and repo code, and a
// real-machine smoke showed a read-heavy first turn can exceed the 3-minute
// Q&A budget (issue #999).
const issueTurnTimeout = 5 * time.Minute

// issueDraftTools is the drafting allowlist: pi's documented read-only set.
// grep/find/ls stay read-only but let the agent locate code instead of
// walking files one by one — without them the first turn can time out.
var issueDraftTools = []string{"read", "grep", "find", "ls"}

const issueDraftPreamble = "你是 Sift 的 issue 起草助手，与操作者多轮讨论后替他把 issue 写清楚。规则：\n" +
	"- stdin 是该项目的 open issue 只读取证快照；代码事实可用只读工具（read/grep/find/ls）查当前目录仓库。只基于已取证的事实，取不到的明说「取不到」。\n" +
	"- 你的职责是补全结构：背景/问题/证据或复现/验收标准/范围边界；操作者只做判断。\n" +
	"- 每当草稿成形或被修改，必须完整重发一个围栏块：第一行是 \"```issue\"，第二行是 issue 标题，其余行是 Markdown 正文，最后以 \"```\" 收尾。围栏外可以有你的讨论与提问。\n" +
	"- 需要澄清就直接问，一次问最关键的少数几条。\n" +
	"- 你没有任何写权限：登记由宿主处理。当操作者表示要登记时，你只需简短回应并等待宿主；绝不要声称登记/创建已发生——那不是你的能力，也不是你能观察到的事。\n" +
	"- 你没有任何写权限：登记由操作者完成，不要建议调用写工具。"

// runIssueNew hosts the drafting REPL. stdin/out/err are the process streams;
// tests drive stdin line by line and substitute the pi and forge seams.
func runIssueNew(args []string, home config.Home, stdin io.Reader, stdout, stderr io.Writer) int {
	projectID := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--project" && i+1 < len(args) {
			projectID = args[i+1]
			i++
			continue
		}
		if strings.HasPrefix(args[i], "--project=") {
			projectID = strings.TrimPrefix(args[i], "--project=")
			continue
		}
		report(stderr, fmt.Errorf("usage: sift issue new [--project ID]"))
		return 2
	}
	snap, err := config.Load(home, time.Now())
	if err != nil {
		report(stderr, fmt.Errorf("读取配置失败：%w", err))
		return 1
	}
	proj, err := resolveDraftProject(snap, projectID)
	if err != nil {
		report(stderr, err)
		return 1
	}
	if _, e := issuePi.LookPath("pi"); e != nil {
		fmt.Fprintln(stderr, "✗ 未检测到 pi：`sift issue new` 的讨论与起草依赖 pi。")
		fmt.Fprintln(stderr, piInstallManual())
		return 1
	}
	sess, err := newIssueSession(home)
	if err != nil {
		report(stderr, err)
		return 1
	}
	defer os.RemoveAll(sess.dir)

	fmt.Fprintf(stdout, "✓ 起草会话（项目 %s，%s）\n", proj.ID, proj.Forge.Project)
	fmt.Fprintln(stdout, "用法：直接输入讨论内容；#N 取该 issue 全文与评论（不烧 token）；「草稿」查看当前草稿；「登记」进入确认；q 退出。")
	fmt.Fprintln(stdout, "建议在项目仓库目录下运行，agent 可直接读代码取证。")

	in := bufio.NewReader(stdin)
	var pendingContext string
	for {
		fmt.Fprintf(stdout, "你: ")
		line, readErr := in.ReadString('\n')
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if readErr != nil {
				// EOF (Ctrl-D / piped input exhausted): leave like q, keeping
				// the draft on disk instead of spinning on a dead reader.
				fmt.Fprintln(stdout, "\n（输入结束，退出。草稿已保留：", sess.draftPath, "）")
				return 0
			}
			continue
		}
		switch {
		case trimmed == "q" || trimmed == "quit" || trimmed == "exit":
			fmt.Fprintln(stdout, "再见。草稿已保留：", sess.draftPath)
			return 0
		case isRegisterCommand(trimmed):
			ctx, cancel := context.WithTimeout(context.Background(), issueTurnTimeout)
			code := issueRegisterFlow(ctx, in, proj, snap, sess, stdout, stderr)
			cancel()
			if code == 0 {
				return 0
			}
			if code < 0 {
				continue // 放弃登记，回到讨论
			}
			return code // 硬错误（forge 失败等）
		case trimmed == "草稿" || trimmed == "/draft":
			issueShowDraft(sess, stdout)
			continue
		case isIssueRefLine(trimmed):
			id := strings.TrimPrefix(trimmed, "#")
			pendingContext += issueFetchRefForContext(proj, id, stdout, stderr)
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), issueTurnTimeout)
		text := trimmed
		if pendingContext != "" {
			text = pendingContext + "\n\n（以上为补充取证，请纳入讨论。）\n\n" + text
			pendingContext = ""
		}
		if err := issueTurn(ctx, sess, text, []config.Project{proj}, stdout, stderr); err != nil {
			if ctx.Err() != nil {
				fmt.Fprintf(stderr, "✗ 本回合超时（%s），会话保留，可重试\n", issueTurnTimeout)
			} else {
				fmt.Fprintf(stderr, "✗ 本回合失败：%v（会话保留，可重试）\n", err)
			}
		}
		cancel()
	}
}

// resolveDraftProject picks the drafting target: explicit --project wins,
// then a unique cwd-inside-repo match, then a single registered project.
func resolveDraftProject(snap *config.Snapshot, projectID string) (config.Project, error) {
	enabled := []config.Project{}
	for _, p := range snap.Config.Projects {
		if p.Enabled {
			enabled = append(enabled, p)
		}
	}
	if len(enabled) == 0 {
		return config.Project{}, fmt.Errorf("还没有绑定的项目：先运行 `sift init` 或 `sift project add`")
	}
	if projectID != "" {
		for _, p := range enabled {
			if p.ID == projectID {
				return p, nil
			}
		}
		return config.Project{}, fmt.Errorf("未找到启用的项目 %q（sift project list 查看）", projectID)
	}
	if cwd, e := os.Getwd(); e == nil {
		match := ""
		uniq := true
		for _, p := range enabled {
			if p.Repo != "" && (cwd == p.Repo || strings.HasPrefix(cwd, p.Repo+string(os.PathSeparator))) {
				if match != "" {
					uniq = false
					break
				}
				match = p.ID
			}
		}
		if match != "" && uniq {
			for _, p := range enabled {
				if p.ID == match {
					return p, nil
				}
			}
		}
	}
	if len(enabled) == 1 {
		return enabled[0], nil
	}
	ids := make([]string, len(enabled))
	for i, p := range enabled {
		ids[i] = p.ID
	}
	return config.Project{}, fmt.Errorf("多个启用项目，用 --project 指定其一：%s", strings.Join(ids, ", "))
}

// newIssueSession prepares a private session directory under SIFT_HOME.
func newIssueSession(home config.Home) (*issueSession, error) {
	dir := filepath.Join(home.Path, "issue-sessions", fmt.Sprintf("%d-%d", time.Now().UnixNano(), os.Getpid()))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("创建会话目录失败：%w", err)
	}
	return &issueSession{dir: dir, draftPath: filepath.Join(dir, "draft.md")}, nil
}

// issueTurn runs one pi turn, teeing the reply to the user while scanning it
// for the latest ```issue fence. The first turn also receives the evidence
// pack on stdin and creates the session file the later turns continue.
func issueTurn(ctx context.Context, s *issueSession, text string, projects []config.Project, stdout, stderr io.Writer) error {
	var stdinR io.Reader = strings.NewReader("")
	promptText := text
	if s.turns == 0 {
		pack := gatherIssueEvidence(ctx, projects, text, stderr)
		stdinR = strings.NewReader(pack)
		promptText = issueDraftPreamble + "\n\n" + draftBindingNote(projects) + "\n\n操作者：" + text
	}
	args := append([]string{"--tools"}, issueDraftTools...)
	if s.sessionFile != "" {
		args = append(args, "--session", s.sessionFile)
	} else {
		args = append(args, "--session-dir", s.dir)
	}
	args = append(args, "-p", promptText)
	var captured strings.Builder
	tee := io.MultiWriter(stdout, &captured)
	runErr := issuePi.Run(ctx, stdinR, tee, stderr, args...)
	// Resolve the session file even after a failed/killed turn: pi saves
	// incrementally, so a timed-out turn may still have persisted partial
	// context the next turn should continue (real-machine smoke, #999).
	if s.sessionFile == "" {
		if f := newestSessionFile(s.dir); f != "" {
			s.sessionFile = f
		}
	}
	if runErr != nil {
		return runErr
	}
	s.turns++
	if title, body, ok := parseIssueFence(captured.String()); ok {
		if err := os.WriteFile(s.draftPath, []byte(title+"\n\n"+body+"\n"), 0o600); err != nil {
			fmt.Fprintf(stderr, "⚠ 草稿写入失败：%v\n", err)
		} else {
			fmt.Fprintf(stdout, "（草稿已更新：%s）\n", s.draftPath)
		}
	}
	return nil
}

// newestSessionFile returns the most recently modified .jsonl session file in
// dir, or "".
func newestSessionFile(dir string) string {
	files, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil || len(files) == 0 {
		return ""
	}
	newest := files[0]
	var newestMod int64
	for _, f := range files {
		if st, err := os.Stat(f); err == nil && st.ModTime().UnixNano() > newestMod {
			newestMod = st.ModTime().UnixNano()
			newest = f
		}
	}
	return newest
}

// parseIssueFence extracts the last ```issue fence from a model reply: the
// first line inside is the title, the remainder is the body.
func parseIssueFence(out string) (title, body string, ok bool) {
	lines := strings.Split(out, "\n")
	var block []string
	inFence := false
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		if !inFence {
			if t == "```issue" {
				inFence = true
				block = nil
			}
			continue
		}
		if t == "```" {
			inFence = false
			if len(block) > 0 {
				title, body, ok = strings.TrimSpace(block[0]), strings.Join(block[1:], "\n"), true
			}
			block = nil
			continue
		}
		block = append(block, ln)
	}
	return title, body, ok
}

// issueShowDraft prints the current draft, or says there is none yet.
func issueShowDraft(s *issueSession, stdout io.Writer) {
	data, err := os.ReadFile(s.draftPath)
	if err != nil || len(strings.TrimSpace(string(data))) == 0 {
		fmt.Fprintln(stdout, "（还没有草稿：继续讨论，或让 agent 输出 ```issue 围栏草稿）")
		return
	}
	fmt.Fprintln(stdout, "── 当前草稿 ──")
	fmt.Fprintln(stdout, strings.TrimSpace(string(data)))
}

// isRegisterCommand recognizes the natural spellings of "register now":
// the bare 登记, slash form, or a short polite wrapper like 好，登记 / 可以登记
// / 那就登记吧（real-machine smoke showed users do not type the bare verb).
// The acknowledged-prefix set is closed so sentences that merely mention
// 登记 (什么是登记 / 先别登记) stay discussion lines.
func isRegisterCommand(s string) bool {
	if s == "/register" {
		return true
	}
	norm := strings.Map(func(r rune) rune {
		switch r {
		case '，', ',', '。', '.', '！', '!', '？', '?', ' ', '\t':
			return -1
		}
		return r
	}, s)
	norm = strings.TrimSuffix(strings.TrimSuffix(norm, "吧"), "了")
	if norm == "登记" {
		return true
	}
	for _, ack := range []string{"好", "好的", "可以", "行", "那就", "嗯", "ok", "OK"} {
		if norm == ack+"登记" {
			return true
		}
	}
	return false
}

// isIssueRefLine reports whether a host command line is exactly one #N
// reference (the deterministic evidence fetch).
func isIssueRefLine(s string) bool {
	if !strings.HasPrefix(s, "#") {
		return false
	}
	_, err := strconv.Atoi(s[1:])
	return err == nil
}

// issueFetchRefForContext fetches one issue plus its comments without a model
// call, prints them for the human, and returns the text injected into the
// next model turn.
func issueFetchRefForContext(proj config.Project, id string, stdout, stderr io.Writer) string {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	ref := forge.ProjectRef{Kind: forge.Kind(proj.Forge.Kind), Host: proj.Forge.Host, ProjectKey: proj.Forge.Project}
	f := newIssueForge(proj.Forge)
	var b strings.Builder
	fmt.Fprintf(&b, "## 补充取证 #%s\n", id)
	issue, e := f.GetIssue(ctx, ref, id)
	if e != nil {
		fmt.Fprintf(&b, "取不到：%v\n", e)
		fmt.Fprintf(stdout, "#%s 取不到：%v\n", id, e)
		return b.String()
	}
	writeEvidenceIssue(&b, issue, evidenceBodyRunes*2)
	comments, _, ce := f.ListIssueComments(ctx, ref, id, "")
	if ce == nil && len(comments) > 0 {
		shown := comments
		if len(shown) > evidenceMaxComments {
			shown = shown[:evidenceMaxComments]
		}
		for _, c := range shown {
			fmt.Fprintf(&b, "- %s：%s\n", c.Author, truncateRunes(c.Body, evidenceCommentRunes))
		}
	}
	fmt.Fprint(stdout, b.String())
	return b.String()
}

// draftBindingNote tells the agent which forge project the register gate
// will write to. When the shell's cwd is not that repository, the agent's
// read/grep evidence necessarily comes from another codebase — the note
// forces it to attribute code facts to the repo they came from instead of
// silently drafting about the wrong project (real-machine smoke, #999).
func draftBindingNote(projects []config.Project) string {
	if len(projects) == 0 {
		return ""
	}
	p := projects[0]
	note := fmt.Sprintf("绑定项目：%s %s（登记将写入该项目；当前目录：%s）", p.Forge.Kind, p.Forge.Project, func() string {
		if cwd, e := os.Getwd(); e == nil {
			return cwd
		}
		return "未知"
	}())
	if p.Repo != "" {
		if cwd, e := os.Getwd(); e == nil && cwd != p.Repo && !strings.HasPrefix(cwd, p.Repo+string(os.PathSeparator)) {
			note += "。注意：当前目录不是绑定仓库，read/grep 取到的是其他代码库；引用代码事实时必须注明来源仓库，不要把当前目录的代码当作绑定项目的内容。"
		}
	}
	return note
}

// issueRegisterFlow is the deterministic gate. Returns 0 = registered and
// done, -1 = cancelled back to discussion, >0 = hard failure exit code.
func issueRegisterFlow(ctx context.Context, in *bufio.Reader, proj config.Project, snap *config.Snapshot, s *issueSession, stdout, stderr io.Writer) int {
	if _, err := os.Stat(s.draftPath); err != nil {
		fmt.Fprintln(stdout, "还没有草稿——先让讨论收敛出草稿。我现在请 agent 输出完整草稿。")
		if err := issueTurn(ctx, s, "请现在把最终草稿放进 ```issue 围栏（首行标题，其余正文）。只输出围栏，不要额外解释。", []config.Project{proj}, stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "✗ 起草回合失败：%v\n", err)
			return -1
		}
		if _, err := os.Stat(s.draftPath); err != nil {
			fmt.Fprintln(stdout, "仍未取得草稿，回到讨论。")
			return -1
		}
	}
	raw, err := os.ReadFile(s.draftPath)
	if err != nil {
		fmt.Fprintf(stderr, "✗ 读草稿失败：%v\n", err)
		return 1
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) < 1 || strings.TrimSpace(lines[0]) == "" {
		fmt.Fprintln(stdout, "草稿没有标题，回到讨论补一个标题。")
		return -1
	}
	title := strings.TrimSpace(lines[0])
	body := strings.TrimSpace(strings.Join(lines[1:], "\n"))
	for {
		fmt.Fprintln(stdout, "── 待登记草稿 ──")
		fmt.Fprintf(stdout, "标题：%s\n\n%s\n", title, body)
		switch strings.ToLower(prompt(in, stdout, "登记(y) / 编辑(e) / 放弃", "")) {
		case "y", "yes":
		case "e", "edit":
			if err := editDraftFile(s.draftPath); err != nil {
				fmt.Fprintf(stderr, "✗ 编辑器失败：%v\n", err)
			}
			if raw, err = os.ReadFile(s.draftPath); err == nil {
				ls := strings.Split(strings.TrimSpace(string(raw)), "\n")
				if len(ls) > 0 && strings.TrimSpace(ls[0]) != "" {
					title = strings.TrimSpace(ls[0])
					body = strings.TrimSpace(strings.Join(ls[1:], "\n"))
				}
			}
			continue
		default:
			fmt.Fprintln(stdout, "已取消登记，回到讨论。")
			return -1
		}
		break
	}
	// Title dedupe: warn on an exact existing title and require a second
	// explicit confirmation.
	ref := forge.ProjectRef{Kind: forge.Kind(proj.Forge.Kind), Host: proj.Forge.Host, ProjectKey: proj.Forge.Project}
	if issues, _, e := newIssueForge(proj.Forge).ListOpenIssues(ctx, ref, 1); e == nil {
		for _, i := range issues {
			if strings.EqualFold(strings.TrimSpace(i.Title), title) {
				fmt.Fprintf(stdout, "⚠ 已有同题 open issue：%s %s\n", i.ID, i.URL)
				if strings.ToLower(prompt(in, stdout, "仍要创建重复 issue？", "n")) != "y" {
					fmt.Fprintln(stdout, "已取消登记，回到讨论。")
					return -1
				}
				break
			}
		}
	}
	trigger := snap.Config.Labels.Trigger
	labels := []string{}
	if strings.ToLower(prompt(in, stdout, fmt.Sprintf("打上触发标签 %s 立即开跑？", trigger), "n")) == "y" {
		labels = append(labels, trigger)
	}
	created, err := newIssueWriter(proj.Forge).CreateIssue(ctx, ref, title, body, labels)
	if err != nil {
		fmt.Fprintf(stderr, "✗ 登记失败：%v（会话保留，可修正后重新「登记」）\n", err)
		return 2
	}
	fmt.Fprintf(stdout, "✓ 已登记 #%s：%s\n", created.ID, created.URL)
	if len(labels) == 0 {
		fmt.Fprintf(stdout, "未打触发标签；想开跑：%s\n", triggerLabelCommand(proj, created.ID, trigger))
	}
	return 0
}

// editDraftFile opens $EDITOR (vi fallback) on the draft file synchronously.
func editDraftFile(path string) error {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	cmd := exec.Command(editor, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// triggerLabelCommand renders the manual trigger command in the project's own
// CLI dialect, mirroring the init closeout hint.
func triggerLabelCommand(proj config.Project, issueID, label string) string {
	_, cli := forgeCLINames(proj.Forge)
	if cli == "glab" {
		return fmt.Sprintf("glab issue update %s --label %s -R %s", issueID, label, proj.Forge.Project)
	}
	return fmt.Sprintf("gh issue edit %s --add-label %s --repo %s", issueID, label, proj.Forge.Project)
}
