-- A sealed batch and its member snapshots are immutable authority.  The
-- existing INSERT guards prevent forged admissions, but UPDATE must be closed
-- too so a direct SQL writer cannot retarget a member after admission.
CREATE TRIGGER attention_batch_member_authority_immutable_update
BEFORE UPDATE ON attention_batch_members
WHEN EXISTS (SELECT 1 FROM attention_batches b WHERE b.id=OLD.batch_id AND b.state IN ('sealed','delivered','cancelled'))
 AND (
  NEW.batch_id IS NOT OLD.batch_id OR NEW.interrupt_id IS NOT OLD.interrupt_id OR
  NEW.admission_id IS NOT OLD.admission_id OR NEW.member_key IS NOT OLD.member_key OR
  NEW.channel_id IS NOT OLD.channel_id OR NEW.channel_snapshot_json IS NOT OLD.channel_snapshot_json OR
  NEW.delivery_id IS NOT OLD.delivery_id OR NEW.interrupt_version IS NOT OLD.interrupt_version OR
  NEW.nonce IS NOT OLD.nonce OR NEW.headline IS NOT OLD.headline OR NEW.reason IS NOT OLD.reason OR
  NEW.severity IS NOT OLD.severity OR NEW.links_json IS NOT OLD.links_json OR NEW.options_json IS NOT OLD.options_json OR
  NEW.joined_at_ms IS NOT OLD.joined_at_ms
 )
BEGIN SELECT RAISE(ABORT,'sealed attention batch member authority is immutable'); END;

CREATE TRIGGER attention_batch_member_authority_snapshot_immutable
BEFORE UPDATE ON attention_batch_member_authority
WHEN EXISTS (SELECT 1 FROM attention_batches b WHERE b.id=OLD.batch_id AND b.state <> 'collecting')
 AND (
  NEW.batch_id IS NOT OLD.batch_id OR NEW.interrupt_id IS NOT OLD.interrupt_id OR
  NEW.interrupt_version IS NOT OLD.interrupt_version OR NEW.nonce IS NOT OLD.nonce OR
  NEW.headline IS NOT OLD.headline OR NEW.reason IS NOT OLD.reason OR NEW.severity IS NOT OLD.severity OR
  NEW.links_json IS NOT OLD.links_json OR NEW.options_json IS NOT OLD.options_json
 )
BEGIN SELECT RAISE(ABORT,'sealed attention member snapshot is immutable'); END;
