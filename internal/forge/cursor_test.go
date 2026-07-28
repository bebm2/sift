package forge

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestIssueCursorReplaysTimestampBoundaryWithoutLoss(t *testing.T) {
	for _, platform := range []struct {
		name  string
		kind  Kind
		query string
	}{
		{"github", KindGitHub, "since=2025-12-31T23%3A59%3A59Z"},
		{"gitlab", KindGitLab, "updated_after=2025-12-31T23%3A59%3A59Z"},
	} {
		t.Run(platform.name, func(t *testing.T) {
			calls := 0
			run := func(_ context.Context, _ string, args []string, _ []byte) ([]byte, []byte, error) {
				calls++
				path := args[1]
				if calls > 2 && !strings.Contains(path, platform.query) {
					t.Fatalf("follow-up path %q does not use cursor boundary %q", path, platform.query)
				}
				if calls == 1 {
					return cursorIssues(platform.kind, 1, 100), nil, nil
				}
				if calls == 2 {
					return cursorIssues(platform.kind, 101, 1), nil, nil
				}
				return cursorIssues(platform.kind, 101, 2), nil, nil
			}
			a := NewAdapter(platform.kind, "", run)
			project := ProjectRef{Kind: platform.kind, Host: platform.name + ".example", ProjectKey: "owner/repo"}
			items, cursor, err := a.ListIssuesByLabel(context.Background(), project, "sift", "")
			if err != nil || len(items) != 101 || cursor == "" {
				t.Fatalf("initial items=%d cursor=%q err=%v", len(items), cursor, err)
			}
			items, _, err = a.ListIssuesByLabel(context.Background(), project, "sift", cursor)
			if err != nil || len(items) != 2 || items[1].ID != "102" {
				t.Fatalf("boundary replay items=%+v err=%v", items, err)
			}
		})
	}
}

func cursorIssues(kind Kind, first, count int) []byte {
	rows := make([]string, count)
	for i := range rows {
		id := first + i
		if kind == KindGitLab {
			rows[i] = fmt.Sprintf(`{"iid":%d,"title":"t","web_url":"https://x/%d","state":"opened","updated_at":"2026-01-01T00:00:00Z","author":{"username":"a"}}`, id, id)
		} else {
			rows[i] = fmt.Sprintf(`{"number":%d,"title":"t","html_url":"https://x/%d","state":"open","updated_at":"2026-01-01T00:00:00Z","user":{"login":"a"}}`, id, id)
		}
	}
	return []byte("[" + strings.Join(rows, ",") + "]")
}

func TestLabelEventCursorIsForwarded(t *testing.T) {
	for _, platform := range []struct {
		name  string
		kind  Kind
		query string
	}{
		{"github", KindGitHub, "since=2025-12-31T23%3A59%3A59Z"},
		{"gitlab", KindGitLab, "created_after=2025-12-31T23%3A59%3A59Z"},
	} {
		t.Run(platform.name, func(t *testing.T) {
			calls := 0
			a := NewAdapter(platform.kind, "", func(_ context.Context, _ string, args []string, _ []byte) ([]byte, []byte, error) {
				calls++
				if calls == 2 && !strings.Contains(args[1], platform.query) {
					t.Fatalf("follow-up path %q does not use label-event cursor", args[1])
				}
				actor := `"actor":{"login":"a"}`
				if platform.kind == KindGitLab {
					actor = `"user":{"username":"a"}`
				}
				return []byte(`[{"id":1,"event":"labeled","action":"add",` + actor + `,"label":{"name":"sift"},"created_at":"2026-01-01T00:00:00Z"}]`), nil, nil
			})
			project := ProjectRef{Kind: platform.kind, Host: platform.name + ".example", ProjectKey: "owner/repo"}
			_, cursor, err := a.ListLabelEvents(context.Background(), project, TargetRef{Kind: TargetIssue, ID: "1"}, "")
			if err != nil || cursor == "" {
				t.Fatalf("initial cursor=%q err=%v", cursor, err)
			}
			if _, _, err = a.ListLabelEvents(context.Background(), project, TargetRef{Kind: TargetIssue, ID: "1"}, cursor); err != nil {
				t.Fatal(err)
			}
		})
	}
}
