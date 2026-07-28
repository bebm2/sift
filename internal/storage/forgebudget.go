package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// Forge API budget charging port (specs/forge.md §9, specs/storage.md §9.1/§11).
//
// API calls are charged once per remote HTTP request at the Forge adapter
// layer and nowhere else (DESIGN §9.2: a single charging port per budget
// kind). The counter uses a fixed UTC hour bucket scoped per project so a
// project approaching its own limit slows down without starving healthy
// projects (DESIGN §11, forge.md invariant 9). Restart recovers the same
// bucket: the counter row is the source of truth, never in-memory state.
//
// Charging is the general CAS (consumed + amount <= limit), not the token
// post-charge exception: a request that would cross the limit is rejected
// before the CLI is launched, surfacing as ErrForgeBudgetExhausted. The
// caller (Forge adapter) maps that to forge.ErrRateLimited. Idempotency is
// anchored on the unique operation_key (the stable charge key): a replay of
// an already-committed charge returns the original consumption without
// billing a second time.

const (
	forgeAPIBudgetKind  = "forge_api"
	forgeAPIBudgetScope = "project"

	// ForgeAPIBucketHourMS is the span of one UTC fixed-hour bucket.
	ForgeAPIBucketHourMS int64 = 60 * 60 * 1000

	// ForgeAPIBudgetAlertPurpose is the forge_alert purpose emitted when a
	// project's hourly consumption reaches the configured warning ratio
	// (forge.md §9). One alert per (project, hour bucket), deduped by the
	// stable outbox operation key.
	ForgeAPIBudgetAlertPurpose = "forge_api_budget_warning"
)

// ErrForgeBudgetExhausted is returned when a charge would push consumption
// past the hourly limit. The charge is not recorded; the caller must not
// launch the Forge CLI call.
var ErrForgeBudgetExhausted = errors.New("storage: forge api budget exhausted")

// ForgeAPIBucketStartMS is the UTC fixed-hour bucket start for a wall-clock
// millisecond timestamp (forge.md §9: fixed hour bucket, no sliding window).
func ForgeAPIBucketStartMS(nowMS int64) int64 {
	return nowMS - mod(nowMS, ForgeAPIBucketHourMS)
}

// ChargeForgeAPICallCmd carries one Forge API charge attempt.
type ChargeForgeAPICallCmd struct {
	// ProjectID is the Sift-internal project id (scope_id of the per-project
	// hour bucket).
	ProjectID string
	// CallAttemptKey is the stable, unique idempotency key (budget_entries
	// operation_key): outbox uses forge-call:<attempt_id>:<seq>; Intake uses
	// an equally stable tick/project/call-seq key (forge.md §9).
	CallAttemptKey string
	// NowMS is the caller-injected wall-clock millisecond timestamp used to
	// resolve the hour bucket. Storage never reads the system clock
	// (storage.md §1 invariant 7).
	NowMS int64
	// Limit is the hourly_api_limit in effect (config.md §3.8). It freezes
	// into the counter row at first charge in a bucket; later charges in the
	// same bucket use the frozen value.
	Limit int64
	// WarningRatio is the (0,1) ratio at which Intake slows down and a
	// single alert is emitted (config.md §3.8, forge.md §9).
	WarningRatio float64
}

// ChargeForgeAPICallResult reports the counter state after the charge.
type ChargeForgeAPICallResult struct {
	BucketStartMS int64
	Consumed      int64
	Limit         int64
	// Charged is false when the operation key already had a committed charge
	// (idempotent replay, no double billing).
	Charged bool
	// SlowPoll is true when consumption has reached the warning ratio; Intake
	// should drop to the configured slow_poll_interval.
	SlowPoll bool
	// Exhausted is true when consumption has reached the limit.
	Exhausted bool
}

// ChargeForgeAPICall records one Forge API charge in the per-project hour
// bucket (specs/forge.md §9). It is the sole charging port for the forge API
// budget. A charge that would exceed the limit is rejected without recording;
// a replay of a committed charge is idempotent. Crossing the warning ratio
// emits at most one forge_alert per (project, hour bucket).
func (d *DB) ChargeForgeAPICall(ctx context.Context, cmd ChargeForgeAPICallCmd) (ChargeForgeAPICallResult, error) {
	if cmd.ProjectID == "" {
		return ChargeForgeAPICallResult{}, errors.New("storage: forge api charge requires project id")
	}
	if cmd.CallAttemptKey == "" {
		return ChargeForgeAPICallResult{}, errors.New("storage: forge api charge requires a call attempt key")
	}
	if cmd.NowMS <= 0 {
		return ChargeForgeAPICallResult{}, errors.New("storage: forge api charge requires a timestamp")
	}
	if cmd.Limit < 1 {
		return ChargeForgeAPICallResult{}, fmt.Errorf("storage: forge api limit must be >= 1, got %d", cmd.Limit)
	}
	if cmd.WarningRatio <= 0 || cmd.WarningRatio >= 1 {
		return ChargeForgeAPICallResult{}, fmt.Errorf("storage: forge api warning ratio must be in (0,1), got %v", cmd.WarningRatio)
	}

	bucketStart := ForgeAPIBucketStartMS(cmd.NowMS)
	bucketEnd := bucketStart + ForgeAPIBucketHourMS

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return ChargeForgeAPICallResult{}, err
	}
	defer tx.Rollback()

	// Idempotent replay: a committed charge for this key is returned as-is,
	// without billing again (forge.md §9: replay must not double-charge).
	var existingAmount int64
	scanErr := tx.QueryRowContext(ctx,
		`SELECT amount FROM budget_entries WHERE operation_key=?`, cmd.CallAttemptKey).Scan(&existingAmount)
	if scanErr == nil {
		consumed, err := forgeAPIConsumedTx(ctx, tx, cmd.ProjectID, bucketStart)
		if err != nil {
			return ChargeForgeAPICallResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return ChargeForgeAPICallResult{}, err
		}
		return chargeResult(bucketStart, consumed, cmd.Limit, cmd.WarningRatio, false), nil
	}
	if !errors.Is(scanErr, sql.ErrNoRows) {
		return ChargeForgeAPICallResult{}, scanErr
	}

	// Ensure the counter row exists with the frozen limit. ON CONFLICT DO
	// NOTHING preserves the limit frozen at first charge in this bucket.
	if _, err := tx.ExecContext(ctx, `INSERT INTO budget_counters
		(kind, scope, scope_id, bucket_start_ms, bucket_end_ms, limit_value, consumed_value, version, updated_at_ms)
		VALUES (?, ?, ?, ?, ?, ?, 0, 1, ?)
		ON CONFLICT (kind, scope, scope_id, bucket_start_ms) DO NOTHING`,
		forgeAPIBudgetKind, forgeAPIBudgetScope, cmd.ProjectID, bucketStart, bucketEnd, cmd.Limit, cmd.NowMS); err != nil {
		return ChargeForgeAPICallResult{}, fmt.Errorf("storage: ensure forge api counter: %w", err)
	}

	// General CAS: consumed + 1 <= limit. Rejected charges record nothing.
	res, err := tx.ExecContext(ctx, `UPDATE budget_counters
		SET consumed_value=consumed_value+1, version=version+1, updated_at_ms=?
		WHERE kind=? AND scope=? AND scope_id=? AND bucket_start_ms=?
		  AND consumed_value + 1 <= limit_value`,
		cmd.NowMS, forgeAPIBudgetKind, forgeAPIBudgetScope, cmd.ProjectID, bucketStart)
	if err != nil {
		return ChargeForgeAPICallResult{}, fmt.Errorf("storage: charge forge api counter: %w", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		// The row exists (just ensured) but the CAS guard failed: exhausted.
		return ChargeForgeAPICallResult{}, ErrForgeBudgetExhausted
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO budget_entries
		(id, kind, scope, scope_id, bucket_start_ms, amount, reason, run_id, operation_key, created_at_ms)
		VALUES (?, ?, ?, ?, ?, 1, 'forge api call', NULL, ?, ?)`,
		newID(), forgeAPIBudgetKind, forgeAPIBudgetScope, cmd.ProjectID, bucketStart, cmd.CallAttemptKey, cmd.NowMS); err != nil {
		return ChargeForgeAPICallResult{}, fmt.Errorf("storage: insert forge api budget entry: %w", err)
	}

	consumed, err := forgeAPIConsumedTx(ctx, tx, cmd.ProjectID, bucketStart)
	if err != nil {
		return ChargeForgeAPICallResult{}, err
	}
	result := chargeResult(bucketStart, consumed, cmd.Limit, cmd.WarningRatio, true)

	if result.SlowPoll {
		if err := forgeAPIEmitWarningAlert(ctx, tx, cmd.ProjectID, bucketStart, cmd.NowMS); err != nil {
			return ChargeForgeAPICallResult{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return ChargeForgeAPICallResult{}, fmt.Errorf("storage: commit forge api charge: %w", err)
	}
	if result.SlowPoll {
		// The alert is a new outbox operation only when the stable key has
		// not been used this bucket; wake so the worker picks it up promptly.
		d.wakeOutbox()
	}
	return result, nil
}

// ForgeAPIBudgetStatus is the read Intake consults to decide slow-poll without
// charging (forge.md §9: "接近上限" lowers Intake to the slow interval). With
// no charges yet in the current hour the bucket is empty and not throttled.
type ForgeAPIBudgetStatus struct {
	BucketStartMS int64
	Consumed      int64
	Limit         int64
	SlowPoll      bool
	Exhausted     bool
}

// ForgeAPIBudgetStatus reads the current hour bucket for a project. Limit and
// WarningRatio resolve the throttle flags consistently with ChargeForgeAPICall.
func (d *DB) ForgeAPIBudgetStatus(ctx context.Context, projectID string, nowMS, limit int64, warningRatio float64) (ForgeAPIBudgetStatus, error) {
	if projectID == "" {
		return ForgeAPIBudgetStatus{}, errors.New("storage: forge api status requires project id")
	}
	if limit < 1 || warningRatio <= 0 || warningRatio >= 1 {
		return ForgeAPIBudgetStatus{}, fmt.Errorf("storage: forge api status requires limit>=1 and warning ratio in (0,1)")
	}
	bucketStart := ForgeAPIBucketStartMS(nowMS)
	var s ForgeAPIBudgetStatus
	s.BucketStartMS = bucketStart
	s.Limit = limit
	err := d.db.QueryRowContext(ctx,
		`SELECT consumed_value FROM budget_counters
		 WHERE kind=? AND scope=? AND scope_id=? AND bucket_start_ms=?`,
		forgeAPIBudgetKind, forgeAPIBudgetScope, projectID, bucketStart).Scan(&s.Consumed)
	if errors.Is(err, sql.ErrNoRows) {
		return s, nil
	}
	if err != nil {
		return ForgeAPIBudgetStatus{}, err
	}
	s.SlowPoll = float64(s.Consumed) >= float64(limit)*warningRatio
	s.Exhausted = s.Consumed >= limit
	return s, nil
}

// ProjectIDByForge resolves the Sift-internal project id from a forge
// identity (projects.forge_kind/host/project_key). It backs the Forge adapter
// charging bridge; production callers already hold the project id and do not
// need it.
func (d *DB) ProjectIDByForge(ctx context.Context, kind, host, projectKey string) (string, error) {
	if kind == "" || host == "" || projectKey == "" {
		return "", errors.New("storage: forge identity is incomplete")
	}
	var id string
	err := d.db.QueryRowContext(ctx,
		`SELECT id FROM projects WHERE forge_kind=? AND forge_host=? AND forge_project_key=? AND enabled=1`,
		kind, host, projectKey).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("storage: no enabled project for %s %s %s", kind, host, projectKey)
	}
	if err != nil {
		return "", err
	}
	return id, nil
}

func forgeAPIConsumedTx(ctx context.Context, tx *sql.Tx, projectID string, bucketStart int64) (int64, error) {
	var consumed int64
	err := tx.QueryRowContext(ctx,
		`SELECT consumed_value FROM budget_counters
		 WHERE kind=? AND scope=? AND scope_id=? AND bucket_start_ms=?`,
		forgeAPIBudgetKind, forgeAPIBudgetScope, projectID, bucketStart).Scan(&consumed)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return consumed, nil
}

func chargeResult(bucketStart, consumed, limit int64, warningRatio float64, charged bool) ChargeForgeAPICallResult {
	return ChargeForgeAPICallResult{
		BucketStartMS: bucketStart,
		Consumed:      consumed,
		Limit:         limit,
		Charged:       charged,
		SlowPoll:      float64(consumed) >= float64(limit)*warningRatio,
		Exhausted:     consumed >= limit,
	}
}

// forgeAPIEmitWarningAlert enqueues a single forge_alert per (project, hour
// bucket) when consumption reaches the warning ratio. insertOperation is
// idempotent on the stable key, so repeated crossings in the same bucket
// produce no second operation.
// forgeAPIEmitWarningAlert enqueues a single forge_alert per (project, hour
// bucket). The payload is intentionally stable across crossings so the
// outbox operation-key dedup (by payload digest) collapses every crossing
// in the same bucket into the one operation forge.md §9 requires; transient
// consumption figures are re-read by the alert worker at delivery time.
func forgeAPIEmitWarningAlert(ctx context.Context, tx *sql.Tx, projectID string, bucketStart int64, nowMS int64) error {
	payload, _ := json.Marshal(map[string]any{
		"purpose":         ForgeAPIBudgetAlertPurpose,
		"project_id":      projectID,
		"bucket_start_ms": bucketStart,
	})
	op := Operation{
		Key:     AlertOperationKey(ForgeAPIBudgetAlertPurpose, fmt.Sprintf("project:%s:%d", projectID, bucketStart), 1),
		Kind:    OperationForgeAlert,
		Payload: payload,
	}
	return insertOperation(ctx, tx, op, "", "", nowMS)
}
