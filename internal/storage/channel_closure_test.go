package storage

import (
	"context"
	"strings"
	"testing"
)

func TestReportQuotaCommandRetainsFrozenChannels(t *testing.T) {
	cfg := reportRuntimeConfig{}
	cfg.Attention.Channels = []reportRuntimeChannel{{ID: "ops", Enabled: true, Type: "webhook", TargetRef: "secret_ref:OPS", Capabilities: []string{"voice"}, Renderer: "plain-v1", Default: true}}
	cmd := reportQuotaCmd(ReportSubmitCmd{RunID: "run", NowMS: testNow}, 1, testNow, testNow+1, cfg)
	if len(cmd.Channels) != 1 || cmd.Channels[0].ID != "ops" || cmd.Channels[0].TargetRef != "secret_ref:OPS" || cmd.Channels[0].Isolated {
		t.Fatalf("quota channels = %#v", cmd.Channels)
	}
}

func TestMemberedBatchCannotBeRetargeted(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	if err := db.SeedProjectForTest(ctx, "cfg", "project", testNow); err != nil {
		t.Fatal(err)
	}
	if err := db.SeedForgeRunForTest(ctx, "run", "project", "cfg", "42", testNow); err != nil {
		t.Fatal(err)
	}
	at := int64(testNow + 1)
	cmd := t6Command(testNow)
	cmd.AttentionDailyQuota = map[InterruptSeverity]int{SeverityLow: 0, SeverityNormal: 0, SeverityHigh: 0}
	cmd.Channels = []InterruptChannel{{ID: "ops", Type: "webhook", TargetRef: "secret_ref:OPS", Renderer: "plain-v1", Capabilities: []string{"visual"}}}
	cmd.BatchAtMS = &at
	if _, err := db.EmitInterrupt(ctx, cmd); err != nil {
		t.Fatal(err)
	}
	var batch string
	if err := db.db.QueryRow(`SELECT batch_id FROM attention_batch_members`).Scan(&batch); err != nil {
		t.Fatal(err)
	}
	for column, value := range map[string]any{
		"project_id":            "other-project",
		"channel_id":            "other-channel",
		"channel_snapshot_json": `{"id":"other"}`,
		"forge_kind":            "gitlab",
		"forge_host":            "retarget.invalid",
		"forge_project_key":     "other/project",
		"target_kind":           "change",
		"target_id":             "99",
		"kind":                  "critical_fuse",
		"delivery_id":           "other:publish:1",
		"scope":                 "global",
		"scope_id":              "global",
		"episode_admission_id":  "other-admission",
		"due_at_ms":             testNow + 99,
	} {
		if _, err := db.db.Exec(`UPDATE attention_batches SET `+column+`=? WHERE id=?`, value, batch); err == nil || !strings.Contains(err.Error(), "immutable") {
			t.Fatalf("retarget %s error = %v", column, err)
		}
	}
}

func TestChannelDiagnosticsIncludesBatchFailureProjection(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	if err := db.SeedProjectForTest(ctx, "cfg", "project", testNow); err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"batch_id":"batch","batch_kind":"daily_summary","channel":{"id":"ops","renderer":"plain-v1","target_ref":"secret_ref:OPS","type":"webhook"},"delivery_id":"batch:publish:1","delivery_kind":"attention_batch","due_at_ms":1,"forge_alert_target":{"forge_host":"github.com","forge_kind":"github","forge_project_key":"org/repo-project","target_id":"42","target_kind":"issue"},"project_id":"project","scope":"day","scope_id":"UTC:1"}`)
	if err := db.EnqueueChannelPublish(ctx, Operation{Key: "attention-batch:batch:publish:1", Kind: OperationChannelPublish, Payload: payload}, "batch:publish:1", testNow); err != nil {
		t.Fatal(err)
	}
	db.SetChannelPolicy(3, 3)
	claim, err := db.ClaimOutboxOperationKind(ctx, "worker", OperationChannelPublish, testNow, 100)
	if err != nil || claim == nil {
		t.Fatalf("claim = %#v, %v", claim, err)
	}
	if err := db.CompleteOutboxAttempt(ctx, *claim, CompleteOutcome{State: OperationRetryable, ErrorClass: ErrorTransient, ErrorSummary: "safe", NowMS: testNow + 1}); err != nil {
		t.Fatal(err)
	}
	got, err := db.ChannelDiagnostics(ctx)
	if err != nil || len(got) != 1 || got[0]["consecutive_failures"] != int64(1) || got[0]["generated_not_delivered"] != true {
		t.Fatalf("diagnostics = %#v, %v", got, err)
	}
	second, err := db.ClaimOutboxOperationKind(ctx, "worker", OperationChannelPublish, testNow+2, 100)
	if err != nil || second == nil {
		t.Fatalf("second claim = %#v, %v", second, err)
	}
	if err := db.CompleteOutboxAttempt(ctx, *second, CompleteOutcome{State: OperationRetryable, ErrorClass: ErrorRateLimited, ErrorSummary: "safe", NowMS: testNow + 3}); err != nil {
		t.Fatal(err)
	}
	third, err := db.ClaimOutboxOperationKind(ctx, "worker", OperationChannelPublish, testNow+4, 100)
	if err != nil || third == nil || third.ClaimAttemptNo != 3 {
		t.Fatalf("third claim = %#v, %v", third, err)
	}
	if claimed, err := db.ClaimOutboxOperationKind(ctx, "reclaimer", OperationChannelPublish, testNow+105, 100); err != nil || claimed != nil {
		t.Fatalf("terminal reclaim = %#v, %v", claimed, err)
	}
	if err := db.CompleteOutboxAttempt(ctx, *third, CompleteOutcome{State: OperationSucceeded, NowMS: testNow + 106}); err != ErrRejectedStaleWorker {
		t.Fatalf("stale terminal completion = %v", err)
	}
	var state string
	var failures int
	if err := db.db.QueryRow(`SELECT state FROM outbox_operations WHERE operation_key='attention-batch:batch:publish:1'`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if err := db.db.QueryRow(`SELECT consecutive_failures FROM channel_failure_episodes WHERE subject_id='batch:publish:1'`).Scan(&failures); err != nil {
		t.Fatal(err)
	}
	if state != "failed" || failures != 3 {
		t.Fatalf("terminal state/failures = %s/%d", state, failures)
	}
	assertCount(t, db, "outbox_attempts", 3)
	assertCount(t, db, "outbox_attempt_results", 3)
	assertCount(t, db, "channel_failure_episodes", 1)
	assertCount(t, db, "outbox_operations", 2)
}
