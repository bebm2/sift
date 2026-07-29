package brain

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/miaoxiaoyong/sift/internal/storage"
)

func TestEmitInterruptT4PersistsProductionCanonicalTrace(t *testing.T) {
	ctx := context.Background()
	db := openShellDB(t)
	seedIntakeSubject(t, db, "p")
	if err := db.SeedGateCandidateForTest(ctx, "run-01", "p", "cfg-p", "change-01", shellTestBase); err != nil {
		t.Fatal(err)
	}
	fake := &FakeProvider{Responses: []FakeResponse{{ResultText: `{"headline":"无法确认旧执行体已停止","conclusion":"termination_unconfirmed","key_points":["worktree 保持隔离"],"recommended_option_id":"retry","options":["retry","reject","hold"]}`, InputTokens: 1, OutputTokens: 1}}}
	shell := newShellAt(db, shellCfg(100), fake, shellTestBase+1, shellTestBase+2, shellTestBase+3, shellTestBase+4)
	db.SetInterruptT4(shell.CallT4)
	attempt := 1
	cmd := storage.EmitInterruptCmd{RunID: "run-01", ExpectedRunVersion: 1, AttemptNo: &attempt, Reason: storage.InterruptStartupStall,
		Facts:      map[string]string{"attempt_no": "1", "generation": "1", "diagnostic_cause": "termination_unconfirmed", "isolation_consequence": "worktree 保持隔离", "recommended_action": "retry", "attempt_diagnostic_ref": "/attempt", "worktree_ref": "/worktree"},
		Generation: storage.InterruptGeneration{AttemptNo: 1, Generation: 1}, GatePhase: storage.GateNone, GuardrailLevel: storage.GuardrailNone,
		AttentionDailyQuota: map[storage.InterruptSeverity]int{storage.SeverityLow: 3, storage.SeverityNormal: 3, storage.SeverityHigh: 3}, DayTimezone: "UTC", Source: storage.SourceSystem, NowMS: shellTestBase,
	}
	interrupt, err := db.EmitInterrupt(ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if interrupt.Brief != "结论：termination_unconfirmed；要点：worktree 保持隔离；建议：重新探测旧执行体（retry）" {
		t.Fatalf("persisted brief = %q", interrupt.Brief)
	}

	input := T4Input{RunID: "run-01", AttemptNo: &attempt, Interrupt: T4Interrupt{Reason: "startup_stall", BaseSeverity: "high", MinModality: "text", FallbackHeadline: "无法确认旧执行体已停止", FallbackBrief: "事实：attempt_no=1；generation=1；diagnostic_cause=termination\\_unconfirmed；isolation_consequence=worktree 保持隔离；recommended_action=retry；attempt_diagnostic_ref=/attempt；worktree_ref=/worktree。建议：retry", BriefFragments: []string{"/attempt", "/worktree", "1", "retry", "termination_unconfirmed", "worktree 保持隔离"}, Links: []T4Link{{Label: "attempt_diagnostic_ref", Target: "/attempt"}, {Label: "worktree_ref", Target: "/worktree"}}, CandidateOptions: []T4Option{{ID: "retry", Label: "重新探测旧执行体", Effect: "请求受控终止再探测", Risk: "未确认消失时仍保持隔离"}, {ID: "reject", Label: "放弃此 Run", Effect: "停止处理并保持隔离", Risk: "不代表旧执行体已停止"}, {ID: "hold", Label: "继续等待", Effect: "保持等待和隔离", Risk: "旧执行体可能仍在运行"}}}}
	wantInput, err := BuildT4Input(input)
	if err != nil {
		t.Fatal(err)
	}
	wantOutput, err := T4Contract(input).ValidateOutput([]byte(fake.Responses[0].ResultText))
	if err != nil {
		t.Fatal(err)
	}
	var trace bytes.Buffer
	if err := db.ExportBrainCallsJSONL(ctx, &trace); err != nil {
		t.Fatal(err)
	}
	var record struct {
		Input  json.RawMessage `json:"input"`
		Output json.RawMessage `json:"validated_output"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(trace.Bytes()), &record); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(record.Input, wantInput) || !bytes.Equal(record.Output, wantOutput) {
		t.Fatalf("trace input/output = %s / %s, want %s / %s", record.Input, record.Output, wantInput, wantOutput)
	}
}
