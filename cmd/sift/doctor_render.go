package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/xsift/sift/internal/cli/render"
	"github.com/xsift/sift/internal/config"
)

// renderDoctorWithOptions keeps the default doctor view task-oriented. The
// expanded and developer views are explicit so a first-run user is not handed
// 49 implementation checks when one stale project needs attention.
func renderDoctorWithOptions(w io.Writer, value any, options doctorOptions) {
	if options.details {
		renderDoctorFull(w, value, options.debug)
		return
	}
	renderDoctorSummary(w, value)
}

type renderedDoctorCheck struct {
	ID      string
	Level   string
	Message string
	Details map[string]any
}

type renderedDoctorProject struct {
	ID    string
	Repo  string
	Forge string
}

func decodeDoctorResult(value any) (map[string]any, []renderedDoctorCheck, bool) {
	body, err := json.Marshal(value)
	if err != nil {
		return nil, nil, false
	}
	var result map[string]any
	if json.Unmarshal(body, &result) != nil {
		return nil, nil, false
	}
	rawChecks, _ := result["checks"].([]any)
	checks := make([]renderedDoctorCheck, 0, len(rawChecks))
	for _, raw := range rawChecks {
		check, _ := raw.(map[string]any)
		id, _ := check["id"].(string)
		level, _ := check["level"].(string)
		message, _ := check["message"].(string)
		details, _ := check["details"].(map[string]any)
		checks = append(checks, renderedDoctorCheck{ID: id, Level: level, Message: message, Details: details})
	}
	return result, checks, true
}

func doctorProjectID(id string) string {
	for _, prefix := range []string{"policy:", "hooks:"} {
		if project, ok := strings.CutPrefix(id, prefix); ok && project != "" && project != "storage" {
			return project
		}
	}
	if rest, ok := strings.CutPrefix(id, "forge-cli:"); ok {
		if project, _, ok := strings.Cut(rest, ":"); ok && project != "" {
			return project
		}
	}
	return ""
}

func doctorProjectsForRender() (string, map[string]renderedDoctorProject) {
	home, err := config.ResolveHome()
	if err != nil {
		return "", nil
	}
	snap, err := config.Load(home, time.Now())
	if err != nil {
		return "", nil
	}
	cwd, _ := os.Getwd()
	projects := make(map[string]renderedDoctorProject, len(snap.Config.Projects))
	current := ""
	for _, project := range snap.Config.Projects {
		projects[project.ID] = renderedDoctorProject{ID: project.ID, Repo: project.Repo, Forge: string(project.Forge.Kind) + ":" + project.Forge.Project}
		if cwd == "" {
			continue
		}
		rel, err := filepath.Rel(project.Repo, cwd)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			current = project.ID
		}
	}
	return current, projects
}

func renderDoctorSummary(w io.Writer, value any) {
	result, checks, ok := decodeDoctorResult(value)
	if !ok {
		fmt.Fprintln(w, "✗ 无法读取诊断结果")
		return
	}
	code := doctorExitCode(result)
	headline := []string{"正常", "需要关注", "需要处理"}
	if code < 0 || code >= len(headline) {
		code = 2
	}
	fmt.Fprintf(w, "Sift 状态：%s\n", headline[code])
	if offline, _ := result["offline"].(bool); offline {
		fmt.Fprintln(w, "模式：离线（未验证 daemon 实时状态）")
	} else {
		fmt.Fprintln(w, "模式：在线")
	}

	currentID, projects := doctorProjectsForRender()
	byProject := make(map[string][]renderedDoctorCheck)
	var global, boundaries, qualifications []renderedDoctorCheck
	for _, check := range checks {
		if check.Level == "ok" || check.Level == "info" {
			continue
		}
		if projectID := doctorProjectID(check.ID); projectID != "" {
			byProject[projectID] = append(byProject[projectID], check)
			continue
		}
		if knownV0Boundary(check.ID) && !strings.HasPrefix(check.ID, "process-group:") {
			boundaries = append(boundaries, check)
			continue
		}
		if strings.HasPrefix(check.ID, "process-group:") {
			qualifications = append(qualifications, check)
			continue
		}
		global = append(global, check)
	}

	if current, found := projects[currentID]; found {
		fmt.Fprintf(w, "\n当前项目：%s（%s）\n", current.ID, current.Forge)
		findings := byProject[currentID]
		if len(findings) == 0 {
			fmt.Fprintln(w, "  ✓ 未发现项目级问题")
		} else {
			for _, check := range findings {
				renderDoctorSummaryCheck(w, check, current, true)
			}
		}
	} else if currentID == "" {
		fmt.Fprintln(w, "\n当前目录：未匹配已登记项目")
	}

	otherIDs := make([]string, 0, len(byProject))
	for id := range byProject {
		if id != currentID {
			otherIDs = append(otherIDs, id)
		}
	}
	sort.Strings(otherIDs)
	if len(otherIDs) > 0 {
		fmt.Fprintln(w, "\n其他已登记项目")
		for _, id := range otherIDs {
			project := projects[id]
			if project.ID == "" {
				project = renderedDoctorProject{ID: id}
			}
			renderOtherProjectDoctorSummary(w, byProject[id], project)
		}
	}

	if len(global) > 0 {
		fmt.Fprintln(w, "\n运行环境")
		for _, check := range global {
			if check.Level == "warning" && check.ID != "outbox:backlog" {
				// Generic warnings remain available in --details. The default only
				// shows warnings that have a user-facing lifecycle meaning.
				continue
			}
			renderDoctorGlobalSummaryCheck(w, check)
		}
	}
	if len(qualifications) > 0 {
		fmt.Fprintf(w, "\nAgent 资格：%d 项尚未完成 process-group 验证；首次真实 Run 后会更新，不阻塞当前配置。\n", len(qualifications))
	}
	if len(boundaries) > 0 {
		fmt.Fprintf(w, "已知 V0 同 UID 安全边界：%d 项（非当前配置故障）。\n", len(boundaries))
	}
	fmt.Fprintln(w, "\n完整安全检查：sift doctor --details")
	fmt.Fprintln(w, "开发诊断（check ID、路径、阶段耗时）：sift doctor --debug")
}

func renderOtherProjectDoctorSummary(w io.Writer, checks []renderedDoctorCheck, project renderedDoctorProject) {
	for _, check := range checks {
		if strings.HasPrefix(check.ID, "policy:") && check.Level == "error" && strings.Contains(check.Message, "exit status") {
			fmt.Fprintf(w, "  %s 其他项目 %s（%s）：本地仓库不可读，无法检查项目策略与 hooks\n", render.Status("error"), project.ID, project.Repo)
			fmt.Fprintf(w, "     该项目不是当前目录；若已废弃，运行 sift project remove %s\n", project.ID)
			return
		}
	}
	for _, check := range checks {
		renderDoctorSummaryCheck(w, check, project, false)
	}
}

func renderDoctorSummaryCheck(w io.Writer, check renderedDoctorCheck, project renderedDoctorProject, current bool) {
	prefix := "其他项目 " + project.ID
	if current {
		prefix = ""
	}
	if !current && project.Repo != "" {
		prefix += "（" + project.Repo + "）"
	}
	if prefix != "" {
		prefix += "："
	}
	status := render.Status(check.Level)
	switch {
	case strings.HasPrefix(check.ID, "policy:"):
		fmt.Fprintf(w, "  %s %s项目策略无法读取\n", status, prefix)
		if !current {
			fmt.Fprintf(w, "     该项目不是当前目录；若已废弃，运行 sift project remove %s\n", project.ID)
		} else {
			fmt.Fprintln(w, "     确认仓库可读，并检查 base 分支的 .sift/policy.yaml")
		}
	case strings.HasPrefix(check.ID, "hooks:") && check.Message == "hooks state drifted from baseline":
		fmt.Fprintf(w, "  %s %shooks 状态与已保存基线不一致\n", status, prefix)
		fmt.Fprintln(w, "     运行 sift doctor --details 查看差异；不影响其他项目")
	case strings.HasPrefix(check.ID, "hooks:"):
		fmt.Fprintf(w, "  %s %s本地仓库 hooks 无法读取\n", status, prefix)
		if !current {
			fmt.Fprintf(w, "     确认路径仍是 Git 仓库；若已废弃，运行 sift project remove %s\n", project.ID)
		}
	default:
		fmt.Fprintf(w, "  %s %s%s\n", status, prefix, doctorMessage(check.Message, check.Level))
	}
}

func renderDoctorGlobalSummaryCheck(w io.Writer, check renderedDoctorCheck) {
	if check.ID == "outbox:backlog" {
		fmt.Fprintf(w, "  %s 有待处理的投递操作，daemon 会自动重试\n", render.Status(check.Level))
		return
	}
	fmt.Fprintf(w, "  %s %s\n", render.Status(check.Level), doctorMessage(check.Message, check.Level))
}

func renderDoctorStages(w io.Writer, result map[string]any) {
	stages, _ := result["stage_ms"].(map[string]any)
	if len(stages) == 0 {
		return
	}
	keys := make([]string, 0, len(stages))
	for key := range stages {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%vms", key, stages[key]))
	}
	fmt.Fprintf(w, "开发诊断：stage_ms %s\n", strings.Join(parts, " "))
}
