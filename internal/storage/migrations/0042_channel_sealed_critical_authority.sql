-- Critical-fuse admission and limits determine the sealed batch authority too.
-- Replace the 0040 guard so every authority field is immutable after sealing.
DROP TRIGGER attention_batch_sealed_identity_immutable;

CREATE TRIGGER attention_batch_sealed_identity_immutable
BEFORE UPDATE ON attention_batches
WHEN OLD.state <> 'collecting'
 AND (NEW.id IS NOT OLD.id
   OR NEW.project_id IS NOT OLD.project_id
   OR NEW.channel_id IS NOT OLD.channel_id
   OR NEW.channel_snapshot_json IS NOT OLD.channel_snapshot_json
   OR NEW.forge_kind IS NOT OLD.forge_kind
   OR NEW.forge_host IS NOT OLD.forge_host
   OR NEW.forge_project_key IS NOT OLD.forge_project_key
   OR NEW.target_kind IS NOT OLD.target_kind
   OR NEW.target_id IS NOT OLD.target_id
   OR NEW.kind IS NOT OLD.kind
   OR NEW.delivery_id IS NOT OLD.delivery_id
   OR NEW.scope IS NOT OLD.scope
   OR NEW.scope_id IS NOT OLD.scope_id
   OR NEW.episode_admission_id IS NOT OLD.episode_admission_id
   OR NEW.due_at_ms IS NOT OLD.due_at_ms
   OR NEW.critical_window_ms IS NOT OLD.critical_window_ms
   OR NEW.critical_total_limit IS NOT OLD.critical_total_limit
   OR NEW.critical_per_run_limit IS NOT OLD.critical_per_run_limit
   OR NEW.operation_key IS NOT OLD.operation_key
   OR NEW.payload_json IS NOT OLD.payload_json
   OR NEW.payload_digest IS NOT OLD.payload_digest
   OR NEW.created_at_ms IS NOT OLD.created_at_ms
   OR NEW.sealed_at_ms IS NOT OLD.sealed_at_ms)
BEGIN SELECT RAISE(ABORT,'sealed attention batch identity is immutable'); END;
