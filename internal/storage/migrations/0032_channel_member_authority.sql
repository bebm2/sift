-- Keep the batch's frozen project, verified Forge target, and Channel snapshot
-- authoritative even if a caller bypasses the admission write path. A member
-- may only join a collecting batch that exactly matches its Run and snapshot.
CREATE TRIGGER IF NOT EXISTS attention_batch_member_authority_matches
BEFORE INSERT ON attention_batch_members
WHEN NOT EXISTS (
 SELECT 1
 FROM attention_batches b
 JOIN interrupts i ON i.id=NEW.interrupt_id
 JOIN runs r ON r.id=i.run_id
 WHERE b.id=NEW.batch_id
   AND b.state='collecting'
   AND b.project_id=r.project_id
   AND b.forge_kind=r.forge_kind
   AND b.forge_host=r.forge_host
   AND b.forge_project_key=r.forge_project_key
   AND b.target_kind=CASE WHEN r.issue_id IS NOT NULL THEN 'issue' ELSE r.discussion_target_kind END
   AND b.target_id=COALESCE(r.issue_id,r.discussion_target_id)
   AND NEW.channel_id=b.channel_id
   AND NEW.channel_snapshot_json=b.channel_snapshot_json
)
BEGIN SELECT RAISE(ABORT,'attention batch member authority mismatch'); END;
