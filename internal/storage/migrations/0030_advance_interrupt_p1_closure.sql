-- Close the remaining durable effect-binding references.  The JSON shape and
-- canonical option checks remain owned by 0028; these checks bind every
-- reference to facts that are present for the source Interrupt. Initial
-- code-review/merge and agent-blocked emissions precede durable Change/attempt
-- creation, so their frozen source facts are validated by their owner ports.
CREATE TRIGGER interrupt_binding_effect_references_insert
BEFORE INSERT ON interrupt_command_effect_bindings FOR EACH ROW
WHEN
  (json_extract(NEW.binding_json,'$.arm')='design_approval' AND NOT EXISTS (
    SELECT 1 FROM task_spec_snapshots s JOIN interrupts i ON i.id=NEW.interrupt_id
    WHERE s.id=json_extract(NEW.binding_json,'$.task_spec_snapshot_id') AND s.run_id=i.run_id))
  OR (json_extract(NEW.binding_json,'$.arm')='startup_stall' AND NOT EXISTS (
    SELECT 1 FROM attempts a JOIN interrupts i ON i.id=NEW.interrupt_id
    WHERE a.run_id=i.run_id AND a.attempt_no=json_extract(NEW.binding_json,'$.attempt_no')
      AND a.generation=json_extract(NEW.binding_json,'$.generation') AND i.attempt_no=a.attempt_no))
  OR (json_extract(NEW.binding_json,'$.arm')='failure_review_attempt' AND NOT EXISTS (
    SELECT 1 FROM attempts a JOIN interrupts i ON i.id=NEW.interrupt_id
    WHERE a.run_id=i.run_id AND a.attempt_no=json_extract(NEW.binding_json,'$.attempt_no')
      AND a.generation=json_extract(NEW.binding_json,'$.generation') AND i.attempt_no=a.attempt_no))
  OR (json_extract(NEW.binding_json,'$.arm')='failure_review_attempt'
      AND json_extract(NEW.binding_json,'$.retry_kind')='new_attempt' AND NOT EXISTS (
    SELECT 1 FROM attempts a JOIN interrupts i ON i.id=NEW.interrupt_id
    WHERE a.run_id=i.run_id AND a.attempt_no=json_extract(NEW.binding_json,'$.attempt_no')
      AND a.generation=json_extract(NEW.binding_json,'$.generation') AND a.phase='finished'
      AND ((a.result_exit_code IS NOT NULL AND a.result_exit_code<>0) OR a.result_signal IS NOT NULL)))
  OR (json_extract(NEW.binding_json,'$.arm')='report_quota_failure_review' AND NOT EXISTS (
    SELECT 1 FROM report_quota_exhaustions q JOIN interrupts i ON i.id=NEW.interrupt_id
    JOIN events e ON e.id=q.security_event_id
    WHERE q.run_id=i.run_id AND q.daily_bucket_start_ms=json_extract(NEW.binding_json,'$.daily_bucket_start_ms')
      AND q.daily_bucket_end_ms=json_extract(NEW.binding_json,'$.daily_bucket_end_ms')
      AND q.security_event_id=json_extract(NEW.binding_json,'$.security_event_id') AND e.source='system'))
BEGIN SELECT RAISE(ABORT,'invalid interrupt binding effect reference'); END;
