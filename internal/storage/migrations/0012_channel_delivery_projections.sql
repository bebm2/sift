-- Channel delivery projections are append-only identities for durable retry state.
ALTER TABLE interrupt_deliveries ADD COLUMN delivery_id TEXT;
ALTER TABLE interrupt_deliveries ADD COLUMN channel_id TEXT;
ALTER TABLE interrupt_deliveries ADD COLUMN channel_snapshot_json TEXT;
ALTER TABLE interrupt_deliveries ADD COLUMN interrupt_version INTEGER;
ALTER TABLE interrupt_deliveries ADD COLUMN nonce TEXT;
ALTER TABLE interrupt_deliveries ADD COLUMN escalation_no INTEGER;
CREATE UNIQUE INDEX IF NOT EXISTS interrupt_deliveries_delivery_id ON interrupt_deliveries(delivery_id) WHERE delivery_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS batch_deliveries (
 batch_id TEXT NOT NULL PRIMARY KEY,
 delivery_id TEXT NOT NULL UNIQUE,
 operation_key TEXT NOT NULL UNIQUE,
 state TEXT NOT NULL CHECK(state IN ('pending','delivered','failed')),
 attempt_count INTEGER NOT NULL DEFAULT 0,
 remote_ref TEXT,
 last_error TEXT,
 created_at_ms INTEGER NOT NULL,
 delivered_at_ms INTEGER
);
CREATE TABLE IF NOT EXISTS channel_failure_episodes (
 subject_id TEXT NOT NULL,
 generation INTEGER NOT NULL CHECK(generation=1),
 consecutive_failures INTEGER NOT NULL CHECK(consecutive_failures>=0),
 state TEXT NOT NULL CHECK(state IN ('open','alerted','ended_delivered','ended_failed')),
 last_error_class TEXT,
 alert_operation_key TEXT UNIQUE,
 created_at_ms INTEGER NOT NULL,
 updated_at_ms INTEGER NOT NULL,
 ended_at_ms INTEGER,
 PRIMARY KEY(subject_id,generation)
);
