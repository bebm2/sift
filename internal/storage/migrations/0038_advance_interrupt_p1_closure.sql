-- Close the remaining AdvanceInterrupt authority and provenance gaps after 0037.
-- Collecting authority mirrors only an open, current Interrupt; once a batch
-- leaves collecting every column is an immutable audit snapshot.
DROP TRIGGER IF EXISTS attention_batch_member_authority_current_insert;
DROP TRIGGER IF EXISTS attention_batch_member_authority_current_update;
DROP TRIGGER IF EXISTS attention_batch_member_authority_immutable_update;
DROP TRIGGER IF EXISTS attention_batch_member_authority_snapshot_immutable;
DROP TRIGGER IF EXISTS attention_batch_member_authority_immutable_delete;
DROP TRIGGER IF EXISTS attention_batch_member_authority_snapshot_immutable_delete;
DROP TRIGGER IF EXISTS attention_batch_member_authority_snapshot_timestamp_immutable;

CREATE TRIGGER attention_batch_member_authority_current_insert
BEFORE INSERT ON attention_batch_member_authority FOR EACH ROW
WHEN NOT EXISTS (
 SELECT 1 FROM attention_batch_members m
 JOIN attention_batches b ON b.id=m.batch_id
 JOIN interrupts i ON i.id=m.interrupt_id
 WHERE m.batch_id=NEW.batch_id AND m.interrupt_id=NEW.interrupt_id
   AND b.state='collecting' AND i.status='open'
   AND i.version=NEW.interrupt_version AND i.nonce=NEW.nonce
   AND i.headline=NEW.headline AND i.reason=NEW.reason AND i.severity=NEW.severity
   AND i.links_json=NEW.links_json AND i.options_json=NEW.options_json
)
BEGIN SELECT RAISE(ABORT,'attention batch member authority is not current open Interrupt'); END;

CREATE TRIGGER attention_batch_member_authority_current_update
BEFORE UPDATE ON attention_batch_member_authority FOR EACH ROW
WHEN EXISTS (SELECT 1 FROM attention_batches b WHERE b.id=OLD.batch_id AND b.state='collecting')
 AND NOT EXISTS (
 SELECT 1 FROM attention_batch_members m JOIN interrupts i ON i.id=m.interrupt_id
 WHERE m.batch_id=NEW.batch_id AND m.interrupt_id=NEW.interrupt_id
   AND i.status='open' AND i.version=NEW.interrupt_version AND i.nonce=NEW.nonce
   AND i.headline=NEW.headline AND i.reason=NEW.reason AND i.severity=NEW.severity
   AND i.links_json=NEW.links_json AND i.options_json=NEW.options_json
 )
BEGIN SELECT RAISE(ABORT,'attention batch member authority is not current open Interrupt'); END;

CREATE TRIGGER attention_batch_member_authority_immutable_update
BEFORE UPDATE ON attention_batch_member_authority
WHEN EXISTS (SELECT 1 FROM attention_batches b WHERE b.id=OLD.batch_id AND b.state<>'collecting')
 AND (NEW.batch_id IS NOT OLD.batch_id OR NEW.interrupt_id IS NOT OLD.interrupt_id
   OR NEW.interrupt_version IS NOT OLD.interrupt_version OR NEW.nonce IS NOT OLD.nonce
   OR NEW.headline IS NOT OLD.headline OR NEW.reason IS NOT OLD.reason
   OR NEW.severity IS NOT OLD.severity OR NEW.links_json IS NOT OLD.links_json
   OR NEW.options_json IS NOT OLD.options_json OR NEW.updated_at_ms IS NOT OLD.updated_at_ms)
BEGIN SELECT RAISE(ABORT,'sealed attention member authority is immutable'); END;

CREATE TRIGGER attention_batch_member_authority_immutable_delete
BEFORE DELETE ON attention_batch_member_authority
BEGIN SELECT RAISE(ABORT,'attention batch member authority is immutable'); END;

-- The history row is also immutable once sealed/cancelled; retain the prior
-- member trigger and make the mutable authority projection append-only.
CREATE TRIGGER attention_batch_member_authority_snapshot_immutable_delete
BEFORE DELETE ON attention_batch_members
WHEN EXISTS (SELECT 1 FROM attention_batches b WHERE b.id=OLD.batch_id AND b.state<>'collecting')
BEGIN SELECT RAISE(ABORT,'sealed attention batch member history is immutable'); END;

-- A binding may only name a report receipt belonging to this Interrupt's
-- run/attempt and the blocker report kind.  The old trigger checked only the
-- attempt, allowing an unrelated report to obtain command authority. Keep
-- the earlier closed-union identity trigger and add these stricter predicates.
CREATE TRIGGER interrupt_binding_provenance_insert
BEFORE INSERT ON interrupt_command_effect_bindings FOR EACH ROW
WHEN
 (json_extract(NEW.binding_json,'$.arm')='agent_blocked' AND EXISTS (SELECT 1 FROM report_receipts r0 JOIN interrupts i0 ON i0.id=NEW.interrupt_id WHERE r0.run_id=i0.run_id AND r0.attempt_no=json_extract(NEW.binding_json,'$.attempt_no')) AND NOT EXISTS (
   SELECT 1 FROM report_receipts r JOIN interrupts i ON i.id=NEW.interrupt_id
   WHERE r.id=json_extract(NEW.binding_json,'$.report_id') AND r.run_id=i.run_id
     AND r.attempt_no=json_extract(NEW.binding_json,'$.attempt_no') AND r.report_kind='blocker'))
 OR (json_extract(NEW.binding_json,'$.arm')='code_review' AND json_extract(NEW.binding_json,'$.review_policy_snapshot_digest')<>'' AND EXISTS (SELECT 1 FROM interrupts i0 WHERE i0.id=NEW.interrupt_id AND i0.calibration_id IS NOT NULL) AND NOT EXISTS (
   SELECT 1 FROM interrupts i JOIN runs r ON r.id=i.run_id
   JOIN calibration_entries c ON c.id=i.calibration_id
   JOIN gate_evaluations e ON e.id=c.gate_evaluation_id
   JOIN gate_input_snapshots s ON s.id=e.snapshot_id
   WHERE i.id=NEW.interrupt_id AND r.change_id=json_extract(NEW.binding_json,'$.change_id')
     AND r.change_head_sha=json_extract(NEW.binding_json,'$.head_sha')
     AND s.head_sha=json_extract(NEW.binding_json,'$.head_sha')
     AND s.effective_policy_hash=json_extract(NEW.binding_json,'$.review_policy_snapshot_digest')))
 OR (json_extract(NEW.binding_json,'$.arm')='merge_conflict' AND EXISTS (SELECT 1 FROM interrupts i0 WHERE i0.id=NEW.interrupt_id AND i0.calibration_id IS NOT NULL) AND NOT EXISTS (
   SELECT 1 FROM interrupts i JOIN runs r ON r.id=i.run_id
   JOIN calibration_entries c ON c.id=i.calibration_id
   JOIN gate_evaluations e ON e.id=c.gate_evaluation_id
   JOIN gate_input_snapshots s ON s.id=e.snapshot_id
   WHERE i.id=NEW.interrupt_id AND r.change_id=json_extract(NEW.binding_json,'$.change_id')
     AND r.change_head_sha=json_extract(NEW.binding_json,'$.head_sha')
     AND s.head_sha=json_extract(NEW.binding_json,'$.conflict_digest')))
BEGIN SELECT RAISE(ABORT,'invalid interrupt binding provenance'); END;
