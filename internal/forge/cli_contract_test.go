package forge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestAdapterPaginationAndActorFailClosed(t *testing.T) {
	calls := 0
	run := func(_ context.Context, _ string, args []string, _ []byte) ([]byte, []byte, error) {
		calls++
		if !strings.Contains(args[1], "page=2") {
			rows := make([]string, 100)
			for i := range rows {
				rows[i] = fmt.Sprintf(`{"number":%d,"title":"t","body":"b","html_url":"https://x/%d","state":"open","user":{"login":"a"},"labels":[{"name":"sift"}]}`, i+1, i+1)
			}
			return []byte("[" + strings.Join(rows, ",") + "]"), nil, nil
		}
		return []byte(`[{"number":101,"title":"t","body":"b","html_url":"https://x/101","state":"open","user":{"login":"a"},"labels":[{"name":"sift"}]}]`), nil, nil
	}
	a := NewGitHub("gh", run)
	issues, _, err := a.ListIssuesByLabel(context.Background(), ProjectRef{Kind: KindGitHub, Host: "github.com", ProjectKey: "o/r"}, "sift", "")
	if err != nil || len(issues) != 101 || calls != 2 {
		t.Fatalf("pagination: %d calls=%d err=%v", len(issues), calls, err)
	}
	run = func(context.Context, string, []string, []byte) ([]byte, []byte, error) {
		return []byte(`[{"id":1,"body":"x","created_at":"2026-01-01T00:00:00Z","user":{}}]`), nil, nil
	}
	a = NewGitHub("gh", run)
	comments, _, err := a.ListIssueComments(context.Background(), ProjectRef{Kind: KindGitHub, Host: "github.com", ProjectKey: "o/r"}, "1", "")
	if err != nil || len(comments) != 0 {
		t.Fatalf("missing actor must be dropped: %#v %v", comments, err)
	}
}

func TestAdapterRateLimitMapping(t *testing.T) {
	a := NewGitHub("gh", func(context.Context, string, []string, []byte) ([]byte, []byte, error) {
		return nil, []byte("HTTP 429: rate limit exceeded"), errors.New("exit status 1")
	})
	_, err := a.GetIssue(context.Background(), ProjectRef{Kind: KindGitHub, Host: "github.com", ProjectKey: "o/r"}, "1")
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("error=%v", err)
	}
}

func TestGitLabNormalization(t *testing.T) {
	a := NewGitLab("glab", func(context.Context, string, []string, []byte) ([]byte, []byte, error) {
		return []byte(`{"iid":7,"web_url":"https://gitlab/x/7","state":"opened","diff_refs":{"head_sha":"abc"},"title":"Draft: test","detailed_merge_status":"conflict"}`), nil, nil
	})
	c, err := a.GetChange(context.Background(), ProjectRef{Kind: KindGitLab, Host: "gitlab.example", ProjectKey: "o/r"}, "7")
	if err != nil || c.ID != "7" || !c.IsDraft || c.Mergeability != Conflicting {
		t.Fatalf("change=%+v err=%v", c, err)
	}
}
