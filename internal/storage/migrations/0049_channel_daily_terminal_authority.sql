-- Daily summaries never own critical-fuse authority, including during the
-- transition out of collecting.  The 0046 collecting-only guard allowed a
-- single UPDATE to change state and add critical limits atomically.
CREATE TRIGGER attention_batch_daily_limits_null_update
BEFORE UPDATE ON attention_batches
WHEN OLD.state='collecting' AND NEW.kind='daily_summary'
 AND (NEW.critical_window_ms IS NOT NULL
   OR NEW.critical_total_limit IS NOT NULL
   OR NEW.critical_per_run_limit IS NOT NULL)
BEGIN SELECT RAISE(ABORT,'daily attention batch cannot carry critical limits'); END;
