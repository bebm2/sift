-- Daily summaries never carry critical-fuse authority.  0044 normalized
-- collecting legacy rows; enforce that invariant for every future write.
CREATE TRIGGER attention_batch_collecting_daily_limits_null_insert
BEFORE INSERT ON attention_batches
WHEN NEW.kind='daily_summary'
 AND (NEW.critical_window_ms IS NOT NULL
   OR NEW.critical_total_limit IS NOT NULL
   OR NEW.critical_per_run_limit IS NOT NULL)
BEGIN SELECT RAISE(ABORT,'daily attention batch cannot carry critical limits'); END;

CREATE TRIGGER attention_batch_collecting_daily_limits_null_update
BEFORE UPDATE ON attention_batches
WHEN NEW.kind='daily_summary' AND NEW.state='collecting'
 AND (NEW.critical_window_ms IS NOT NULL
   OR NEW.critical_total_limit IS NOT NULL
   OR NEW.critical_per_run_limit IS NOT NULL)
BEGIN SELECT RAISE(ABORT,'daily attention batch cannot carry critical limits'); END;
