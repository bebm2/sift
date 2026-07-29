-- Restore invariants lost by the historical 0021 forward table rebuild and make
-- critical episodes durable for all subsequently created batches.
CREATE TRIGGER IF NOT EXISTS interrupts_close_write_once
BEFORE UPDATE ON interrupts FOR EACH ROW
WHEN OLD.close_reason IS NOT NULL
 AND (NEW.close_reason IS NOT OLD.close_reason OR NEW.closed_at_ms IS NOT OLD.closed_at_ms)
BEGIN SELECT RAISE(ABORT, 'interrupt close reason is write-once'); END;

CREATE UNIQUE INDEX IF NOT EXISTS attention_batches_critical_episode_identity
ON attention_batches(scope,scope_id,episode_admission_id,channel_id,forge_kind,forge_host,forge_project_key,target_kind,target_id)
WHERE kind='critical_fuse';

CREATE TRIGGER IF NOT EXISTS attention_batches_critical_episode_required
BEFORE INSERT ON attention_batches FOR EACH ROW
WHEN (NEW.kind='critical_fuse' AND NEW.episode_admission_id IS NULL)
  OR (NEW.kind='daily_summary' AND NEW.episode_admission_id IS NOT NULL)
BEGIN SELECT RAISE(ABORT, 'invalid critical batch episode'); END;
