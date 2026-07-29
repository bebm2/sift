-- Complete the frozen state needed by AdvanceInterrupt after an upgrade from 0010.
ALTER TABLE interrupts ADD COLUMN channel_snapshot_json TEXT;
ALTER TABLE interrupts ADD COLUMN day_timezone TEXT NOT NULL DEFAULT 'UTC';
ALTER TABLE interrupts ADD COLUMN daily_summary_at TEXT NOT NULL DEFAULT '09:00';
ALTER TABLE interrupts ADD COLUMN critical_window_ms INTEGER NOT NULL DEFAULT 900000 CHECK (critical_window_ms > 0);
ALTER TABLE interrupts ADD COLUMN critical_total_limit INTEGER NOT NULL DEFAULT 5 CHECK (critical_total_limit > 0);
ALTER TABLE interrupts ADD COLUMN critical_per_run_limit INTEGER NOT NULL DEFAULT 2 CHECK (critical_per_run_limit > 0);

-- 0011 predated reason defaults. Repair its placeholder values without changing
-- the historical migration checksum.
UPDATE interrupts SET
 expires_after_ms=CASE reason
   WHEN 'design_approval' THEN 86400000 WHEN 'guardrail_violation' THEN 86400000
   WHEN 'code_review' THEN 259200000 WHEN 'agent_blocked' THEN 28800000
   WHEN 'merge_conflict' THEN 28800000 WHEN 'failure_review' THEN 86400000
   WHEN 'startup_stall' THEN 3600000 ELSE expires_after_ms END,
 on_max_escalations=CASE reason
   WHEN 'agent_blocked' THEN 'auto_reject' WHEN 'merge_conflict' THEN 'auto_reject'
   WHEN 'failure_review' THEN 'auto_reject' ELSE 'hold' END,
 base_severity=CASE reason
   WHEN 'guardrail_violation' THEN 'high' WHEN 'merge_conflict' THEN 'high'
   WHEN 'failure_review' THEN 'high' WHEN 'startup_stall' THEN 'high' ELSE 'normal' END,
 nonce_issued_at_ms=COALESCE(nonce_issued_at_ms, created_at_ms);

CREATE TABLE IF NOT EXISTS attention_batches (
 id TEXT NOT NULL PRIMARY KEY, state TEXT NOT NULL CHECK(state IN ('collecting','sealed','delivered','failed','cancelled')),
 project_id TEXT NOT NULL, channel_id TEXT NOT NULL, channel_snapshot_json TEXT NOT NULL,
 forge_kind TEXT NOT NULL, forge_host TEXT NOT NULL, forge_project_key TEXT NOT NULL,
 target_kind TEXT NOT NULL, target_id TEXT NOT NULL, operation_key TEXT, updated_at_ms INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS attention_admissions (
 id TEXT NOT NULL PRIMARY KEY, interrupt_id TEXT NOT NULL REFERENCES interrupts(id),
 admission_key TEXT NOT NULL UNIQUE, kind TEXT NOT NULL CHECK(kind IN ('quota_charged','quota_batched','critical_admitted','critical_fused')),
 metric_identity TEXT NOT NULL, run_id TEXT NOT NULL REFERENCES runs(id),
 critical_source TEXT CHECK(critical_source IN ('initial','escalation')), created_at_ms INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS attention_admissions_interrupt_critical ON attention_admissions(interrupt_id);

CREATE TABLE IF NOT EXISTS interrupt_command_effect_bindings (
 interrupt_id TEXT NOT NULL PRIMARY KEY REFERENCES interrupts(id),
 binding_json TEXT NOT NULL,
 created_at_ms INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS attention_batch_members (
 batch_id TEXT NOT NULL REFERENCES attention_batches(id),
 interrupt_id TEXT NOT NULL REFERENCES interrupts(id),
 admission_id TEXT NOT NULL REFERENCES attention_admissions(id),
 member_key TEXT NOT NULL UNIQUE,
 channel_id TEXT NOT NULL, channel_snapshot_json TEXT NOT NULL,
 delivery_id TEXT NOT NULL UNIQUE, interrupt_version INTEGER NOT NULL,
 nonce TEXT NOT NULL, headline TEXT NOT NULL, reason TEXT NOT NULL,
 severity TEXT NOT NULL, links_json TEXT NOT NULL, options_json TEXT NOT NULL,
 joined_at_ms INTEGER NOT NULL, excluded_at_ms INTEGER,
 PRIMARY KEY(batch_id, interrupt_id)
);
