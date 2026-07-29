package brain

import (
	"context"
	"testing"

	"github.com/miaoxiaoyong/sift/internal/storage"
)

func t4Input() T4Input {
	return T4Input{RunID: "run-4", Interrupt: T4Interrupt{
		Reason: "failure_review", BaseSeverity: "high", MinModality: "text",
		FallbackHeadline: "Review required", FallbackBrief: "check failed",
		BriefFragments:   []string{"check failed", "review needed"},
		CandidateOptions: []T4Option{{ID: "review", Label: "Review", Effect: "open review", Risk: "delay"}},
	}}
}

func t6Input() T6Input {
	return T6Input{RunID: "run-6", FrozenAtMS: 10, Candidate: T6Candidate{
		Reason: "failure_review", Severity: "normal", MinModality: "text", ExpiresAtMS: 20,
		ChannelCandidates: []string{"chat"}, DefaultChannelID: "chat",
	}, Availability: T6Availability{State: "available"}, Attention: T6Attention{
		FallbackImmediateMinSeverity: "high",
		Remaining:                    []T6Quota{{Severity: "low"}, {Severity: "normal"}, {Severity: "high"}},
	}}
}

func TestT4T6T7ContractsRejectUnsafeOrExecutableOutput(t *testing.T) {
	t4 := t4Input()
	if _, err := T4Contract(t4).ValidateOutput([]byte(`{"headline":"Review required","conclusion":"check failed","key_points":["review needed"],"recommended_option_id":"review","options":["review"]}`)); err != nil {
		t.Fatalf("valid T4 output: %v", err)
	}
	if _, err := T4Contract(t4).ValidateOutput([]byte(`{"headline":"changed","conclusion":"check failed","key_points":["review needed"],"recommended_option_id":"review","options":["review"]}`)); err == nil {
		t.Fatal("T4 accepted a rewritten deterministic headline")
	}

	t6 := t6Input()
	if _, err := T6Contract(t6).ValidateOutput([]byte(`{"delivery":"batch","channel_id":"chat","suggested_downgrade":false,"rationale":"wait"}`)); err != nil {
		t.Fatalf("valid T6 output: %v", err)
	}
	if _, err := T6Contract(t6).ValidateOutput([]byte(`{"delivery":"next_window","channel_id":"chat","suggested_downgrade":false,"rationale":"wait"}`)); err == nil {
		t.Fatal("T6 accepted next_window without a frozen window")
	}

	key := "aggregate:v1:global:all:1:2"
	valid := []byte(`{"proposal_kind":"policy","target_scope":"global","title":"Review trend","body":"Human review only.","evidence_entry_ids":["e1"],"requires_human_approval":true}`)
	if _, err := T7Contract(key, []string{"e1"}).ValidateOutput(valid); err != nil {
		t.Fatalf("valid T7 output: %v", err)
	}
	if _, err := T7Contract(key, []string{"e1"}).ValidateOutput([]byte(`{"proposal_kind":"policy","target_scope":"global","title":"Review trend","body":"Human review only.","evidence_entry_ids":["e1"],"requires_human_approval":true,"policy_patch":{}}`)); err == nil {
		t.Fatal("T7 accepted executable policy_patch field")
	}
}

func TestT4T6T7InvalidOutputFallsBack(t *testing.T) {
	ctx := context.Background()
	db := openShellDB(t)
	seedIntakeSubject(t, db, "p")
	if err := db.SeedForgeRunForTest(ctx, "run-4", "p", "cfg-p", "4", shellTestBase); err != nil {
		t.Fatal(err)
	}
	if err := db.SeedForgeRunForTest(ctx, "run-6", "p", "cfg-p", "6", shellTestBase); err != nil {
		t.Fatal(err)
	}
	t4JSON, err := BuildT4Input(t4Input())
	if err != nil {
		t.Fatal(err)
	}
	t6JSON, err := BuildT6Input(t6Input())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		contract TouchpointContract
		p        CallParams
	}{
		{"T4", T4Contract(t4Input()), CallParams{Scope: storage.BrainScopeRun, SubjectKey: "run:run-4", RunID: "run-4", Input: t4JSON}},
		{"T6", T6Contract(t6Input()), CallParams{Scope: storage.BrainScopeRun, SubjectKey: "run:run-6", RunID: "run-6", Input: t6JSON}},
		{"T7", T7Contract("aggregate:v1:global:all:1:2", []string{"e1"}), CallParams{Scope: storage.BrainScopeAggregate, SubjectKey: "aggregate:v1:global:all:1:2", Input: []byte(`{"aggregate_key":"aggregate:v1:global:all:1:2"}`)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &FakeProvider{Responses: []FakeResponse{{ResultText: `{"bogus":true}`}, {ResultText: `{"bogus":true}`}}}
			shell := newShellAt(db, shellCfg(100), fake, shellTestBase+1, shellTestBase+2, shellTestBase+3, shellTestBase+4, shellTestBase+5, shellTestBase+6)
			res, err := shell.Call(ctx, tc.contract, tc.p)
			if err != nil || res.Status != storage.BrainCallFallback || res.FallbackReason != "attempts exhausted: invalid_output" {
				t.Fatalf("Call = %#v, %v", res, err)
			}
		})
	}
}

func TestT4T6T7ProviderDisabledFallback(t *testing.T) {
	ctx := context.Background()
	db := openShellDB(t)
	seedIntakeSubject(t, db, "p")
	if err := db.SeedForgeRunForTest(ctx, "run-4", "p", "cfg-p", "4", shellTestBase); err != nil {
		t.Fatal(err)
	}
	if err := db.SeedForgeRunForTest(ctx, "run-6", "p", "cfg-p", "6", shellTestBase); err != nil {
		t.Fatal(err)
	}
	cfg := shellCfg(100)
	cfg.Executable = ""
	shell := newShellAt(db, cfg, &FakeProvider{}, shellTestBase+1, shellTestBase+2, shellTestBase+3, shellTestBase+4)
	for _, tc := range []struct {
		name     string
		contract TouchpointContract
		p        CallParams
	}{
		{"T4", T4Contract(t4Input()), CallParams{Scope: storage.BrainScopeRun, SubjectKey: "run:run-4", RunID: "run-4", Input: []byte(`{"run_id":"run-4"}`)}},
		{"T6", T6Contract(t6Input()), CallParams{Scope: storage.BrainScopeRun, SubjectKey: "run:run-6", RunID: "run-6", Input: []byte(`{"run_id":"run-6"}`)}},
		{"T7", T7Contract("aggregate:v1:global:all:1:2", []string{"e1"}), CallParams{Scope: storage.BrainScopeAggregate, SubjectKey: "aggregate:v1:global:all:1:2", Input: []byte(`{"aggregate_key":"aggregate:v1:global:all:1:2"}`)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := shell.Call(ctx, tc.contract, tc.p)
			if err != nil || res.Status != storage.BrainCallFallback || res.FallbackReason != "provider_disabled" {
				t.Fatalf("Call = %#v, %v", res, err)
			}
		})
	}
}
