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
	if err := tx.QueryRowContext(ctx, `SELECT a.phase,a.generation,c.run_token_hash,r.project_id,r.config_snapshot_id,r.version FROM attempts a JOIN attempt_claims c USING(run_id,attempt_no) JOIN runs r USING(id) WHERE a.run_id=? AND a.attempt_no=?`, cmd.RunID, cmd.AttemptNo).Scan(&phase, &storedGeneration, &tokenHash, &projectID, &snapshotID, &runVersion); err != nil {
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
			Report struct{ EventsPerMinute, Burst int } `json:"report"`
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
	// The receipt/event and token are durable before the optional Attention path.
	if cmd.Kind != "blocker" {
		return ReportResult{"accepted", receiptID, eventID}, nil
	}
	// Blocker is deliberately routed through the same production emitter as all
	// other HITL causes. It is not a second Report-specific write port.
	var worktree string
	if err := d.db.QueryRowContext(ctx, `SELECT worktree_path FROM attempts WHERE run_id=? AND attempt_no=?`, cmd.RunID, cmd.AttemptNo).Scan(&worktree); err != nil {
		return ReportResult{}, err
	}
	n := cmd.AttemptNo
	facts := map[string]string{"blocker_summary": cmd.Payload["blocker_summary"].(string), "attempted_summary": cmd.Payload["attempted_summary"].(string), "recommended_action": cmd.Payload["recommended_action"].(string), "agent_log_ref": strings.TrimRight(worktree, "/") + "/agent.log"}
	in, emitErr := d.EmitInterrupt(ctx, EmitInterruptCmd{RunID: cmd.RunID, ExpectedRunVersion: runVersion, AttemptNo: &n, Reason: InterruptAgentBlocked, Facts: facts, Generation: InterruptGeneration{AttemptNo: cmd.AttemptNo, Generation: cmd.Generation, ReportID: receiptID}, GatePhase: GateNone, GuardrailLevel: GuardrailNone, MaxEscalations: reportCfg.Attention.MaxEscalations, AttentionDailyQuota: map[InterruptSeverity]int{SeverityLow: reportCfg.Attention.DailyQuota.Low, SeverityNormal: reportCfg.Attention.DailyQuota.Normal, SeverityHigh: reportCfg.Attention.DailyQuota.High}, DayTimezone: reportCfg.Attention.DayTimezone, DailySummaryAt: reportCfg.Attention.DailySummaryAt, CriticalWindowMS: reportCfg.Attention.CriticalFuse.Window, CriticalTotalLimit: reportCfg.Attention.CriticalFuse.TotalLimit, CriticalPerRunLimit: reportCfg.Attention.CriticalFuse.PerRunLimit, Source: SourceAgent, NowMS: cmd.NowMS})
	if emitErr != nil {
		return ReportResult{}, emitErr
	}
	if _, err := d.db.ExecContext(ctx, `UPDATE report_receipts SET direct_interrupt_id=? WHERE id=? AND direct_interrupt_id IS NULL`, in.ID, receiptID); err != nil {
		return ReportResult{}, err
	}
	return ReportResult{"accepted", receiptID, eventID}, nil
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
		DailySummaryAt string `json:"daily_summary_at"`
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
	return ReportQuotaExhaustionCmd{RunID: cmd.RunID, ExpectedRunVersion: version, DailyBucketStartMS: start, DailyBucketEndMS: end, AttentionDailyQuota: map[InterruptSeverity]int{SeverityLow: cfg.Attention.DailyQuota.Low, SeverityNormal: cfg.Attention.DailyQuota.Normal, SeverityHigh: cfg.Attention.DailyQuota.High}, DayTimezone: cfg.Attention.DayTimezone, DailySummaryAt: cfg.Attention.DailySummaryAt, CriticalWindowMS: cfg.Attention.CriticalFuse.Window, CriticalTotalLimit: cfg.Attention.CriticalFuse.TotalLimit, CriticalPerRunLimit: cfg.Attention.CriticalFuse.PerRunLimit, NowMS: cmd.NowMS}
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
