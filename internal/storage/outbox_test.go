package storage

import (
	"context"
	"errors"
	"testing"
)

func TestV2OutboxClaimLeaseBackoffAndStableKey(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	op := Operation{Key: CommentOperationKey("summary", "subject", 1), Kind: OperationForgeComment, Payload: []byte(`{"schema_version":1}`)}
	if _, err := db.EnqueueOperation(ctx, op, testNow); err != nil {
		t.Fatal(err)
	}
	// Same frozen effect is idempotent; a different payload under that key is a
	// contract violation rather than a silently replaced effect.
	if _, err := db.EnqueueOperation(ctx, op, testNow); err != nil {
		t.Fatal(err)
	}
	op.Payload = []byte(`{"schema_version":2}`)
	if _, err := db.EnqueueOperation(ctx, op, testNow); !errors.Is(err, ErrOperationConflict) {
		t.Fatalf("err=%v", err)
	}
	claim, err := db.ClaimOutboxOperation(ctx, "boot:worker", testNow, 10)
	if err != nil || claim == nil {
		t.Fatalf("claim=%+v err=%v", claim, err)
	}
	if err := db.CompleteOutboxAttempt(ctx, *claim, CompleteOutcome{State: OperationRetryable, ErrorClass: ErrorTransient, NowMS: testNow + 1, Backoff: BackoffPolicy{InitialDelayMS: 10, MaxDelayMS: 100, Multiplier: 2}}); err != nil {
		t.Fatal(err)
	}
	if got, err := db.ClaimOutboxOperation(ctx, "boot:worker", testNow+10, 10); err != nil || got != nil {
		t.Fatalf("early claim=%+v err=%v", got, err)
	}
	claim, err = db.ClaimOutboxOperation(ctx, "boot:worker", testNow+11, 10)
	if err != nil || claim == nil {
		t.Fatalf("retry claim=%+v err=%v", claim, err)
	}
	if err := db.CompleteOutboxAttempt(ctx, *claim, CompleteOutcome{State: OperationSucceeded, NowMS: testNow + 12}); err != nil {
		t.Fatal(err)
	}
}

func TestOutboxWakeupRunsOnlyAfterCommit(t *testing.T) {
	db, _ := openTestDB(t)
	woke := 0
	db.SetOutboxWakeup(func() { woke++ })
	_, err := db.EnqueueOperation(context.Background(), Operation{Key: "alert:test:x:1", Kind: OperationForgeAlert, Payload: []byte(`{}`)}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if woke != 1 {
		t.Fatalf("woke=%d", woke)
	}
}
