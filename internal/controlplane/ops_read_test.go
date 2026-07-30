package controlplane

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/miaoxiaoyong/sift/internal/storage"
)

// startServerWithDB opens a real database under a temp home, starts a server
// bound to it, and returns both for seeding + RPC assertions.
func startServerWithDB(t *testing.T) (*Server, *storage.DB) {
	t.Helper()
	home := testHome(t)
	dbPath := filepath.Join(home.Path, "sift.db")
	db, err := storage.Open(context.Background(), storage.OpenConfig{Path: dbPath, BinaryVersion: Version, Now: time.Now()})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s, err := Start(home, db)
	if err != nil {
		t.Fatalf("start server: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	s.SetAttentionQuota(map[string]int{"low": 5, "normal": 5, "high": 5})
	return s, db
}

const cpNow = 1_700_000_000_000

func seedCPRun(t *testing.T, db *storage.DB) {
	t.Helper()
	ctx := context.Background()
	if err := db.SeedProjectForTest(ctx, "cfg-cp", "proj-cp", cpNow); err != nil {
		t.Fatal(err)
	}
	if err := db.SeedLaunchRunForTest(ctx, "runCP", "proj-cp", "cfg-cp", cpNow, "/work"); err != nil {
		t.Fatal(err)
	}
}

// TestOpsPSReturnsRealRuns verifies ops.ps reads persisted Run/attempt rows
// instead of the placeholder empty list.
func TestOpsPSReturnsRealRuns(t *testing.T) {
	s, db := startServerWithDB(t)
	seedCPRun(t, db)
	resp := s.operatorRequest(Request{RequestID: "0123456789abcdef0123456789abcdef", Method: "ops.ps", Auth: Auth{Kind: "operator", Token: s.operatorToken}, Params: map[string]any{"run_id": nil, "project_id": nil, "status": nil, "limit": float64(100), "after_run_id": nil}})
	if !resp.OK {
		t.Fatalf("ops.ps = %#v", resp)
	}
	result := resp.Result.(map[string]any)
	runs := result["runs"].([]storage.PSRun)
	if len(runs) != 1 || runs[0].RunID != "runCP" {
		t.Fatalf("runs = %+v, want runCP", runs)
	}
	rem := result["attention_remaining"].(map[string]int)
	// No persisted attention bucket → configured ceiling 5 fully remaining.
	if rem["normal"] != 5 {
		t.Fatalf("normal remaining = %d, want 5", rem["normal"])
	}
}

// TestOpsMetricsCoversNineSeries verifies ops.metrics returns the full report
// and the trigger→started latency distribution without error on real data.
func TestOpsMetricsCoversNineSeries(t *testing.T) {
	s, db := startServerWithDB(t)
	seedCPRun(t, db)
	resp := s.operatorRequest(Request{RequestID: "0123456789abcdef0123456789abcdef", Method: "ops.metrics", Auth: Auth{Kind: "operator", Token: s.operatorToken}, Params: map[string]any{"project_id": nil}})
	if !resp.OK {
		t.Fatalf("ops.metrics = %#v", resp)
	}
	result := resp.Result.(map[string]any)
	metrics := result["metrics"].(storage.MetricsReport)
	if metrics.WeightedAttentionPerChange.Coverage == "" || metrics.FalseReleaseRate.Coverage == "" {
		t.Fatalf("metric coverage notes missing: %+v", metrics)
	}
	if _, ok := result["trigger_started_latency"]; !ok {
		t.Fatal("missing trigger_started_latency")
	}
}

// TestOpsTimelineReturnsPersistedEvents verifies ops.timeline reads the
// append-only event stream.
func TestOpsTimelineReturnsPersistedEvents(t *testing.T) {
	s, db := startServerWithDB(t)
	seedCPRun(t, db)
	ctx := context.Background()
	if _, err := db.AppendEvent(ctx, storage.EventCmd{RunID: "runCP", Type: "report.progress", Source: storage.SourceAgent, PayloadJSON: []byte("{}"), OccurredAtMS: cpNow, RecordedAtMS: cpNow}); err != nil {
		t.Fatal(err)
	}
	resp := s.operatorRequest(Request{RequestID: "0123456789abcdef0123456789abcdef", Method: "ops.timeline", Auth: Auth{Kind: "operator", Token: s.operatorToken}, Params: map[string]any{"run_id": "runCP", "project_id": nil, "type": nil, "after_seq": float64(0), "limit": float64(100)}})
	if !resp.OK {
		t.Fatalf("ops.timeline = %#v", resp)
	}
	report := resp.Result.(storage.TimelineReport)
	if len(report.Events) == 0 {
		t.Fatal("timeline returned no events")
	}
}

// TestOpsLogsReadsAgentLog verifies ops.logs reads the persisted agent.log file
// with a bounded base64 payload and EOF semantics.
func TestOpsLogsReadsAgentLog(t *testing.T) {
	s, db := startServerWithDB(t)
	seedCPRun(t, db)
	// SeedLaunchRunForTest creates attempts/1; write its agent.log.
	logDir := filepath.Join(s.Home.Path, "runs", "runCP", "attempts", "1")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logDir, "agent.log"), []byte("hello agent log\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	resp := s.operatorRequest(Request{RequestID: "0123456789abcdef0123456789abcdef", Method: "ops.logs", Auth: Auth{Kind: "operator", Token: s.operatorToken}, Params: map[string]any{"run_id": "runCP", "attempt_no": nil, "offset": float64(0), "limit": float64(262144)}})
	if !resp.OK {
		t.Fatalf("ops.logs = %#v", resp)
	}
	result := resp.Result.(map[string]any)
	if result["attempt_no"] != 1 {
		t.Fatalf("attempt_no = %v, want 1", result["attempt_no"])
	}
	if result["eof"] != true {
		t.Fatalf("eof = %v, want true", result["eof"])
	}
}

// TestOpsLogsNotFound verifies a missing log fails closed with not_found.
func TestOpsLogsNotFound(t *testing.T) {
	s, db := startServerWithDB(t)
	seedCPRun(t, db)
	resp := s.operatorRequest(Request{RequestID: "0123456789abcdef0123456789abcdef", Method: "ops.logs", Auth: Auth{Kind: "operator", Token: s.operatorToken}, Params: map[string]any{"run_id": "runCP", "attempt_no": float64(1), "offset": float64(0), "limit": float64(262144)}})
	if resp.OK || resp.Error.Code != "not_found" {
		t.Fatalf("ops.logs missing log = %#v, want not_found", resp)
	}
}

// TestOpsMetricsRejectsExtraParams verifies the closed param set.
func TestOpsMetricsRejectsExtraParams(t *testing.T) {
	s, _ := startServerWithDB(t)
	resp := s.operatorRequest(Request{RequestID: "0123456789abcdef0123456789abcdef", Method: "ops.metrics", Auth: Auth{Kind: "operator", Token: s.operatorToken}, Params: map[string]any{"project_id": nil, "bogus": 1}})
	if resp.OK || resp.Error.Code != "invalid_request" {
		t.Fatalf("ops.metrics extra param = %#v, want invalid_request", resp)
	}
}
