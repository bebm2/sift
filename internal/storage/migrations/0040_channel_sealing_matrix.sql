-- Close the remaining direct-SQL mutation paths after a Channel batch is sealed.
-- Sealing is the last authority boundary: delivery projection updates may still
-- advance state, but identity, members and frozen payload must not change.
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
   OR NEW.due_at_ms IS NOT OLD.due_at_ms
   OR NEW.operation_key IS NOT OLD.operation_key
   OR NEW.payload_json IS NOT OLD.payload_json
   OR NEW.payload_digest IS NOT OLD.payload_digest
   OR NEW.created_at_ms IS NOT OLD.created_at_ms
   OR NEW.sealed_at_ms IS NOT OLD.sealed_at_ms)
BEGIN SELECT RAISE(ABORT,'sealed attention batch identity is immutable'); END;

CREATE TRIGGER attention_batch_sealed_member_immutable_update
BEFORE UPDATE ON attention_batch_members
WHEN EXISTS (SELECT 1 FROM attention_batches b WHERE b.id=OLD.batch_id AND b.state <> 'collecting')
BEGIN SELECT RAISE(ABORT,'sealed attention batch member history is immutable'); END;

CREATE TRIGGER attention_batch_sealed_member_immutable_delete
BEFORE DELETE ON attention_batch_members
WHEN EXISTS (SELECT 1 FROM attention_batches b WHERE b.id=OLD.batch_id AND b.state <> 'collecting')
BEGIN SELECT RAISE(ABORT,'sealed attention batch member history is immutable'); END;

CREATE TRIGGER attention_batch_immutable_delete
BEFORE DELETE ON attention_batches
WHEN OLD.state <> 'collecting'
BEGIN SELECT RAISE(ABORT,'sealed attention batch is immutable'); END;
