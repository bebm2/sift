-- Restore collecting authority's current-open mirror check while forbidding
-- primary-identity retargets, and close calibrated conflict provenance.
ALTER TABLE gate_input_snapshots ADD COLUMN conflict_digest TEXT;

DROP TRIGGER IF EXISTS attention_batch_member_authority_current_update;
CREATE TRIGGER attention_batch_member_authority_current_update
BEFORE UPDATE ON attention_batch_member_authority FOR EACH ROW
WHEN EXISTS (SELECT 1 FROM attention_batches WHERE id=OLD.batch_id AND state='collecting')
 AND (
   NEW.batch_id IS NOT OLD.batch_id OR NEW.interrupt_id IS NOT OLD.interrupt_id
   OR NOT EXISTS (
     SELECT 1 FROM attention_batch_members m
     JOIN attention_batches b ON b.id=m.batch_id
     JOIN interrupts i ON i.id=m.interrupt_id
     WHERE m.batch_id=NEW.batch_id AND m.interrupt_id=NEW.interrupt_id
       AND b.state='collecting' AND i.status='open'
       AND i.version=NEW.interrupt_version AND i.nonce=NEW.nonce
       AND i.headline=NEW.headline AND i.reason=NEW.reason AND i.severity=NEW.severity
       AND i.links_json=NEW.links_json AND i.options_json=NEW.options_json
   )
 )
BEGIN SELECT RAISE(ABORT,'attention batch member authority is not current open Interrupt'); END;

DROP TRIGGER IF EXISTS interrupt_binding_provenance_insert;
CREATE TRIGGER interrupt_binding_provenance_insert
BEFORE INSERT ON interrupt_command_effect_bindings FOR EACH ROW
WHEN
 (json_extract(NEW.binding_json,'$.arm')='design_approval' AND NOT EXISTS (
   SELECT 1 FROM task_spec_snapshots s JOIN interrupts i ON i.id=NEW.interrupt_id
   WHERE s.id=json_extract(NEW.binding_json,'$.task_spec_snapshot_id') AND s.run_id=i.run_id
     AND json_extract(NEW.binding_json,'$.run_id')=i.run_id))
 OR (json_extract(NEW.binding_json,'$.arm')='guardrail_violation' AND NOT EXISTS (
   SELECT 1 FROM interrupts i JOIN calibration_entries c ON c.id=i.calibration_id
   JOIN gate_evaluations e ON e.id=c.gate_evaluation_id JOIN gate_input_snapshots s ON s.id=e.snapshot_id
   WHERE i.id=NEW.interrupt_id AND e.run_id=i.run_id
     AND json_extract(NEW.binding_json,'$.run_id')=i.run_id
     AND s.head_sha=json_extract(NEW.binding_json,'$.head_sha')
     AND json_extract(e.verdict_json,'$.rule_id')=json_extract(NEW.binding_json,'$.rule_id')
     AND json_extract(e.verdict_json,'$.matched_paths_digest')=json_extract(NEW.binding_json,'$.matched_paths_digest')))
 OR (json_extract(NEW.binding_json,'$.arm')='code_review' AND NOT EXISTS (
   SELECT 1 FROM interrupts i JOIN runs r ON r.id=i.run_id
   JOIN calibration_entries c ON c.id=i.calibration_id JOIN gate_evaluations e ON e.id=c.gate_evaluation_id
   JOIN gate_input_snapshots s ON s.id=e.snapshot_id
   WHERE i.id=NEW.interrupt_id AND r.change_id=json_extract(NEW.binding_json,'$.change_id')
     AND r.change_head_sha=json_extract(NEW.binding_json,'$.head_sha')
     AND s.head_sha=json_extract(NEW.binding_json,'$.head_sha')
     AND s.effective_policy_hash=json_extract(NEW.binding_json,'$.review_policy_snapshot_digest')))
 OR (json_extract(NEW.binding_json,'$.arm')='merge_conflict' AND NOT EXISTS (
   SELECT 1 FROM interrupts i JOIN runs r ON r.id=i.run_id
   JOIN calibration_entries c ON c.id=i.calibration_id JOIN gate_evaluations e ON e.id=c.gate_evaluation_id
   JOIN gate_input_snapshots s ON s.id=e.snapshot_id
   WHERE i.id=NEW.interrupt_id AND r.change_id=json_extract(NEW.binding_json,'$.change_id')
     AND r.change_head_sha=json_extract(NEW.binding_json,'$.head_sha')
     AND s.head_sha=json_extract(NEW.binding_json,'$.head_sha')
     AND s.conflict_digest=json_extract(NEW.binding_json,'$.conflict_digest')
     AND json_extract(s.canonical_json,'$.change.mergeability')='conflicting'
     AND json_extract(e.verdict_json,'$.mergeability')='conflicting'))
 OR (json_extract(NEW.binding_json,'$.arm')='agent_blocked' AND NOT EXISTS (
   SELECT 1 FROM report_receipts r JOIN interrupts i ON i.id=NEW.interrupt_id
   WHERE r.id=json_extract(NEW.binding_json,'$.report_id') AND r.run_id=i.run_id
     AND r.attempt_no=json_extract(NEW.binding_json,'$.attempt_no') AND r.report_kind='blocker'))
 OR (json_extract(NEW.binding_json,'$.arm') IN ('startup_stall','failure_review_attempt') AND NOT EXISTS (
   SELECT 1 FROM interrupts i JOIN attempts a ON a.run_id=i.run_id AND a.attempt_no=i.attempt_no
   WHERE i.id=NEW.interrupt_id AND json_extract(NEW.binding_json,'$.run_id')=i.run_id
     AND a.attempt_no=json_extract(NEW.binding_json,'$.attempt_no')
     AND a.generation=json_extract(NEW.binding_json,'$.generation')))
 OR (json_extract(NEW.binding_json,'$.arm')='report_quota_failure_review' AND NOT EXISTS (
   SELECT 1 FROM interrupts i JOIN report_quota_exhaustions q ON q.run_id=i.run_id
   JOIN events e ON e.id=q.security_event_id
   WHERE i.id=NEW.interrupt_id AND q.daily_bucket_start_ms=json_extract(NEW.binding_json,'$.daily_bucket_start_ms')
     AND q.daily_bucket_end_ms=json_extract(NEW.binding_json,'$.daily_bucket_end_ms')
     AND q.security_event_id=json_extract(NEW.binding_json,'$.security_event_id') AND e.source='system'))
BEGIN SELECT RAISE(ABORT,'invalid interrupt binding identity'); END;
