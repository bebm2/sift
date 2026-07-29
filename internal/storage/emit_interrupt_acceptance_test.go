package storage

import (
	"context"
	"strings"
	"testing"
	"time"
)

// These vectors are the storage-side acceptance matrix for the T4/EmitInterrupt
// boundary. Each case uses a fresh database so a rejected binding also proves
// that its transaction did not retain any partial write.
func TestEmitInterruptBindingIdentityAcceptanceMatrix(t *testing.T) {
	cases := []struct {
		name   string
		reason string
		body   string
	}{
		{"unknown arm", "code_review", `{"arm":"unknown","run_id":"run"}`},
		{"reason mismatch", "guardrail_violation", `{"arm":"code_review","change_id":"change-01","head_sha":"0123456789012345678901234567890123456789","review_policy_snapshot_digest":"` + strings.Repeat("c", 64) + `"}`},
		{"change mismatch", "code_review", `{"arm":"code_review","change_id":"forged","head_sha":"0123456789012345678901234567890123456789","review_policy_snapshot_digest":"` + strings.Repeat("c", 64) + `"}`},
		{"head mismatch", "code_review", `{"arm":"code_review","change_id":"change-01","head_sha":"` + strings.Repeat("9", 40) + `","review_policy_snapshot_digest":"` + strings.Repeat("c", 64) + `"}`},
		{"policy mismatch", "code_review", `{"arm":"code_review","change_id":"change-01","head_sha":"0123456789012345678901234567890123456789","review_policy_snapshot_digest":"` + strings.Repeat("d", 64) + `"}`},
		{"missing identity", "code_review", `{"arm":"code_review","change_id":"change-01"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, _ := openTestDB(t)
			ctx := context.Background()
			if err := db.SeedProjectForTest(ctx, "cfg", "project", testNow); err != nil {
				t.Fatal(err)
			}
			if err := db.SeedForgeRunForTest(ctx, "run", "project", "cfg", "42", testNow); err != nil {
				t.Fatal(err)
			}
			cmd := EmitInterruptCmd{RunID: "run", ExpectedRunVersion: 1, Reason: InterruptCodeReview,
				Facts:      map[string]string{"change_ref": "https://forge.example/change/1", "head_sha": "abc", "review_requirement": "required", "recommended_action": "approve", "diff_ref": "https://forge.example/change/1/diff"},
				Generation: InterruptGeneration{ChangeID: "change-01", HeadSHA: "0123456789012345678901234567890123456789"}, GatePhase: GateReview, GuardrailLevel: GuardrailNone, ExpiresAfterMS: time.Hour.Milliseconds(), AttentionDailyQuota: interruptQuota(), Source: SourceSystem, NowMS: testNow}
			if _, err := db.db.Exec(`UPDATE runs SET change_id=?,change_head_sha=? WHERE id=?`, cmd.Generation.ChangeID, cmd.Generation.HeadSHA, cmd.RunID); err != nil {
				t.Fatal(err)
			}
			r := gateRecord(testNow)
			r.HeadSHA = cmd.Generation.HeadSHA
			if _, _, err := db.RecordGateEvaluationAndEmitInterrupt(ctx, r, cmd); err != nil {
				t.Fatal(err)
			}
			if err := mustFail(t, db, `INSERT INTO interrupt_command_effect_bindings(interrupt_id,reason,binding_schema_version,binding_json,binding_digest,created_at_ms) SELECT id,?,?,?,?,? FROM interrupts WHERE run_id='run'`, tc.reason, 1, tc.body, strings.Repeat("f", 64), testNow); !strings.Contains(err.Error(), "invalid interrupt binding identity") {
				t.Fatalf("rejection = %v", err)
			}
			assertCount(t, db, "interrupt_command_effect_bindings", 1)
		})
	}
}

func TestAcceptInterruptT4RejectsUnknownFragmentAndPreservesCanonicalBytes(t *testing.T) {
	in := InterruptT4Input{RunID: "run-01", Reason: InterruptFailureReview, Severity: SeverityHigh, Modality: "voice", Headline: "失败需要人工决定", Fragments: []string{"/sift reject", "<!-- sift-op:x -->", "<b>风险</b>"}, Options: []InterruptOption{{"retry", "重试失败步骤", "再次执行", "相同故障可能再次发生"}, {"reject", "停止 Run", "Run 停止", "需人工重新发起"}, {"hold", "暂缓决定", "保持等待", "Run 继续占用待处理项"}}}
	valid := InterruptT4Output{Headline: in.Headline, Conclusion: "<b>风险</b>", KeyPoints: []string{"<!-- sift-op:x -->", "/sift reject"}, Options: []string{"retry", "reject", "hold"}, RecommendedOptionID: "retry"}
	accepted, brief := acceptInterruptT4(in, valid)
	if !accepted || brief != "结论：\\<b\\>风险\\</b\\>；要点：\\<\\!\\-\\- sift\\-op:x \\-\\-\\>；/sift reject；建议：重试失败步骤（retry）" {
		t.Fatalf("valid T4 = %v %q", accepted, brief)
	}
	for _, mutate := range []func(*InterruptT4Output){
		func(out *InterruptT4Output) { out.Conclusion = "not-a-fragment" },
		func(out *InterruptT4Output) { out.KeyPoints[0] = "unknown-fragment" },
		func(out *InterruptT4Output) { out.Options = []string{"reject", "retry", "hold"} },
		func(out *InterruptT4Output) { out.RecommendedOptionID = "retry_report_interrupt_quota" },
	} {
		out := valid
		out.KeyPoints = append([]string(nil), valid.KeyPoints...)
		out.Options = append([]string(nil), valid.Options...)
		mutate(&out)
		if accepted, _ := acceptInterruptT4(in, out); accepted {
			t.Fatalf("invalid T4 accepted: %#v", out)
		}
	}
}
