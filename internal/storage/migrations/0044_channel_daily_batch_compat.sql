-- Daily summaries never own critical-fuse authority. Normalize collecting
-- rows created by intermediate binaries while retaining sealed history.
UPDATE attention_batches
SET critical_window_ms = NULL,
    critical_total_limit = NULL,
    critical_per_run_limit = NULL
WHERE kind = 'daily_summary' AND state = 'collecting';
