-- A daily summary never owns critical-fuse authority in any state.  Keep the
-- legacy NULL-compatible rows, but reject every future INSERT/UPDATE that
-- attempts to attach any critical limit, including a transition to terminal.
DROP TRIGGER IF EXISTS attention_batch_daily_limits_null_update;
CREATE TRIGGER attention_batch_daily_limits_null_update
BEFORE UPDATE ON attention_batches
WHEN NEW.kind='daily_summary'
 AND (NEW.critical_window_ms IS NOT NULL
   OR NEW.critical_total_limit IS NOT NULL
   OR NEW.critical_per_run_limit IS NOT NULL)
BEGIN SELECT RAISE(ABORT,'daily attention batch cannot carry critical limits'); END;
