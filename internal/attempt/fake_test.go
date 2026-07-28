package attempt

import (
	"context"
	"errors"
	"testing"
	"time"
)

// FakeAgent honors the Runner contract the real Runtime (M3) will honor: Launch
// records start evidence from the injected clock, Result returns ErrNotFinished
// until Complete publishes the result, and the evidence carries the configured
// exit code and head SHA.
func TestFakeAgentContract(t *testing.T) {
	ctx := context.Background()
	now := int64(1_700_000_000_000)
	clock := func() time.Time { return time.UnixMilli(now) }
	a := NewFakeAgent(0, "headsha", "digest", WithFakeNow(clock))

	if _, err := a.Launch(ctx, Launch{RunID: "r1", AttemptNo: 1, AgentID: "fake"}); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	// Result is not available before Complete.
	if _, err := a.Result(ctx, "r1", 1); !errors.Is(err, ErrNotFinished) {
		t.Fatalf("Result before Complete err = %v, want ErrNotFinished", err)
	}
	now += 5_000
	r := a.Complete("r1", 1)
	if r.ExitCode == nil || *r.ExitCode != 0 || r.FinalHeadSHA != "headsha" || r.Digest != "digest" {
		t.Fatalf("Complete result = %+v", r)
	}
	got, err := a.Result(ctx, "r1", 1)
	if err != nil {
		t.Fatalf("Result after Complete: %v", err)
	}
	if got.FinalHeadSHA != r.FinalHeadSHA {
		t.Fatalf("Result drifted: %+v vs %+v", got, r)
	}
}

func TestFakeAgentRejectsIncompleteLaunch(t *testing.T) {
	ctx := context.Background()
	a := NewFakeAgent(0, "h", "d")
	if _, err := a.Launch(ctx, Launch{RunID: "", AttemptNo: 1}); !errors.Is(err, ErrNotFinished) {
		t.Fatalf("empty run Launch err = %v", err)
	}
	if _, err := a.Launch(ctx, Launch{RunID: "r", AttemptNo: 0}); !errors.Is(err, ErrNotFinished) {
		t.Fatalf("zero attempt Launch err = %v", err)
	}
}
