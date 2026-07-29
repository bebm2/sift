-- Forward-only repair for databases created before 0001 was corrected.
-- Keep the published 0001 bytes unchanged: quota-batched attention has no charge.
PRAGMA foreign_keys=OFF;

DROP INDEX IF EXISTS interrupts_status_expires;
DROP INDEX IF EXISTS interrupts_run_status;
DROP INDEX IF EXISTS interrupts_open_dispatch_due;
DROP TRIGGER IF EXISTS interrupts_identity_immutable;
DROP TRIGGER IF EXISTS interrupts_nonce_issued_required_insert;
DROP TRIGGER IF EXISTS interrupts_startup_stall_max_reject_insert;

CREATE TABLE interrupts_forward (
    id TEXT NOT NULL PRIMARY KEY, run_id TEXT NOT NULL REFERENCES runs(id), attempt_no INTEGER, calibration_id TEXT REFERENCES calibration_entries(id),
    generation_key TEXT NOT NULL UNIQUE, reason TEXT NOT NULL CHECK (reason IN ('design_approval','guardrail_violation','code_review','agent_blocked','merge_conflict','failure_review','startup_stall')),
    severity TEXT NOT NULL CHECK (severity IN ('low','normal','high','critical')), headline TEXT NOT NULL CHECK (length(headline)<=40), brief_markdown TEXT NOT NULL,
    options_json TEXT NOT NULL, min_modality TEXT NOT NULL CHECK (min_modality IN ('voice','text','visual')), links_json TEXT NOT NULL DEFAULT '[]', nonce TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1 CHECK (version>=1), status TEXT NOT NULL CHECK (status IN ('open','closed')), dispatch_state TEXT NOT NULL CHECK (dispatch_state IN ('ready','batched','held','probe_in_progress')),
    expires_at_ms INTEGER NOT NULL, on_expire TEXT NOT NULL CHECK (on_expire IN ('hold','escalate','auto_reject')), escalation_count INTEGER NOT NULL DEFAULT 0 CHECK (escalation_count>=0),
    max_escalations INTEGER NOT NULL CHECK (max_escalations>=0), close_reason TEXT, closed_at_ms INTEGER, charged_budget_entry_id TEXT UNIQUE REFERENCES budget_entries(id),
    created_at_ms INTEGER NOT NULL, updated_at_ms INTEGER NOT NULL, expires_after_ms INTEGER NOT NULL DEFAULT 1 CHECK (expires_after_ms>0),
    on_max_escalations TEXT NOT NULL DEFAULT 'hold' CHECK (on_max_escalations IN ('hold','auto_reject')), base_severity TEXT NOT NULL DEFAULT 'normal' CHECK (base_severity IN ('low','normal','high','critical')),
    nonce_issued_at_ms INTEGER, channel_id TEXT, delivery TEXT CHECK (delivery IN ('immediate','batch','next_window','held')), suggested_downgrade INTEGER NOT NULL DEFAULT 0 CHECK (suggested_downgrade IN (0,1)),
    next_dispatch_at_ms INTEGER, held_reason TEXT, channel_snapshot_json TEXT, day_timezone TEXT NOT NULL DEFAULT 'UTC', daily_summary_at TEXT NOT NULL DEFAULT '09:00',
    critical_window_ms INTEGER NOT NULL DEFAULT 900000 CHECK (critical_window_ms>0), critical_total_limit INTEGER NOT NULL DEFAULT 5 CHECK (critical_total_limit>0), critical_per_run_limit INTEGER NOT NULL DEFAULT 2 CHECK (critical_per_run_limit>0),
    FOREIGN KEY (run_id,attempt_no) REFERENCES attempts(run_id,attempt_no),
    CHECK ((status='open' AND close_reason IS NULL AND closed_at_ms IS NULL) OR (status='closed' AND close_reason IS NOT NULL AND closed_at_ms IS NOT NULL)),
    CHECK (reason <> 'startup_stall' OR on_expire <> 'auto_reject')
);
INSERT INTO interrupts_forward SELECT id,run_id,attempt_no,calibration_id,generation_key,reason,severity,headline,brief_markdown,options_json,min_modality,links_json,nonce,version,status,dispatch_state,expires_at_ms,on_expire,escalation_count,max_escalations,close_reason,closed_at_ms,charged_budget_entry_id,created_at_ms,updated_at_ms,expires_after_ms,on_max_escalations,base_severity,nonce_issued_at_ms,channel_id,delivery,suggested_downgrade,next_dispatch_at_ms,held_reason,channel_snapshot_json,day_timezone,daily_summary_at,critical_window_ms,critical_total_limit,critical_per_run_limit FROM interrupts;
DROP TABLE interrupts;
ALTER TABLE interrupts_forward RENAME TO interrupts;
CREATE UNIQUE INDEX interrupts_calibration_id ON interrupts(calibration_id) WHERE calibration_id IS NOT NULL;
CREATE INDEX interrupts_status_expires ON interrupts(status,expires_at_ms);
CREATE INDEX interrupts_run_status ON interrupts(run_id,status);
CREATE INDEX interrupts_open_dispatch_due ON interrupts(status,dispatch_state,next_dispatch_at_ms);
CREATE TRIGGER interrupts_identity_immutable BEFORE UPDATE ON interrupts FOR EACH ROW BEGIN
 SELECT CASE WHEN NEW.id IS NOT OLD.id OR NEW.run_id IS NOT OLD.run_id OR NEW.attempt_no IS NOT OLD.attempt_no OR NEW.generation_key IS NOT OLD.generation_key OR NEW.reason IS NOT OLD.reason OR NEW.created_at_ms IS NOT OLD.created_at_ms THEN RAISE(ABORT,'interrupt identity is immutable') END;
END;
CREATE TABLE IF NOT EXISTS attention_batches_dummy (id TEXT);
DROP TABLE attention_batches_dummy;
ALTER TABLE attention_batches ADD COLUMN episode_admission_id TEXT;
PRAGMA foreign_keys=ON;
