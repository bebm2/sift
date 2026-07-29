package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// ReportQuotaExhaustionCmd is the committed Report quota fact that may produce
// the Report-only failure_review Interrupt. Callers invoke it only after the
// Report rate-token transaction has consumed its token.
type ReportQuotaExhaustionCmd struct {
	RunID                                   string
	ExpectedRunVersion                      int64
	DailyBucketStartMS                      int64
	DailyBucketEndMS                        int64
	AttentionDailyQuota                     map[InterruptSeverity]int
	DayTimezone                             string
	CriticalWindowMS                        int64
	CriticalTotalLimit, CriticalPerRunLimit int
	NowMS                                   int64
}

// RecordReportQuotaExhaustion is the production owner for the Report quota
// exhaustion fact. It commits the system security event and its unique
// exhaustion identity before attempting the best-effort EmitInterrupt step.
// A structural emission rejection never rolls back the durable quota fact.
func (d *DB) RecordReportQuotaExhaustion(ctx context.Context, cmd ReportQuotaExhaustionCmd) (Interrupt, error) {
	if cmd.RunID == "" || cmd.ExpectedRunVersion < 1 || cmd.DailyBucketStartMS <= 0 || cmd.DailyBucketEndMS <= cmd.DailyBucketStartMS || cmd.NowMS <= 0 {
		return Interrupt{}, fmt.Errorf("%w: invalid report quota exhaustion", ErrInterruptRejected)
	}
	digest := reportQuotaFailureDigest(cmd.RunID, cmd.DailyBucketStartMS, cmd.DailyBucketEndMS)
	generation := InterruptGeneration{ReportDailyBucketStartMS: cmd.DailyBucketStartMS, ReportDailyBucketEndMS: cmd.DailyBucketEndMS, FailureDigest: digest}
	key, err := interruptGenerationKey(cmd.RunID, InterruptFailureReview, generation)
	if err != nil {
		return Interrupt{}, err
	}

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return Interrupt{}, err
	}
	defer tx.Rollback()
	var eventID string
	if err := tx.QueryRowContext(ctx, `SELECT security_event_id FROM report_quota_exhaustions WHERE run_id=? AND daily_bucket_start_ms=?`, cmd.RunID, cmd.DailyBucketStartMS).Scan(&eventID); err != nil && err != sql.ErrNoRows {
		return Interrupt{}, err
	} else if err == sql.ErrNoRows {
		var status string
		var version int64
		var projectID string
		if err := tx.QueryRowContext(ctx, `SELECT status,version,project_id FROM runs WHERE id=?`, cmd.RunID).Scan(&status, &version, &projectID); err != nil {
			return Interrupt{}, err
		}
		if RunStatus(status) != RunRunning || version != cmd.ExpectedRunVersion {
			return Interrupt{}, ErrRejectedStale
		}
		eventID = newID()
		payload, _ := json.Marshal(map[string]any{"daily_bucket_start_ms": cmd.DailyBucketStartMS, "daily_bucket_end_ms": cmd.DailyBucketEndMS, "failure_class": "report_interrupt_quota_exhausted"})
		if _, err := tx.ExecContext(ctx, `INSERT INTO events(id,run_id,project_id,type,source,payload_schema_version,payload_json,occurred_at_ms,recorded_at_ms) VALUES(?,?,?,'security.report_quota_exhausted','system',1,?,?,?)`, eventID, cmd.RunID, projectID, string(payload), cmd.NowMS, cmd.NowMS); err != nil {
			return Interrupt{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO report_quota_exhaustions(run_id,daily_bucket_start_ms,daily_bucket_end_ms,security_event_id,failure_digest,generation_key,created_at_ms) VALUES(?,?,?,?,?,?,?)`, cmd.RunID, cmd.DailyBucketStartMS, cmd.DailyBucketEndMS, eventID, digest, key, cmd.NowMS); err != nil {
			return Interrupt{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Interrupt{}, err
	}

	generation.SecurityEventID = eventID
	return d.EmitInterrupt(ctx, EmitInterruptCmd{
		RunID: cmd.RunID, ExpectedRunVersion: cmd.ExpectedRunVersion,
		Reason: InterruptFailureReview, FailureReviewVariant: FailureReviewReportQuota,
		Facts:      map[string]string{"failure_class": "report_interrupt_quota_exhausted", "failure_evidence_ref": "sift://event/" + eventID, "recommended_action": "hold"},
		Generation: generation, GatePhase: GateNone, GuardrailLevel: GuardrailNone,
		AttentionDailyQuota: cmd.AttentionDailyQuota, DayTimezone: cmd.DayTimezone,
		CriticalWindowMS: cmd.CriticalWindowMS, CriticalTotalLimit: cmd.CriticalTotalLimit, CriticalPerRunLimit: cmd.CriticalPerRunLimit,
		Source: SourceSystem, NowMS: cmd.NowMS,
	})
}

func reportQuotaFailureDigest(runID string, start, end int64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf(`{"daily_bucket_end_ms":%d,"daily_bucket_start_ms":%d,"failure_class":"report_interrupt_quota_exhausted","recommended_action":"hold","run_id":%q}`, end, start, runID)))
	return hex.EncodeToString(sum[:])
}
