package storage

import (
	"context"
	"testing"
	"time"
)

func TestOutboxCommitWakeupClaimsWithoutPeriodicTick(t *testing.T) {
	db, _ := openTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	claimed := make(chan *ClaimedOperation, 1)
	scheduler := NewOutboxScheduler(func(ctx context.Context) error {
		claim, err := db.ClaimOutboxOperation(ctx, "test:outbox", testNow, 1000)
		if err != nil {
			return err
		}
		if claim != nil {
			claimed <- claim
		}
		return nil
	})
	db.SetOutboxWakeup(scheduler.Wake)
	done := make(chan error, 1)
	go func() { done <- scheduler.Run(ctx) }()

	if _, err := db.EnqueueOperation(ctx, Operation{Key: "alert:wakeup:subject:1", Kind: OperationForgeAlert, Payload: []byte(`{}`)}, testNow); err != nil {
		t.Fatal(err)
	}
	select {
	case claim := <-claimed:
		if claim.Key != "alert:wakeup:subject:1" {
			t.Fatalf("claimed key = %q", claim.Key)
		}
	case <-time.After(time.Second):
		t.Fatal("outbox was not claimed from the post-commit wakeup")
	}
	cancel()
	if err := <-done; err != context.Canceled {
		t.Fatalf("scheduler exit = %v", err)
	}
}
