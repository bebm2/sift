package forge

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// Forge adapter charging tests (forge.md §9). The adapter is the single
// charging point: every CLI subprocess charges once before launching, a
// budget refusal surfaces as ErrRateLimited without running the CLI, and
// charge keys are stable per caller-supplied base so crash replay is
// idempotent. Without a charger (fake/no-budget) nothing changes.

// recordingCharger records charge keys in order and optionally rejects after
// a budget is reached.
type recordingCharger struct {
	mu          sync.Mutex
	keys        []string
	exhaustFrom int // charge at/after this sequence returns Exhausted (0 = never)
}

func (r *recordingCharger) Charge(_ context.Context, _ ProjectRef, key string) (ChargeResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.keys = append(r.keys, key)
	if r.exhaustFrom > 0 && len(r.keys) >= r.exhaustFrom {
		return ChargeResult{Exhausted: true}, nil
	}
	return ChargeResult{Charged: true, Consumed: int64(len(r.keys)), Limit: 100}, nil
}

func ghProject() ProjectRef {
	return ProjectRef{Kind: KindGitHub, Host: "github.com", ProjectKey: "o/r"}
}

// cannedRunner returns a valid single-issue JSON for any GET and an empty
// object for non-GET; it counts subprocess launches.
func cannedRunner(launched *int) Runner {
	return func(_ context.Context, _ string, _ []string, _ []byte) ([]byte, []byte, error) {
		*launched++
		return []byte(`{"number":1,"title":"t","body":"b","html_url":"https://x/1","state":"open","user":{"login":"a"},"labels":[{"name":"sift"}]}`), nil, nil
	}
}

func TestChargeNoChargerIsNoOp(t *testing.T) {
	launched := 0
	a := NewGitHub("gh", cannedRunner(&launched))
	// No charger: charging never engages, even with a charge key present.
	ctx := WithChargeKey(context.Background(), "forge-call:att-1")
	if _, err := a.GetIssue(ctx, ghProject(), "1"); err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if launched != 1 {
		t.Fatalf("launched = %d, want 1", launched)
	}
}

func TestChargeOncePerSubprocessWithStableSeq(t *testing.T) {
	launched := 0
	ch := &recordingCharger{}
	a := NewGitHub("gh", cannedRunner(&launched)).WithCharger(ch)
	ctx := WithChargeKey(context.Background(), "forge-call:att-42")
	if _, err := a.GetIssue(ctx, ghProject(), "1"); err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if _, err := a.GetIssue(ctx, ghProject(), "1"); err != nil {
		t.Fatalf("GetIssue 2: %v", err)
	}
	// Two distinct keys, incrementing seq, stable base.
	want := []string{"forge-call:att-42:1", "forge-call:att-42:2"}
	if len(ch.keys) != 2 || ch.keys[0] != want[0] || ch.keys[1] != want[1] {
		t.Fatalf("keys = %v, want %v", ch.keys, want)
	}
	if launched != 2 {
		t.Fatalf("launched = %d, want 2", launched)
	}
}

func TestChargeBasesAreIndependent(t *testing.T) {
	launched := 0
	ch := &recordingCharger{}
	a := NewGitHub("gh", cannedRunner(&launched)).WithCharger(ch)
	for _, base := range []string{"forge-call:att-A", "forge-call:att-B", "forge-call:att-A"} {
		if _, err := a.GetIssue(WithChargeKey(context.Background(), base), ghProject(), "1"); err != nil {
			t.Fatalf("GetIssue %s: %v", base, err)
		}
	}
	// Each base restarts its own sequence; att-A advances 1→2 on revisit.
	want := []string{"forge-call:att-A:1", "forge-call:att-B:1", "forge-call:att-A:2"}
	if len(ch.keys) != 3 || ch.keys[0] != want[0] || ch.keys[1] != want[1] || ch.keys[2] != want[2] {
		t.Fatalf("keys = %v, want %v", ch.keys, want)
	}
}

func TestChargeNoBaseIsNoOp(t *testing.T) {
	launched := 0
	ch := &recordingCharger{}
	a := NewGitHub("gh", cannedRunner(&launched)).WithCharger(ch)
	// Charger present but caller did not opt into charging for this op: no
	// charge, call still proceeds.
	if _, err := a.GetIssue(context.Background(), ghProject(), "1"); err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if len(ch.keys) != 0 {
		t.Fatalf("charged without base: %v", ch.keys)
	}
	if launched != 1 {
		t.Fatalf("launched = %d, want 1", launched)
	}
}

func TestChargeExhaustedRejectsWithoutRunningCLI(t *testing.T) {
	launched := 0
	ch := &recordingCharger{exhaustFrom: 1} // the very first charge is exhausted
	a := NewGitHub("gh", cannedRunner(&launched)).WithCharger(ch)
	ctx := WithChargeKey(context.Background(), "forge-call:att-1")
	_, err := a.GetIssue(ctx, ghProject(), "1")
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
	// The CLI subprocess must not launch when the budget is exhausted.
	if launched != 0 {
		t.Fatalf("launched = %d, want 0 (CLI must not run on budget refusal)", launched)
	}
	if len(ch.keys) != 1 {
		t.Fatalf("charger invoked %d times, want 1", len(ch.keys))
	}
}

func TestChargePerPaginationPage(t *testing.T) {
	pages := 0
	run := func(_ context.Context, _ string, args []string, _ []byte) ([]byte, []byte, error) {
		pages++
		if pages < 2 {
			rows := make([]string, 100)
			for i := range rows {
				rows[i] = `{"number":1,"title":"t","body":"b","html_url":"https://x/1","state":"open","user":{"login":"a"},"labels":[{"name":"sift"}]}`
			}
			return []byte("[" + strings.Join(rows, ",") + "]"), nil, nil
		}
		return []byte(`[]`), nil, nil
	}
	ch := &recordingCharger{}
	a := NewGitHub("gh", run).WithCharger(ch)
	ctx := WithChargeKey(context.Background(), "intake:tick-7:proj-1")
	if _, _, err := a.ListIssuesByLabel(ctx, ghProject(), "sift", ""); err != nil {
		t.Fatalf("ListIssuesByLabel: %v", err)
	}
	// One charge per page (forge.md §9: no --paginate hiding request count).
	if pages != 2 || len(ch.keys) != 2 {
		t.Fatalf("pages = %d charges = %v, want 2/2", pages, ch.keys)
	}
	if ch.keys[0] != "intake:tick-7:proj-1:1" || ch.keys[1] != "intake:tick-7:proj-1:2" {
		t.Fatalf("charge keys = %v", ch.keys)
	}
}

func TestChargeGetChangeDiffGitHubRawRun(t *testing.T) {
	launched := 0
	run := func(_ context.Context, _ string, _ []string, _ []byte) ([]byte, []byte, error) {
		launched++
		return []byte("diff --git a/x b/x\n"), nil, nil
	}
	ch := &recordingCharger{}
	a := NewGitHub("gh", run).WithCharger(ch)
	ctx := WithChargeKey(context.Background(), "forge-call:att-9")
	if _, err := a.GetChangeDiff(ctx, ghProject(), "1"); err != nil {
		t.Fatalf("GetChangeDiff: %v", err)
	}
	// The GitHub diff path calls a.Run directly, bypassing call(); it must
	// still charge exactly once.
	if len(ch.keys) != 1 || ch.keys[0] != "forge-call:att-9:1" {
		t.Fatalf("charge keys = %v, want [forge-call:att-9:1]", ch.keys)
	}
	if launched != 1 {
		t.Fatalf("launched = %d, want 1", launched)
	}
}

func TestChargeCrashReplayIdempotentKey(t *testing.T) {
	// A crash between charge and commit leaves no budget entry; on restart the
	// same logical operation re-runs from a fresh in-memory sequence counter,
	// so it re-derives the identical charge key and the storage port dedupes.
	run := cannedRunner(new(int))
	ch1 := &recordingCharger{}
	a1 := NewGitHub("gh", run).WithCharger(ch1)
	ctx := WithChargeKey(context.Background(), "forge-call:att-5")
	if _, err := a1.GetIssue(ctx, ghProject(), "1"); err != nil {
		t.Fatalf("first process: %v", err)
	}

	ch2 := &recordingCharger{}
	a2 := NewGitHub("gh", cannedRunner(new(int))).WithCharger(ch2)
	if _, err := a2.GetIssue(ctx, ghProject(), "1"); err != nil {
		t.Fatalf("restarted process: %v", err)
	}
	if ch1.keys[0] != "forge-call:att-5:1" || ch2.keys[0] != "forge-call:att-5:1" {
		t.Fatalf("replay keys differ: %q vs %q (both must be :1)", ch1.keys[0], ch2.keys[0])
	}
}
