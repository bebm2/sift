-- T7 proposals are immutable, inert drafts. They are never configuration,
-- context, Gate, Interrupt, or outbox inputs (brain.md §13 / A7).
CREATE TABLE proposal_drafts (
    id                    TEXT NOT NULL PRIMARY KEY,
    logical_call_id       TEXT NOT NULL UNIQUE REFERENCES brain_calls (id),
    prompt_version        TEXT NOT NULL,
    output_schema_version INTEGER NOT NULL CHECK (output_schema_version >= 1),
    aggregate_key         TEXT NOT NULL,
    proposal_kind         TEXT NOT NULL CHECK (proposal_kind IN ('policy', 'context')),
    target_scope          TEXT NOT NULL CHECK (target_scope IN ('project', 'global')),
    title                 TEXT NOT NULL,
    body                  TEXT NOT NULL,
    evidence_entry_ids    TEXT NOT NULL,
    status                TEXT NOT NULL CHECK (status = 'pending_human_approval'),
    created_at_ms         INTEGER NOT NULL CHECK (created_at_ms >= 0)
);

CREATE INDEX proposal_drafts_aggregate_created
    ON proposal_drafts (aggregate_key, created_at_ms, id);

CREATE TRIGGER proposal_drafts_append_only_update
BEFORE UPDATE ON proposal_drafts
BEGIN SELECT RAISE(ABORT, 'proposal_drafts are append-only'); END;
CREATE TRIGGER proposal_drafts_append_only_delete
BEFORE DELETE ON proposal_drafts
BEGIN SELECT RAISE(ABORT, 'proposal_drafts are append-only'); END;
