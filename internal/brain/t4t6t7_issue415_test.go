package brain

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/miaoxiaoyong/sift/internal/decode"
	"github.com/miaoxiaoyong/sift/internal/storage"
)

func issue415T7Input(key, project string, kinds []TaskKind) T7Input {
	digest := strings.Repeat("a", 64)
	return T7Input{AggregateKey: key, Window: T7Window{StartMS: 1, EndMS: 2}, Categories: []T7CategoryEvidence{{EvidenceID: "cat", TaskKind: TaskBug, CertificationVersion: digest, EvidenceSummary: T7EvidenceSummary{WindowStartMS: 1, WindowEndMS: 2, CertificationRulesVersion: digest, EvidenceDigest: digest}}}, ReplaySummary: T7ReplaySummary{EvidenceID: "replay", DatasetVersion: "v1", GateVersion: "gate/v1"}, SemanticMaterial: []T7SemanticMaterial{}, TraceProjectID: project, AllCategoryKinds: kinds}
}

func TestIssue415ReasonsUseStorageCanonicalSet(t *testing.T) {
	for _, reason := range storage.ActiveInterruptReasons() {
		t4 := t4Input()
		t4.Interrupt.Reason = InterruptReason(reason)
		if _, err := BuildT4Input(t4); err != nil {
			t.Fatalf("T4 rejected %q: %v", reason, err)
		}
		t6 := t6Input()
		t6.Candidate.Reason = InterruptReason(reason)
		if _, err := BuildT6Input(t6); err != nil {
			t.Fatalf("T6 rejected %q: %v", reason, err)
		}
	}
	for _, reason := range []InterruptReason{"human_input", "merge_approval", "policy_block", "rate_limited", "run_stalled"} {
		t4 := t4Input()
		t4.Interrupt.Reason = reason
		if _, err := BuildT4Input(t4); err == nil {
			t.Fatalf("T4 accepted retired %q", reason)
		}
		t6 := t6Input()
		t6.Candidate.Reason = reason
		if _, err := BuildT6Input(t6); err == nil {
			t.Fatalf("T6 accepted retired %q", reason)
		}
	}
}

func TestIssue415T7TraceProjectAndPreReserveValidation(t *testing.T) {
	project := "project-7"
	key := "aggregate:v1:project:" + base64.RawURLEncoding.EncodeToString([]byte(project)) + ":bug:1:2"
	input, err := BuildT7Input(issue415T7Input(key, project, nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := T7Contract(key, project, nil, []string{"cat"}).ValidateInput(CallParams{Scope: storage.BrainScopeAggregate, SubjectKey: key, ProjectID: project, Input: input}); err != nil {
		t.Fatalf("project round trip: %v", err)
	}
	if err := T7Contract(key, "wrong", nil, []string{"cat"}).ValidateInput(CallParams{Scope: storage.BrainScopeAggregate, SubjectKey: key, ProjectID: "wrong", Input: input}); err == nil {
		t.Fatal("project trace drift accepted")
	}

	global := "aggregate:v1:global:all:1:2"
	good, err := BuildT7Input(issue415T7Input(global, "", []TaskKind{TaskBug}))
	if err != nil {
		t.Fatal(err)
	}
	bad := issue415T7Input(global, "", []TaskKind{TaskBug})
	bad.Categories[0].EvidenceSummary.NegativeSamples = 1
	badJSON, err := decode.Canonical(bad)
	if err != nil {
		t.Fatal(err)
	}
	db := openShellDB(t)
	provider := &FakeProvider{}
	shell := newShellAt(db, shellCfg(100), provider, shellTestBase+1, shellTestBase+2, shellTestBase+3)
	contract := T7Contract(global, "", []TaskKind{TaskBug}, []string{"cat"})
	if _, err := shell.Call(context.Background(), contract, CallParams{Scope: storage.BrainScopeAggregate, SubjectKey: global, Input: badJSON}); err == nil {
		t.Fatal("count overflow accepted")
	}
	if len(provider.Requests) != 0 {
		t.Fatal("provider called for invalid input")
	}
	result, err := shell.Call(context.Background(), contract, CallParams{Scope: storage.BrainScopeAggregate, SubjectKey: global, Input: good})
	if err != nil || result.CallSeq != 1 {
		t.Fatalf("invalid input reserved a trace: %+v %v", result, err)
	}
}

func TestIssue415ValidAdaptersPreserveBrainIdentity(t *testing.T) {
	result := CallResult{CallID: "call-valid", Status: "valid", PromptVersion: "T/v1/test", OutputSchemaVersion: 1}
	t4, source, err := T4ResultFromCall(CallResult{CallID: result.CallID, Status: result.Status, PromptVersion: result.PromptVersion, OutputSchemaVersion: result.OutputSchemaVersion, Output: []byte(`{"headline":"Review required","conclusion":"check failed","key_points":["review needed"],"recommended_option_id":"review","options":["review"]}`)}, t4Input())
	if err != nil || t4.Normal == nil || t4.Fallback != nil || source.Kind != "brain" || source.LogicalCallID != result.CallID || source.PromptVersion != result.PromptVersion || source.OutputSchemaVersion != 1 {
		t.Fatalf("T4 valid adapter = %#v %#v %v", t4, source, err)
	}
	t6, source, err := T6ResultFromCall(CallResult{CallID: result.CallID, Status: result.Status, PromptVersion: result.PromptVersion, OutputSchemaVersion: result.OutputSchemaVersion, Output: []byte(`{"delivery":"batch","channel_id":"chat","suggested_downgrade":false,"rationale":"wait"}`)}, t6Input())
	if err != nil || t6.Delivery == nil || source.Kind != "brain" || source.LogicalCallID != result.CallID || source.PromptVersion != result.PromptVersion {
		t.Fatalf("T6 valid adapter = %#v %#v %v", t6, source, err)
	}
	t7, source, err := T7ResultFromCall(CallResult{CallID: result.CallID, Status: result.Status, PromptVersion: result.PromptVersion, OutputSchemaVersion: result.OutputSchemaVersion, Output: []byte(`{"proposal_kind":"policy","target_scope":"global","title":"Review trend","body":"Human review only.","evidence_entry_ids":["cat"],"requires_human_approval":true}`)}, "aggregate:v1:global:all:1:2", []string{"cat"})
	if err != nil || t7.Proposal == nil || t7.NoDraft || source.Kind != "brain" || source.LogicalCallID != result.CallID || source.PromptVersion != result.PromptVersion {
		t.Fatalf("T7 valid adapter = %#v %#v %v", t7, source, err)
	}
}

func TestIssue415FallbackAdaptersAreClosed(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{{"provider_disabled", "provider_disabled"}, {"token_budget_exceeded", "token_threshold"}, {"input_too_large", "input_too_large"}, {"invalid_output", "invalid_output"}, {"provider_error", "provider_error"}, {"recovery", "recovery"}} {
		reason := tc.want
		call := CallResult{CallID: "call-" + reason, Status: "fallback", FallbackReason: tc.raw}
		t4, source, err := T4ResultFromCall(call, t4Input())
		if err != nil || t4.Fallback == nil || t4.Normal != nil || source.Reason != tc.want || source.Version != "T4/fallback/v1" {
			t.Fatalf("T4 %q: %#v %#v %v", reason, t4, source, err)
		}
		t7, source, err := T7ResultFromCall(call, "aggregate:v1:global:all:1:2", []string{"cat"})
		if err != nil || !t7.NoDraft || t7.Proposal != nil || source.Reason != tc.want || source.Version != "T7/fallback/v1" {
			t.Fatalf("T7 %q: %#v %#v %v", reason, t7, source, err)
		}
		for _, severity := range []InterruptSeverity{"low", "normal", "high", "critical"} {
			in := t6Input()
			in.Candidate.Severity = severity
			out, _, err := T6ResultFromCall(call, in)
			want := T6Delivery("batch")
			if severity == "high" || severity == "critical" {
				want = "immediate"
			}
			if err != nil || out.Delivery == nil || *out.Delivery != want {
				t.Fatalf("T6 %s: %#v %v", severity, out, err)
			}
		}
	}
	in := t4Input()
	in.Interrupt.Links = []T4Link{{Label: "evidence", Target: "https://example.test/e"}}
	var fallback T4Input
	if err := decode.Decode(T4FallbackOutput(in), &fallback, decode.Closed); err != nil || len(fallback.Interrupt.Links) != 1 || fallback.Interrupt.FallbackBrief != in.Interrupt.FallbackBrief {
		t.Fatalf("lossless fallback: %#v %v", fallback, err)
	}
}
