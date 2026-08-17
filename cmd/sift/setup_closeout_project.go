package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/xsift/sift/internal/cli/render"
	"github.com/xsift/sift/internal/config"
)

// setupProjectContext is the just-registered project as resolved from the
// persisted config. It deliberately contains only non-secret project routing
// facts suitable for first-run output.
type setupProjectContext struct {
	ID   string
	Repo string
	Kind string
	Key  string
}

func registeredSetupProject(home config.Home, repo string) setupProjectContext {
	for _, project := range registeredSetupProjects(home) {
		if project.Repo == repo {
			return project
		}
	}
	return setupProjectContext{}
}

func registeredSetupProjects(home config.Home) map[string]setupProjectContext {
	snap, err := config.Load(home, time.Now())
	if err != nil {
		return nil
	}
	projects := make(map[string]setupProjectContext, len(snap.Config.Projects))
	for _, project := range snap.Config.Projects {
		projects[project.ID] = setupProjectContext{
			ID:   project.ID,
			Repo: project.Repo,
			Kind: string(project.Forge.Kind),
			Key:  project.Forge.Project,
		}
	}
	return projects
}

func printRegisteredSetupProject(out io.Writer, project setupProjectContext) {
	if project.ID == "" {
		return
	}
	fmt.Fprintf(out, "%s 当前项目已登记：%s\n", render.Status("ok"), project.ID)
	fmt.Fprintf(out, "  repo: %s\n", project.Repo)
	fmt.Fprintf(out, "  forge: %s:%s\n", project.Kind, project.Key)
}

// doctorProjectID extracts the registered project id carried by project-scoped
// doctor checks. Global checks deliberately return "": they cannot establish
// whether the current repository is safe to trigger.
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

type closeoutDoctorCheck struct {
	ID      string
	Message string
}

type closeoutDoctorSummary struct {
	errors               int
	currentProjectErrors int
	otherProjectErrors   int
}

func otherProjectDoctorTitle(id string, project setupProjectContext) string {
	name := project.ID
	if project.Repo != "" {
		name += "（" + project.Repo + "）"
	}
	return "其他已登记项目 " + name + "：" + doctorProblemTitle(id)
}

func otherProjectDoctorAction(project setupProjectContext) string {
	return "该项目不是当前目录；若已废弃，运行 `sift project remove " + project.ID + "`；否则恢复本地 Git 仓库后重跑 sift doctor"
}
