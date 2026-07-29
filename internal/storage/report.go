package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ReportSubmitCmd is the daemon-side production port for report.submit. The
// token is checked against the launch claim; callers never receive database access.
type ReportSubmitCmd struct {
	Token      string
	RunID      string
	AttemptNo  int
	Generation int
	ReportKey  string
	Kind       string
	Payload    map[string]any
	NowMS      int64
}
type ReportResult struct {
	Disposition string `json:"disposition"`
	ReceiptID   string `json:"receipt_id"`
	EventID     string `json:"event_id"`
}

func (d *DB) RecordReport(ctx context.Context, cmd ReportSubmitCmd) (ReportResult, error) {
	if cmd.RunID == "" || cmd.AttemptNo < 1 || cmd.Generation < 1 || cmd.NowMS <= 0 || len(cmd.ReportKey) != 32 || !lowerHex(cmd.ReportKey) || cmd.Token == "" {
		return ReportResult{}, errors.New("report: invalid request")
	}
	if cmd.Kind != "progress" && cmd.Kind != "goal" && cmd.Kind != "blocker" && cmd.Kind != "completed" {
		return ReportResult{}, errors.New("report: invalid kind")
	}
	if err := validateReportPayload(cmd.Kind, cmd.Payload); err != nil {
		return ReportResult{}, err
	}
	wrapped := map[string]any{"kind": cmd.Kind, "payload": cmd.Payload}
	canonical, _ := json.Marshal(wrapped)
	sum := sha256.Sum256(canonical)
	digest := hex.EncodeToString(sum[:])
	if cmd.Kind == "blocker" {
		return d.recordBlockerReport(ctx, cmd, digest)
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return ReportResult{}, err
	}
	defer tx.Rollback()
	var phase string
	var storedGeneration int
	var tokenHash string
	var projectID, snapshotID string
	var runVersion int64
	if err := tx.QueryRowContext(ctx, `SELECT a.phase,a.generation,c.run_token_hash,r.project_id,r.config_snapshot_id,r.version FROM attempts a JOIN attempt_claims c USING(run_id,attempt_no) JOIN runs r ON r.id=a.run_id WHERE a.run_id=? AND a.attempt_no=?`, cmd.RunID, cmd.AttemptNo).Scan(&phase, &storedGeneration, &tokenHash, &projectID, &snapshotID, &runVersion); err != nil {
		return ReportResult{}, errors.New("report: unauthorized")
	}
	if tokenHash != handoffHash(cmd.Token) || storedGeneration != cmd.Generation {
		return ReportResult{}, errors.New("report: unauthorized")
	}
	if phase != "running" {
		return ReportResult{}, errors.New("report: report is not accepted in this phase")
	}
	var id, oldDigest, oldEvent string
	err = tx.QueryRowContext(ctx, `SELECT id,payload_digest,event_id FROM report_receipts WHERE run_id=? AND attempt_no=? AND report_key=?`, cmd.RunID, cmd.AttemptNo, cmd.ReportKey).Scan(&id, &oldDigest, &oldEvent)
	if err == nil {
		if oldDigest == digest {
			return ReportResult{Disposition: "duplicate", ReceiptID: id, EventID: oldEvent}, tx.Commit()
		}
		return ReportResult{}, errors.New("report: report key conflict")
	}
	if err != sql.ErrNoRows {
		return ReportResult{}, err
	}
	// The rate bucket is the linearization point for a non-duplicate report.
	var capacity, available, numerator, period, remainder, last int64
	err = tx.QueryRowContext(ctx, `SELECT capacity_units,available_units,refill_numerator,refill_period_ms,refill_remainder,last_refill_at_ms FROM rate_limit_buckets WHERE kind='report' AND scope_id=?`, "run:"+cmd.RunID+":attempt:"+fmt.Sprint(cmd.AttemptNo)).Scan(&capacity, &available, &numerator, &period, &remainder, &last)
	if err == sql.ErrNoRows {
		var raw string
		if err = tx.QueryRowContext(ctx, `SELECT canonical_json FROM config_snapshots WHERE id=?`, snapshotID).Scan(&raw); err != nil {
			return ReportResult{}, err
		}
		var c struct {
			Report struct {
				EventsPerMinute int `json:"events_per_minute"`
				Burst           int `json:"burst"`
			} `json:"report"`
		}
		if json.Unmarshal([]byte(raw), &c) != nil || c.Report.Burst < 1 || c.Report.EventsPerMinute < 1 {
			return ReportResult{}, errors.New("report: invalid snapshot")
		}
		capacity = int64(c.Report.Burst)
		available = capacity
		numerator = int64(c.Report.EventsPerMinute)
		period = 60000
		last = cmd.NowMS
		if _, err = tx.ExecContext(ctx, `INSERT INTO rate_limit_buckets(kind,scope_id,capacity_units,available_units,refill_numerator,refill_period_ms,refill_remainder,last_refill_at_ms,version) VALUES('report',?,?,?,?,?,?,?,1)`, "run:"+cmd.RunID+":attempt:"+fmt.Sprint(cmd.AttemptNo), capacity, available, numerator, period, 0, last); err != nil {
			return ReportResult{}, err
		}
	} else if err != nil {
		return ReportResult{}, err
	}
	if cmd.NowMS > last {
		add := (cmd.NowMS - last) * numerator / period
		if add > 0 {
			available += add
			if available > capacity {
				available = capacity
			}
			last = cmd.NowMS
		}
	}
	if available < 1 {
		return ReportResult{}, errors.New("report: rate limit exceeded")
	}
	available--
	if _, err = tx.ExecContext(ctx, `UPDATE rate_limit_buckets SET available_units=?,last_refill_at_ms=?,version=version+1 WHERE kind='report' AND scope_id=?`, available, last, "run:"+cmd.RunID+":attempt:"+fmt.Sprint(cmd.AttemptNo)); err != nil {
		return ReportResult{}, err
	}
	eventID, receiptID := newID(), newID()
	var reportChargeID string
	var reportCfg reportRuntimeConfig
	if cmd.Kind == "blocker" {
		var raw string
		if err := tx.QueryRowContext(ctx, `SELECT canonical_json FROM config_snapshots WHERE id=?`, snapshotID).Scan(&raw); err != nil {
			return ReportResult{}, err
		}
		if err := json.Unmarshal([]byte(raw), &reportCfg); err != nil || reportCfg.Report.InterruptsPerRunDailyQuota < 1 {
			return ReportResult{}, errors.New("report: invalid snapshot")
		}
		start, end, err := reportDayBucket(cmd.NowMS, reportCfg.Attention.DayTimezone)
		if err != nil {
			return ReportResult{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO budget_counters(kind,scope,scope_id,bucket_start_ms,bucket_end_ms,limit_value,consumed_value,version,updated_at_ms) VALUES('report','run',?,?,?, ?,0,1,?) ON CONFLICT DO NOTHING`, cmd.RunID, start, end, reportCfg.Report.InterruptsPerRunDailyQuota, cmd.NowMS); err != nil {
			return ReportResult{}, err
		}
		res, err := tx.ExecContext(ctx, `UPDATE budget_counters SET consumed_value=consumed_value+1,version=version+1,updated_at_ms=? WHERE kind='report' AND scope='run' AND scope_id=? AND bucket_start_ms=? AND consumed_value<limit_value`, cmd.NowMS, cmd.RunID, start)
		if err != nil {
			return ReportResult{}, err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			if err := tx.Commit(); err != nil {
				return ReportResult{}, err
			}
			_, emitErr := d.RecordReportQuotaExhaustion(ctx, reportQuotaCmd(cmd, runVersion, start, end, reportCfg))
			if emitErr != nil && !errors.Is(emitErr, ErrInterruptRejected) {
				return ReportResult{}, emitErr
			}
			return ReportResult{}, errors.New("report: report_interrupt_quota_exhausted")
		}
		reportChargeID = newID()
		if _, err := tx.ExecContext(ctx, `INSERT INTO budget_entries(id,kind,scope,scope_id,bucket_start_ms,amount,reason,run_id,operation_key,created_at_ms) VALUES(?,'report','run',?,?,1,'report_agent_blocked',?,?,?)`, reportChargeID, cmd.RunID, start, cmd.RunID, "report-interrupt-quota:"+receiptID, cmd.NowMS); err != nil {
			return ReportResult{}, err
		}
	}
	payload, _ := json.Marshal(map[string]any{"report_key": cmd.ReportKey, "payload_digest": digest, "generation": cmd.Generation, "report": cmd.Payload})
	if _, err = tx.ExecContext(ctx, `INSERT INTO events(id,run_id,attempt_no,project_id,type,source,payload_schema_version,payload_json,occurred_at_ms,recorded_at_ms) VALUES(?,?,?,?,?, 'agent',1,?,?,?)`, eventID, cmd.RunID, cmd.AttemptNo, projectID, "report."+cmd.Kind, string(payload), cmd.NowMS, cmd.NowMS); err != nil {
		return ReportResult{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO report_receipts(id,run_id,attempt_no,report_key,report_kind,payload_digest,event_id,report_interrupt_charge_entry_id,received_at_ms) VALUES(?,?,?,?,?,?,?,?,?)`, receiptID, cmd.RunID, cmd.AttemptNo, cmd.ReportKey, cmd.Kind, digest, eventID, nullable(reportChargeID), cmd.NowMS); err != nil {
		return ReportResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return ReportResult{}, err
	}
	return ReportResult{"accepted", receiptID, eventID}, nil
}

// recordBlockerReport makes the Report receipt/event, rate token, child quota,
// and agent_blocked Interrupt one SQLite transaction. EmitInterrupt invokes
// before only after T4/T6 have completed and rolls this transaction back on
// any structural or publish failure.
func (d *DB) recordBlockerReport(ctx context.Context, cmd ReportSubmitCmd, digest string) (ReportResult, error) {
	var result ReportResult
	var exhausted bool
	var cfg reportRuntimeConfig
	var runVersion int64
	var worktree string
	if err := d.db.QueryRowContext(ctx, `SELECT a.worktree_path,r.version FROM attempts a JOIN runs r ON r.id=a.run_id WHERE a.run_id=? AND a.attempt_no=?`, cmd.RunID, cmd.AttemptNo).Scan(&worktree, &runVersion); err != nil {
		return ReportResult{}, errors.New("report: unauthorized")
	}
	if err := json.Unmarshal(mustReportSnapshot(ctx, d.db, cmd.RunID, cmd.AttemptNo), &cfg); err != nil {
		return ReportResult{}, errors.New("report: invalid snapshot")
	}
	n := cmd.AttemptNo
	receiptID := newID()
	var batchAt *int64
	if at, ok := nextSummary(cmd.NowMS, timezoneOrUTC(cfg.Attention.DayTimezone), summaryOrDefault(cfg.Attention.DailySummaryAt)); ok {
		batchAt = &at
	}
	facts := map[string]string{"blocker_summary": cmd.Payload["blocker_summary"].(string), "attempted_summary": cmd.Payload["attempted_summary"].(string), "recommended_action": cmd.Payload["recommended_action"].(string), "agent_log_ref": strings.TrimRight(worktree, "/") + "/agent.log"}
	err := func() error {
		_, emitErr := d.emitReportInterruptHooks(ctx, EmitInterruptCmd{RunID: cmd.RunID, ExpectedRunVersion: runVersion, AttemptNo: &n, Reason: InterruptAgentBlocked, Facts: facts, Generation: InterruptGeneration{AttemptNo: cmd.AttemptNo, Generation: cmd.Generation, ReportID: receiptID}, GatePhase: GateNone, GuardrailLevel: GuardrailNone, MaxEscalations: cfg.Attention.MaxEscalations, AttentionDailyQuota: map[InterruptSeverity]int{SeverityLow: cfg.Attention.DailyQuota.Low, SeverityNormal: cfg.Attention.DailyQuota.Normal, SeverityHigh: cfg.Attention.DailyQuota.High}, DayTimezone: cfg.Attention.DayTimezone, DailySummaryAt: cfg.Attention.DailySummaryAt, CriticalWindowMS: cfg.Attention.CriticalFuse.Window, CriticalTotalLimit: cfg.Attention.CriticalFuse.TotalLimit, CriticalPerRunLimit: cfg.Attention.CriticalFuse.PerRunLimit, Channels: reportChannels(cfg), BatchAtMS: batchAt, Source: SourceAgent, NowMS: cmd.NowMS}, func(tx *sql.Tx) error {
			var phase, tokenHash, projectID, snapshotID string
			var generation int
			if err := tx.QueryRowContext(ctx, `SELECT a.phase,a.generation,c.run_token_hash,r.project_id,r.config_snapshot_id,r.version FROM attempts a JOIN attempt_claims c USING(run_id,attempt_no) JOIN runs r ON r.id=a.run_id WHERE a.run_id=? AND a.attempt_no=?`, cmd.RunID, cmd.AttemptNo).Scan(&phase, &generation, &tokenHash, &projectID, &snapshotID, &runVersion); err != nil || phase != "running" || generation != cmd.Generation || tokenHash != handoffHash(cmd.Token) {
				return errors.New("report: unauthorized")
			}
			var oldID, oldDigest, oldEvent string
			if err := tx.QueryRowContext(ctx, `SELECT id,payload_digest,event_id FROM report_receipts WHERE run_id=? AND attempt_no=? AND report_key=?`, cmd.RunID, cmd.AttemptNo, cmd.ReportKey).Scan(&oldID, &oldDigest, &oldEvent); err == nil {
				if oldDigest == digest {
					result = ReportResult{"duplicate", oldID, oldEvent}
					return errReportDuplicate
				}
				return errors.New("report: report key conflict")
			}
			if err := consumeReportTokenTx(ctx, tx, cmd, snapshotID); err != nil {
				return err
			}
			start, end, err := reportDayBucket(cmd.NowMS, cfg.Attention.DayTimezone)
			if err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO budget_counters(kind,scope,scope_id,bucket_start_ms,bucket_end_ms,limit_value,consumed_value,version,updated_at_ms) VALUES('report','run',?,?,?, ?,0,1,?) ON CONFLICT DO NOTHING`, cmd.RunID, start, end, cfg.Report.InterruptsPerRunDailyQuota, cmd.NowMS); err != nil {
				return err
			}
			res, err := tx.ExecContext(ctx, `UPDATE budget_counters SET consumed_value=consumed_value+1,version=version+1,updated_at_ms=? WHERE kind='report' AND scope='run' AND scope_id=? AND bucket_start_ms=? AND consumed_value<limit_value`, cmd.NowMS, cmd.RunID, start)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n != 1 {
				exhausted = true
				return errReportQuotaExhausted
			}
			eventID, chargeID := newID(), newID()
			payload, _ := json.Marshal(map[string]any{"report_key": cmd.ReportKey, "payload_digest": digest, "generation": cmd.Generation, "report": cmd.Payload})
			if _, err = tx.ExecContext(ctx, `INSERT INTO budget_entries(id,kind,scope,scope_id,bucket_start_ms,amount,reason,run_id,operation_key,created_at_ms) VALUES(?,'report','run',?,?,1,'report_agent_blocked',?,?,?)`, chargeID, cmd.RunID, start, cmd.RunID, "report-interrupt-quota:"+receiptID, cmd.NowMS); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO events(id,run_id,attempt_no,project_id,type,source,payload_schema_version,payload_json,occurred_at_ms,recorded_at_ms) VALUES(?,?,?,?,?, 'agent',1,?,?,?)`, eventID, cmd.RunID, cmd.AttemptNo, projectID, "report.blocker", string(payload), cmd.NowMS, cmd.NowMS); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO report_receipts(id,run_id,attempt_no,report_key,report_kind,payload_digest,event_id,report_interrupt_charge_entry_id,received_at_ms) VALUES(?,?,?,?,?,?,?,?,?)`, receiptID, cmd.RunID, cmd.AttemptNo, cmd.ReportKey, cmd.Kind, digest, eventID, chargeID, cmd.NowMS); err != nil {
				return err
			}
			result = ReportResult{"accepted", receiptID, eventID}
			return nil
		}, func(tx *sql.Tx, in Interrupt) error {
			_, err := tx.ExecContext(ctx, `UPDATE report_receipts SET direct_interrupt_id=? WHERE id=? AND direct_interrupt_id IS NULL`, in.ID, receiptID)
			return err
		})
		return emitErr
	}()
	if errors.Is(err, errReportDuplicate) {
		return result, nil
	}
	if errors.Is(err, errReportQuotaExhausted) && exhausted {
		start, end, bucketErr := reportDayBucket(cmd.NowMS, cfg.Attention.DayTimezone)
		if bucketErr != nil {
			return ReportResult{}, bucketErr
		}
		if bucketErr = d.commitReportQuotaExhaustion(ctx, cmd, cfg, runVersion, start, end); bucketErr != nil {
			return ReportResult{}, bucketErr
		}
		_, emitErr := d.RecordReportQuotaExhaustion(ctx, reportQuotaCmd(cmd, runVersion, start, end, cfg))
		if emitErr != nil && !errors.Is(emitErr, ErrInterruptRejected) {
			return ReportResult{}, emitErr
		}
		return ReportResult{}, errors.New("report: report_interrupt_quota_exhausted")
	}
	if err != nil {
		return ReportResult{}, err
	}
	return result, nil
}

var errReportDuplicate = errors.New("report duplicate")
var errReportQuotaExhausted = errors.New("report quota exhausted")

func mustReportSnapshot(ctx context.Context, db *sql.DB, runID string, attempt int) []byte {
	var raw string
	_ = db.QueryRowContext(ctx, `SELECT canonical_json FROM config_snapshots WHERE id=(SELECT config_snapshot_id FROM runs WHERE id=?)`, runID).Scan(&raw)
	return []byte(raw)
}

func (d *DB) commitReportQuotaExhaustion(ctx context.Context, cmd ReportSubmitCmd, cfg reportRuntimeConfig, runVersion, start, end int64) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var phase, tokenHash, snapshotID string
	var generation int
	if err := tx.QueryRowContext(ctx, `SELECT a.phase,a.generation,c.run_token_hash,r.config_snapshot_id FROM attempts a JOIN attempt_claims c USING(run_id,attempt_no) JOIN runs r ON r.id=a.run_id WHERE a.run_id=? AND a.attempt_no=?`, cmd.RunID, cmd.AttemptNo).Scan(&phase, &generation, &tokenHash, &snapshotID); err != nil || phase != "running" || generation != cmd.Generation || tokenHash != handoffHash(cmd.Token) {
		return errors.New("report: unauthorized")
	}
	// The exhaustion row is the rate-token linearization point. Replays and
	// later blockers reuse it without consuming another token.
	var existing string
	if err := tx.QueryRowContext(ctx, `SELECT security_event_id FROM report_quota_exhaustions WHERE run_id=? AND daily_bucket_start_ms=?`, cmd.RunID, start).Scan(&existing); err == nil {
		return tx.Commit()
	} else if err != sql.ErrNoRows {
		return err
	}
	if err := consumeReportTokenTx(ctx, tx, cmd, snapshotID); err != nil {
		return err
	}
	var projectID string
	if err := tx.QueryRowContext(ctx, `SELECT project_id FROM runs WHERE id=? AND version=?`, cmd.RunID, runVersion).Scan(&projectID); err != nil {
		return err
	}
	eventID := newID()
	payload, _ := json.Marshal(map[string]any{"daily_bucket_start_ms": start, "daily_bucket_end_ms": end, "failure_class": "report_interrupt_quota_exhausted"})
	if _, err = tx.ExecContext(ctx, `INSERT INTO events(id,run_id,project_id,type,source,payload_schema_version,payload_json,occurred_at_ms,recorded_at_ms) VALUES(?,?,?,'security.report_quota_exhausted','system',1,?,?,?)`, eventID, cmd.RunID, projectID, string(payload), cmd.NowMS, cmd.NowMS); err != nil {
		return err
	}
	digest := reportQuotaFailureDigest(cmd.RunID, start, end)
	key, err := interruptGenerationKey(cmd.RunID, InterruptFailureReview, InterruptGeneration{ReportDailyBucketStartMS: start, ReportDailyBucketEndMS: end, FailureDigest: digest})
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO report_quota_exhaustions(run_id,daily_bucket_start_ms,daily_bucket_end_ms,security_event_id,failure_digest,generation_key,created_at_ms) VALUES(?,?,?,?,?,?,?)`, cmd.RunID, start, end, eventID, digest, key, cmd.NowMS); err != nil {
		// Another writer may have linearized this day after our initial read.
		// Roll back the tentative token/event, then reuse the durable winner.
		_ = tx.Rollback()
		var winner string
		if lookupErr := d.db.QueryRowContext(ctx, `SELECT security_event_id FROM report_quota_exhaustions WHERE run_id=? AND daily_bucket_start_ms=?`, cmd.RunID, start).Scan(&winner); lookupErr == nil {
			return nil
		}
		return err
	}
	return tx.Commit()
}

func consumeReportTokenTx(ctx context.Context, tx *sql.Tx, cmd ReportSubmitCmd, snapshotID string) error {
	var capacity, available, numerator, period, remainder, last int64
	scope := "run:" + cmd.RunID + ":attempt:" + fmt.Sprint(cmd.AttemptNo)
	err := tx.QueryRowContext(ctx, `SELECT capacity_units,available_units,refill_numerator,refill_period_ms,refill_remainder,last_refill_at_ms FROM rate_limit_buckets WHERE kind='report' AND scope_id=?`, scope).Scan(&capacity, &available, &numerator, &period, &remainder, &last)
	if err == sql.ErrNoRows {
		var raw string
		if err = tx.QueryRowContext(ctx, `SELECT canonical_json FROM config_snapshots WHERE id=?`, snapshotID).Scan(&raw); err != nil {
			return err
		}
		var c struct {
			Report struct {
				EventsPerMinute int `json:"events_per_minute"`
				Burst           int `json:"burst"`
			} `json:"report"`
		}
		if json.Unmarshal([]byte(raw), &c) != nil || c.Report.Burst < 1 || c.Report.EventsPerMinute < 1 {
			return errors.New("report: invalid snapshot")
		}
		capacity, available, numerator, period, last = int64(c.Report.Burst), int64(c.Report.Burst), int64(c.Report.EventsPerMinute), 60000, cmd.NowMS
		if _, err = tx.ExecContext(ctx, `INSERT INTO rate_limit_buckets(kind,scope_id,capacity_units,available_units,refill_numerator,refill_period_ms,refill_remainder,last_refill_at_ms,version) VALUES('report',?,?,?,?,?,?,?,1)`, scope, capacity, available, numerator, period, 0, last); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if cmd.NowMS > last {
		add := (cmd.NowMS - last) * numerator / period
		if add > 0 {
			available += add
			if available > capacity {
				available = capacity
			}
			last = cmd.NowMS
		}
	}
	if available < 1 {
		return errors.New("report: rate limit exceeded")
	}
	available--
	_, err = tx.ExecContext(ctx, `UPDATE rate_limit_buckets SET available_units=?,last_refill_at_ms=?,version=version+1 WHERE kind='report' AND scope_id=?`, available, last, scope)
	return err
}

type reportRuntimeChannel struct {
	ID           string   `json:"id"`
	Enabled      bool     `json:"enabled"`
	Type         string   `json:"type"`
	TargetRef    string   `json:"target_ref"`
	Capabilities []string `json:"capabilities"`
	Renderer     string   `json:"renderer"`
	Default      bool     `json:"default"`
}

type reportRuntimeConfig struct {
	Attention struct {
		DayTimezone string `json:"day_timezone"`
		DailyQuota  struct {
			Low    int `json:"low"`
			Normal int `json:"normal"`
			High   int `json:"high"`
		} `json:"daily_quota"`
		MaxEscalations int `json:"max_escalations"`
		CriticalFuse   struct {
			Window      int64 `json:"window"`
			TotalLimit  int   `json:"total_limit"`
			PerRunLimit int   `json:"per_run_limit"`
		} `json:"critical_fuse"`
		DailySummaryAt string                 `json:"daily_summary_at"`
		Channels       []reportRuntimeChannel `json:"channels"`
	} `json:"attention"`
	Report struct {
		InterruptsPerRunDailyQuota int `json:"interrupts_per_run_daily_quota"`
	} `json:"report"`
}

func reportDayBucket(nowMS int64, zone string) (int64, int64, error) {
	loc, err := time.LoadLocation(timezoneOrUTC(zone))
	if err != nil {
		return 0, 0, fmt.Errorf("report: invalid day timezone: %w", err)
	}
	now := time.UnixMilli(nowMS).In(loc)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	return start.UnixMilli(), start.AddDate(0, 0, 1).UnixMilli(), nil
}

func reportQuotaCmd(cmd ReportSubmitCmd, version, start, end int64, cfg reportRuntimeConfig) ReportQuotaExhaustionCmd {
	return ReportQuotaExhaustionCmd{RunID: cmd.RunID, ExpectedRunVersion: version, DailyBucketStartMS: start, DailyBucketEndMS: end, AttentionDailyQuota: map[InterruptSeverity]int{SeverityLow: cfg.Attention.DailyQuota.Low, SeverityNormal: cfg.Attention.DailyQuota.Normal, SeverityHigh: cfg.Attention.DailyQuota.High}, DayTimezone: cfg.Attention.DayTimezone, DailySummaryAt: cfg.Attention.DailySummaryAt, CriticalWindowMS: cfg.Attention.CriticalFuse.Window, CriticalTotalLimit: cfg.Attention.CriticalFuse.TotalLimit, CriticalPerRunLimit: cfg.Attention.CriticalFuse.PerRunLimit, Channels: reportChannels(cfg), NowMS: cmd.NowMS}
}

func reportChannels(cfg reportRuntimeConfig) []InterruptChannel {
	channels := make([]InterruptChannel, 0, len(cfg.Attention.Channels))
	for _, c := range cfg.Attention.Channels {
		channels = append(channels, InterruptChannel{ID: c.ID, Type: c.Type, TargetRef: c.TargetRef, Capabilities: c.Capabilities, Renderer: c.Renderer, Default: c.Default, Isolated: !c.Enabled})
	}
	return channels
}

func validateReportPayload(kind string, p map[string]any) error {
	if p == nil {
		return errors.New("report: payload must be an object")
	}
	want := map[string]string{"progress": "message", "goal": "goal", "blocker": "blocker_summary,attempted_summary,recommended_action", "completed": "summary"}[kind]
	keys := strings.Split(want, ",")
	if len(p) != len(keys) {
		return errors.New("report: payload is not closed")
	}
	for _, k := range keys {
		v, ok := p[k].(string)
		if !ok || v == "" || strings.ContainsAny(v, "\r\n\x00") {
			return errors.New("report: invalid payload field")
		}
	}
	return nil
}
