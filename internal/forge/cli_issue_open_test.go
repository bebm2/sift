package forge

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// openIssuePlatform is one platform's expectations for ListOpenIssues: the
// request path carries the platform's state/sort spellings and no labels
// filter, GitHub rows that are secretly PRs are dropped, and the walk stops at
// the page cap with truncated=true.
type openIssuePlatform struct {
	name    string
	kind    Kind
	project ProjectRef
	wantIn  string // required substring of every request path
}

func TestListOpenIssuesPathsAndPRFilter(t *testing.T) {
	for _, p := range []openIssuePlatform{
		{"github", KindGitHub, ProjectRef{Kind: KindGitHub, Host: "github.com", ProjectKey: "o/r"}, "/repos/o/r/issues?state=open&sort=updated&direction=desc"},
		{"gitlab", KindGitLab, ProjectRef{Kind: KindGitLab, Host: "gitlab.example.com", ProjectKey: "o/r"}, "/projects/o%2Fr/issues?state=opened&order_by=updated_at&sort=desc"},
	} {
		t.Run(p.name, func(t *testing.T) {
			paths := []string{}
			a := NewAdapter(p.kind, "", func(_ context.Context, _ string, args []string, _ []byte) ([]byte, []byte, error) {
				paths = append(paths, args[1])
				row := `{"number":7,"iid":7,"title":"真正的 issue","body":"正文","html_url":"https://x/7","web_url":"https://x/7","state":"open","updated_at":"2026-01-01T00:00:01Z","user":{"login":"alice"},"author":{"username":"alice"},"labels":[{"name":"sift"}]}`
				if p.kind == KindGitLab {
					row = strings.Replace(row, `"state":"open"`, `"state":"opened"`, 1)
				}
				return []byte("[" + row + "]"), nil, nil
			})
			issues, truncated, err := a.ListOpenIssues(context.Background(), p.project, 1)
			if err != nil {
				t.Fatalf("err=%v", err)
			}
			if truncated {
				t.Fatalf("truncated=true on a short page")
			}
			if len(issues) != 1 || issues[0].ID != "7" || issues[0].Title != "真正的 issue" || issues[0].Author != "alice" {
				t.Fatalf("issues=%+v", issues)
			}
			if len(paths) == 0 || !strings.Contains(paths[0], p.wantIn) {
				t.Fatalf("request path=%v, want substring %q", paths, p.wantIn)
			}
			if strings.Contains(paths[0], "labels=") {
				t.Fatalf("open listing must not filter by label: %q", paths[0])
			}
		})
	}
}

func TestListOpenIssuesSkipsGitHubPullRequests(t *testing.T) {
	a := NewGitHub("", func(_ context.Context, _ string, _ []string, _ []byte) ([]byte, []byte, error) {
		return []byte(`[
			{"number":1,"title":"issue","state":"open","updated_at":"2026-01-01T00:00:01Z","html_url":"https://x/1","user":{"login":"a"}},
			{"number":2,"title":"pr","state":"open","updated_at":"2026-01-01T00:00:02Z","html_url":"https://x/2","user":{"login":"a"},"pull_request":{}}
		]`), nil, nil
	})
	issues, _, err := a.ListOpenIssues(context.Background(), ProjectRef{Kind: KindGitHub, ProjectKey: "o/r"}, 1)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(issues) != 1 || issues[0].ID != "1" {
		t.Fatalf("issues=%+v, want only the non-PR #1", issues)
	}
}

func TestListOpenIssuesPageCapTruncates(t *testing.T) {
	calls := 0
	a := NewGitHub("", func(_ context.Context, _ string, args []string, _ []byte) ([]byte, []byte, error) {
		calls++
		page := 1
		if i := strings.Index(args[1], "page="); i >= 0 {
			fmt.Sscanf(args[1][i+len("page="):], "%d", &page)
		}
		return v3Page(KindGitHub, page, 100), nil, nil
	})
	issues, truncated, err := a.ListOpenIssues(context.Background(), ProjectRef{Kind: KindGitHub, ProjectKey: "o/r"}, 2)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if calls != 2 {
		t.Fatalf("calls=%d, want 2 (page cap respected)", calls)
	}
	if len(issues) != 200 {
		t.Fatalf("issues=%d, want 200", len(issues))
	}
	if !truncated {
		t.Fatalf("truncated=false, want true (a full page remained)")
	}
}

func TestListOpenIssuesRejectsBadPageCap(t *testing.T) {
	a := NewGitHub("", nil)
	if _, _, err := a.ListOpenIssues(context.Background(), ProjectRef{Kind: KindGitHub, ProjectKey: "o/r"}, 0); err == nil {
		t.Fatalf("maxPages=0 accepted, want error")
	}
}
