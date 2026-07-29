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
		{"null identity", "code_review", `{"arm":"code_review","change_id":null,"head_sha":"0123456789012345678901234567890123456789","review_policy_snapshot_digest":"` + strings.Repeat("c", 64) + `"}`},
		{"wrong identity type", "code_review", `{"arm":"code_review","change_id":42,"head_sha":"0123456789012345678901234567890123456789","review_policy_snapshot_digest":"` + strings.Repeat("c", 64) + `"}`},
		{"extra field", "code_review", `{"arm":"code_review","change_id":"change-01","head_sha":"0123456789012345678901234567890123456789","review_policy_snapshot_digest":"` + strings.Repeat("c", 64) + `","extra":"forged"}`},
		{"noncanonical key order", "code_review", `{"change_id":"change-01","arm":"code_review","head_sha":"0123456789012345678901234567890123456789","review_policy_snapshot_digest":"` + strings.Repeat("c", 64) + `"}`},
		{"failure review arm mismatch", "failure_review", `{"arm":"failure_review_attempt","run_id":"run","attempt_no":1,"generation":1,"retry_kind":"gate_recheck","change_id":null,"head_sha":null,"terminal_attempt_no":null,"terminal_generation":null}`},
		{"quota arm mismatch", "failure_review", `{"arm":"report_quota_failure_review","run_id":"run","daily_bucket_start_ms":1,"daily_bucket_end_ms":2,"security_event_id":"0123456789abcdef0123456789abcdef"}`},
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
			if err := mustFail(t, db, `INSERT INTO interrupt_command_effect_bindings(interrupt_id,reason,binding_schema_version,binding_json,binding_digest,created_at_ms) SELECT id,?,?,?,?,? FROM interrupts WHERE run_id='run'`, tc.reason, 1, tc.body, strings.Repeat("f", 64), testNow); err == nil || (!strings.Contains(err.Error(), "invalid interrupt binding identity") && !strings.Contains(err.Error(), "JSON key order is not canonical")) {
				t.Fatalf("rejection = %v", err)
			}
			assertCount(t, db, "interrupt_command_effect_bindings", 1)
		})
	}
}

func TestGateFailureReviewPersistsExactBindingProvenance(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	if err := db.SeedProjectForTest(ctx, "cfg", "project", testNow); err != nil {
		t.Fatal(err)
	}
	if err := db.SeedForgeRunForTest(ctx, "run", "project", "cfg", "42", testNow); err != nil {
		t.Fatal(err)
	}
	const head = "0123456789012345678901234567890123456789"
	mustExec(t, db, `UPDATE runs SET change_id='change-01',change_head_sha=? WHERE id='run'`, head)
	r := gateRecord(testNow)
	r.HeadSHA = head
	cmd := EmitInterruptCmd{RunID: "run", ExpectedRunVersion: 1, Reason: InterruptCodeReview,
		Facts:      map[string]string{"change_ref": "https://forge.example/change/1", "head_sha": head, "review_requirement": "required", "recommended_action": "approve", "diff_ref": "https://forge.example/change/1/diff"},
		Generation: InterruptGeneration{ChangeID: "change-01", HeadSHA: head}, GatePhase: GateReview, GuardrailLevel: GuardrailNone, AttentionDailyQuota: interruptQuota(), Source: SourceSystem, NowMS: testNow}
	if _, _, err := db.RecordGateEvaluationAndEmitInterrupt(ctx, r, cmd); err != nil {
		t.Fatal(err)
	}
	var binding, digest, snapshotHead, policy, evaluationRun, calibrationRun string
	if err := db.db.QueryRow(`SELECT b.binding_json,b.binding_digest,s.head_sha,s.effective_policy_hash,e.run_id,c.run_id FROM interrupt_command_effect_bindings b JOIN interrupts i ON i.id=b.interrupt_id JOIN calibration_entries c ON c.id=i.calibration_id JOIN gate_evaluations e ON e.id=c.gate_evaluation_id JOIN gate_input_snapshots s ON s.id=e.snapshot_id WHERE i.run_id='run'`).Scan(&binding, &digest, &snapshotHead, &policy, &evaluationRun, &calibrationRun); err != nil {
		t.Fatal(err)
	}
	want := `{"arm":"code_review","change_id":"change-01","head_sha":"` + head + `","review_policy_snapshot_digest":"` + strings.Repeat("c", 64) + `"}`
	if binding != want || snapshotHead != head || policy != strings.Repeat("c", 64) || evaluationRun != "run" || calibrationRun != "run" {
		t.Fatalf("provenance binding=%q snapshot=%q policy=%q evaluation=%q calibration=%q", binding, snapshotHead, policy, evaluationRun, calibrationRun)
	}
	var digestOK int
	if err := db.db.QueryRow(`SELECT lower(hex(sift_sha256(binding_json)))=binding_digest FROM interrupt_command_effect_bindings WHERE binding_json=?`, binding).Scan(&digestOK); err != nil || digestOK != 1 {
		t.Fatalf("binding digest valid=%d err=%v", digestOK, err)
	}
}

func TestGateBindingFailureRollsBackProvenanceAndEmission(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	if err := db.SeedProjectForTest(ctx, "cfg", "project", testNow); err != nil {
		t.Fatal(err)
	}
	if err := db.SeedForgeRunForTest(ctx, "run", "project", "cfg", "42", testNow); err != nil {
		t.Fatal(err)
	}
	const head = "0123456789012345678901234567890123456789"
	mustExec(t, db, `UPDATE runs SET change_id='change-01',change_head_sha=? WHERE id='run'`, head)
	mustExec(t, db, `CREATE TRIGGER fail_gate_binding BEFORE INSERT ON interrupt_command_effect_bindings BEGIN SELECT RAISE(ABORT,'injected gate binding failure'); END`)
	r := gateRecord(testNow)
	r.HeadSHA = head
	cmd := EmitInterruptCmd{RunID: "run", ExpectedRunVersion: 1, Reason: InterruptCodeReview, Facts: map[string]string{"change_ref": "https://forge.example/change/1", "head_sha": head, "review_requirement": "required", "recommended_action": "approve", "diff_ref": "https://forge.example/change/1/diff"}, Generation: InterruptGeneration{ChangeID: "change-01", HeadSHA: head}, GatePhase: GateReview, GuardrailLevel: GuardrailNone, AttentionDailyQuota: interruptQuota(), Source: SourceSystem, NowMS: testNow}
	if _, _, err := db.RecordGateEvaluationAndEmitInterrupt(ctx, r, cmd); err == nil || !strings.Contains(err.Error(), "injected gate binding failure") {
		t.Fatalf("error=%v", err)
	}
	for _, table := range []string{"gate_input_snapshots", "gate_evaluations", "calibration_entries", "ledger_entries", "interrupts", "attention_admissions", "budget_entries", "events", "outbox_operations", "interrupt_deliveries", "interrupt_command_effect_bindings"} {
		assertCount(t, db, table, 0)
	}
	var status string
	var version int
	if err := db.db.QueryRow(`SELECT status,version FROM runs WHERE id='run'`).Scan(&status, &version); err != nil {
		t.Fatal(err)
	}
	if status != "queued" || version != 1 {
		t.Fatalf("run transition leaked: %s/%d", status, version)
	}
}

func TestEmitInterruptBindingFailureRollsBackFiveThings(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	if err := db.SeedProjectForTest(ctx, "cfg", "project", testNow); err != nil {
		t.Fatal(err)
	}
	if err := db.SeedForgeRunForTest(ctx, "run", "project", "cfg", "42", testNow); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, `CREATE TRIGGER fail_emit_binding BEFORE INSERT ON interrupt_command_effect_bindings BEGIN SELECT RAISE(ABORT, 'injected binding failure'); END`)
	cmd := EmitInterruptCmd{RunID: "run", ExpectedRunVersion: 1, Reason: InterruptDesignApproval,
		Facts:      map[string]string{"risk_summary": "risk", "recommended_action": "approve", "task_spec_ref": "/task"},
		Generation: InterruptGeneration{TaskSpecSnapshotID: "task-01"}, GatePhase: GateNone, GuardrailLevel: GuardrailNone,
		AttentionDailyQuota: interruptQuota(), Source: SourceSystem, NowMS: testNow}
	if _, err := db.EmitInterrupt(ctx, cmd); err == nil || !strings.Contains(err.Error(), "injected binding failure") {
		t.Fatalf("EmitInterrupt error = %v", err)
	}
	for _, table := range []string{"interrupts", "attention_admissions", "budget_entries", "events", "outbox_operations", "interrupt_deliveries", "interrupt_command_effect_bindings"} {
		assertCount(t, db, table, 0)
	}
	var status string
	var version int
	if err := db.db.QueryRow(`SELECT status,version FROM runs WHERE id='run'`).Scan(&status, &version); err != nil {
		t.Fatal(err)
	}
	if status != "queued" || version != 1 {
		t.Fatalf("run transition leaked: status=%s version=%d", status, version)
	}
}

func TestEmitInterruptT4CanonicalAttemptAndQuotaVectors(t *testing.T) {
	attempt := InterruptT4Input{RunID: "run-01", AttemptNo: intPtr(1), Reason: InterruptFailureReview, Severity: SeverityHigh, Modality: "voice", Headline: "失败需要人工决定", Brief: "事实：failure_class=CI；failure_evidence_ref=/r/ci；recommended_action=retry。建议：retry", Fragments: []string{"/sift reject", "<!-- sift-op:x -->", "<b>风险</b>"}, Links: []InterruptLink{{Label: "failure_evidence_ref", Target: "/r/ci"}}, Options: []InterruptOption{{"retry", "重试失败步骤", "再次执行", "相同故障可能再次发生"}, {"reject", "停止 Run", "Run 停止", "需人工重新发起"}, {"hold", "暂缓决定", "保持等待", "Run 继续占用待处理项"}}}
	attemptOut := InterruptT4Output{Headline: attempt.Headline, Conclusion: "<b>风险</b>", KeyPoints: []string{"<!-- sift-op:x -->", "/sift reject"}, Options: []string{"retry", "reject", "hold"}, RecommendedOptionID: "retry"}
	assertCanonicalT4Bytes(t, attempt, attemptOut, `{"attempt_no":1,"interrupt":{"base_severity":"high","brief_fragments":["/sift reject","<!-- sift-op:x -->","<b>风险</b>"],"candidate_options":[{"effect":"再次执行","id":"retry","label":"重试失败步骤","risk":"相同故障可能再次发生"},{"effect":"Run 停止","id":"reject","label":"停止 Run","risk":"需人工重新发起"},{"effect":"保持等待","id":"hold","label":"暂缓决定","risk":"Run 继续占用待处理项"}],"fallback_brief":"事实：failure_class=CI；failure_evidence_ref=/r/ci；recommended_action=retry。建议：retry","fallback_headline":"失败需要人工决定","links":[{"label":"failure_evidence_ref","target":"/r/ci"}],"min_modality":"voice","reason":"failure_review"},"run_id":"run-01"}`, `{"conclusion":"<b>风险</b>","headline":"失败需要人工决定","key_points":["<!-- sift-op:x -->","/sift reject"],"options":["retry","reject","hold"],"recommended_option_id":"retry"}`)

	quota := attempt
	quota.AttemptNo = nil
	quota.Brief = "事实：failure_class=report_interrupt_quota_exhausted；failure_evidence_ref=sift://event/0123456789abcdef0123456789abcdef；recommended_action=hold。建议：hold"
	quota.Headline = "报告打扰额度已耗尽"
	quota.Fragments = []string{"请人工处理", "额度已耗尽"}
	quota.Links = []InterruptLink{{Label: "failure_evidence_ref", Target: "sift://event/0123456789abcdef0123456789abcdef"}}
	quota.Options = []InterruptOption{{"reject", "停止 Run", "Run 停止", "需人工重新发起"}, {"hold", "暂缓决定", "保持 Interrupt 人工 held", "Run 继续运行"}}
	quotaOut := InterruptT4Output{Headline: quota.Headline, Conclusion: "额度已耗尽", KeyPoints: []string{"请人工处理"}, Options: []string{"reject", "hold"}, RecommendedOptionID: "hold"}
	assertCanonicalT4Bytes(t, quota, quotaOut, `{"attempt_no":null,"interrupt":{"base_severity":"high","brief_fragments":["请人工处理","额度已耗尽"],"candidate_options":[{"effect":"Run 停止","id":"reject","label":"停止 Run","risk":"需人工重新发起"},{"effect":"保持 Interrupt 人工 held","id":"hold","label":"暂缓决定","risk":"Run 继续运行"}],"fallback_brief":"事实：failure_class=report_interrupt_quota_exhausted；failure_evidence_ref=sift://event/0123456789abcdef0123456789abcdef；recommended_action=hold。建议：hold","fallback_headline":"报告打扰额度已耗尽","links":[{"label":"failure_evidence_ref","target":"sift://event/0123456789abcdef0123456789abcdef"}],"min_modality":"voice","reason":"failure_review"},"run_id":"run-01"}`, `{"conclusion":"额度已耗尽","headline":"报告打扰额度已耗尽","key_points":["请人工处理"],"options":["reject","hold"],"recommended_option_id":"hold"}`)
}

func assertCanonicalT4Bytes(t *testing.T, in InterruptT4Input, out InterruptT4Output, wantIn, wantOut string) {
	t.Helper()
	options := make([]map[string]string, len(in.Options))
	for i, option := range in.Options {
		options[i] = map[string]string{"effect": option.Effect, "id": option.ID, "label": option.Label, "risk": option.Risk}
	}
	links := make([]map[string]string, len(in.Links))
	for i, link := range in.Links {
		links[i] = map[string]string{"label": link.Label, "target": link.Target}
	}
	input, err := canonicalJSON(map[string]any{"attempt_no": in.AttemptNo, "interrupt": map[string]any{"base_severity": in.Severity, "brief_fragments": in.Fragments, "candidate_options": options, "fallback_brief": in.Brief, "fallback_headline": in.Headline, "links": links, "min_modality": in.Modality, "reason": in.Reason}, "run_id": in.RunID})
	if err != nil || string(input) != wantIn {
		t.Fatalf("canonical T4 input = %s, err=%v", input, err)
	}
	output, err := canonicalJSON(map[string]any{"conclusion": out.Conclusion, "headline": out.Headline, "key_points": out.KeyPoints, "options": out.Options, "recommended_option_id": out.RecommendedOptionID})
	if err != nil || string(output) != wantOut {
		t.Fatalf("canonical T4 output = %s, err=%v", output, err)
	}
	if accepted, _ := acceptInterruptT4(in, out); !accepted {
		t.Fatal("golden output was rejected")
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
