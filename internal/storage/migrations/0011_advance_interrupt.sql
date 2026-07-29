-- AdvanceInterrupt freezes all expiry inputs needed after the initial emission.
ALTER TABLE interrupts ADD COLUMN expires_after_ms INTEGER NOT NULL DEFAULT 1 CHECK (expires_after_ms > 0);
ALTER TABLE interrupts ADD COLUMN on_max_escalations TEXT NOT NULL DEFAULT 'hold'
    CHECK (on_max_escalations IN ('hold', 'auto_reject'));
ALTER TABLE interrupts ADD COLUMN base_severity TEXT NOT NULL DEFAULT 'normal'
    CHECK (base_severity IN ('low', 'normal', 'high', 'critical'));
ALTER TABLE interrupts ADD COLUMN nonce_issued_at_ms INTEGER;

CREATE INDEX interrupts_open_dispatch_due
    ON interrupts (status, dispatch_state, next_dispatch_at_ms);
