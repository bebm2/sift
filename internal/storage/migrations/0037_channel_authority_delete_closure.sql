-- Sealed member snapshots are audit authority, not mutable cache rows.  Close
-- the direct-SQL path that first deletes the snapshot child and then its member,
-- and freeze the timestamp alongside the snapshot fields.
CREATE TRIGGER attention_batch_member_authority_immutable_delete
BEFORE DELETE ON attention_batch_members
WHEN EXISTS (SELECT 1 FROM attention_batches b WHERE b.id=OLD.batch_id AND b.state IN ('sealed','delivered','cancelled'))
BEGIN SELECT RAISE(ABORT,'sealed attention batch member authority is immutable'); END;

CREATE TRIGGER attention_batch_member_authority_snapshot_immutable_delete
BEFORE DELETE ON attention_batch_member_authority
WHEN EXISTS (SELECT 1 FROM attention_batches b WHERE b.id=OLD.batch_id AND b.state <> 'collecting')
BEGIN SELECT RAISE(ABORT,'sealed attention member snapshot is immutable'); END;

CREATE TRIGGER attention_batch_member_authority_snapshot_timestamp_immutable
BEFORE UPDATE ON attention_batch_member_authority
WHEN EXISTS (SELECT 1 FROM attention_batches b WHERE b.id=OLD.batch_id AND b.state <> 'collecting')
 AND NEW.updated_at_ms IS NOT OLD.updated_at_ms
BEGIN SELECT RAISE(ABORT,'sealed attention member snapshot is immutable'); END;
