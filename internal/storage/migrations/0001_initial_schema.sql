-- Migration 0001: initial M1 schema for sift.db.
--
-- Source of truth: docs/specs/storage.md (active) §2–§15, DESIGN §7.
-- Every table, enum CHECK, composite foreign key, append-only trigger and
-- index below exists because that spec names it. Do not add columns or
-- tables here without a matching spec change and a new migration.
--
-- Conventions (storage.md §2.1): ids are 32-char lowercase hex TEXT,
-- timestamps are Unix epoch milliseconds INTEGER (NULL when unset),
-- booleans are INTEGER restricted to 0/1, enums are TEXT with CHECK.
-- Columns allow NULL only where the spec says so; everything else is NOT
-- NULL, including composite-primary-key columns.

-- ---------------------------------------------------------------------------
-- §4 Configuration and project projections
-- ---------------------------------------------------------------------------

CREATE TABLE config_snapshots (
    id                TEXT NOT NULL PRIMARY KEY,
    config_hash       TEXT NOT NULL UNIQUE,
    schema_version    INTEGER NOT NULL,
    canonical_json    TEXT NOT NULL,
    source_present    INTEGER NOT NULL CHECK (source_present IN (0, 1)),
    source_mtime_ms   INTEGER,
    loaded_at_ms      INTEGER NOT NULL,
    binary_version    TEXT NOT NULL
);

CREATE TABLE daemon_boots (
    id                 TEXT NOT NULL PRIMARY KEY,
    config_snapshot_id TEXT NOT NULL REFERENCES config_snapshots (id),
    pid                INTEGER NOT NULL,
    binary_version     TEXT NOT NULL,
    protocol_major     INTEGER NOT NULL,
    started_at_ms      INTEGER NOT NULL,
    stopped_at_ms      INTEGER,
    stop_reason        TEXT
);

CREATE TABLE projects (
    id                         TEXT NOT NULL PRIMARY KEY,
    config_snapshot_id         TEXT NOT NULL REFERENCES config_snapshots (id),
    forge_kind                 TEXT NOT NULL CHECK (forge_kind IN ('github', 'gitlab')),
    forge_host                 TEXT NOT NULL,
    forge_project_key          TEXT NOT NULL,
    repo_path                  TEXT NOT NULL,
    enabled                    INTEGER NOT NULL CHECK (enabled IN (0, 1)),
    health                     TEXT NOT NULL CHECK (health IN ('active', 'isolated')),
    isolation_reason           TEXT CHECK (isolation_reason IN (
        'config_invalid', 'repo_invalid', 'agent_unavailable',
        'forge_auth_or_capability', 'policy_invalid')),
    capabilities_json          TEXT NOT NULL DEFAULT '{}',
    capabilities_checked_at_ms INTEGER,
    created_at_ms              INTEGER NOT NULL,
    updated_at_ms              INTEGER NOT NULL,
    UNIQUE (forge_kind, forge_host, forge_project_key),
    CHECK ((health = 'active' AND isolation_reason IS NULL)
        OR (health = 'isolated' AND isolation_reason IS NOT NULL))
);

-- Enabled projects hold a unique normalized repo_path.
CREATE UNIQUE INDEX projects_enabled_repo_path ON projects (repo_path) WHERE enabled = 1;

CREATE TABLE project_hook_baselines (
    project_id             TEXT NOT NULL PRIMARY KEY REFERENCES projects (id),
    git_config_digest      TEXT NOT NULL,
    core_hooks_path_value  TEXT,
    effective_hooks_path   TEXT NOT NULL,
    hooks_directory_digest TEXT NOT NULL,
    baseline_digest        TEXT NOT NULL,
    source_run_id          TEXT REFERENCES runs (id),
    source_attempt_no      INTEGER,
    captured_at_ms         INTEGER NOT NULL,
    updated_at_ms          INTEGER NOT NULL,
    FOREIGN KEY (source_run_id, source_attempt_no)
        REFERENCES attempts (run_id, attempt_no),
    -- Initial baselines have both source columns NULL; an attempt source has both set.
    CHECK ((source_run_id IS NULL) = (source_attempt_no IS NULL))
);

-- ---------------------------------------------------------------------------
-- §5 Run and attempt
-- ---------------------------------------------------------------------------

CREATE TABLE task_spec_snapshots (
    id              TEXT NOT NULL PRIMARY KEY,
    run_id          TEXT NOT NULL REFERENCES runs (id),
    version         INTEGER NOT NULL CHECK (version >= 1),
    schema_version  INTEGER NOT NULL,
    canonical_json  TEXT NOT NULL,
    content_digest  TEXT NOT NULL,
    source_event_id TEXT REFERENCES events (id),
    created_at_ms   INTEGER NOT NULL,
    UNIQUE (run_id, version),
    -- Candidate key for composite foreign keys (runs.current_task_spec_id,
    -- attempts.task_spec_snapshot_id).
    UNIQUE (run_id, id)
);

CREATE TABLE runs (
    id                   TEXT NOT NULL PRIMARY KEY,
    source_kind          TEXT NOT NULL CHECK (source_kind IN ('forge', 'manual')),
    project_id           TEXT NOT NULL REFERENCES projects (id),
    config_snapshot_id   TEXT NOT NULL REFERENCES config_snapshots (id),
    forge_kind           TEXT CHECK (forge_kind IN ('github', 'gitlab')),
    forge_host           TEXT,
    forge_project_key    TEXT,
    issue_id             TEXT,
    issue_url            TEXT,
    issue_author         TEXT,
    status               TEXT NOT NULL CHECK (status IN ('queued', 'running', 'waiting_human', 'done', 'failed')),
    version              INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
    kind                 TEXT,
    agent_id             TEXT,
    hitl_before_start    INTEGER NOT NULL DEFAULT 0 CHECK (hitl_before_start IN (0, 1)),
    current_task_spec_id TEXT,
    retry_count          INTEGER NOT NULL DEFAULT 0 CHECK (retry_count >= 0),
    max_attempts         INTEGER NOT NULL CHECK (max_attempts >= 1),
    change_id            TEXT,
    change_url           TEXT,
    change_head_sha      TEXT,
    gate_bypassed        INTEGER NOT NULL DEFAULT 0 CHECK (gate_bypassed IN (0, 1)),
    failure_reason       TEXT CHECK (failure_reason IN (
        'closed_upstream', 'change_closed', 'untriggered', 'hard_guardrail',
        'agent_exit', 'attempts_exhausted', 'human_reject', 'hitl_expired',
        'operator_kill', 'contract_violation', 'no_change')),
    created_at_ms        INTEGER NOT NULL,
    updated_at_ms        INTEGER NOT NULL,
    completed_at_ms      INTEGER,
    FOREIGN KEY (id, current_task_spec_id)
        REFERENCES task_spec_snapshots (run_id, id),
    -- forge sources carry project/forge/issue; manual sources still bind
    -- project/forge but never carry issue fields (storage.md §5.2).
    CHECK ((source_kind = 'forge'
            AND forge_kind IS NOT NULL AND forge_host IS NOT NULL
            AND forge_project_key IS NOT NULL AND issue_id IS NOT NULL)
        OR (source_kind = 'manual'
            AND forge_kind IS NOT NULL AND forge_host IS NOT NULL
            AND forge_project_key IS NOT NULL
            AND issue_id IS NULL AND issue_url IS NULL AND issue_author IS NULL)),
    CHECK (status <> 'done' OR change_id IS NOT NULL),
    CHECK ((status IN ('done', 'failed') AND completed_at_ms IS NOT NULL)
        OR (status NOT IN ('done', 'failed') AND completed_at_ms IS NULL))
);

-- Intake idempotency key (only forge-sourced runs carry an issue).
CREATE UNIQUE INDEX runs_intake_idempotency
    ON runs (forge_kind, forge_host, forge_project_key, issue_id)
    WHERE issue_id IS NOT NULL;

CREATE TABLE attempts (
    run_id                   TEXT NOT NULL REFERENCES runs (id) ON DELETE RESTRICT,
    attempt_no               INTEGER NOT NULL CHECK (attempt_no >= 1),
    phase                    TEXT NOT NULL CHECK (phase IN ('pending', 'starting', 'spawning', 'running', 'finished', 'orphaned')),
    generation               INTEGER NOT NULL CHECK (generation >= 1),
    backend                  TEXT NOT NULL CHECK (backend IN ('process', 'tmux')),
    agent_id                 TEXT NOT NULL,
    task_spec_snapshot_id    TEXT NOT NULL,
    worktree_path            TEXT NOT NULL,
    branch_name              TEXT NOT NULL,
    base_ref                 TEXT NOT NULL,
    base_sha                 TEXT NOT NULL,
    final_head_sha           TEXT,
    wrapper_pid              INTEGER,
    wrapper_started_at_ms    INTEGER,
    wrapper_executable       TEXT,
    wrapper_pgid             INTEGER,
    wrapper_instance_id      TEXT,
    agent_pid                INTEGER,
    agent_started_at_ms      INTEGER,
    agent_executable         TEXT,
    control_nonce_hash       TEXT,
    heartbeat_at_ms          INTEGER,
    result_exit_code         INTEGER,
    result_signal            TEXT,
    result_digest            TEXT,
    result_observed_at_ms    INTEGER,
    attempt_resolution       TEXT CHECK (attempt_resolution IN ('reject', 'retry_after_absence')),
    resolution_at_ms         INTEGER,
    isolation_state          TEXT NOT NULL DEFAULT 'none' CHECK (isolation_state IN ('none', 'frozen')),
    isolation_reason         TEXT CHECK (isolation_reason IN (
        'startup_stall', 'process_identity_unknown', 'termination_unconfirmed',
        'process_group_unverified', 'late_execution_fact')),
    isolated_at_ms           INTEGER,
    isolation_released_at_ms INTEGER,
    created_at_ms            INTEGER NOT NULL,
    updated_at_ms            INTEGER NOT NULL,
    finished_at_ms           INTEGER,
    PRIMARY KEY (run_id, attempt_no),
    FOREIGN KEY (run_id, task_spec_snapshot_id)
        REFERENCES task_spec_snapshots (run_id, id),
    -- Wrapper identity is all-or-none; the agent identity triple likewise.
    CHECK ((wrapper_pid IS NULL AND wrapper_started_at_ms IS NULL AND wrapper_executable IS NULL
            AND wrapper_pgid IS NULL AND wrapper_instance_id IS NULL)
        OR (wrapper_pid IS NOT NULL AND wrapper_started_at_ms IS NOT NULL AND wrapper_executable IS NOT NULL
            AND wrapper_pgid IS NOT NULL AND wrapper_instance_id IS NOT NULL)),
    CHECK ((agent_pid IS NULL AND agent_started_at_ms IS NULL AND agent_executable IS NULL)
        OR (agent_pid IS NOT NULL AND agent_started_at_ms IS NOT NULL AND agent_executable IS NOT NULL)),
    CHECK (phase <> 'running' OR agent_pid IS NOT NULL),
    CHECK (phase <> 'finished' OR (result_digest IS NOT NULL AND result_observed_at_ms IS NOT NULL
        AND ((result_exit_code IS NULL) <> (result_signal IS NULL)))),
    -- Resolution and its timestamp are written together, once.
    CHECK ((attempt_resolution IS NULL) = (resolution_at_ms IS NULL)),
    CHECK ((isolation_state = 'frozen' AND isolation_reason IS NOT NULL
            AND isolated_at_ms IS NOT NULL AND isolation_released_at_ms IS NULL)
        OR (isolation_state = 'none'))
);

-- At most one live (pre-terminal) attempt per Run.
CREATE UNIQUE INDEX attempts_single_live_phase ON attempts (run_id)
    WHERE phase IN ('pending', 'starting', 'spawning', 'running');

CREATE TABLE attempt_claims (
    run_id                  TEXT NOT NULL,
    attempt_no              INTEGER NOT NULL,
    generation              INTEGER NOT NULL CHECK (generation >= 1),
    launch_operation_key    TEXT NOT NULL UNIQUE,
    dispatch_id             TEXT,
    bootstrap_nonce_hash    TEXT,
    run_token_hash          TEXT,
    wrapper_instance_id     TEXT,
    wrapper_session_hash    TEXT,
    spawn_permit_hash       TEXT,
    acquired_at_ms          INTEGER,
    permit_issued_at_ms     INTEGER,
    started_confirmed_at_ms INTEGER,
    created_at_ms           INTEGER NOT NULL,
    updated_at_ms           INTEGER NOT NULL,
    PRIMARY KEY (run_id, attempt_no),
    FOREIGN KEY (run_id, attempt_no) REFERENCES attempts (run_id, attempt_no),
    -- dispatch id, bootstrap nonce hash and run token hash are prepared
    -- together by PrepareLaunchDispatch (storage.md §5.4).
    CHECK ((dispatch_id IS NULL AND bootstrap_nonce_hash IS NULL AND run_token_hash IS NULL)
        OR (dispatch_id IS NOT NULL AND bootstrap_nonce_hash IS NOT NULL AND run_token_hash IS NOT NULL))
);

CREATE TABLE attempt_probes (
    id                      TEXT NOT NULL PRIMARY KEY,
    run_id                  TEXT NOT NULL,
    attempt_no              INTEGER NOT NULL,
    interrupt_id            TEXT NOT NULL REFERENCES interrupts (id),
    state                   TEXT NOT NULL CHECK (state IN ('pending', 'running', 'succeeded', 'failed', 'superseded')),
    expected_run_version    INTEGER NOT NULL,
    expected_generation     INTEGER NOT NULL,
    requested_by_event_id   TEXT NOT NULL REFERENCES events (id),
    absence_evidence_json   TEXT,
    absence_evidence_digest TEXT,
    created_at_ms           INTEGER NOT NULL,
    started_at_ms           INTEGER,
    finished_at_ms          INTEGER,
    FOREIGN KEY (run_id, attempt_no) REFERENCES attempts (run_id, attempt_no),
    CHECK ((absence_evidence_json IS NULL) = (absence_evidence_digest IS NULL))
);

-- Each Interrupt carries at most one pending/running probe.
CREATE UNIQUE INDEX attempt_probes_one_live_per_interrupt ON attempt_probes (interrupt_id)
    WHERE state IN ('pending', 'running');

-- ---------------------------------------------------------------------------
-- §6 Interrupt
-- ---------------------------------------------------------------------------

CREATE TABLE interrupts (
    id                      TEXT NOT NULL PRIMARY KEY,
    run_id                  TEXT NOT NULL REFERENCES runs (id),
    attempt_no              INTEGER,
    generation_key          TEXT NOT NULL UNIQUE,
    reason                  TEXT NOT NULL CHECK (reason IN (
        'design_approval', 'guardrail_violation', 'code_review', 'agent_blocked',
        'merge_conflict', 'failure_review', 'startup_stall')),
    severity                TEXT NOT NULL CHECK (severity IN ('low', 'normal', 'high', 'critical')),
    headline                TEXT NOT NULL CHECK (length(headline) <= 40),
    brief_markdown          TEXT NOT NULL,
    options_json            TEXT NOT NULL,
    min_modality            TEXT NOT NULL CHECK (min_modality IN ('voice', 'text', 'visual')),
    links_json              TEXT NOT NULL DEFAULT '[]',
    nonce                   TEXT NOT NULL,
    version                 INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
    status                  TEXT NOT NULL CHECK (status IN ('open', 'closed')),
    dispatch_state          TEXT NOT NULL CHECK (dispatch_state IN ('ready', 'batched', 'held', 'probe_in_progress')),
    expires_at_ms           INTEGER NOT NULL,
    on_expire               TEXT NOT NULL CHECK (on_expire IN ('hold', 'escalate', 'auto_reject')),
    escalation_count        INTEGER NOT NULL DEFAULT 0 CHECK (escalation_count >= 0),
    max_escalations         INTEGER NOT NULL CHECK (max_escalations >= 0),
    close_reason            TEXT CHECK (close_reason IN (
        'responded', 'expired_auto_reject', 'superseded_by_fact',
        'superseded_by_decision', 'external_fact')),
    closed_at_ms            INTEGER,
    charged_budget_entry_id TEXT NOT NULL UNIQUE REFERENCES budget_entries (id),
    created_at_ms           INTEGER NOT NULL,
    updated_at_ms           INTEGER NOT NULL,
    FOREIGN KEY (run_id, attempt_no) REFERENCES attempts (run_id, attempt_no),
    CHECK ((status = 'open' AND close_reason IS NULL AND closed_at_ms IS NULL)
        OR (status = 'closed' AND close_reason IS NOT NULL AND closed_at_ms IS NOT NULL)),
    -- startup_stall never auto-rejects (PRD §4.2/§4.3).
    CHECK (reason <> 'startup_stall' OR on_expire <> 'auto_reject')
);

CREATE TABLE interrupt_deliveries (
    id             TEXT NOT NULL PRIMARY KEY,
    interrupt_id   TEXT NOT NULL REFERENCES interrupts (id),
    surface        TEXT NOT NULL CHECK (surface IN ('forge_comment', 'channel')),
    priority       TEXT NOT NULL CHECK (priority IN ('normal', 'strong')),
    operation_key  TEXT NOT NULL UNIQUE,
    state          TEXT NOT NULL CHECK (state IN ('pending', 'delivered', 'failed')),
    attempt_count  INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    remote_ref     TEXT,
    last_error     TEXT,
    created_at_ms  INTEGER NOT NULL,
    delivered_at_ms INTEGER
);

-- ---------------------------------------------------------------------------
-- §7 Append-only events and idempotency receipts
-- ---------------------------------------------------------------------------

CREATE TABLE events (
    seq                    INTEGER PRIMARY KEY AUTOINCREMENT,
    id                     TEXT NOT NULL UNIQUE,
    run_id                 TEXT REFERENCES runs (id),
    attempt_no             INTEGER,
    project_id             TEXT REFERENCES projects (id),
    type                   TEXT NOT NULL,
    source                 TEXT NOT NULL CHECK (source IN ('system', 'forge', 'operator', 'agent', 'recovery')),
    actor                  TEXT,
    payload_schema_version INTEGER NOT NULL,
    payload_json           TEXT NOT NULL,
    idempotency_key        TEXT UNIQUE,
    occurred_at_ms         INTEGER NOT NULL,
    recorded_at_ms         INTEGER NOT NULL
);

CREATE TABLE forge_cursors (
    project_id       TEXT NOT NULL REFERENCES projects (id),
    stream           TEXT NOT NULL CHECK (stream IN ('issues', 'issue_comments', 'issue_labels', 'changes', 'change_comments', 'checks')),
    cursor           TEXT,
    etag             TEXT,
    since_ms         INTEGER,
    poll_mode        TEXT NOT NULL CHECK (poll_mode IN ('idle', 'active', 'interrupt', 'slow')),
    next_poll_at_ms  INTEGER NOT NULL,
    updated_at_ms    INTEGER NOT NULL,
    PRIMARY KEY (project_id, stream)
);

CREATE TABLE forge_event_receipts (
    id              TEXT NOT NULL PRIMARY KEY,
    project_id      TEXT NOT NULL REFERENCES projects (id),
    forge_event_id  TEXT NOT NULL,
    event_kind      TEXT NOT NULL,
    target_kind     TEXT NOT NULL CHECK (target_kind IN ('issue', 'change')),
    target_id       TEXT NOT NULL,
    actor           TEXT,
    raw_digest      TEXT NOT NULL,
    disposition     TEXT NOT NULL CHECK (disposition IN ('accepted', 'fact_observed', 'ignored_untrusted_actor', 'ignored_missing_actor')),
    domain_event_id TEXT REFERENCES events (id),
    observed_at_ms  INTEGER NOT NULL,
    UNIQUE (project_id, forge_event_id)
);

CREATE TABLE report_receipts (
    id             TEXT NOT NULL PRIMARY KEY,
    run_id         TEXT NOT NULL,
    attempt_no     INTEGER NOT NULL,
    report_key     TEXT NOT NULL,
    report_kind    TEXT NOT NULL CHECK (report_kind IN ('progress', 'goal', 'blocker', 'completed')),
    payload_digest TEXT NOT NULL,
    event_id       TEXT NOT NULL REFERENCES events (id),
    received_at_ms INTEGER NOT NULL,
    FOREIGN KEY (run_id, attempt_no) REFERENCES attempts (run_id, attempt_no),
    UNIQUE (run_id, attempt_no, report_key)
);

CREATE TABLE intake_items (
    id                         TEXT NOT NULL PRIMARY KEY,
    project_id                 TEXT NOT NULL REFERENCES projects (id),
    forge_kind                 TEXT NOT NULL CHECK (forge_kind IN ('github', 'gitlab')),
    normalized_host            TEXT NOT NULL,
    forge_project_key          TEXT NOT NULL,
    issue_id                   TEXT NOT NULL,
    issue_url                  TEXT NOT NULL,
    issue_digest               TEXT NOT NULL,
    state                      TEXT NOT NULL CHECK (state IN ('pending_evaluation', 'evaluating', 'awaiting_clarification', 'awaiting_duplicate_confirmation', 'ready', 'consumed')),
    version                    INTEGER NOT NULL CHECK (version >= 1),
    latest_assessment_id       TEXT,
    linked_run_id              TEXT REFERENCES runs (id),
    duplicate_candidate_run_id TEXT REFERENCES runs (id),
    clarification_generation   INTEGER NOT NULL DEFAULT 0 CHECK (clarification_generation >= 0),
    created_at_ms              INTEGER NOT NULL,
    updated_at_ms              INTEGER NOT NULL,
    UNIQUE (forge_kind, normalized_host, forge_project_key, issue_id),
    FOREIGN KEY (id, latest_assessment_id)
        REFERENCES intake_assessments (intake_id, id) DEFERRABLE INITIALLY DEFERRED,
    CHECK ((state = 'consumed') = (linked_run_id IS NOT NULL)),
    CHECK (state NOT IN ('awaiting_clarification', 'awaiting_duplicate_confirmation')
        OR (latest_assessment_id IS NOT NULL AND clarification_generation >= 1)),
    CHECK (state <> 'awaiting_duplicate_confirmation' OR duplicate_candidate_run_id IS NOT NULL)
);

CREATE TABLE intake_assessments (
    id                       TEXT NOT NULL PRIMARY KEY,
    intake_id                TEXT NOT NULL REFERENCES intake_items (id),
    logical_call_id          TEXT NOT NULL REFERENCES brain_calls (id),
    disposition              TEXT NOT NULL CHECK (disposition IN ('ready', 'needs_clarification', 'possible_duplicate')),
    questions_json           TEXT NOT NULL,
    possible_duplicate_run_id TEXT,
    rationale                TEXT NOT NULL,
    created_at_ms            INTEGER NOT NULL,
    -- Candidate key for intake_items.latest_assessment_id composite FK.
    UNIQUE (intake_id, id),
    -- T1 output mutual-exclusion matrix (storage.md §7.6).
    CHECK ((disposition = 'ready' AND questions_json = '[]' AND possible_duplicate_run_id IS NULL)
        OR (disposition = 'needs_clarification' AND questions_json <> '[]' AND possible_duplicate_run_id IS NULL)
        OR (disposition = 'possible_duplicate' AND possible_duplicate_run_id IS NOT NULL AND questions_json = '[]'))
);

-- ---------------------------------------------------------------------------
-- §8 Transactional outbox
-- ---------------------------------------------------------------------------

CREATE TABLE outbox_operations (
    id                     TEXT NOT NULL PRIMARY KEY,
    operation_key          TEXT NOT NULL UNIQUE,
    kind                   TEXT NOT NULL CHECK (kind IN ('forge_comment', 'forge_labels', 'create_change', 'merge_change', 'channel_publish', 'launch_agent', 'command_ack', 'forge_alert')),
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
    FOREIGN KEY (run_id, attempt_no) REFERENCES attempts (run_id, attempt_no),
    CHECK (attempt_no IS NULL OR run_id IS NOT NULL),
    CHECK ((lease_owner IS NULL) = (lease_expires_at_ms IS NULL)),
    CHECK (state <> 'executing' OR (lease_owner IS NOT NULL AND lease_expires_at_ms IS NOT NULL))
);

CREATE TABLE outbox_attempts (
    id            TEXT NOT NULL PRIMARY KEY,
    operation_id  TEXT NOT NULL REFERENCES outbox_operations (id),
    attempt_no    INTEGER NOT NULL CHECK (attempt_no >= 1),
    worker_id     TEXT NOT NULL,
    started_at_ms INTEGER NOT NULL,
    UNIQUE (operation_id, attempt_no)
);

CREATE TABLE outbox_attempt_results (
    attempt_id     TEXT NOT NULL PRIMARY KEY REFERENCES outbox_attempts (id),
    finished_at_ms INTEGER NOT NULL,
    outcome        TEXT NOT NULL CHECK (outcome IN ('success', 'retry', 'failed', 'stale', 'conflict')),
    error_class    TEXT CHECK (error_class IN ('transient', 'rate_limited', 'auth_or_capability', 'contract_violation', 'semantic_conflict')),
    error_summary  TEXT,
    evidence_digest TEXT
);

-- ---------------------------------------------------------------------------
-- §9 Budgets
-- ---------------------------------------------------------------------------

CREATE TABLE budget_counters (
    kind            TEXT NOT NULL CHECK (kind IN ('token', 'forge_api', 'attention', 'report')),
    scope           TEXT NOT NULL CHECK (scope IN ('global', 'project', 'run', 'severity')),
    scope_id        TEXT NOT NULL,
    bucket_start_ms INTEGER NOT NULL,
    bucket_end_ms   INTEGER NOT NULL,
    limit_value     INTEGER NOT NULL CHECK (limit_value >= 0),
    consumed_value  INTEGER NOT NULL DEFAULT 0 CHECK (consumed_value >= 0),
    version         INTEGER NOT NULL CHECK (version >= 1),
    updated_at_ms   INTEGER NOT NULL,
    PRIMARY KEY (kind, scope, scope_id, bucket_start_ms),
    CHECK (bucket_end_ms > bucket_start_ms)
);

CREATE TABLE rate_limit_buckets (
    kind              TEXT NOT NULL CHECK (kind IN ('report')),
    scope_id          TEXT NOT NULL,
    capacity_units    INTEGER NOT NULL CHECK (capacity_units >= 0),
    available_units   INTEGER NOT NULL CHECK (available_units >= 0 AND available_units <= capacity_units),
    refill_numerator  INTEGER NOT NULL CHECK (refill_numerator >= 1),
    refill_period_ms  INTEGER NOT NULL CHECK (refill_period_ms >= 1),
    refill_remainder  INTEGER NOT NULL DEFAULT 0 CHECK (refill_remainder >= 0),
    last_refill_at_ms INTEGER NOT NULL,
    version           INTEGER NOT NULL CHECK (version >= 1),
    PRIMARY KEY (kind, scope_id)
);

CREATE TABLE budget_entries (
    id              TEXT NOT NULL PRIMARY KEY,
    kind            TEXT NOT NULL CHECK (kind IN ('token', 'forge_api', 'attention', 'report')),
    scope           TEXT NOT NULL CHECK (scope IN ('global', 'project', 'run', 'severity')),
    scope_id        TEXT NOT NULL,
    bucket_start_ms INTEGER NOT NULL,
    amount          INTEGER NOT NULL CHECK (amount >= 1),
    reason          TEXT NOT NULL,
    run_id          TEXT REFERENCES runs (id),
    operation_key   TEXT NOT NULL UNIQUE,
    created_at_ms   INTEGER NOT NULL
);

-- ---------------------------------------------------------------------------
-- §10 Brain, Gate, calibration and Ledger
-- ---------------------------------------------------------------------------

CREATE TABLE brain_call_counters (
    scope         TEXT NOT NULL CHECK (scope IN ('intake', 'run', 'aggregate')),
    subject_key   TEXT NOT NULL,
    touchpoint    TEXT NOT NULL CHECK (touchpoint IN ('T1', 'T2', 'T3', 'T4', 'T5', 'T6', 'T7')),
    next_call_seq INTEGER NOT NULL CHECK (next_call_seq >= 1),
    version       INTEGER NOT NULL CHECK (version >= 1),
    updated_at_ms INTEGER NOT NULL,
    PRIMARY KEY (scope, subject_key, touchpoint)
);

CREATE TABLE brain_calls (
    id                    TEXT NOT NULL PRIMARY KEY,
    scope                 TEXT NOT NULL CHECK (scope IN ('intake', 'run', 'aggregate')),
    subject_key           TEXT NOT NULL,
    project_id            TEXT REFERENCES projects (id),
    run_id                TEXT REFERENCES runs (id),
    attempt_no            INTEGER,
    touchpoint            TEXT NOT NULL CHECK (touchpoint IN ('T1', 'T2', 'T3', 'T4', 'T5', 'T6', 'T7')),
    call_seq              INTEGER NOT NULL CHECK (call_seq >= 1),
    prompt_version        TEXT NOT NULL,
    output_schema_version INTEGER NOT NULL,
    input_json            TEXT NOT NULL,
    input_digest          TEXT NOT NULL,
    status                TEXT NOT NULL CHECK (status IN ('running', 'valid', 'fallback')),
    selected_attempt_no   INTEGER,
    fallback_reason       TEXT,
    validated_output_json TEXT,
    gate_input_snapshot_id TEXT REFERENCES gate_input_snapshots (id),
    started_at_ms         INTEGER NOT NULL,
    finished_at_ms        INTEGER,
    UNIQUE (scope, subject_key, touchpoint, call_seq),
    FOREIGN KEY (run_id, attempt_no) REFERENCES attempts (run_id, attempt_no),
    FOREIGN KEY (id, selected_attempt_no)
        REFERENCES brain_attempts (logical_call_id, provider_attempt) DEFERRABLE INITIALLY DEFERRED,
    CHECK (attempt_no IS NULL OR run_id IS NOT NULL),
    -- Touchpoint scope rules as database CHECKs, not caller discipline
    -- (storage.md §10.1): T1 intake scope with project and no run; T2 run
    -- scope without attempt; T3–T6 run scope, attempt optional; T7 aggregate
    -- scope without run/attempt.
    CHECK ((touchpoint = 'T1' AND scope = 'intake' AND project_id IS NOT NULL
            AND run_id IS NULL AND attempt_no IS NULL)
        OR (touchpoint = 'T2' AND scope = 'run' AND run_id IS NOT NULL AND attempt_no IS NULL)
        OR (touchpoint IN ('T3', 'T4', 'T5', 'T6') AND scope = 'run' AND run_id IS NOT NULL)
        OR (touchpoint = 'T7' AND scope = 'aggregate' AND run_id IS NULL AND attempt_no IS NULL)),
    -- Single-finalize lifecycle (storage.md §10.1).
    CHECK ((status = 'running' AND selected_attempt_no IS NULL AND fallback_reason IS NULL
            AND validated_output_json IS NULL AND finished_at_ms IS NULL)
        OR (status = 'valid' AND selected_attempt_no IS NOT NULL AND validated_output_json IS NOT NULL
            AND finished_at_ms IS NOT NULL AND fallback_reason IS NULL)
        OR (status = 'fallback' AND fallback_reason IS NOT NULL AND finished_at_ms IS NOT NULL
            AND selected_attempt_no IS NULL))
);

CREATE TABLE brain_attempts (
    id                  TEXT NOT NULL PRIMARY KEY,
    logical_call_id     TEXT NOT NULL REFERENCES brain_calls (id),
    provider_attempt    INTEGER NOT NULL CHECK (provider_attempt BETWEEN 0 AND 2),
    outcome             TEXT NOT NULL CHECK (outcome IN ('valid', 'invalid_output', 'provider_error', 'fallback')),
    provider_error_code TEXT CHECK (provider_error_code IN (
        'timeout', 'nonzero_exit', 'output_too_large', 'invalid_envelope',
        'usage_missing', 'usage_invalid', 'spawn_failed')),
    request_digest      TEXT NOT NULL,
    raw_output_text     TEXT,
    raw_output_digest   TEXT,
    raw_output_bytes    INTEGER,
    raw_output_truncated INTEGER NOT NULL DEFAULT 0 CHECK (raw_output_truncated IN (0, 1)),
    stderr_summary      TEXT,
    stderr_truncated    INTEGER NOT NULL DEFAULT 0 CHECK (stderr_truncated IN (0, 1)),
    exit_code           INTEGER,
    input_tokens        INTEGER CHECK (input_tokens IS NULL OR input_tokens >= 0),
    output_tokens       INTEGER CHECK (output_tokens IS NULL OR output_tokens >= 0),
    started_at_ms       INTEGER NOT NULL,
    finished_at_ms      INTEGER NOT NULL,
    -- Candidate key for brain_calls.selected_attempt_no composite FK.
    UNIQUE (logical_call_id, provider_attempt),
    -- provider_attempt 0 is the synthesized fallback record: no provider ever ran.
    CHECK (provider_attempt <> 0 OR (outcome = 'fallback' AND provider_error_code IS NULL
        AND raw_output_text IS NULL AND raw_output_digest IS NULL AND raw_output_bytes IS NULL
        AND stderr_summary IS NULL AND exit_code IS NULL
        AND input_tokens IS NULL AND output_tokens IS NULL)),
    CHECK ((outcome = 'provider_error') = (provider_error_code IS NOT NULL)),
    CHECK (outcome <> 'valid' OR (input_tokens IS NOT NULL AND output_tokens IS NOT NULL
        AND raw_output_digest IS NOT NULL)),
    CHECK (provider_error_code IS NULL OR provider_error_code <> 'output_too_large'
        OR raw_output_truncated = 1)
);

CREATE TABLE gate_input_snapshots (
    id                    TEXT NOT NULL PRIMARY KEY,
    gate_input_hash       TEXT NOT NULL UNIQUE,
    schema_version        INTEGER NOT NULL,
    canonical_json        TEXT NOT NULL,
    head_sha              TEXT NOT NULL,
    effective_policy_hash TEXT NOT NULL,
    certification_version TEXT NOT NULL,
    risk_source_version   TEXT NOT NULL,
    created_at_ms         INTEGER NOT NULL
);

CREATE TABLE gate_evaluations (
    id             TEXT NOT NULL PRIMARY KEY,
    run_id         TEXT NOT NULL REFERENCES runs (id),
    snapshot_id    TEXT NOT NULL REFERENCES gate_input_snapshots (id),
    gate_version   TEXT NOT NULL,
    verdict_json   TEXT NOT NULL,
    verdict_digest TEXT NOT NULL,
    cache_hit      INTEGER NOT NULL CHECK (cache_hit IN (0, 1)),
    created_at_ms  INTEGER NOT NULL
);

CREATE TABLE gate_cache (
    gate_input_hash TEXT NOT NULL,
    gate_version    TEXT NOT NULL,
    snapshot_id     TEXT NOT NULL REFERENCES gate_input_snapshots (id),
    verdict_json    TEXT NOT NULL,
    verdict_digest  TEXT NOT NULL,
    created_at_ms   INTEGER NOT NULL,
    PRIMARY KEY (gate_input_hash, gate_version)
);

CREATE TABLE calibration_entries (
    id                 TEXT NOT NULL PRIMARY KEY,
    run_id             TEXT NOT NULL REFERENCES runs (id),
    gate_evaluation_id TEXT NOT NULL REFERENCES gate_evaluations (id),
    predicted_decision TEXT NOT NULL,
    human_decision     TEXT,
    decision_source    TEXT CHECK (decision_source IN ('command', 'manual_merge', 'manual_close')),
    gate_bypassed      INTEGER NOT NULL DEFAULT 0 CHECK (gate_bypassed IN (0, 1)),
    features_json      TEXT NOT NULL,
    predicted_at_ms    INTEGER NOT NULL,
    decided_at_ms      INTEGER,
    -- The human outcome is completed exactly once, as a group.
    CHECK ((human_decision IS NULL AND decision_source IS NULL AND decided_at_ms IS NULL)
        OR (human_decision IS NOT NULL AND decision_source IS NOT NULL AND decided_at_ms IS NOT NULL))
);

CREATE TABLE certifications (
    task_kind             TEXT NOT NULL,
    certification_version TEXT NOT NULL,
    total_samples         INTEGER NOT NULL CHECK (total_samples >= 0),
    negative_samples      INTEGER NOT NULL CHECK (negative_samples >= 0),
    leak_count            INTEGER NOT NULL CHECK (leak_count >= 0),
    false_block_count     INTEGER NOT NULL CHECK (false_block_count >= 0),
    certified             INTEGER NOT NULL CHECK (certified IN (0, 1)),
    evidence_digest       TEXT NOT NULL,
    updated_at_ms         INTEGER NOT NULL,
    PRIMARY KEY (task_kind, certification_version)
);

CREATE TABLE ledger_entries (
    id                      TEXT NOT NULL PRIMARY KEY,
    run_id                  TEXT NOT NULL REFERENCES runs (id),
    interrupt_id            TEXT REFERENCES interrupts (id),
    entry_kind              TEXT NOT NULL CHECK (entry_kind IN ('human_decision', 'attention_delivery', 'semantic_material', 'gate_sample')),
    features_schema_version INTEGER NOT NULL,
    features_json           TEXT NOT NULL,
    natural_language        TEXT,
    created_at_ms           INTEGER NOT NULL
);

-- ---------------------------------------------------------------------------
-- §15 M1 index minimum set (unique-constraint indexes are implicit)
-- ---------------------------------------------------------------------------

CREATE INDEX task_spec_snapshots_run_version ON task_spec_snapshots (run_id, version DESC);
CREATE INDEX runs_status_updated ON runs (status, updated_at_ms);
CREATE INDEX runs_project_status ON runs (project_id, status);
CREATE INDEX runs_change_id ON runs (change_id);
CREATE INDEX project_hook_baselines_updated ON project_hook_baselines (updated_at_ms);
CREATE INDEX attempts_phase_updated ON attempts (phase, updated_at_ms);
CREATE INDEX attempts_run_attempt_desc ON attempts (run_id, attempt_no DESC);
CREATE INDEX interrupts_status_expires ON interrupts (status, expires_at_ms);
CREATE INDEX interrupts_run_status ON interrupts (run_id, status);
CREATE INDEX events_run_seq ON events (run_id, seq);
CREATE INDEX events_project_seq ON events (project_id, seq);
CREATE INDEX outbox_operations_state_next ON outbox_operations (state, next_attempt_at_ms);
CREATE INDEX outbox_operations_lease_expiry ON outbox_operations (lease_expires_at_ms);
-- critical fuse sliding window (storage.md §9.3).
CREATE INDEX budget_entries_kind_created_run ON budget_entries (kind, created_at_ms, run_id);
CREATE INDEX forge_cursors_next_poll ON forge_cursors (next_poll_at_ms);
CREATE INDEX brain_calls_run_attempt_touchpoint ON brain_calls (run_id, attempt_no, touchpoint, call_seq);
CREATE INDEX intake_items_state_updated ON intake_items (state, updated_at_ms);
CREATE INDEX gate_evaluations_run_created ON gate_evaluations (run_id, created_at_ms);
CREATE INDEX ledger_entries_run_created ON ledger_entries (run_id, created_at_ms);

-- ---------------------------------------------------------------------------
-- §13 Append-only enforcement
-- ---------------------------------------------------------------------------

CREATE TRIGGER config_snapshots_append_only_update
BEFORE UPDATE ON config_snapshots FOR EACH ROW
BEGIN SELECT RAISE(ABORT, 'append-only table'); END;
CREATE TRIGGER config_snapshots_append_only_delete
BEFORE DELETE ON config_snapshots FOR EACH ROW
BEGIN SELECT RAISE(ABORT, 'append-only table'); END;

CREATE TRIGGER task_spec_snapshots_append_only_update
BEFORE UPDATE ON task_spec_snapshots FOR EACH ROW
BEGIN SELECT RAISE(ABORT, 'append-only table'); END;
CREATE TRIGGER task_spec_snapshots_append_only_delete
BEFORE DELETE ON task_spec_snapshots FOR EACH ROW
BEGIN SELECT RAISE(ABORT, 'append-only table'); END;

CREATE TRIGGER events_append_only_update
BEFORE UPDATE ON events FOR EACH ROW
BEGIN SELECT RAISE(ABORT, 'append-only table'); END;
CREATE TRIGGER events_append_only_delete
BEFORE DELETE ON events FOR EACH ROW
BEGIN SELECT RAISE(ABORT, 'append-only table'); END;

CREATE TRIGGER forge_event_receipts_append_only_update
BEFORE UPDATE ON forge_event_receipts FOR EACH ROW
BEGIN SELECT RAISE(ABORT, 'append-only table'); END;
CREATE TRIGGER forge_event_receipts_append_only_delete
BEFORE DELETE ON forge_event_receipts FOR EACH ROW
BEGIN SELECT RAISE(ABORT, 'append-only table'); END;

CREATE TRIGGER report_receipts_append_only_update
BEFORE UPDATE ON report_receipts FOR EACH ROW
BEGIN SELECT RAISE(ABORT, 'append-only table'); END;
CREATE TRIGGER report_receipts_append_only_delete
BEFORE DELETE ON report_receipts FOR EACH ROW
BEGIN SELECT RAISE(ABORT, 'append-only table'); END;

CREATE TRIGGER outbox_attempts_append_only_update
BEFORE UPDATE ON outbox_attempts FOR EACH ROW
BEGIN SELECT RAISE(ABORT, 'append-only table'); END;
CREATE TRIGGER outbox_attempts_append_only_delete
BEFORE DELETE ON outbox_attempts FOR EACH ROW
BEGIN SELECT RAISE(ABORT, 'append-only table'); END;

CREATE TRIGGER outbox_attempt_results_append_only_update
BEFORE UPDATE ON outbox_attempt_results FOR EACH ROW
BEGIN SELECT RAISE(ABORT, 'append-only table'); END;
CREATE TRIGGER outbox_attempt_results_append_only_delete
BEFORE DELETE ON outbox_attempt_results FOR EACH ROW
BEGIN SELECT RAISE(ABORT, 'append-only table'); END;

CREATE TRIGGER budget_entries_append_only_update
BEFORE UPDATE ON budget_entries FOR EACH ROW
BEGIN SELECT RAISE(ABORT, 'append-only table'); END;
CREATE TRIGGER budget_entries_append_only_delete
BEFORE DELETE ON budget_entries FOR EACH ROW
BEGIN SELECT RAISE(ABORT, 'append-only table'); END;

CREATE TRIGGER brain_attempts_append_only_update
BEFORE UPDATE ON brain_attempts FOR EACH ROW
BEGIN SELECT RAISE(ABORT, 'append-only table'); END;
CREATE TRIGGER brain_attempts_append_only_delete
BEFORE DELETE ON brain_attempts FOR EACH ROW
BEGIN SELECT RAISE(ABORT, 'append-only table'); END;

CREATE TRIGGER intake_assessments_append_only_update
BEFORE UPDATE ON intake_assessments FOR EACH ROW
BEGIN SELECT RAISE(ABORT, 'append-only table'); END;
CREATE TRIGGER intake_assessments_append_only_delete
BEFORE DELETE ON intake_assessments FOR EACH ROW
BEGIN SELECT RAISE(ABORT, 'append-only table'); END;

CREATE TRIGGER gate_input_snapshots_append_only_update
BEFORE UPDATE ON gate_input_snapshots FOR EACH ROW
BEGIN SELECT RAISE(ABORT, 'append-only table'); END;
CREATE TRIGGER gate_input_snapshots_append_only_delete
BEFORE DELETE ON gate_input_snapshots FOR EACH ROW
BEGIN SELECT RAISE(ABORT, 'append-only table'); END;

CREATE TRIGGER gate_evaluations_append_only_update
BEFORE UPDATE ON gate_evaluations FOR EACH ROW
BEGIN SELECT RAISE(ABORT, 'append-only table'); END;
CREATE TRIGGER gate_evaluations_append_only_delete
BEFORE DELETE ON gate_evaluations FOR EACH ROW
BEGIN SELECT RAISE(ABORT, 'append-only table'); END;

CREATE TRIGGER gate_cache_append_only_update
BEFORE UPDATE ON gate_cache FOR EACH ROW
BEGIN SELECT RAISE(ABORT, 'append-only table'); END;
CREATE TRIGGER gate_cache_append_only_delete
BEFORE DELETE ON gate_cache FOR EACH ROW
BEGIN SELECT RAISE(ABORT, 'append-only table'); END;

CREATE TRIGGER ledger_entries_append_only_update
BEFORE UPDATE ON ledger_entries FOR EACH ROW
BEGIN SELECT RAISE(ABORT, 'append-only table'); END;
CREATE TRIGGER ledger_entries_append_only_delete
BEFORE DELETE ON ledger_entries FOR EACH ROW
BEGIN SELECT RAISE(ABORT, 'append-only table'); END;

-- daemon_boots: the only permitted write is the one-time completion of the
-- stop fields; every other column is frozen (storage.md §4.2, §13).
CREATE TRIGGER daemon_boots_stop_completion_only
BEFORE UPDATE ON daemon_boots FOR EACH ROW
WHEN NOT (OLD.stopped_at_ms IS NULL AND NEW.stopped_at_ms IS NOT NULL
    AND OLD.stop_reason IS NULL AND NEW.stop_reason IS NOT NULL
    AND NEW.id IS OLD.id
    AND NEW.config_snapshot_id IS OLD.config_snapshot_id
    AND NEW.pid IS OLD.pid
    AND NEW.binary_version IS OLD.binary_version
    AND NEW.protocol_major IS OLD.protocol_major
    AND NEW.started_at_ms IS OLD.started_at_ms)
BEGIN SELECT RAISE(ABORT, 'daemon_boots allows only a one-time stop completion'); END;
CREATE TRIGGER daemon_boots_append_only_delete
BEFORE DELETE ON daemon_boots FOR EACH ROW
BEGIN SELECT RAISE(ABORT, 'append-only table'); END;

-- calibration_entries: the only permitted write is the one-time completion of
-- the human decision fields (storage.md §10.5, §13).
CREATE TRIGGER calibration_entries_decision_completion_only
BEFORE UPDATE ON calibration_entries FOR EACH ROW
WHEN NOT (OLD.human_decision IS NULL AND NEW.human_decision IS NOT NULL
    AND OLD.decision_source IS NULL AND NEW.decision_source IS NOT NULL
    AND OLD.decided_at_ms IS NULL AND NEW.decided_at_ms IS NOT NULL
    AND NEW.id IS OLD.id
    AND NEW.run_id IS OLD.run_id
    AND NEW.gate_evaluation_id IS OLD.gate_evaluation_id
    AND NEW.predicted_decision IS OLD.predicted_decision
    AND NEW.gate_bypassed IS OLD.gate_bypassed
    AND NEW.features_json IS OLD.features_json
    AND NEW.predicted_at_ms IS OLD.predicted_at_ms)
BEGIN SELECT RAISE(ABORT, 'calibration_entries allows only a one-time human decision completion'); END;
CREATE TRIGGER calibration_entries_append_only_delete
BEFORE DELETE ON calibration_entries FOR EACH ROW
BEGIN SELECT RAISE(ABORT, 'append-only table'); END;

-- brain_calls: DELETE is forbidden; UPDATE may only perform the single
-- running -> valid|fallback finalize. Identity, input and call_seq columns
-- can never change (storage.md §10.1, §13).
CREATE TRIGGER brain_calls_finalize_only
BEFORE UPDATE ON brain_calls FOR EACH ROW
WHEN NOT (OLD.status = 'running' AND NEW.status IN ('valid', 'fallback')
    AND NEW.id IS OLD.id
    AND NEW.scope IS OLD.scope
    AND NEW.subject_key IS OLD.subject_key
    AND NEW.project_id IS OLD.project_id
    AND NEW.run_id IS OLD.run_id
    AND NEW.attempt_no IS OLD.attempt_no
    AND NEW.touchpoint IS OLD.touchpoint
    AND NEW.call_seq IS OLD.call_seq
    AND NEW.prompt_version IS OLD.prompt_version
    AND NEW.output_schema_version IS OLD.output_schema_version
    AND NEW.input_json IS OLD.input_json
    AND NEW.input_digest IS OLD.input_digest
    AND NEW.started_at_ms IS OLD.started_at_ms)
BEGIN SELECT RAISE(ABORT, 'brain_calls allows only a one-time running -> valid|fallback finalize'); END;
CREATE TRIGGER brain_calls_append_only_delete
BEFORE DELETE ON brain_calls FOR EACH ROW
BEGIN SELECT RAISE(ABORT, 'append-only table'); END;

-- Column-level immutability on mutable projections (storage.md §13).

-- outbox payload is fixed at creation; retries touch only execution fields.
CREATE TRIGGER outbox_operations_payload_immutable
BEFORE UPDATE ON outbox_operations FOR EACH ROW
WHEN NEW.payload_schema_version IS NOT OLD.payload_schema_version
    OR NEW.payload_json IS NOT OLD.payload_json
    OR NEW.payload_digest IS NOT OLD.payload_digest
BEGIN SELECT RAISE(ABORT, 'outbox payload is immutable'); END;

-- attempt_resolution is written exactly once and is never revised.
CREATE TRIGGER attempts_resolution_write_once
BEFORE UPDATE ON attempts FOR EACH ROW
WHEN OLD.attempt_resolution IS NOT NULL
    AND (NEW.attempt_resolution IS NOT OLD.attempt_resolution
        OR NEW.resolution_at_ms IS NOT OLD.resolution_at_ms)
BEGIN SELECT RAISE(ABORT, 'attempt_resolution is write-once'); END;

-- A spawn permit already issued for this attempt/generation is not replaceable.
CREATE TRIGGER attempt_claims_permit_irreplaceable
BEFORE UPDATE ON attempt_claims FOR EACH ROW
WHEN OLD.spawn_permit_hash IS NOT NULL AND NEW.spawn_permit_hash IS NOT OLD.spawn_permit_hash
BEGIN SELECT RAISE(ABORT, 'spawn permit is not replaceable'); END;

-- Interrupt generation key and charged budget entry are creation-time facts.
CREATE TRIGGER interrupts_identity_immutable
BEFORE UPDATE ON interrupts FOR EACH ROW
WHEN NEW.generation_key IS NOT OLD.generation_key
    OR NEW.charged_budget_entry_id IS NOT OLD.charged_budget_entry_id
BEGIN SELECT RAISE(ABORT, 'interrupt generation key and charged budget entry are immutable'); END;
