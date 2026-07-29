package brain

import (
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/miaoxiaoyong/sift/internal/contract"
	"github.com/miaoxiaoyong/sift/internal/decode"
)

type InterruptReason string

func (InterruptReason) EnumValues() []string {
	return []string{"design_approval", "failure_review", "human_input", "merge_approval", "policy_block", "rate_limited", "run_stalled"}
}

type InterruptSeverity string

func (InterruptSeverity) EnumValues() []string {
	return []string{"low", "normal", "high", "critical"}
}

type InterruptModality string

func (InterruptModality) EnumValues() []string { return []string{"voice", "text", "visual"} }

type T4Link struct {
	Label  string `json:"label"`
	Target string `json:"target"`
}

type T4Option struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Effect string `json:"effect"`
	Risk   string `json:"risk"`
}

type T4Interrupt struct {
	Reason           InterruptReason   `json:"reason"`
	BaseSeverity     InterruptSeverity `json:"base_severity"`
	MinModality      InterruptModality `json:"min_modality"`
	FallbackHeadline string            `json:"fallback_headline"`
	FallbackBrief    string            `json:"fallback_brief"`
	BriefFragments   []string          `json:"brief_fragments"`
	Links            []T4Link          `json:"links"`
	CandidateOptions []T4Option        `json:"candidate_options"`
}

type T4Input struct {
	RunID     string      `json:"run_id"`
	AttemptNo *int        `json:"attempt_no"`
	Interrupt T4Interrupt `json:"interrupt"`
}

// BuildT4Input validates the already-verified Interrupt skeleton and returns
// its canonical form. The caller remains responsible for literal canonical
// option/headline equality with the Interrupt emitter.
func BuildT4Input(in T4Input) ([]byte, error) {
	if len(in.RunID) == 0 || len(in.RunID) > 256 || (in.AttemptNo != nil && *in.AttemptNo < 1) {
		return nil, errors.New("brain: invalid T4 run or attempt identity")
	}
	i := in.Interrupt
	if !inEnum(i.Reason) || !inEnum(i.BaseSeverity) || !inEnum(i.MinModality) || runeCount(i.FallbackHeadline) < 1 || runeCount(i.FallbackHeadline) > 40 || hasControlOrNewline(i.FallbackHeadline) || len(i.FallbackBrief) < 1 || len(i.FallbackBrief) > 8192 || len(i.BriefFragments) < 1 || len(i.BriefFragments) > 32 || len(i.Links) > 32 || len(i.CandidateOptions) < 1 || len(i.CandidateOptions) > 4 {
		return nil, errors.New("brain: invalid T4 interrupt skeleton")
	}
	for n, f := range i.BriefFragments {
		if len(f) < 1 || len(f) > 1000 || hasControlOrNewline(f) || (n > 0 && i.BriefFragments[n-1] >= f) {
			return nil, errors.New("brain: T4 brief_fragments must be bounded, safe, sorted, and unique")
		}
	}
	for n, l := range i.Links {
		if len(l.Label) < 1 || len(l.Label) > 128 || len(l.Target) < 1 || len(l.Target) > 4096 || !validT4Link(l.Target) || (n > 0 && (i.Links[n-1].Target > l.Target || i.Links[n-1].Target == l.Target && i.Links[n-1].Label >= l.Label)) {
			return nil, errors.New("brain: invalid T4 links")
		}
	}
	seen := map[string]bool{}
	for _, o := range i.CandidateOptions {
		if !optionID.MatchString(o.ID) || seen[o.ID] || len(o.Label) < 1 || len(o.Label) > 256 || len(o.Effect) < 1 || len(o.Effect) > 1000 || len(o.Risk) < 1 || len(o.Risk) > 1000 || hasControlOrNewline(o.Label) || hasControlOrNewline(o.Effect) || hasControlOrNewline(o.Risk) {
			return nil, errors.New("brain: invalid T4 candidate option")
		}
		seen[o.ID] = true
	}
	return decode.Canonical(in)
}

type T4Output struct {
	contract.ClosedType `json:"-"`
	Headline            *string   `json:"headline" sift:"required,maxbytes=160"`
	Conclusion          *string   `json:"conclusion" sift:"required,maxbytes=1000"`
	KeyPoints           *[]string `json:"key_points" sift:"required,minitems=1,maxitems=3,itemminbytes=1,itemmaxbytes=1000"`
	RecommendedOptionID *string   `json:"recommended_option_id" sift:"required,maxbytes=64"`
	Options             *[]string `json:"options" sift:"required,minitems=1,maxitems=4,itemminbytes=1,itemmaxbytes=64"`
}

func T4Contract(in T4Input) TouchpointContract {
	return TouchpointContract{Touchpoint: "T4", Asset: T4Asset(), ValidateOutput: func(result []byte) ([]byte, error) {
		var out T4Output
		if err := decode.Decode(result, &out, decode.Closed); err != nil {
			return nil, err
		}
		if *out.Headline != in.Interrupt.FallbackHeadline || !contains(in.Interrupt.BriefFragments, *out.Conclusion) || !containsOption(in.Interrupt.CandidateOptions, *out.RecommendedOptionID) || len(*out.Options) != len(in.Interrupt.CandidateOptions) {
			return nil, errors.New("brain: T4 output does not preserve the frozen skeleton")
		}
		seen := map[string]bool{}
		for _, point := range *out.KeyPoints {
			if !contains(in.Interrupt.BriefFragments, point) || seen[point] {
				return nil, errors.New("brain: invalid T4 key point")
			}
			seen[point] = true
		}
		for n, id := range *out.Options {
			if id != in.Interrupt.CandidateOptions[n].ID {
				return nil, errors.New("brain: T4 options must exactly preserve canonical order")
			}
		}
		return decode.Canonical(out)
	}}
}

type T6Delivery string

func (T6Delivery) EnumValues() []string { return []string{"immediate", "batch", "next_window"} }

type T6AvailabilityState string

func (T6AvailabilityState) EnumValues() []string {
	return []string{"available", "unavailable", "unknown"}
}

type T6Quota struct {
	Severity  InterruptSeverity `json:"severity"`
	Remaining int64             `json:"remaining"`
}
type T6Availability struct {
	State          T6AvailabilityState `json:"state"`
	NextWindowAtMS *int64              `json:"next_window_at_ms"`
}
type T6Candidate struct {
	Reason            InterruptReason   `json:"reason"`
	Severity          InterruptSeverity `json:"severity"`
	MinModality       InterruptModality `json:"min_modality"`
	ExpiresAtMS       int64             `json:"expires_at_ms"`
	ChannelCandidates []string          `json:"channel_candidates"`
	DefaultChannelID  string            `json:"default_channel_id"`
}
type T6Attention struct {
	FallbackImmediateMinSeverity InterruptSeverity `json:"fallback_immediate_min_severity"`
	Remaining                    []T6Quota         `json:"remaining"`
}
type T6Input struct {
	RunID        string         `json:"run_id"`
	AttemptNo    *int           `json:"attempt_no"`
	FrozenAtMS   int64          `json:"frozen_at_ms"`
	Candidate    T6Candidate    `json:"candidate"`
	Availability T6Availability `json:"availability"`
	Attention    T6Attention    `json:"attention"`
}

func BuildT6Input(in T6Input) ([]byte, error) {
	if len(in.RunID) == 0 || len(in.RunID) > 256 || in.FrozenAtMS < 0 || (in.AttemptNo != nil && *in.AttemptNo < 1) || !inEnum(in.Candidate.Reason) || !inEnum(in.Candidate.Severity) || !inEnum(in.Candidate.MinModality) || in.Candidate.ExpiresAtMS <= in.FrozenAtMS || in.Attention.FallbackImmediateMinSeverity != "high" {
		return nil, errors.New("brain: invalid T6 frozen candidate")
	}
	if !inEnum(in.Availability.State) || (in.Availability.State == "available" && in.Availability.NextWindowAtMS != nil) || (in.Availability.State == "unavailable" && (in.Availability.NextWindowAtMS == nil || *in.Availability.NextWindowAtMS <= in.FrozenAtMS)) || (in.Availability.NextWindowAtMS != nil && *in.Availability.NextWindowAtMS <= in.FrozenAtMS) {
		return nil, errors.New("brain: invalid T6 availability")
	}
	if len(in.Candidate.ChannelCandidates) < 1 || len(in.Candidate.ChannelCandidates) > 8 || !contains(in.Candidate.ChannelCandidates, in.Candidate.DefaultChannelID) || len(in.Attention.Remaining) != 3 {
		return nil, errors.New("brain: invalid T6 channels or quota")
	}
	for n, c := range in.Candidate.ChannelCandidates {
		if len(c) < 1 || len(c) > 128 || (n > 0 && in.Candidate.ChannelCandidates[n-1] >= c) {
			return nil, errors.New("brain: T6 channels must be sorted and unique")
		}
	}
	for n, q := range in.Attention.Remaining {
		if q.Severity != []InterruptSeverity{"low", "normal", "high"}[n] || q.Remaining < 0 {
			return nil, errors.New("brain: invalid T6 quota snapshot")
		}
	}
	return decode.Canonical(in)
}

type T6Output struct {
	contract.ClosedType `json:"-"`
	Delivery            *T6Delivery `json:"delivery" sift:"required"`
	ChannelID           *string     `json:"channel_id" sift:"required,maxbytes=128"`
	SuggestedDowngrade  *bool       `json:"suggested_downgrade" sift:"required"`
	Rationale           *string     `json:"rationale" sift:"required,minbytes=1,maxbytes=2000"`
}

func T6Contract(in T6Input) TouchpointContract {
	return TouchpointContract{Touchpoint: "T6", Asset: T6Asset(), ValidateOutput: func(result []byte) ([]byte, error) {
		var out T6Output
		if err := decode.Decode(result, &out, decode.Closed); err != nil {
			return nil, err
		}
		if !contains(in.Candidate.ChannelCandidates, *out.ChannelID) || *out.Rationale != strings.TrimSpace(*out.Rationale) || hasControlOrNewline(*out.Rationale) {
			return nil, errors.New("brain: invalid T6 output")
		}
		severity := in.Candidate.Severity
		if *out.SuggestedDowngrade {
			severity = downgrade(severity)
		}
		if (severity == "high" || severity == "critical") && *out.Delivery != "immediate" || (*out.Delivery == "immediate" && severity != "critical" && in.Availability.State == "unavailable") || (*out.Delivery == "next_window" && (in.Availability.NextWindowAtMS == nil || *in.Availability.NextWindowAtMS >= in.Candidate.ExpiresAtMS)) {
			return nil, errors.New("brain: T6 delivery violates frozen scheduling constraints")
		}
		return decode.Canonical(out)
	}}
}

type T7ProposalKind string

func (T7ProposalKind) EnumValues() []string { return []string{"policy", "context"} }

type T7TargetScope string

func (T7TargetScope) EnumValues() []string { return []string{"project", "global"} }

type T7Output struct {
	contract.ClosedType   `json:"-"`
	ProposalKind          *T7ProposalKind `json:"proposal_kind" sift:"required"`
	TargetScope           *T7TargetScope  `json:"target_scope" sift:"required"`
	Title                 *string         `json:"title" sift:"required,minbytes=1,maxbytes=160"`
	Body                  *string         `json:"body" sift:"required,minbytes=1,maxbytes=8192"`
	EvidenceEntryIDs      *[]string       `json:"evidence_entry_ids" sift:"required,minitems=1,maxitems=64,itemminbytes=1,itemmaxbytes=256"`
	RequiresHumanApproval *bool           `json:"requires_human_approval" sift:"required"`
}

// T7Contract validates an inert proposal against the aggregate scope and its
// deterministically selected evidence IDs. It deliberately has no action,
// Gate, Interrupt, or policy-write capability.
func T7Contract(aggregateKey string, evidenceIDs []string) TouchpointContract {
	return TouchpointContract{Touchpoint: "T7", Asset: T7Asset(), ValidateOutput: func(result []byte) ([]byte, error) {
		var out T7Output
		if err := decode.Decode(result, &out, decode.Closed); err != nil {
			return nil, err
		}
		scope, ok := aggregateScope(aggregateKey)
		if !ok || string(*out.TargetScope) != scope || !*out.RequiresHumanApproval || *out.Title != strings.TrimSpace(*out.Title) || hasControlOrNewline(*out.Title) || !validT7Body(*out.Body) {
			return nil, errors.New("brain: invalid T7 inert proposal")
		}
		seen := map[string]bool{}
		for n, id := range *out.EvidenceEntryIDs {
			if !contains(evidenceIDs, id) || seen[id] || (n > 0 && (*out.EvidenceEntryIDs)[n-1] >= id) {
				return nil, errors.New("brain: T7 evidence IDs must be supplied, sorted, and unique")
			}
			seen[id] = true
		}
		return decode.Canonical(out)
	}}
}

var optionID = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

func inEnum(v interface{ EnumValues() []string }) bool {
	s := fmt.Sprint(v)
	return contains(v.EnumValues(), s)
}
func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
func containsOption(options []T4Option, id string) bool {
	for _, o := range options {
		if o.ID == id {
			return true
		}
	}
	return false
}
func runeCount(s string) int { return utf8.RuneCountInString(s) }
func hasControlOrNewline(s string) bool {
	return strings.ContainsAny(s, "\r\n") || strings.IndexFunc(s, unicode.IsControl) >= 0
}
func validT4Link(s string) bool {
	return strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "/") || regexp.MustCompile(`^sift://event/[0-9a-f]{32}$`).MatchString(s)
}
func downgrade(s InterruptSeverity) InterruptSeverity {
	if s == "critical" {
		return "high"
	}
	if s == "high" {
		return "normal"
	}
	if s == "normal" {
		return "low"
	}
	return "low"
}
func validT7Body(s string) bool {
	return !strings.ContainsAny(s, "\r\x00") && strings.IndexFunc(s, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }) < 0 && strings.TrimSpace(s) == s
}
func aggregateScope(key string) (string, bool) {
	p := strings.Split(key, ":")
	if len(p) < 6 || p[0] != "aggregate" || p[1] != "v1" {
		return "", false
	}
	if p[2] == "global" && len(p) == 6 && (p[3] == "all" || validTaskKind(TaskKind(p[3]))) {
		start, a := aggregateTime(p[4])
		end, b := aggregateTime(p[5])
		return "global", a && b && end > start
	}
	if p[2] == "project" && len(p) == 7 && len(p[3]) > 0 && (p[4] == "all" || validTaskKind(TaskKind(p[4]))) {
		if _, err := base64.RawURLEncoding.DecodeString(p[3]); err != nil {
			return "", false
		}
		start, a := aggregateTime(p[5])
		end, b := aggregateTime(p[6])
		return "project", a && b && end > start
	}
	return "", false
}

func aggregateTime(raw string) (int64, bool) {
	if raw == "" || (len(raw) > 1 && raw[0] == '0') {
		return 0, false
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	return value, err == nil && value >= 0
}
