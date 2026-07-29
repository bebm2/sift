-- Close the remaining AdvanceInterrupt schema gaps without rewriting frozen history.
ALTER TABLE attention_admissions ADD COLUMN attention_charge_entry_id TEXT REFERENCES budget_entries(id);
ALTER TABLE attention_admissions ADD COLUMN severity TEXT;
ALTER TABLE attention_admissions ADD COLUMN quota_day TEXT;
ALTER TABLE attention_admissions ADD COLUMN day_timezone TEXT;

DROP INDEX IF EXISTS attention_admissions_interrupt_critical;
CREATE UNIQUE INDEX IF NOT EXISTS attention_admissions_interrupt_initial
    ON attention_admissions(interrupt_id)
    WHERE kind IN ('quota_charged','quota_batched');
CREATE UNIQUE INDEX IF NOT EXISTS attention_admissions_interrupt_critical
    ON attention_admissions(interrupt_id)
    WHERE kind IN ('critical_admitted','critical_fused');
