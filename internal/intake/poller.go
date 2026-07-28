// Package intake owns per-project Forge polling and the handoff to T1.
package intake

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/miaoxiaoyong/sift/internal/forge"
	"github.com/miaoxiaoyong/sift/internal/storage"
)

type Project struct {
	ID           string
	Ref          forge.ProjectRef
	TriggerLabel string
}
type Poller struct {
	DB                 *storage.DB
	Forge              forge.Client
	Projects           []Project
	Now                func() time.Time
	Idle, Active, Slow time.Duration
	HourlyLimit        int64
	WarningRatio       float64
	OnIssue            func(context.Context, Project, forge.Issue) error
	Isolated           func(Project, error)
}

// PollOnce polls each project independently. A bad credential/capability stops
// only that project; healthy projects are still polled and scheduled.
func (p *Poller) PollOnce(ctx context.Context) error {
	now := time.Time{}
	if p.Now != nil {
		now = p.Now()
	}
	if now.IsZero() {
		now = time.UnixMilli(1)
	}
	for _, project := range p.Projects {
		if err := p.pollProject(ctx, project, now); err != nil {
			var ce *forge.ClassifiedError
			if errors.As(err, &ce) && errors.Is(err, forge.ErrAuthOrCapability) {
				_ = p.DB.SetProjectHealth(ctx, project.ID, "forge_auth_or_capability", now.UnixMilli())
				if p.Isolated != nil {
					p.Isolated(project, err)
				}
				continue
			}
			return err
		}
	}
	return nil
}
func (p *Poller) pollProject(ctx context.Context, project Project, now time.Time) error {
	cur, err := p.DB.IntakeCursor(ctx, project.ID, "issues")
	if err != nil {
		return err
	}
	issues, next, err := p.Forge.ListIssuesByLabel(ctx, project.Ref, project.TriggerLabel, forge.Cursor(cur.Cursor))
	if err != nil {
		return err
	}
	items := make([]storage.IntakeItemInput, 0, len(issues))
	for _, i := range issues {
		if i.ID == "" || i.Author == "" || i.URL == "" {
			return &forge.ClassifiedError{Class: forge.ErrContractViolation, Summary: "normalized issue missing required facts"}
		}
		digest := issueDigest(i)
		items = append(items, storage.IntakeItemInput{IssueID: i.ID, IssueURL: i.URL, IssueDigest: digest, ForgeKind: string(project.Ref.Kind), Host: project.Ref.Host, ProjectKey: project.Ref.ProjectKey, EventID: "issue:" + i.ID + ":" + digest, EventKind: "issue_observed", TargetKind: "issue", Actor: i.Author, ObservedAtMS: now.UnixMilli(), RawDigest: digest})
	}
	mode := "idle"
	interval := p.Idle
	if len(issues) > 0 {
		mode = "active"
		interval = p.Active
	}
	if p.HourlyLimit > 0 && p.WarningRatio > 0 && p.WarningRatio < 1 {
		if status, e := p.DB.ForgeAPIBudgetStatus(ctx, project.ID, now.UnixMilli(), p.HourlyLimit, p.WarningRatio); e == nil && status.SlowPoll {
			mode = "slow"
			interval = p.Slow
		}
	}
	if interval <= 0 {
		interval = time.Minute
	}
	if err := p.DB.PersistIntakeBatch(ctx, storage.PersistIntakeBatchCmd{ProjectID: project.ID, Stream: "issues", Cursor: string(next), PollMode: mode, NextPollAtMS: now.Add(interval).UnixMilli(), NowMS: now.UnixMilli(), Items: items}); err != nil {
		return err
	}
	// T1 is deliberately after the transaction. A crash here leaves the cursor
	// advanced but the durable intake item pending, ready for a later evaluator.
	if p.OnIssue != nil {
		for _, i := range issues {
			if err := p.OnIssue(ctx, project, i); err != nil {
				return err
			}
		}
	}
	return nil
}
func issueDigest(i forge.Issue) string {
	b, _ := json.Marshal(i)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
