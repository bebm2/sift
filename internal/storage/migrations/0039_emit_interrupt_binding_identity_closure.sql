-- Bind every newly inserted effect binding to its immutable source facts.  Old
-- rows are retained unchanged; no legacy-shaped bypass applies to new writes.
DROP TRIGGER IF EXISTS interrupt_binding_identity_insert;

CREATE TRIGGER interrupt_binding_identity_insert
BEFORE INSERT ON interrupt_command_effect_bindings FOR EACH ROW
WHEN
 NEW.reason <> (CASE WHEN json_extract(NEW.binding_json,'$.arm') IN ('failure_review_attempt','report_quota_failure_review') THEN 'failure_review' ELSE json_extract(NEW.binding_json,'$.arm') END)
 OR (json_extract(NEW.binding_json,'$.arm')='design_approval' AND NOT EXISTS (
   SELECT 1 FROM task_spec_snapshots s JOIN interrupts i ON i.id=NEW.interrupt_id
   WHERE s.id=json_extract(NEW.binding_json,'$.task_spec_snapshot_id') AND s.run_id=i.run_id AND json_extract(NEW.binding_json,'$.run_id')=i.run_id))
 OR (json_extract(NEW.binding_json,'$.arm')='code_review' AND NOT EXISTS (
   SELECT 1 FROM interrupts i JOIN runs r ON r.id=i.run_id JOIN calibration_entries c ON c.id=i.calibration_id JOIN gate_evaluations e ON e.id=c.gate_evaluation_id JOIN gate_input_snapshots s ON s.id=e.snapshot_id
   WHERE i.id=NEW.interrupt_id AND e.run_id=i.run_id AND r.change_id=json_extract(NEW.binding_json,'$.change_id') AND s.head_sha=json_extract(NEW.binding_json,'$.head_sha') AND s.effective_policy_hash=json_extract(NEW.binding_json,'$.review_policy_snapshot_digest')))
 OR (json_extract(NEW.binding_json,'$.arm')='merge_conflict' AND NOT EXISTS (
   SELECT 1 FROM interrupts i JOIN runs r ON r.id=i.run_id
   WHERE i.id=NEW.interrupt_id AND r.change_id=json_extract(NEW.binding_json,'$.change_id') AND r.change_head_sha=json_extract(NEW.binding_json,'$.head_sha')))
 OR (json_extract(NEW.binding_json,'$.arm')='guardrail_violation' AND NOT EXISTS (
   SELECT 1 FROM interrupts i JOIN calibration_entries c ON c.id=i.calibration_id JOIN gate_evaluations e ON e.id=c.gate_evaluation_id JOIN gate_input_snapshots s ON s.id=e.snapshot_id
   WHERE i.id=NEW.interrupt_id AND e.run_id=i.run_id AND json_extract(NEW.binding_json,'$.run_id')=i.run_id AND s.head_sha=json_extract(NEW.binding_json,'$.head_sha') AND json_extract(e.verdict_json,'$.rule_id')=json_extract(NEW.binding_json,'$.rule_id') AND json_extract(e.verdict_json,'$.matched_paths_digest')=json_extract(NEW.binding_json,'$.matched_paths_digest')))
 OR (json_extract(NEW.binding_json,'$.arm') IN ('agent_blocked','startup_stall','failure_review_attempt') AND NOT EXISTS (
   SELECT 1 FROM interrupts i JOIN attempts a ON a.run_id=i.run_id AND a.attempt_no=i.attempt_no
   WHERE i.id=NEW.interrupt_id AND json_extract(NEW.binding_json,'$.run_id')=i.run_id AND a.generation=json_extract(NEW.binding_json,'$.generation') AND a.attempt_no=json_extract(NEW.binding_json,'$.attempt_no')))
 OR (json_extract(NEW.binding_json,'$.arm')='failure_review_attempt' AND json_extract(NEW.binding_json,'$.retry_kind')='new_attempt' AND NOT EXISTS (
   SELECT 1 FROM attempts a JOIN interrupts i ON i.id=NEW.interrupt_id WHERE a.run_id=i.run_id AND a.attempt_no=json_extract(NEW.binding_json,'$.attempt_no') AND a.generation=json_extract(NEW.binding_json,'$.generation') AND a.phase='finished' AND ((a.result_exit_code IS NOT NULL AND a.result_exit_code<>0) OR a.result_signal IS NOT NULL) AND json_extract(NEW.binding_json,'$.terminal_attempt_no')=json_extract(NEW.binding_json,'$.attempt_no') AND json_extract(NEW.binding_json,'$.terminal_generation')=json_extract(NEW.binding_json,'$.generation')))
 OR (json_extract(NEW.binding_json,'$.arm')='failure_review_attempt' AND json_extract(NEW.binding_json,'$.retry_kind')='gate_recheck' AND NOT EXISTS (
   SELECT 1 FROM interrupts i JOIN runs r ON r.id=i.run_id JOIN calibration_entries c ON c.id=i.calibration_id JOIN gate_evaluations e ON e.id=c.gate_evaluation_id JOIN gate_input_snapshots s ON s.id=e.snapshot_id
   WHERE i.id=NEW.interrupt_id AND e.run_id=i.run_id AND r.change_id=json_extract(NEW.binding_json,'$.change_id') AND s.head_sha=json_extract(NEW.binding_json,'$.head_sha')))
 OR (json_extract(NEW.binding_json,'$.arm')='report_quota_failure_review' AND NOT EXISTS (
   SELECT 1 FROM interrupts i JOIN report_quota_exhaustions q ON q.run_id=i.run_id JOIN events e ON e.id=q.security_event_id WHERE i.id=NEW.interrupt_id AND q.daily_bucket_start_ms=json_extract(NEW.binding_json,'$.daily_bucket_start_ms') AND q.daily_bucket_end_ms=json_extract(NEW.binding_json,'$.daily_bucket_end_ms') AND q.security_event_id=json_extract(NEW.binding_json,'$.security_event_id') AND e.source='system'))
BEGIN SELECT RAISE(ABORT,'invalid interrupt binding identity'); END;
