package forge

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Fake is the in-memory Forge adapter of the M1 skeleton chain (WBS M1 §1.6).
// It implements Client with the same contract the real CLI adapter (M2) will
// honor, so the skeleton chain exercises the real port surface: issues and
// label events are scripted, and a Change can be driven to the merged state to
// inject the "Change 已合并" external fact (PRD §4.1 / §4.5).
//
// All scripted data carries required fields (author/actor); the Fake never
// emits a driving event with an empty actor, matching the fail-closed contract
// the real adapter enforces at its boundary.
type Fake struct {
	mu sync.Mutex

	issues      map[string][]Issue           // project key -> issues (insertion order)
	labelEvents map[string][]LabelEvent      // "<projectKey>\x00<targetID>" -> events
	changes     map[string]map[string]Change // project key -> change id -> change
}

// NewFake returns an empty scripted Fake.
func NewFake() *Fake {
	return &Fake{
		issues:      map[string][]Issue{},
		labelEvents: map[string][]LabelEvent{},
		changes:     map[string]map[string]Change{},
	}
}

func projectKey(p ProjectRef) string { return string(p.Kind) + ":" + p.Host + ":" + p.ProjectKey }

// AddIssue scripts an issue under a project. The issue is returned (sorted/
// deduped labels) and appended in insertion order; ListIssuesByLabel filters by
// label. Author and URL are required — the Fake refuses to script a driving
// payload it would then have to drop at the boundary.
func (f *Fake) AddIssue(p ProjectRef, issue Issue) Issue {
	if issue.Author == "" || issue.URL == "" || issue.ID == "" {
		panic(fmt.Sprintf("forge: fake issue %q missing required author/url/id", issue.ID))
	}
	issue.Labels = sortDedupe(issue.Labels)
	f.mu.Lock()
	f.issues[projectKey(p)] = append(f.issues[projectKey(p)], issue)
	f.mu.Unlock()
	return issue
}

// AddLabelEvent scripts one observed label mutation. Actor is required; an empty
// actor would be a boundary drop in the real adapter, so the Fake rejects it.
func (f *Fake) AddLabelEvent(p ProjectRef, ev LabelEvent) {
	if ev.Actor == "" || ev.TargetID == "" || ev.Label == "" {
		panic(fmt.Sprintf("forge: fake label event on %q missing required actor/target/label", ev.TargetID))
	}
	if ev.Action != LabelAdded && ev.Action != LabelRemoved {
		panic(fmt.Sprintf("forge: fake label event %q invalid action %q", ev.TargetID, ev.Action))
	}
	if ev.ObservedAt.IsZero() {
		ev.ObservedAt = time.Now()
	}
	key := projectKey(p) + "\x00" + ev.TargetID
	f.mu.Lock()
	f.labelEvents[key] = append(f.labelEvents[key], ev)
	f.mu.Unlock()
}

// AddChange scripts a Change under a project, initially in the open state. The
// reconciler later observes it via GetChange.
func (f *Fake) AddChange(p ProjectRef, changeID, headSHA string) Change {
	c := Change{ID: changeID, HeadSHA: headSHA, State: ChangeOpen}
	f.mu.Lock()
	pk := projectKey(p)
	if f.changes[pk] == nil {
		f.changes[pk] = map[string]Change{}
	}
	f.changes[pk][changeID] = c
	f.mu.Unlock()
	return c
}

// MergeChange injects the "Change 已合并" external fact (PRD §4.5 fact
// observation). It transitions the scripted Change to merged at at, returning
// the updated projection. This is the M1 stand-in for what M4 will drive
// through the Gate → create_change → merge_change outbox path; the skeleton
// injects the fact directly and converges done without adjudicating it.
func (f *Fake) MergeChange(p ProjectRef, changeID string, at time.Time) (Change, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	pk := projectKey(p)
	byID, ok := f.changes[pk]
	if !ok {
		return Change{}, &ClassifiedError{Class: ErrSemanticConflict, Summary: "unknown project for change " + changeID}
	}
	c, ok := byID[changeID]
	if !ok {
		return Change{}, &ClassifiedError{Class: ErrSemanticConflict, Summary: "unknown change " + changeID}
	}
	c.State = ChangeMerged
	c.MergedAt = at
	byID[changeID] = c
	return c, nil
}

// ListIssuesByLabel returns scripted issues carrying label since the cursor
// (oldest first). The cursor is opaque to callers; the Fake advances it by
// returned count, so repeated calls drain the queue as intake expects.
func (f *Fake) ListIssuesByLabel(_ context.Context, p ProjectRef, label string, since Cursor) ([]Issue, Cursor, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	all := f.issues[projectKey(p)]
	start := 0
	if since != "" {
		fmt.Sscanf(string(since), "%d", &start)
	}
	var out []Issue
	for _, iss := range all[start:] {
		if hasLabel(iss.Labels, label) {
			out = append(out, iss)
		}
	}
	next := Cursor(fmt.Sprintf("%d", len(all)))
	return out, next, nil
}

// ListLabelEvents returns the scripted events for one target in observation
// order.
func (f *Fake) ListLabelEvents(_ context.Context, p ProjectRef, targetID string, _ Cursor) ([]LabelEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := projectKey(p) + "\x00" + targetID
	return append([]LabelEvent(nil), f.labelEvents[key]...), nil
}

// GetChange returns the current scripted projection of one Change.
func (f *Fake) GetChange(_ context.Context, p ProjectRef, changeID string) (Change, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	byID, ok := f.changes[projectKey(p)]
	if !ok {
		return Change{}, &ClassifiedError{Class: ErrSemanticConflict, Summary: "unknown project for change " + changeID}
	}
	c, ok := byID[changeID]
	if !ok {
		return Change{}, &ClassifiedError{Class: ErrSemanticConflict, Summary: "unknown change " + changeID}
	}
	return c, nil
}

func sortDedupe(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func hasLabel(labels []string, want string) bool {
	for _, l := range labels {
		if l == want {
			return true
		}
	}
	return false
}
