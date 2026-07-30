-- 0057_rerun_checks_outbox_kind.sql
-- Add the rerun_checks outbox kind (storage.md §8.1, §8.2) so the Gate
-- re-evaluation retry_checks/flaky_retry successor can enqueue exactly one
-- rerun_checks operation plus its immutable check_rerun_consumptions row in the
-- same transaction. SQLite CHECK constraints are immutable, so outbox_operations
-- is rebuilt with the expanded closed kind set, preserving every column, FK,
-- CHECK, index and trigger, mirroring the 0054 rebuild. migrate.go applies this
-- with foreign_keys temporarily disabled and runs PRAGMA foreign_key_check
-- after commit.

PRAGMA foreign_keys=OFF;

DROP INDEX IF EXISTS outbox_operations_state_next;
DROP INDEX IF EXISTS outbox_operations_lease_expiry;
DROP TRIGGER IF EXISTS outbox_operations_payload_immutable;
DROP TRIGGER IF EXISTS channel_failure_alert_closed_payload;

CREATE TABLE outbox_operations_forward (
    id                     TEXT NOT NULL PRIMARY KEY,
    operation_key          TEXT NOT NULL UNIQUE,
    kind                   TEXT NOT NULL CHECK (kind IN ('forge_comment', 'forge_labels', 'create_change', 'merge_change', 'rerun_checks', 'channel_publish', 'launch_agent', 'command_ack', 'gate_re_evaluation', 'forge_alert')),
    run_id                 TEXT REFERENCES runs (id),
    attempt_no             INTEGER,
    interrupt_id           TEXT REFERENCES interrupts (id),
    state                  TEXT NOT NULL CHECK (state IN ('pending', 'executing', 'retryable', 'succeeded', 'failed', 'stale', 'conflict')),
    payload_schema_version INTEGER NOT NULL,
    payload_json           TEXT NOT NULL,
    payload_digest         TEXT NOT NULL,
    lease_owner            TEXT,
    lease_expires_at_ms    INTEGER,
    attempt_count          INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    next_attempt_at_ms     INTEGER NOT NULL,
    remote_evidence_json   TEXT,
    remote_evidence_digest TEXT,
    last_error_class       TEXT CHECK (last_error_class IN ('transient', 'rate_limited', 'auth_or_capability', 'contract_violation', 'semantic_conflict')),
    last_error_summary     TEXT,
    created_at_ms          INTEGER NOT NULL,
    updated_at_ms          INTEGER NOT NULL,
    completed_at_ms        INTEGER,
    version                INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
    FOREIGN KEY (run_id, attempt_no) REFERENCES attempts (run_id, attempt_no),
    CHECK (attempt_no IS NULL OR run_id IS NOT NULL),
    CHECK ((lease_owner IS NULL) = (lease_expires_at_ms IS NULL)),
    CHECK (state <> 'executing' OR (lease_owner IS NOT NULL AND lease_expires_at_ms IS NOT NULL))
);

INSERT INTO outbox_operations_forward
    (id, operation_key, kind, run_id, attempt_no, interrupt_id, state, payload_schema_version, payload_json, payload_digest,
     lease_owner, lease_expires_at_ms, attempt_count, next_attempt_at_ms, remote_evidence_json, remote_evidence_digest,
     last_error_class, last_error_summary, created_at_ms, updated_at_ms, completed_at_ms, version)
SELECT id, operation_key, kind, run_id, attempt_no, interrupt_id, state, payload_schema_version, payload_json, payload_digest,
       lease_owner, lease_expires_at_ms, attempt_count, next_attempt_at_ms, remote_evidence_json, remote_evidence_digest,
       last_error_class, last_error_summary, created_at_ms, updated_at_ms, completed_at_ms, version
FROM outbox_operations;

DROP TABLE outbox_operations;
ALTER TABLE outbox_operations_forward RENAME TO outbox_operations;

CREATE INDEX outbox_operations_state_next ON outbox_operations (state, next_attempt_at_ms);
CREATE INDEX outbox_operations_lease_expiry ON outbox_operations (lease_expires_at_ms);

-- outbox payload is fixed at creation; retries touch only execution fields.
CREATE TRIGGER outbox_operations_payload_immutable
BEFORE UPDATE ON outbox_operations FOR EACH ROW
WHEN NEW.payload_schema_version IS NOT OLD.payload_schema_version
    OR NEW.payload_json IS NOT OLD.payload_json
    OR NEW.payload_digest IS NOT OLD.payload_digest
BEGIN SELECT RAISE(ABORT, 'outbox payload is immutable'); END;

-- Keep Channel failure alerts as the closed forge_alert payload defined by
-- storage.md §6.6. Other forge_alert producers retain their existing contracts.
CREATE TRIGGER IF NOT EXISTS channel_failure_alert_closed_payload
BEFORE INSERT ON outbox_operations
WHEN NEW.kind='forge_alert' AND NEW.operation_key LIKE 'alert:channel_failure:%'
 AND (
   json_valid(NEW.payload_json)=0
   OR (SELECT count(*) FROM json_each(NEW.payload_json)) <> 7
   OR json_type(NEW.payload_json,'$.forge_host') <> 'text'
   OR json_type(NEW.payload_json,'$.forge_kind') <> 'text'
   OR json_type(NEW.payload_json,'$.forge_project_key') <> 'text'
   OR json_type(NEW.payload_json,'$.markdown') <> 'text'
   OR json_type(NEW.payload_json,'$.purpose') <> 'text'
   OR json_type(NEW.payload_json,'$.target_id') <> 'text'
   OR json_type(NEW.payload_json,'$.target_kind') <> 'text'
   OR json_extract(NEW.payload_json,'$.purpose') <> 'channel_failure'
   OR json_extract(NEW.payload_json,'$.forge_host') = ''
   OR json_extract(NEW.payload_json,'$.forge_kind') = ''
   OR json_extract(NEW.payload_json,'$.forge_project_key') = ''
   OR json_extract(NEW.payload_json,'$.markdown') = ''
   OR json_extract(NEW.payload_json,'$.target_id') = ''
   OR json_extract(NEW.payload_json,'$.target_kind') = ''
 )
BEGIN SELECT RAISE(ABORT,'invalid closed channel failure alert'); END;

-- check_rerun_consumptions (storage.md §8.2): one immutable row per Gate-created
-- rerun_checks operation, inserted in the same transaction, preventing a crash
-- recovery or concurrent Gate evaluation from double-consuming a flaky retry
-- quota. The PK (run_id, head_sha, check_run_id, retry_no) is at-most-one per
-- head/check/retry; operation_id is UNIQUE so a replayed operation cannot fork
-- a second consumption row.
CREATE TABLE check_rerun_consumptions (
    run_id        TEXT NOT NULL REFERENCES runs (id),
    head_sha      TEXT NOT NULL,
    check_run_id  TEXT NOT NULL,
    retry_no      INTEGER NOT NULL CHECK (retry_no >= 1),
    operation_id  TEXT NOT NULL UNIQUE REFERENCES outbox_operations (id),
    created_at_ms INTEGER NOT NULL,
    PRIMARY KEY (run_id, head_sha, check_run_id, retry_no)
);

CREATE TRIGGER check_rerun_consumptions_no_update
BEFORE UPDATE ON check_rerun_consumptions
BEGIN SELECT RAISE(ABORT, 'check rerun consumption is immutable'); END;

CREATE TRIGGER check_rerun_consumptions_no_delete
BEFORE DELETE ON check_rerun_consumptions
BEGIN SELECT RAISE(ABORT, 'check rerun consumption is immutable'); END;

PRAGMA foreign_keys=ON;
