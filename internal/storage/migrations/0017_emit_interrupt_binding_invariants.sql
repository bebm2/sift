-- Enforce the v1 immutable binding identity for rows written after T4 closure.
-- SQLite cannot add a CHECK constraint to the historical table, so these
-- insert-only guards preserve the forward-only migration contract.
CREATE TRIGGER interrupt_command_effect_bindings_closed_insert
BEFORE INSERT ON interrupt_command_effect_bindings FOR EACH ROW
WHEN NEW.reason NOT IN ('design_approval','guardrail_violation','code_review','agent_blocked','merge_conflict','failure_review','startup_stall')
  OR NEW.binding_schema_version <> 1
  OR NEW.binding_digest IS NULL
  OR NOT json_valid(NEW.binding_json)
  OR json_extract(NEW.binding_json,'$.arm') IS NULL
BEGIN SELECT RAISE(ABORT, 'invalid interrupt command effect binding'); END;

CREATE TRIGGER interrupt_command_effect_bindings_failed_attempt_insert
BEFORE INSERT ON interrupt_command_effect_bindings FOR EACH ROW
WHEN json_extract(NEW.binding_json,'$.arm') = 'failure_review_attempt'
 AND json_extract(NEW.binding_json,'$.retry_kind') = 'new_attempt'
 AND NOT EXISTS (
   SELECT 1 FROM attempts
   WHERE run_id=json_extract(NEW.binding_json,'$.run_id')
     AND attempt_no=json_extract(NEW.binding_json,'$.attempt_no')
     AND generation=json_extract(NEW.binding_json,'$.generation')
     AND phase='finished'
     AND (result_exit_code IS NOT NULL AND result_exit_code <> 0 OR result_signal IS NOT NULL)
 )
BEGIN SELECT RAISE(ABORT, 'failure_review binding requires failed attempt'); END;

CREATE TRIGGER interrupt_command_effect_bindings_report_quota_insert
BEFORE INSERT ON interrupt_command_effect_bindings FOR EACH ROW
WHEN json_extract(NEW.binding_json,'$.arm') = 'report_quota_failure_review'
 AND NOT EXISTS (
   SELECT 1 FROM report_quota_exhaustions
   WHERE run_id=json_extract(NEW.binding_json,'$.run_id')
     AND daily_bucket_start_ms=json_extract(NEW.binding_json,'$.daily_bucket_start_ms')
     AND daily_bucket_end_ms=json_extract(NEW.binding_json,'$.daily_bucket_end_ms')
     AND security_event_id=json_extract(NEW.binding_json,'$.security_event_id')
 )
BEGIN SELECT RAISE(ABORT, 'report quota binding mismatch'); END;
