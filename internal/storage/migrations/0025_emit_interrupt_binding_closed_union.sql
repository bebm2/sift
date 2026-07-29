-- Complete the T4 binding union after 0022.  The writer computes the
-- canonical SHA-256; this trigger closes the SQL shape, ownership and option
-- invariants for every arm so direct SQL writers cannot bypass the port.
DROP TRIGGER IF EXISTS interrupt_binding_closed_union_insert;
DROP TRIGGER IF EXISTS interrupt_binding_exact_shape_insert;
DROP TRIGGER IF EXISTS interrupt_binding_variant_options_insert;

CREATE TRIGGER interrupt_binding_closed_union_insert
BEFORE INSERT ON interrupt_command_effect_bindings FOR EACH ROW
WHEN json_valid(NEW.binding_json) = 0
  OR NEW.binding_schema_version <> 1
  OR NEW.binding_digest IS NULL
  OR length(NEW.binding_digest) <> 64
  OR NEW.binding_digest GLOB '*[^0-9a-f]*'
  OR lower(hex(sift_sha256(NEW.binding_json))) <> NEW.binding_digest
  OR NEW.reason NOT IN ('design_approval','guardrail_violation','code_review','agent_blocked','merge_conflict','failure_review','startup_stall')
  OR json_extract(NEW.binding_json,'$.arm') NOT IN ('design_approval','guardrail_violation','code_review','agent_blocked','merge_conflict','startup_stall','failure_review_attempt','report_quota_failure_review')
  OR (json_extract(NEW.binding_json,'$.arm') IN ('failure_review_attempt','report_quota_failure_review') AND NEW.reason <> 'failure_review')
  OR (json_extract(NEW.binding_json,'$.arm') NOT IN ('failure_review_attempt','report_quota_failure_review') AND NEW.reason <> json_extract(NEW.binding_json,'$.arm'))
  OR NOT EXISTS (SELECT 1 FROM interrupts i WHERE i.id=NEW.interrupt_id AND i.reason=NEW.reason)
  OR json_type(NEW.binding_json,'$.run_id') <> 'text'
  OR json_extract(NEW.binding_json,'$.run_id') <> (SELECT run_id FROM interrupts WHERE id=NEW.interrupt_id)
  OR (json_extract(NEW.binding_json,'$.arm') IN ('startup_stall','failure_review_attempt') AND NOT EXISTS (SELECT 1 FROM attempts a WHERE a.run_id=json_extract(NEW.binding_json,'$.run_id') AND a.attempt_no=json_extract(NEW.binding_json,'$.attempt_no') AND a.generation=json_extract(NEW.binding_json,'$.generation')))
  OR (json_extract(NEW.binding_json,'$.arm')='startup_stall' AND NOT EXISTS (SELECT 1 FROM interrupts i WHERE i.id=NEW.interrupt_id AND i.attempt_no=json_extract(NEW.binding_json,'$.attempt_no')))
  OR (json_extract(NEW.binding_json,'$.arm')='failure_review_attempt' AND json_extract(NEW.binding_json,'$.retry_kind')='new_attempt' AND NOT EXISTS (SELECT 1 FROM attempts a WHERE a.run_id=json_extract(NEW.binding_json,'$.run_id') AND a.attempt_no=json_extract(NEW.binding_json,'$.attempt_no') AND a.generation=json_extract(NEW.binding_json,'$.generation') AND a.phase='finished' AND ((a.result_exit_code IS NOT NULL AND a.result_exit_code<>0) OR a.result_signal IS NOT NULL)))
  OR (json_extract(NEW.binding_json,'$.arm')='report_quota_failure_review' AND NOT EXISTS (SELECT 1 FROM report_quota_exhaustions q WHERE q.run_id=json_extract(NEW.binding_json,'$.run_id') AND q.daily_bucket_start_ms=json_extract(NEW.binding_json,'$.daily_bucket_start_ms') AND q.daily_bucket_end_ms=json_extract(NEW.binding_json,'$.daily_bucket_end_ms') AND q.security_event_id=json_extract(NEW.binding_json,'$.security_event_id')))
BEGIN SELECT RAISE(ABORT,'invalid closed interrupt binding'); END;

CREATE TRIGGER interrupt_binding_exact_shape_insert
BEFORE INSERT ON interrupt_command_effect_bindings FOR EACH ROW
WHEN
 (json_extract(NEW.binding_json,'$.arm')='design_approval' AND ((SELECT count(*) FROM json_each(NEW.binding_json))<>3 OR EXISTS (SELECT 1 FROM json_each(NEW.binding_json) WHERE key NOT IN ('arm','run_id','task_spec_snapshot_id')) OR json_type(NEW.binding_json,'$.task_spec_snapshot_id')<>'text'))
 OR (json_extract(NEW.binding_json,'$.arm')='guardrail_violation' AND ((SELECT count(*) FROM json_each(NEW.binding_json))<>4 OR EXISTS (SELECT 1 FROM json_each(NEW.binding_json) WHERE key NOT IN ('arm','run_id','rule_id','matched_paths_digest')) OR json_type(NEW.binding_json,'$.rule_id')<>'text' OR json_type(NEW.binding_json,'$.matched_paths_digest')<>'text'))
 OR (json_extract(NEW.binding_json,'$.arm')='code_review' AND ((SELECT count(*) FROM json_each(NEW.binding_json))<>4 OR EXISTS (SELECT 1 FROM json_each(NEW.binding_json) WHERE key NOT IN ('arm','run_id','change_id','head_sha')) OR json_type(NEW.binding_json,'$.change_id')<>'text' OR json_type(NEW.binding_json,'$.head_sha')<>'text'))
 OR (json_extract(NEW.binding_json,'$.arm')='agent_blocked' AND ((SELECT count(*) FROM json_each(NEW.binding_json))<>5 OR EXISTS (SELECT 1 FROM json_each(NEW.binding_json) WHERE key NOT IN ('arm','run_id','attempt_no','generation','report_id')) OR json_type(NEW.binding_json,'$.attempt_no')<>'integer' OR json_type(NEW.binding_json,'$.generation')<>'integer' OR (json_type(NEW.binding_json,'$.report_id') NOT IN ('text','null'))))
 OR (json_extract(NEW.binding_json,'$.arm')='merge_conflict' AND ((SELECT count(*) FROM json_each(NEW.binding_json))<>5 OR EXISTS (SELECT 1 FROM json_each(NEW.binding_json) WHERE key NOT IN ('arm','run_id','change_id','head_sha','conflict_digest')) OR json_type(NEW.binding_json,'$.change_id')<>'text' OR json_type(NEW.binding_json,'$.head_sha')<>'text' OR json_type(NEW.binding_json,'$.conflict_digest')<>'text'))
 OR (json_extract(NEW.binding_json,'$.arm')='startup_stall' AND ((SELECT count(*) FROM json_each(NEW.binding_json))<>4 OR EXISTS (SELECT 1 FROM json_each(NEW.binding_json) WHERE key NOT IN ('arm','run_id','attempt_no','generation')) OR json_type(NEW.binding_json,'$.attempt_no')<>'integer' OR json_type(NEW.binding_json,'$.generation')<>'integer'))
 OR (json_extract(NEW.binding_json,'$.arm')='failure_review_attempt' AND ((SELECT count(*) FROM json_each(NEW.binding_json))<>9 OR EXISTS (SELECT 1 FROM json_each(NEW.binding_json) WHERE key NOT IN ('arm','run_id','attempt_no','generation','retry_kind','change_id','head_sha','terminal_attempt_no','terminal_generation')) OR json_type(NEW.binding_json,'$.attempt_no')<>'integer' OR json_type(NEW.binding_json,'$.generation')<>'integer' OR json_extract(NEW.binding_json,'$.retry_kind') NOT IN ('new_attempt','gate_recheck')))
 OR (json_extract(NEW.binding_json,'$.arm')='report_quota_failure_review' AND ((SELECT count(*) FROM json_each(NEW.binding_json))<>5 OR EXISTS (SELECT 1 FROM json_each(NEW.binding_json) WHERE key NOT IN ('arm','run_id','daily_bucket_start_ms','daily_bucket_end_ms','security_event_id')) OR json_type(NEW.binding_json,'$.daily_bucket_start_ms')<>'integer' OR json_type(NEW.binding_json,'$.daily_bucket_end_ms')<>'integer' OR json_type(NEW.binding_json,'$.security_event_id')<>'text'))
BEGIN SELECT RAISE(ABORT,'invalid interrupt binding shape'); END;

CREATE TRIGGER interrupt_binding_variant_options_insert
BEFORE INSERT ON interrupt_command_effect_bindings FOR EACH ROW
WHEN (json_extract(NEW.binding_json,'$.arm')='report_quota_failure_review' AND (SELECT json_array_length(i.options_json) FROM interrupts i WHERE i.id=NEW.interrupt_id)<>2)
  OR (json_extract(NEW.binding_json,'$.arm')='report_quota_failure_review' AND EXISTS (SELECT 1 FROM interrupts i WHERE i.id=NEW.interrupt_id AND (json_extract(i.options_json,'$[0].id')<>'reject' OR json_extract(i.options_json,'$[1].id')<>'hold')))
  OR (json_extract(NEW.binding_json,'$.arm')='failure_review_attempt' AND (SELECT json_array_length(i.options_json) FROM interrupts i WHERE i.id=NEW.interrupt_id)<>3)
  OR (json_extract(NEW.binding_json,'$.arm')='failure_review_attempt' AND EXISTS (SELECT 1 FROM interrupts i WHERE i.id=NEW.interrupt_id AND (json_extract(i.options_json,'$[0].id')<>'retry' OR json_extract(i.options_json,'$[1].id')<>'reject' OR json_extract(i.options_json,'$[2].id')<>'hold')))
BEGIN SELECT RAISE(ABORT,'binding options cross variant'); END;
