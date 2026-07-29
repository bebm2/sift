-- Complete the durable Channel authority matrix after the 0020/0021 repairs.
-- A member is admitted only when its interrupt's frozen Forge target is the
-- same target persisted on the batch.  The batch key alone is not authority.
CREATE TRIGGER IF NOT EXISTS attention_batch_member_target_matches_authority
BEFORE INSERT ON attention_batch_members
WHEN NOT EXISTS (
  SELECT 1
  FROM attention_batches b
  JOIN interrupts i ON i.id=NEW.interrupt_id
  JOIN runs r ON r.id=i.run_id
  WHERE b.id=NEW.batch_id
    AND b.project_id=r.project_id
    AND b.forge_kind=r.forge_kind
    AND b.forge_host=r.forge_host
    AND b.forge_project_key=r.forge_project_key
    AND b.target_kind=CASE WHEN r.issue_id IS NOT NULL THEN 'issue' ELSE r.discussion_target_kind END
    AND b.target_id=COALESCE(r.issue_id,r.discussion_target_id)
)
BEGIN SELECT RAISE(ABORT,'batch member target does not match frozen authority'); END;

CREATE TRIGGER IF NOT EXISTS attention_batch_member_identity_immutable
BEFORE UPDATE ON attention_batch_members
WHEN NEW.batch_id IS NOT OLD.batch_id OR NEW.interrupt_id IS NOT OLD.interrupt_id OR
     NEW.admission_id IS NOT OLD.admission_id OR NEW.member_key IS NOT OLD.member_key OR
     NEW.channel_id IS NOT OLD.channel_id OR NEW.channel_snapshot_json IS NOT OLD.channel_snapshot_json OR
     NEW.delivery_id IS NOT OLD.delivery_id OR NEW.interrupt_version IS NOT OLD.interrupt_version OR
     NEW.nonce IS NOT OLD.nonce OR NEW.headline IS NOT OLD.headline OR NEW.reason IS NOT OLD.reason OR
     NEW.severity IS NOT OLD.severity OR NEW.links_json IS NOT OLD.links_json OR
     NEW.options_json IS NOT OLD.options_json OR NEW.joined_at_ms IS NOT OLD.joined_at_ms
BEGIN SELECT RAISE(ABORT,'batch member authority is immutable'); END;
