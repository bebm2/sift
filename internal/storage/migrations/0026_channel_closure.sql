-- Freeze collecting batch authority as soon as a member is admitted.  A batch
-- may gain members while collecting, but it can never be retargeted.
CREATE TRIGGER IF NOT EXISTS attention_batch_membered_authority_immutable
BEFORE UPDATE ON attention_batches
WHEN OLD.state='collecting'
 AND EXISTS (SELECT 1 FROM attention_batch_members m WHERE m.batch_id=OLD.id)
 AND (
  NEW.project_id IS NOT OLD.project_id OR NEW.channel_id IS NOT OLD.channel_id OR
  NEW.channel_snapshot_json IS NOT OLD.channel_snapshot_json OR NEW.forge_kind IS NOT OLD.forge_kind OR
  NEW.forge_host IS NOT OLD.forge_host OR NEW.forge_project_key IS NOT OLD.forge_project_key OR
  NEW.target_kind IS NOT OLD.target_kind OR NEW.target_id IS NOT OLD.target_id OR
  NEW.kind IS NOT OLD.kind OR NEW.delivery_id IS NOT OLD.delivery_id OR NEW.scope IS NOT OLD.scope OR
  NEW.scope_id IS NOT OLD.scope_id OR NEW.episode_admission_id IS NOT OLD.episode_admission_id OR
  NEW.due_at_ms IS NOT OLD.due_at_ms
 )
BEGIN SELECT RAISE(ABORT,'membered attention batch authority is immutable'); END;

-- project_id is a durable authority component, not merely a display field.
CREATE TRIGGER IF NOT EXISTS attention_batch_project_must_exist
BEFORE INSERT ON attention_batches
WHEN NOT EXISTS (SELECT 1 FROM projects p WHERE p.id=NEW.project_id)
BEGIN SELECT RAISE(ABORT,'attention batch project does not exist'); END;
