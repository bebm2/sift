-- Preserve the frozen critical-fuse configuration and restore the 0018
-- forward invariants that 0021's table rebuild accidentally removed.
ALTER TABLE attention_batches ADD COLUMN critical_window_ms INTEGER;
ALTER TABLE attention_batches ADD COLUMN critical_total_limit INTEGER;
ALTER TABLE attention_batches ADD COLUMN critical_per_run_limit INTEGER;
UPDATE attention_batches
SET critical_window_ms = COALESCE(critical_window_ms, (SELECT i.critical_window_ms FROM attention_batch_members m JOIN interrupts i ON i.id=m.interrupt_id WHERE m.batch_id=attention_batches.id ORDER BY m.joined_at_ms LIMIT 1)),
    critical_total_limit = COALESCE(critical_total_limit, (SELECT i.critical_total_limit FROM attention_batch_members m JOIN interrupts i ON i.id=m.interrupt_id WHERE m.batch_id=attention_batches.id ORDER BY m.joined_at_ms LIMIT 1)),
    critical_per_run_limit = COALESCE(critical_per_run_limit, (SELECT i.critical_per_run_limit FROM attention_batch_members m JOIN interrupts i ON i.id=m.interrupt_id WHERE m.batch_id=attention_batches.id ORDER BY m.joined_at_ms LIMIT 1))
WHERE kind='critical_fuse';

CREATE TRIGGER interrupts_nonce_issued_required_insert
BEFORE INSERT ON interrupts FOR EACH ROW
WHEN NEW.nonce_issued_at_ms IS NULL
BEGIN SELECT RAISE(ABORT, 'interrupt nonce_issued_at_ms is required'); END;
CREATE TRIGGER interrupts_startup_stall_max_reject_insert
BEFORE INSERT ON interrupts FOR EACH ROW
WHEN NEW.reason = 'startup_stall' AND NEW.on_max_escalations = 'auto_reject'
BEGIN SELECT RAISE(ABORT, 'startup_stall cannot auto reject at max escalations'); END;

DROP TRIGGER IF EXISTS interrupt_binding_closed_union_insert;
DROP TRIGGER IF EXISTS interrupt_binding_exact_shape_insert;
DROP TRIGGER IF EXISTS interrupt_binding_variant_options_insert;

CREATE TRIGGER interrupt_binding_closed_union_insert
BEFORE INSERT ON interrupt_command_effect_bindings FOR EACH ROW
WHEN NEW.binding_schema_version <> 1
 OR json_valid(NEW.binding_json)=0
 OR length(NEW.binding_digest)<>64
 OR lower(hex(sift_sha256(NEW.binding_json)))<>lower(NEW.binding_digest)
 OR NEW.reason NOT IN ('design_approval','guardrail_violation','code_review','agent_blocked','merge_conflict','failure_review','startup_stall')
 OR json_extract(NEW.binding_json,'$.arm') NOT IN ('design_approval','guardrail_violation','code_review','agent_blocked','merge_conflict','startup_stall','failure_review_attempt','report_quota_failure_review')
 OR (json_extract(NEW.binding_json,'$.arm') IN ('failure_review_attempt','report_quota_failure_review') AND NEW.reason<>'failure_review')
 OR (json_extract(NEW.binding_json,'$.arm') NOT IN ('failure_review_attempt','report_quota_failure_review') AND NEW.reason<>json_extract(NEW.binding_json,'$.arm'))
 OR NOT EXISTS (SELECT 1 FROM interrupts i WHERE i.id=NEW.interrupt_id AND i.reason=NEW.reason)
 OR (json_extract(NEW.binding_json,'$.arm') NOT IN ('code_review','merge_conflict') AND (json_type(NEW.binding_json,'$.run_id')<>'text' OR json_extract(NEW.binding_json,'$.run_id')<>(SELECT run_id FROM interrupts WHERE id=NEW.interrupt_id)))
BEGIN SELECT RAISE(ABORT,'invalid closed interrupt binding'); END;

CREATE TRIGGER interrupt_binding_exact_shape_insert
BEFORE INSERT ON interrupt_command_effect_bindings FOR EACH ROW
WHEN
 (json_extract(NEW.binding_json,'$.arm')='design_approval' AND ((SELECT count(*) FROM json_each(NEW.binding_json))<>3 OR EXISTS (SELECT 1 FROM json_each(NEW.binding_json) WHERE key NOT IN ('arm','run_id','task_spec_snapshot_id'))))
 OR (json_extract(NEW.binding_json,'$.arm')='guardrail_violation' AND ((SELECT count(*) FROM json_each(NEW.binding_json))<>5 OR EXISTS (SELECT 1 FROM json_each(NEW.binding_json) WHERE key NOT IN ('arm','run_id','head_sha','rule_id','matched_paths_digest'))))
 OR (json_extract(NEW.binding_json,'$.arm')='code_review' AND ((SELECT count(*) FROM json_each(NEW.binding_json))<>4 OR EXISTS (SELECT 1 FROM json_each(NEW.binding_json) WHERE key NOT IN ('arm','change_id','head_sha','review_policy_snapshot_digest'))))
 OR (json_extract(NEW.binding_json,'$.arm')='agent_blocked' AND ((SELECT count(*) FROM json_each(NEW.binding_json))<>4 OR EXISTS (SELECT 1 FROM json_each(NEW.binding_json) WHERE key NOT IN ('arm','run_id','attempt_no','generation'))))
 OR (json_extract(NEW.binding_json,'$.arm')='merge_conflict' AND ((SELECT count(*) FROM json_each(NEW.binding_json))<>4 OR EXISTS (SELECT 1 FROM json_each(NEW.binding_json) WHERE key NOT IN ('arm','change_id','head_sha','conflict_digest'))))
 OR (json_extract(NEW.binding_json,'$.arm')='startup_stall' AND ((SELECT count(*) FROM json_each(NEW.binding_json))<>4 OR EXISTS (SELECT 1 FROM json_each(NEW.binding_json) WHERE key NOT IN ('arm','run_id','attempt_no','generation'))))
 OR (json_extract(NEW.binding_json,'$.arm')='failure_review_attempt' AND ((SELECT count(*) FROM json_each(NEW.binding_json))<>9 OR EXISTS (SELECT 1 FROM json_each(NEW.binding_json) WHERE key NOT IN ('arm','run_id','attempt_no','generation','retry_kind','change_id','head_sha','terminal_attempt_no','terminal_generation'))))
 OR (json_extract(NEW.binding_json,'$.arm')='report_quota_failure_review' AND ((SELECT count(*) FROM json_each(NEW.binding_json))<>5 OR EXISTS (SELECT 1 FROM json_each(NEW.binding_json) WHERE key NOT IN ('arm','run_id','daily_bucket_start_ms','daily_bucket_end_ms','security_event_id'))))
BEGIN SELECT RAISE(ABORT,'invalid interrupt binding shape'); END;

CREATE TRIGGER interrupt_binding_variant_options_insert
BEFORE INSERT ON interrupt_command_effect_bindings FOR EACH ROW
WHEN (json_extract(NEW.binding_json,'$.arm')='report_quota_failure_review' AND (SELECT json_array_length(i.options_json) FROM interrupts i WHERE i.id=NEW.interrupt_id)<>2)
 OR (json_extract(NEW.binding_json,'$.arm')='report_quota_failure_review' AND EXISTS (SELECT 1 FROM interrupts i WHERE i.id=NEW.interrupt_id AND (json_extract(i.options_json,'$[0].id')<>'reject' OR json_extract(i.options_json,'$[1].id')<>'hold')))
 OR (json_extract(NEW.binding_json,'$.arm')='failure_review_attempt' AND (SELECT json_array_length(i.options_json) FROM interrupts i WHERE i.id=NEW.interrupt_id)<>3)
 OR (json_extract(NEW.binding_json,'$.arm')='failure_review_attempt' AND EXISTS (SELECT 1 FROM interrupts i WHERE i.id=NEW.interrupt_id AND (json_extract(i.options_json,'$[0].id')<>'retry' OR json_extract(i.options_json,'$[1].id')<>'reject' OR json_extract(i.options_json,'$[2].id')<>'hold')))
BEGIN SELECT RAISE(ABORT,'binding options cross variant'); END;
