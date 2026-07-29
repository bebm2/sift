-- Close the remaining AdvanceInterrupt P1 boundaries after schema 0042.
-- A collecting authority is a projection for one immutable member identity;
-- changing its primary identity is equivalent to deleting one authority and
-- creating another, so it must never be reachable through UPDATE.
DROP TRIGGER IF EXISTS attention_batch_member_authority_current_update;
CREATE TRIGGER attention_batch_member_authority_current_update
BEFORE UPDATE ON attention_batch_member_authority FOR EACH ROW
WHEN EXISTS (SELECT 1 FROM attention_batches WHERE id=OLD.batch_id AND state='collecting')
 AND (NEW.batch_id IS NOT OLD.batch_id OR NEW.interrupt_id IS NOT OLD.interrupt_id)
BEGIN SELECT RAISE(ABORT,'collecting attention member authority identity is immutable'); END;

-- Every effect-binding arm must retain a durable source fact at the storage
-- boundary.  The identity trigger validates the closed union; this trigger
-- validates the source rows, including merge-conflict evidence.
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
   SELECT 1 FROM interrupts i LEFT JOIN runs r ON r.id=i.run_id
   LEFT JOIN calibration_entries c ON c.id=i.calibration_id LEFT JOIN gate_evaluations e ON e.id=c.gate_evaluation_id
   LEFT JOIN gate_input_snapshots s ON s.id=e.snapshot_id
   WHERE i.id=NEW.interrupt_id AND (i.calibration_id IS NULL OR
     (r.change_id=json_extract(NEW.binding_json,'$.change_id') AND r.change_head_sha=json_extract(NEW.binding_json,'$.head_sha')
      AND s.head_sha=json_extract(NEW.binding_json,'$.head_sha')
      AND s.effective_policy_hash=json_extract(NEW.binding_json,'$.review_policy_snapshot_digest')))))
 OR (json_extract(NEW.binding_json,'$.arm')='merge_conflict' AND NOT EXISTS (
   SELECT 1 FROM interrupts i LEFT JOIN runs r ON r.id=i.run_id
   LEFT JOIN calibration_entries c ON c.id=i.calibration_id LEFT JOIN gate_evaluations e ON e.id=c.gate_evaluation_id
   LEFT JOIN gate_input_snapshots s ON s.id=e.snapshot_id
   WHERE i.id=NEW.interrupt_id AND (i.calibration_id IS NULL OR
     (r.change_id=json_extract(NEW.binding_json,'$.change_id') AND r.change_head_sha=json_extract(NEW.binding_json,'$.head_sha')
      AND s.head_sha=json_extract(NEW.binding_json,'$.head_sha')
      AND json_extract(s.canonical_json,'$.change.mergeability')='conflicting'
      AND json_extract(e.verdict_json,'$.mergeability')='conflicting'))))
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
