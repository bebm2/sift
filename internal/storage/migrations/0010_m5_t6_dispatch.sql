-- T6 dispatch decisions are frozen with the Interrupt, never reconstructed from
-- current Channel configuration during an escalation or restart.
ALTER TABLE interrupts ADD COLUMN channel_id TEXT;
ALTER TABLE interrupts ADD COLUMN delivery TEXT CHECK (delivery IN ('immediate', 'batch', 'next_window', 'held'));
ALTER TABLE interrupts ADD COLUMN suggested_downgrade INTEGER NOT NULL DEFAULT 0 CHECK (suggested_downgrade IN (0, 1));
ALTER TABLE interrupts ADD COLUMN next_dispatch_at_ms INTEGER;
ALTER TABLE interrupts ADD COLUMN held_reason TEXT CHECK (held_reason IN ('manual', 'no_compatible_channel', 'channel_isolated', 'batch_after_expiry', 'quota_rejected', 'critical_fuse', 'expiry', 'max_escalations'));
