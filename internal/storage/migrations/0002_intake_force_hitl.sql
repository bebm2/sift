ALTER TABLE intake_items ADD COLUMN force_hitl_before_start INTEGER NOT NULL DEFAULT 0 CHECK (force_hitl_before_start IN (0, 1));
