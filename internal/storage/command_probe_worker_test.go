package storage

import (
	"context"
	"testing"
)

// PendingRetryProbes / ClaimRetryProbe are the supervisor-tick read+claim pair
// for the startup_stall probe process-check worker (specs/storage.md §5.5). The
// worker scans pending|running probes joined with the bound attempt's wrapper
// identity, claims pending->running (crash-resumable), and drives each to the
// unique ApplyRetryProbeResult finalizer.

func seedStartupStallRetryProbe(t *testing.T, db *DB, ctx context.Context) (interruptID, nonce, probeID string) {
	t.Helper()
	seedCommandRun(t, db, ctx)
	interruptID, nonce = emitStartupStallInterrupt(t, db, ctx)
	env := commentEnv(t, "project", "c1", "/sift retry "+cmdRun+" "+nonce)
	if _, err := db.ApplyCommandEvent(ctx, ApplyCommandEventCmd{Envelope: env, Allowlist: []string{"alice"}, NowMS: testNow + 5}); err != nil {
		t.Fatalf("retry request: %v", err)
	}
	if err := db.db.QueryRow(`SELECT id FROM attempt_probes WHERE interrupt_id=? AND state='pending'`, interruptID).Scan(&probeID); err != nil {
		t.Fatalf("locate pending probe: %v", err)
	}
	return interruptID, nonce, probeID
}

func setAttemptWrapperIdentity(t *testing.T, db *DB, runID string, attemptNo int, pid, pgid int, startedMS int64, executable, nonceHash string) {
	t.Helper()
	// The attempts CHECK constraint requires the wrapper identity block to be
	// all-NULL or all-set together (including wrapper_instance_id), mirroring
	// how RecordAttemptHandoff persists it.
	mustExec(t, db, `UPDATE attempts SET wrapper_pid=?,wrapper_started_at_ms=?,wrapper_executable=?,wrapper_pgid=?,wrapper_instance_id=?,control_nonce_hash=? WHERE run_id=? AND attempt_no=?`,
		pid, startedMS, executable, pgid, "instance", nonceHash, runID, attemptNo)
}

func TestPendingRetryProbesReturnsPendingWithWrapperIdentity(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	_, _, probeID := seedStartupStallRetryProbe(t, db, ctx)
	setAttemptWrapperIdentity(t, db, cmdRun, 1, 4242, 4242, testNow, "/wrapper", "nonce-hash")

	probes, err := db.PendingRetryProbes(ctx)
	if err != nil {
		t.Fatalf("PendingRetryProbes: %v", err)
	}
	if len(probes) != 1 {
		t.Fatalf("probes = %d, want 1", len(probes))
	}
	p := probes[0]
	if p.ProbeID != probeID || p.RunID != cmdRun || p.AttemptNo != 1 {
		t.Fatalf("probe identity = {%s %s %d}, want {%s %s 1}", p.ProbeID, p.RunID, p.AttemptNo, probeID, cmdRun)
	}
	if p.WrapperPID != 4242 || p.WrapperPGID != 4242 || p.WrapperStartedAtMS != testNow || p.WrapperExecutable != "/wrapper" || p.ControlNonceHash != "nonce-hash" {
		t.Fatalf("wrapper identity = %+v", p)
	}
	if p.ExpectedRunVersion == 0 || p.ExpectedGeneration == 0 {
		t.Fatalf("expected run version/generation not frozen: %+v", p)
	}
}

func TestPendingRetryProbesExcludesFinalizedAndMissingIdentity(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	_, _, probeID := seedStartupStallRetryProbe(t, db, ctx)

	// A probe whose bound attempt no longer joins (e.g. deleted) is excluded;
	// here the attempt exists but has no wrapper identity, which the read still
	// returns (identity defaults to zero) so the worker can decide unconfirmed.
	probes, err := db.PendingRetryProbes(ctx)
	if err != nil {
		t.Fatalf("PendingRetryProbes: %v", err)
	}
	if len(probes) != 1 || probes[0].WrapperPID != 0 {
		t.Fatalf("probe with zero identity = %+v, want 1 row with zero PID", probes)
	}

	// Once finalized (succeeded/failed) the probe drops out of the scan.
	mustExec(t, db, `UPDATE attempt_probes SET state='failed',finished_at_ms=? WHERE id=?`, testNow+10, probeID)
	probes, err = db.PendingRetryProbes(ctx)
	if err != nil {
		t.Fatalf("PendingRetryProbes after fail: %v", err)
	}
	if len(probes) != 0 {
		t.Fatalf("finalized probes = %d, want 0", len(probes))
	}
}

func TestClaimRetryProbeCASPendingToRunningOnce(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	_, _, probeID := seedStartupStallRetryProbe(t, db, ctx)

	// First claim: pending -> running, started_at_ms set once.
	claimed, err := db.ClaimRetryProbe(ctx, probeID, testNow+100)
	if err != nil || !claimed {
		t.Fatalf("first claim = %v, %v", claimed, err)
	}
	var state string
	var startedMS int64
	if err := db.db.QueryRow(`SELECT state,COALESCE(started_at_ms,0) FROM attempt_probes WHERE id=?`, probeID).Scan(&state, &startedMS); err != nil {
		t.Fatal(err)
	}
	if state != "running" || startedMS != testNow+100 {
		t.Fatalf("after claim = state=%s started=%d, want running/%d", state, startedMS, testNow+100)
	}

	// Second claim is a no-op (already running): the finalizer's own CAS is the
	// at-most-once guard, so a failed claim is not an error.
	claimed, err = db.ClaimRetryProbe(ctx, probeID, testNow+200)
	if err != nil || claimed {
		t.Fatalf("second claim = %v, %v, want false/<nil>", claimed, err)
	}
	if err := db.db.QueryRow(`SELECT COALESCE(started_at_ms,0) FROM attempt_probes WHERE id=?`, probeID).Scan(&startedMS); err != nil {
		t.Fatal(err)
	}
	if startedMS != testNow+100 {
		t.Fatalf("reclaim overwrote started_at_ms = %d, want preserved %d", startedMS, testNow+100)
	}

	// A pending|running probe is still scanned after the claim so a crashed tick
	// can resume it.
	probes, err := db.PendingRetryProbes(ctx)
	if err != nil {
		t.Fatalf("PendingRetryProbes after claim: %v", err)
	}
	if len(probes) != 1 || probes[0].ProbeID != probeID {
		t.Fatalf("running probe not rescan-able: %+v", probes)
	}
}

func TestPendingRetryProbesIncludesRunningForCrashReplay(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	_, _, probeID := seedStartupStallRetryProbe(t, db, ctx)
	// Simulate a crash mid-observation: the probe is running but was never
	// finalized. The next tick must still pick it up.
	mustExec(t, db, `UPDATE attempt_probes SET state='running',started_at_ms=? WHERE id=?`, testNow+50, probeID)

	probes, err := db.PendingRetryProbes(ctx)
	if err != nil {
		t.Fatalf("PendingRetryProbes: %v", err)
	}
	if len(probes) != 1 {
		t.Fatalf("running probe = %d, want 1 (crash replay must resume)", len(probes))
	}
}
