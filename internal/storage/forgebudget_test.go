package storage

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// Forge API budget charging port tests (specs/forge.md §9, storage.md §9.1/§11).
// Covers: increment + entry, CAS rejection at the limit without recording,
// idempotent replay, hour-bucket boundaries, persistence across restart,
// warning-ratio slow-poll signal + single alert, multi-project independence,
// and the no-charge status read Intake consults before polling.

const (
	// 2023-11-14 22:00:00 UTC — an exact UTC hour boundary so the bucket
	// math in these tests is unambiguous.
	forgeHourStart = int64(1_699_999_200_000)
	forgeHourEnd   = forgeHourStart + 60*60*1000 - 1 // last ms of the same hour
	forgeNextHour  = forgeHourStart + 60*60*1000     // first ms of next bucket
)

func chargeOK(t *testing.T, db *DB, project, key string, nowMS, limit int64, ratio float64) ChargeForgeAPICallResult {
	t.Helper()
	res, err := db.ChargeForgeAPICall(context.Background(), ChargeForgeAPICallCmd{
		ProjectID: project, CallAttemptKey: key, NowMS: nowMS, Limit: limit, WarningRatio: ratio,
	})
	if err != nil {
		t.Fatalf("ChargeForgeAPICall %s: %v", key, err)
	}
	return res
}

func TestForgeAPIChargeIncrementsAndPersists(t *testing.T) {
	db, path := openTestDB(t)
	ctx := context.Background()
	insertConfigSnapshot(t, db, "cfg-1")
	insertProject(t, db, "proj-1", "cfg-1")

	// Two distinct charge keys in the same hour bucket accumulate.
	r1 := chargeOK(t, db, "proj-1", "forge-call:att-1:1", forgeHourStart, 1000, 0.8)
	r2 := chargeOK(t, db, "proj-1", "forge-call:att-1:2", forgeHourStart+5, 1000, 0.8)
	if r1.Consumed != 1 || r2.Consumed != 2 || !r1.Charged || !r2.Charged {
		t.Fatalf("charges = %+v, %+v; want consumed 1,2 both charged", r1, r2)
	}
	if r1.BucketStartMS != r2.BucketStartMS {
		t.Fatalf("bucket start drifted: %d vs %d", r1.BucketStartMS, r2.BucketStartMS)
	}

	// Counter reflects the accumulated charge.
	var consumed int64
	if err := db.db.QueryRow(`SELECT consumed_value FROM budget_counters
		WHERE kind='forge_api' AND scope='project' AND scope_id='proj-1' AND bucket_start_ms=?`,
		forgeHourStart).Scan(&consumed); err != nil {
		t.Fatal(err)
	}
	if consumed != 2 {
		t.Fatalf("counter consumed = %d, want 2", consumed)
	}
	var entries int
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM budget_entries WHERE kind='forge_api' AND scope_id='proj-1'`).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if entries != 2 {
		t.Fatalf("budget entries = %d, want 2", entries)
	}

	// Restart recovery: a fresh handle on the same file resumes the same
	// bucket instead of recounting (forge.md §9: 重启恢复同一桶).
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db2, err := Open(ctx, OpenConfig{
		Path:          path,
		BinaryVersion: "test-binary",
		Now:           time.UnixMilli(forgeHourStart),
	})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = db2.Close() })
	r3 := chargeOK(t, db2, "proj-1", "forge-call:att-1:3", forgeHourStart+10, 1000, 0.8)
	if r3.Consumed != 3 {
		t.Fatalf("after restart consumed = %d, want 3 (same bucket)", r3.Consumed)
	}
}

func TestForgeAPIChargeIdempotentReplay(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	insertConfigSnapshot(t, db, "cfg-1")
	insertProject(t, db, "proj-1", "cfg-1")

	first := chargeOK(t, db, "proj-1", "forge-call:att-1:1", forgeHourStart, 1000, 0.8)
	replay, err := db.ChargeForgeAPICall(ctx, ChargeForgeAPICallCmd{
		ProjectID: "proj-1", CallAttemptKey: "forge-call:att-1:1",
		NowMS: forgeHourStart, Limit: 1000, WarningRatio: 0.8,
	})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	// Replayed key: no double billing, Charged=false, consumption unchanged.
	if replay.Charged {
		t.Fatalf("replay Charged = true, want false")
	}
	if replay.Consumed != first.Consumed {
		t.Fatalf("replay consumed = %d, want %d", replay.Consumed, first.Consumed)
	}
	var entries int
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM budget_entries WHERE operation_key='forge-call:att-1:1'`).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if entries != 1 {
		t.Fatalf("entries for replayed key = %d, want 1", entries)
	}
}

func TestForgeAPIChargeCASRejectsAtLimit(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	insertConfigSnapshot(t, db, "cfg-1")
	insertProject(t, db, "proj-1", "cfg-1")

	// Limit of 2: two charges fill the bucket; the third is rejected.
	chargeOK(t, db, "proj-1", "k1", forgeHourStart, 2, 0.8)
	chargeOK(t, db, "proj-1", "k2", forgeHourStart, 2, 0.8)
	_, err := db.ChargeForgeAPICall(ctx, ChargeForgeAPICallCmd{
		ProjectID: "proj-1", CallAttemptKey: "k3", NowMS: forgeHourStart, Limit: 2, WarningRatio: 0.8,
	})
	if !errors.Is(err, ErrForgeBudgetExhausted) {
		t.Fatalf("third charge err = %v, want ErrForgeBudgetExhausted", err)
	}

	// A rejected charge records nothing: counter stays at the limit and no
	// entry exists for the rejected key.
	var consumed int64
	if err := db.db.QueryRow(`SELECT consumed_value FROM budget_counters
		WHERE kind='forge_api' AND scope_id='proj-1' AND bucket_start_ms=?`, forgeHourStart).Scan(&consumed); err != nil {
		t.Fatal(err)
	}
	if consumed != 2 {
		t.Fatalf("consumed after rejected charge = %d, want 2", consumed)
	}
	var entries int
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM budget_entries WHERE operation_key='k3'`).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if entries != 0 {
		t.Fatalf("rejected charge wrote %d entries, want 0", entries)
	}

	// The rejected key can still be retried once the bucket resets.
	r4 := chargeOK(t, db, "proj-1", "k3", forgeNextHour, 2, 0.8)
	if r4.Consumed != 1 || r4.BucketStartMS != forgeNextHour {
		t.Fatalf("post-reset charge = %+v, want consumed 1 in new bucket", r4)
	}
}

func TestForgeAPIChargeHourBucketBoundaries(t *testing.T) {
	db, _ := openTestDB(t)
	insertConfigSnapshot(t, db, "cfg-1")
	insertProject(t, db, "proj-1", "cfg-1")

	last := chargeOK(t, db, "proj-1", "k-last", forgeHourEnd, 1000, 0.8)
	first := chargeOK(t, db, "proj-1", "k-first", forgeNextHour, 1000, 0.8)
	if last.BucketStartMS == first.BucketStartMS {
		t.Fatalf("last-ms and first-ms-of-next-hour share a bucket: %d", last.BucketStartMS)
	}
	if first.Consumed != 1 {
		t.Fatalf("new hour consumed = %d, want 1", first.Consumed)
	}
	// Status read in each bucket is isolated.
	sOld, err := db.ForgeAPIBudgetStatus(context.Background(), "proj-1", forgeHourEnd, 1000, 0.8)
	if err != nil {
		t.Fatal(err)
	}
	sNew, err := db.ForgeAPIBudgetStatus(context.Background(), "proj-1", forgeNextHour, 1000, 0.8)
	if err != nil {
		t.Fatal(err)
	}
	if sOld.Consumed != 1 || sNew.Consumed != 1 {
		t.Fatalf("status old=%d new=%d, want 1,1 (independent buckets)", sOld.Consumed, sNew.Consumed)
	}
}

func TestForgeAPIChargeWarningRatioAlertAndSlowPoll(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	insertConfigSnapshot(t, db, "cfg-1")
	insertProject(t, db, "proj-1", "cfg-1")

	// Limit 10, warning at 80% -> threshold is 8. The 8th charge crosses it.
	for i := 1; i <= 7; i++ {
		r := chargeOK(t, db, "proj-1", fmt.Sprintf("k%d", i), forgeHourStart, 10, 0.8)
		if r.SlowPoll {
			t.Fatalf("charge %d flagged slow-poll before threshold", i)
		}
	}
	cross := chargeOK(t, db, "proj-1", "k8", forgeHourStart, 10, 0.8)
	if !cross.SlowPoll {
		t.Fatalf("charge at threshold: SlowPoll=false, want true (consumed=%d)", cross.Consumed)
	}

	// Exactly one forge_alert for the bucket, keyed by project+hour.
	var alertKey string
	if err := db.db.QueryRow(`SELECT operation_key FROM outbox_operations WHERE kind='forge_alert'`).Scan(&alertKey); err != nil {
		t.Fatalf("alert operation missing: %v", err)
	}
	wantKey := AlertOperationKey(ForgeAPIBudgetAlertPurpose, fmt.Sprintf("project:proj-1:%d", forgeHourStart), 1)
	if alertKey != wantKey {
		t.Fatalf("alert key = %q, want %q", alertKey, wantKey)
	}

	// Further crossings in the same bucket do not create a second alert.
	for i := 9; i <= 10; i++ {
		chargeOK(t, db, "proj-1", fmt.Sprintf("k%d", i), forgeHourStart, 10, 0.8)
	}
	var alerts int
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM outbox_operations WHERE kind='forge_alert'`).Scan(&alerts); err != nil {
		t.Fatal(err)
	}
	if alerts != 1 {
		t.Fatalf("alerts = %d, want 1 per bucket", alerts)
	}

	// Status read agrees with the charge result.
	s, err := db.ForgeAPIBudgetStatus(ctx, "proj-1", forgeHourStart, 10, 0.8)
	if err != nil {
		t.Fatal(err)
	}
	if !s.SlowPoll || !s.Exhausted {
		t.Fatalf("status = %+v, want slow+exhausted", s)
	}
}

func TestForgeAPIChargeProjectsAreIndependent(t *testing.T) {
	db, _ := openTestDB(t)
	insertConfigSnapshot(t, db, "cfg-1")
	insertProject(t, db, "proj-1", "cfg-1")
	insertProject(t, db, "proj-2", "cfg-1")

	// proj-1 exhausts its small bucket; proj-2 is unaffected.
	chargeOK(t, db, "proj-1", "p1-a", forgeHourStart, 1, 0.8)
	_, err := db.ChargeForgeAPICall(context.Background(), ChargeForgeAPICallCmd{
		ProjectID: "proj-1", CallAttemptKey: "p1-b", NowMS: forgeHourStart, Limit: 1, WarningRatio: 0.8,
	})
	if !errors.Is(err, ErrForgeBudgetExhausted) {
		t.Fatalf("proj-1 second charge err = %v, want exhausted", err)
	}
	r := chargeOK(t, db, "proj-2", "p2-a", forgeHourStart, 1, 0.8)
	if r.Consumed != 1 {
		t.Fatalf("proj-2 charge = %+v, want consumed 1 (healthy project proceeds)", r)
	}
}

func TestForgeAPIChargeGuardInputs(t *testing.T) {
	db, _ := openTestDB(t)
	cases := []ChargeForgeAPICallCmd{
		{CallAttemptKey: "k", NowMS: forgeHourStart, Limit: 10, WarningRatio: 0.8},
		{ProjectID: "p", NowMS: forgeHourStart, Limit: 10, WarningRatio: 0.8},
		{ProjectID: "p", CallAttemptKey: "k", Limit: 10, WarningRatio: 0.8},
		{ProjectID: "p", CallAttemptKey: "k", NowMS: forgeHourStart, WarningRatio: 0.8},
		{ProjectID: "p", CallAttemptKey: "k", NowMS: forgeHourStart, Limit: 0, WarningRatio: 0.8},
		{ProjectID: "p", CallAttemptKey: "k", NowMS: forgeHourStart, Limit: 10, WarningRatio: 0},
		{ProjectID: "p", CallAttemptKey: "k", NowMS: forgeHourStart, Limit: 10, WarningRatio: 1},
	}
	for i, c := range cases {
		if _, err := db.ChargeForgeAPICall(context.Background(), c); err == nil {
			t.Fatalf("case %d: expected error, got nil", i)
		}
	}
}

func TestForgeAPIBucketStartMSUTC(t *testing.T) {
	cases := []struct {
		in, want int64
	}{
		{forgeHourStart, forgeHourStart},
		{forgeHourStart + 1, forgeHourStart},
		{forgeHourEnd, forgeHourStart},
		{forgeNextHour, forgeNextHour},
	}
	for _, c := range cases {
		if got := ForgeAPIBucketStartMS(c.in); got != c.want {
			t.Errorf("ForgeAPIBucketStartMS(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestProjectIDByForge(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	insertConfigSnapshot(t, db, "cfg-1")
	insertProject(t, db, "proj-1", "cfg-1")

	id, err := db.ProjectIDByForge(ctx, "github", "github.com", "org/repo-proj-1")
	if err != nil {
		t.Fatalf("ProjectIDByForge: %v", err)
	}
	if id != "proj-1" {
		t.Fatalf("id = %q, want proj-1", id)
	}
	if _, err := db.ProjectIDByForge(ctx, "github", "github.com", "org/missing"); err == nil {
		t.Fatal("missing project returned no error")
	}
}
