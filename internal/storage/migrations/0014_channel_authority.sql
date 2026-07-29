-- Channel batch authority and frozen failure-alert targets.
ALTER TABLE interrupt_deliveries ADD COLUMN forge_kind TEXT;
ALTER TABLE interrupt_deliveries ADD COLUMN forge_host TEXT;
ALTER TABLE interrupt_deliveries ADD COLUMN forge_project_key TEXT;
ALTER TABLE interrupt_deliveries ADD COLUMN forge_alert_target_kind TEXT;
ALTER TABLE interrupt_deliveries ADD COLUMN forge_alert_target_id TEXT;

ALTER TABLE attention_batches ADD COLUMN kind TEXT NOT NULL DEFAULT 'daily_summary' CHECK(kind IN ('daily_summary','critical_fuse'));
ALTER TABLE attention_batches ADD COLUMN delivery_id TEXT;
ALTER TABLE attention_batches ADD COLUMN scope TEXT;
ALTER TABLE attention_batches ADD COLUMN scope_id TEXT;
ALTER TABLE attention_batches ADD COLUMN due_at_ms INTEGER;
ALTER TABLE attention_batches ADD COLUMN payload_json TEXT;
ALTER TABLE attention_batches ADD COLUMN payload_digest TEXT;
ALTER TABLE attention_batches ADD COLUMN created_at_ms INTEGER;
ALTER TABLE attention_batches ADD COLUMN sealed_at_ms INTEGER;
ALTER TABLE attention_batches ADD COLUMN delivered_at_ms INTEGER;

CREATE UNIQUE INDEX IF NOT EXISTS attention_batches_delivery_id ON attention_batches(delivery_id) WHERE delivery_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS attention_batches_operation_key ON attention_batches(operation_key) WHERE operation_key IS NOT NULL;
