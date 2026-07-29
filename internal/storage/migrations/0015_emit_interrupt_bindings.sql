-- Freeze EmitInterrupt effect/source bindings and Report quota exhaustion identity.
ALTER TABLE interrupt_command_effect_bindings ADD COLUMN reason TEXT NOT NULL DEFAULT 'unknown';
ALTER TABLE interrupt_command_effect_bindings ADD COLUMN binding_schema_version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE interrupt_command_effect_bindings ADD COLUMN binding_digest TEXT;

CREATE TABLE report_quota_exhaustions (
    run_id TEXT NOT NULL REFERENCES runs(id),
    daily_bucket_start_ms INTEGER NOT NULL,
    daily_bucket_end_ms INTEGER NOT NULL,
    security_event_id TEXT NOT NULL UNIQUE REFERENCES events(id),
    failure_digest TEXT NOT NULL,
    generation_key TEXT NOT NULL UNIQUE,
    created_at_ms INTEGER NOT NULL,
    PRIMARY KEY (run_id, daily_bucket_start_ms),
    CHECK (daily_bucket_start_ms < daily_bucket_end_ms)
);
CREATE INDEX report_quota_exhaustions_run_bucket ON report_quota_exhaustions(run_id, daily_bucket_start_ms);
CREATE UNIQUE INDEX interrupt_command_effect_bindings_digest ON interrupt_command_effect_bindings(binding_digest);

CREATE TRIGGER interrupt_command_effect_bindings_append_only_update
BEFORE UPDATE ON interrupt_command_effect_bindings FOR EACH ROW
BEGIN SELECT RAISE(ABORT, 'append-only table'); END;
CREATE TRIGGER interrupt_command_effect_bindings_append_only_delete
BEFORE DELETE ON interrupt_command_effect_bindings FOR EACH ROW
BEGIN SELECT RAISE(ABORT, 'append-only table'); END;

CREATE TRIGGER report_quota_exhaustions_append_only_update
BEFORE UPDATE ON report_quota_exhaustions FOR EACH ROW
BEGIN SELECT RAISE(ABORT, 'append-only table'); END;
CREATE TRIGGER report_quota_exhaustions_append_only_delete
BEFORE DELETE ON report_quota_exhaustions FOR EACH ROW
BEGIN SELECT RAISE(ABORT, 'append-only table'); END;
