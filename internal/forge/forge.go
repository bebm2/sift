// Package forge defines the Forge adapter port contract (PRD §5.2, DESIGN §8.1).
//
// The Forge adapter normalizes the gh/glab CLI plumbing verbs into domain-neutral
// types. This file freezes the port surface and the neutral types the M1
// skeleton chain needs; the full verb set, platform normalization details and
// the real CLI adapter land with specs/forge.md in M2. The Fake implementation
// in fake.go serves the V9 first-segment CI chain and shares this contract.
//
// Design invariants carried by the types (not caller discipline):
//   - actor is part of the type: label/comment/event reads with a missing actor
//     never reach the caller; the adapter drops them at the boundary (DESIGN §8.1).
//   - error semantics exposed to the upper layer are exactly the five classes;
//     platform HTTP/exit-code details stay inside the adapter.
package forge

import (
	"context"
	"errors"
	"time"
)

// Kind is the forge platform family (PRD §5.2 platform differences).
type Kind string

const (
	KindGitHub Kind = "github"
	KindGitLab Kind = "gitlab"
)

// ProjectRef identifies one configured forge project (normalized host + key).
type ProjectRef struct {
	Kind       Kind
	Host       string // normalized host, e.g. github.com
	ProjectKey string // e.g. org/repo
}

// Issue is the normalized issue payload. Author and URL are required; a missing
// author is a ContractViolation at the adapter, never an empty string upstream.
type Issue struct {
	ID     string
	Title  string
	Body   string
	Author string
	URL    string
	Labels []string // sorted/deduped by the adapter
}

// LabelAction is the add/remove verb of one label event.
type LabelAction string

const (
	LabelAdded   LabelAction = "added"
	LabelRemoved LabelAction = "removed"
)

// LabelEvent is one observed label mutation. Actor is required (PRD §5.2 / §9.2
// trigger-label backtracking): the adapter drops any event whose actor it cannot
// resolve, so a caller never sees an empty Actor on a driving event.
type LabelEvent struct {
	TargetID   string // issue or change id
	Label      string
	Action     LabelAction
	Actor      string
	ObservedAt time.Time
}

// ChangeState is the normalized lifecycle state of a Change (PRD §5.2). Merge
// capability details (mergeable_state / detailed_merge_status) collapse to the
// conservative intersection here; uncertainty is expressed as the adapter
// returning false on Merged and leaving adjudication to the caller.
type ChangeState string

const (
	ChangeOpen   ChangeState = "open"
	ChangeMerged ChangeState = "merged"
	ChangeClosed ChangeState = "closed"
)

// Change is the normalized Change projection. MergedAt is zero unless State is
// merged; HeadSHA is the merge decision's expected-head contract anchor.
type Change struct {
	ID       string
	HeadSHA  string
	State    ChangeState
	MergedAt time.Time
}

// Cursor is an opaque, adapter-owned pagination marker.
type Cursor string

// Client is the Forge adapter port. The M1 skeleton exercises the verbs below;
// the remaining PRD §5.2 verbs (createChange, mergeChange, commentIssue,
// setLabels, getChangeDiff, getChecks, findChangeForCreateOperation) are added
// with specs/forge.md in M2 against the same error and type contract.
type Client interface {
	// ListIssuesByLabel returns issues carrying label since the cursor, oldest
	// first. It is the intake increment verb (PRD §5.2).
	ListIssuesByLabel(ctx context.Context, project ProjectRef, label string, since Cursor) ([]Issue, Cursor, error)

	// ListLabelEvents returns the label mutation history of one target (issue or
	// change). Actor is always populated; events with an unresolvable actor are
	// dropped at the adapter (DESIGN §8.1, fail closed).
	ListLabelEvents(ctx context.Context, project ProjectRef, targetID string, since Cursor) ([]LabelEvent, error)

	// GetChange reads one Change's state and head. The M1 reconciler uses it to
	// observe the "Change merged" external fact (PRD §4.1 / §4.5).
	GetChange(ctx context.Context, project ProjectRef, changeID string) (Change, error)
}

// Error classification — the only error semantics the upper layer sees (DESIGN
// §8.1). Platform HTTP/exit specifics stay inside the adapter.
var (
	ErrTransient         = errors.New("forge: transient")
	ErrRateLimited       = errors.New("forge: rate limited")
	ErrAuthOrCapability  = errors.New("forge: auth or capability")
	ErrContractViolation = errors.New("forge: contract violation")
	ErrSemanticConflict  = errors.New("forge: semantic conflict")
)

// ClassifiedError wraps one of the sentinel classes with an adapter summary.
type ClassifiedError struct {
	Class   error
	Summary string
}

func (e *ClassifiedError) Error() string { return e.Class.Error() + ": " + e.Summary }
func (e *ClassifiedError) Unwrap() error { return e.Class }
