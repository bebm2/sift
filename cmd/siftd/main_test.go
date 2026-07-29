package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/miaoxiaoyong/sift/internal/config"
	"github.com/miaoxiaoyong/sift/internal/daemon"
	"github.com/miaoxiaoyong/sift/internal/forge"
	"github.com/miaoxiaoyong/sift/internal/forgeworker"
	"github.com/miaoxiaoyong/sift/internal/runtime"
	"github.com/miaoxiaoyong/sift/internal/storage"
)

const productionWakeNow = int64(1_800_000_000_000)

func TestProductionSchedulerWakesOutboxAfterEnqueueAndEmitInterrupt(t *testing.T) {
	testProductionWake(t, func(ctx context.Context, db *storage.DB, now int64) error {
		payload, err := json.Marshal(map[string]any{
			"project_id": "project", "forge_kind": "github", "forge_host": "github.com",
			"forge_project_key": "org/repo", "target_kind": "issue", "target_id": "42",
			"purpose": "summary", "markdown": "queued",
		})
		if err != nil {
			return err
		}
		_, err = db.EnqueueOperation(ctx, storage.Operation{
			Key: storage.CommentOperationKey("summary", "42", 1), Kind: storage.OperationForgeComment, Payload: payload,
		}, now)
		return err
	})

	testProductionWake(t, func(ctx context.Context, db *storage.DB, now int64) error {
		if err := db.SeedReverseSyncRunForTest(ctx, "run", "project", "cfg", "42", "", "queued", now); err != nil {
			return err
		}
		_, err := db.EmitInterrupt(ctx, storage.EmitInterruptCmd{
			RunID: "run", ExpectedRunVersion: 1, Reason: storage.InterruptCodeReview,
			Facts:      map[string]string{"change_ref": "https://example.test/change/1", "head_sha": "0123456789abcdef0123456789abcdef01234567", "review_requirement": "required", "recommended_action": "approve", "diff_ref": "https://example.test/change/1/diff"},
			Generation: storage.InterruptGeneration{ChangeID: "change-1", HeadSHA: "0123456789abcdef0123456789abcdef01234567"},
			GatePhase:  storage.GateNone, GuardrailLevel: storage.GuardrailNone,
			AttentionDailyQuota: map[storage.InterruptSeverity]int{storage.SeverityLow: 10, storage.SeverityNormal: 10, storage.SeverityHigh: 10},
			DayTimezone:         "UTC", Source: storage.SourceSystem, NowMS: now,
		})
		return err
	})
}

func testProductionWake(t *testing.T, enqueue func(context.Context, *storage.DB, int64) error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.UnixMilli(productionWakeNow)
	db, err := storage.Open(ctx, storage.OpenConfig{Path: filepath.Join(t.TempDir(), "sift.db"), BinaryVersion: "test", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.SeedProjectForTest(ctx, "cfg", "project", productionWakeNow); err != nil {
		t.Fatal(err)
	}

	client := forge.NewFake()
	ref := forge.ProjectRef{Kind: forge.KindGitHub, Host: "github.com", ProjectKey: "org/repo"}
	client.AddIssue(ref, forge.Issue{ID: "42", Title: "issue", Body: "body", Author: "alice", URL: "https://example.test/42"})
	completed := make(chan storage.CompleteOutcome, 1)
	workers := &daemon.Daemon{DB: db, Now: func() time.Time { return now }, Comments: []*forgeworker.CommentWorker{{
		DB: db, Client: client, ProjectID: "project", WorkerID: "test:comment", Lease: time.Minute, Now: func() time.Time { return now },
		Complete: func(ctx context.Context, claim storage.ClaimedOperation, outcome storage.CompleteOutcome) error {
			completed <- outcome
			return db.CompleteOutboxAttempt(ctx, claim, outcome)
		},
	}}}
	termination := &daemon.TerminationCoordinator{DB: db, Terminator: runtime.Terminator{}, Runtime: config.Runtime{}, Now: func() time.Time { return now }}
	startSchedulers(ctx, db, workers, termination, schedulerWithLongIntervals())
	// The long interval is deliberately much larger than the assertion window;
	// the only post-start edge available is DB's commit wakeup.
	if err := enqueue(ctx, db, productionWakeNow); err != nil {
		t.Fatal(err)
	}
	select {
	case outcome := <-completed:
		if outcome.State != storage.OperationSucceeded {
			t.Fatalf("production outbox worker outcome = %s, error=%s", outcome.State, outcome.ErrorSummary)
		}
	case <-time.After(750 * time.Millisecond):
		t.Fatal("production outbox worker did not run after commit wakeup")
	}
}

func schedulerWithLongIntervals() config.Scheduler {
	return config.Scheduler{IntakeIdleInterval: 10 * time.Second, IntakeActiveInterval: 10 * time.Second, IntakeInterruptInterval: 10 * time.Second, SupervisorInterval: 10 * time.Second}
}
