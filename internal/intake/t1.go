package intake

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/miaoxiaoyong/sift/internal/brain"
	"github.com/miaoxiaoyong/sift/internal/forge"
	"github.com/miaoxiaoyong/sift/internal/storage"
)

type T1Evaluator struct {
	DB    *storage.DB
	Brain *brain.Shell
	Now   func() time.Time
}

// EvaluateIssue wires a normalized Forge Issue into T1. Provider disabled or
// unavailable is intentionally not a drop: the shell's deterministic fallback
// is persisted as ready and the Issue is enqueued through PersistIntakeDecision.
func (e *T1Evaluator) EvaluateIssue(ctx context.Context, project Project, issue forge.Issue) error {
	item, err := e.DB.FindPendingIntake(ctx, project.ID, issue.ID)
	if err != nil {
		return err
	}
	input, err := brain.BuildT1Input(brain.T1Input{Forge: brain.T1Forge{Kind: string(project.Ref.Kind), Host: project.Ref.Host, ProjectKey: project.Ref.ProjectKey}, Issue: brain.T1Issue{ID: issue.ID, Title: issue.Title, Body: issue.Body, Author: issue.Author, URL: issue.URL, Labels: issue.Labels}, KnownCandidates: []brain.T1Candidate{}})
	if err != nil {
		return err
	}
	now := time.Time{}
	if e.Now != nil {
		now = e.Now()
	}
	if now.IsZero() {
		now = time.UnixMilli(1)
	}
	result, err := e.Brain.Call(ctx, brain.T1Contract(nil), brain.CallParams{Scope: "intake", SubjectKey: fmt.Sprintf("forge:%s:%s:%s:issue:%s", project.Ref.Kind, project.Ref.Host, project.Ref.ProjectKey, issue.ID), ProjectID: project.ID, Input: input})
	if err != nil {
		return err
	}
	var out struct {
		Disposition string   `json:"disposition"`
		Questions   []string `json:"questions"`
		Possible    *string  `json:"possible_duplicate_run_id"`
		Rationale   string   `json:"rationale"`
	}
	if err = json.Unmarshal(result.Output, &out); err != nil {
		return err
	}
	q, _ := json.Marshal(out.Questions)
	return e.DB.PersistIntakeDecision(ctx, storage.IntakeDecisionCmd{IntakeID: item.ID, AssessmentID: storage.NewID(), LogicalCallID: result.CallID, ExpectedVersion: item.Version, Disposition: out.Disposition, QuestionsJSON: string(q), PossibleDuplicateRunID: out.Possible, Rationale: out.Rationale, NowMS: now.UnixMilli()})
}
