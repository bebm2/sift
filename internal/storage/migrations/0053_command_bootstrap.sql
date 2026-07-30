-- 0053_command_bootstrap.sql
-- M5 §5.4 Command bootstrap: immutable command target binding, the
-- initial/final command-event outcome relation, immutable effect facts, the
-- approval-label cutoff and the per-Interrupt hold ceiling. The command
-- receipt/dedup table is command-scoped so it cannot collide with the
-- existing Intake forge_event_receipts semantics; the dedup contract matches
-- specs/command.md §2.2.

-- Immutable one-to-one target binding per Interrupt (specs/command.md §2.3,
-- storage.md §6.4). Created in EmitInterrupt's five-things transaction from
-- the same verified target used in the payload. Command compares the envelope
-- target only with this row.
CREATE TABLE IF NOT EXISTS interrupt_command_targets (
    interrupt_id        TEXT NOT NULL PRIMARY KEY REFERENCES interrupts (id),
    publish_operation_id TEXT NOT NULL UNIQUE REFERENCES outbox_operations (id),
    target_kind         TEXT NOT NULL CHECK (target_kind IN ('issue', 'change')),
    target_id           TEXT NOT NULL,
    created_at_ms       INTEGER NOT NULL
);

-- Initial/final retry resolution (storage.md §6.4). The canonical command
-- identity (event_key) is not an events uniqueness key: a non-retry command
-- inserts initial_event_id=final_event_id,state=final in one transaction; a
-- retry request inserts state=pending,final_event_id=NULL and the sole
-- finalizer CAS-completes it.
CREATE TABLE IF NOT EXISTS command_event_outcomes (
    id               TEXT NOT NULL PRIMARY KEY,
    event_key        TEXT NOT NULL UNIQUE,
    initial_event_id TEXT NOT NULL UNIQUE REFERENCES events (id),
    final_event_id   TEXT UNIQUE REFERENCES events (id),
    state            TEXT NOT NULL CHECK (state IN ('pending', 'final')),
    created_at_ms    INTEGER NOT NULL,
    finalized_at_ms  INTEGER,
    CHECK ((state = 'pending' AND final_event_id IS NULL AND finalized_at_ms IS NULL)
        OR (state = 'final' AND final_event_id IS NOT NULL AND finalized_at_ms IS NOT NULL))
);

-- Immutable human-decision effect facts consumed by the next Gate snapshot
-- (storage.md §6.4). They never overwrite the historical snapshot.
CREATE TABLE IF NOT EXISTS command_effects (
    id                          TEXT NOT NULL PRIMARY KEY,
    interrupt_id                TEXT NOT NULL REFERENCES interrupts (id),
    event_id                    TEXT NOT NULL REFERENCES events (id),
    effect_kind                 TEXT NOT NULL CHECK (effect_kind IN ('one_time_exemption', 'human_review_approval')),
    run_id                      TEXT NOT NULL REFERENCES runs (id),
    change_id                   TEXT,
    head_sha                    TEXT,
    rule_id                     TEXT,
    matched_paths_digest        TEXT,
    review_policy_snapshot_digest TEXT,
    created_at_ms               INTEGER NOT NULL,
    CHECK (effect_kind = 'one_time_exemption'
        AND run_id IS NOT NULL AND head_sha IS NOT NULL AND rule_id IS NOT NULL AND matched_paths_digest IS NOT NULL
        OR effect_kind = 'human_review_approval'
        AND change_id IS NOT NULL AND head_sha IS NOT NULL AND review_policy_snapshot_digest IS NOT NULL)
);

-- Command-scoped candidate receipt / dedup (specs/command.md §2.2). A
-- duplicate returns the stored outcome and creates no event, Ledger entry,
-- probe or ack. event_kind is exactly the envelope source.
CREATE TABLE IF NOT EXISTS command_receipts (
    id                  TEXT NOT NULL PRIMARY KEY,
    project_id          TEXT NOT NULL REFERENCES projects (id),
    event_kind          TEXT NOT NULL CHECK (event_kind IN ('forge_comment', 'approval_label')),
    remote_event_id     TEXT NOT NULL,
    event_key           TEXT NOT NULL,
    target_kind         TEXT NOT NULL CHECK (target_kind IN ('issue', 'change')),
    target_id           TEXT NOT NULL,
    actor               TEXT,
    raw_digest          TEXT NOT NULL,
    disposition         TEXT NOT NULL CHECK (disposition IN ('accepted', 'ignored_untrusted_actor', 'ignored_missing_actor')),
    domain_event_id     TEXT REFERENCES events (id),
    command_outcome_id  TEXT REFERENCES command_event_outcomes (id),
    observed_at_ms      INTEGER NOT NULL,
    UNIQUE (project_id, event_kind, remote_event_id)
);

-- Approval-label anti-replay cutover (specs/command.md §3.2). NULL (the zero
-- sentinel) means no cutover: every positive position is current. SetApproval-
-- LabelCutoff is the only writer from NULL to a position.
ALTER TABLE interrupts ADD COLUMN approval_label_cutoff_position TEXT CHECK (
    approval_label_cutoff_position IS NULL
    OR (length(approval_label_cutoff_position) BETWEEN 1 AND 39
        AND approval_label_cutoff_position GLOB '[0-9]*'
        AND approval_label_cutoff_position NOT GLOB '0*'));

-- Immutable hold ceiling copied from attention.hold_max_duration at emission
-- (specs/command.md §3.1). Restart/config drift cannot change a pending hold.
ALTER TABLE interrupts ADD COLUMN hold_max_duration_ms INTEGER NOT NULL DEFAULT 0 CHECK (hold_max_duration_ms >= 0);

-- command_event_outcomes single-finalizer CAS guard.
CREATE TRIGGER command_event_outcomes_single_finalize
BEFORE UPDATE ON command_event_outcomes FOR EACH ROW
WHEN OLD.state = 'final'
    OR NEW.state NOT IN ('final')
    OR NEW.final_event_id IS NULL
    OR NEW.finalized_at_ms IS NULL
    OR OLD.state <> 'pending'
    OR OLD.final_event_id IS NOT NULL
    OR OLD.finalized_at_ms IS NOT NULL
BEGIN SELECT RAISE(ABORT, 'command_event_outcomes: illegal finalize'); END;

-- command_receipts / command_effects / interrupt_command_targets are
-- append-only: Command never revises a persisted candidate or effect.
CREATE TRIGGER command_receipts_append_only_update
BEFORE UPDATE ON command_receipts FOR EACH ROW
BEGIN SELECT RAISE(ABORT, 'command_receipts is append-only'); END;
CREATE TRIGGER command_receipts_append_only_delete
BEFORE DELETE ON command_receipts FOR EACH ROW
BEGIN SELECT RAISE(ABORT, 'command_receipts is append-only'); END;
CREATE TRIGGER command_effects_append_only_update
BEFORE UPDATE ON command_effects FOR EACH ROW
BEGIN SELECT RAISE(ABORT, 'command_effects is append-only'); END;
CREATE TRIGGER command_effects_append_only_delete
BEFORE DELETE ON command_effects FOR EACH ROW
BEGIN SELECT RAISE(ABORT, 'command_effects is append-only'); END;
CREATE TRIGGER interrupt_command_targets_append_only_update
BEFORE UPDATE ON interrupt_command_targets FOR EACH ROW
BEGIN SELECT RAISE(ABORT, 'interrupt_command_targets is append-only'); END;
CREATE TRIGGER interrupt_command_targets_append_only_delete
BEFORE DELETE ON interrupt_command_targets FOR EACH ROW
BEGIN SELECT RAISE(ABORT, 'interrupt_command_targets is append-only'); END;
