-- Keep immutable batch history separate from the current delivery authority.
-- A digest identifies the immutable JSON, but different Interrupts may
-- legitimately carry the same closed binding (for example repeated reports).
DROP INDEX IF EXISTS interrupt_command_effect_bindings_digest;
CREATE TABLE attention_batch_member_authority (
    batch_id TEXT NOT NULL,
    interrupt_id TEXT NOT NULL,
    interrupt_version INTEGER NOT NULL CHECK (interrupt_version > 0),
    nonce TEXT NOT NULL,
    headline TEXT NOT NULL,
    reason TEXT NOT NULL,
    severity TEXT NOT NULL,
    links_json TEXT NOT NULL,
    options_json TEXT NOT NULL,
    updated_at_ms INTEGER NOT NULL,
    PRIMARY KEY (batch_id, interrupt_id),
    FOREIGN KEY (batch_id, interrupt_id) REFERENCES attention_batch_members(batch_id, interrupt_id) ON DELETE RESTRICT,
    FOREIGN KEY (interrupt_id) REFERENCES interrupts(id) ON DELETE RESTRICT
);
INSERT INTO attention_batch_member_authority(batch_id,interrupt_id,interrupt_version,nonce,headline,reason,severity,links_json,options_json,updated_at_ms)
SELECT batch_id,interrupt_id,interrupt_version,nonce,headline,reason,severity,links_json,options_json,joined_at_ms
FROM attention_batch_members;

-- The binding union is closed at the latest schema boundary.  In particular,
-- agent_blocked is bound to its attempt, not to a Report receipt.
DROP TRIGGER IF EXISTS interrupt_binding_exact_shape_insert;
DROP TRIGGER IF EXISTS interrupt_binding_canonical_json_insert;
DROP TRIGGER IF EXISTS interrupt_binding_arm_canonical_order_insert;
DROP TRIGGER IF EXISTS interrupt_binding_identity_insert;
CREATE TRIGGER interrupt_binding_exact_shape_insert
BEFORE INSERT ON interrupt_command_effect_bindings FOR EACH ROW
WHEN
 (json_extract(NEW.binding_json,'$.arm')='design_approval' AND ((SELECT count(*) FROM json_each(NEW.binding_json))<>3 OR EXISTS (SELECT 1 FROM json_each(NEW.binding_json) WHERE key NOT IN ('arm','run_id','task_spec_snapshot_id'))))
 OR (json_extract(NEW.binding_json,'$.arm')='guardrail_violation' AND ((SELECT count(*) FROM json_each(NEW.binding_json))<>5 OR EXISTS (SELECT 1 FROM json_each(NEW.binding_json) WHERE key NOT IN ('arm','head_sha','matched_paths_digest','rule_id','run_id'))))
 OR (json_extract(NEW.binding_json,'$.arm')='code_review' AND ((SELECT count(*) FROM json_each(NEW.binding_json))<>4 OR EXISTS (SELECT 1 FROM json_each(NEW.binding_json) WHERE key NOT IN ('arm','change_id','head_sha','review_policy_snapshot_digest'))))
 OR (json_extract(NEW.binding_json,'$.arm')='agent_blocked' AND ((SELECT count(*) FROM json_each(NEW.binding_json))<>4 OR EXISTS (SELECT 1 FROM json_each(NEW.binding_json) WHERE key NOT IN ('arm','attempt_no','generation','run_id')) OR json_type(NEW.binding_json,'$.attempt_no')<>'integer' OR json_type(NEW.binding_json,'$.generation')<>'integer' OR json_type(NEW.binding_json,'$.run_id')<>'text'))
 OR (json_extract(NEW.binding_json,'$.arm')='merge_conflict' AND ((SELECT count(*) FROM json_each(NEW.binding_json))<>4 OR EXISTS (SELECT 1 FROM json_each(NEW.binding_json) WHERE key NOT IN ('arm','change_id','conflict_digest','head_sha'))))
 OR (json_extract(NEW.binding_json,'$.arm')='startup_stall' AND ((SELECT count(*) FROM json_each(NEW.binding_json))<>4 OR EXISTS (SELECT 1 FROM json_each(NEW.binding_json) WHERE key NOT IN ('arm','attempt_no','generation','run_id'))))
 OR (json_extract(NEW.binding_json,'$.arm')='failure_review_attempt' AND ((SELECT count(*) FROM json_each(NEW.binding_json))<>9 OR EXISTS (SELECT 1 FROM json_each(NEW.binding_json) WHERE key NOT IN ('arm','attempt_no','change_id','generation','head_sha','retry_kind','run_id','terminal_attempt_no','terminal_generation'))))
 OR (json_extract(NEW.binding_json,'$.arm')='report_quota_failure_review' AND ((SELECT count(*) FROM json_each(NEW.binding_json))<>5 OR EXISTS (SELECT 1 FROM json_each(NEW.binding_json) WHERE key NOT IN ('arm','daily_bucket_end_ms','daily_bucket_start_ms','run_id','security_event_id'))))
 OR json_extract(NEW.binding_json,'$.arm') NOT IN ('design_approval','guardrail_violation','code_review','agent_blocked','merge_conflict','startup_stall','failure_review_attempt','report_quota_failure_review')
BEGIN SELECT RAISE(ABORT,'invalid interrupt binding shape'); END;
CREATE TRIGGER interrupt_binding_canonical_json_insert
BEFORE INSERT ON interrupt_command_effect_bindings FOR EACH ROW
WHEN NEW.binding_json <> json(NEW.binding_json)
BEGIN SELECT RAISE(ABORT,'interrupt binding JSON is not canonical'); END;
CREATE TRIGGER interrupt_binding_arm_canonical_order_insert
BEFORE INSERT ON interrupt_command_effect_bindings FOR EACH ROW
WHEN
 (json_extract(NEW.binding_json,'$.arm')='design_approval' AND NEW.binding_json<>json_object('arm','design_approval','run_id',json_extract(NEW.binding_json,'$.run_id'),'task_spec_snapshot_id',json_extract(NEW.binding_json,'$.task_spec_snapshot_id')))
 OR (json_extract(NEW.binding_json,'$.arm')='guardrail_violation' AND NEW.binding_json<>json_object('arm','guardrail_violation','head_sha',json_extract(NEW.binding_json,'$.head_sha'),'matched_paths_digest',json_extract(NEW.binding_json,'$.matched_paths_digest'),'rule_id',json_extract(NEW.binding_json,'$.rule_id'),'run_id',json_extract(NEW.binding_json,'$.run_id')))
 OR (json_extract(NEW.binding_json,'$.arm')='code_review' AND NEW.binding_json<>json_object('arm','code_review','change_id',json_extract(NEW.binding_json,'$.change_id'),'head_sha',json_extract(NEW.binding_json,'$.head_sha'),'review_policy_snapshot_digest',json_extract(NEW.binding_json,'$.review_policy_snapshot_digest')))
 OR (json_extract(NEW.binding_json,'$.arm')='agent_blocked' AND NEW.binding_json<>json_object('arm','agent_blocked','attempt_no',json_extract(NEW.binding_json,'$.attempt_no'),'generation',json_extract(NEW.binding_json,'$.generation'),'run_id',json_extract(NEW.binding_json,'$.run_id')))
 OR (json_extract(NEW.binding_json,'$.arm')='merge_conflict' AND NEW.binding_json<>json_object('arm','merge_conflict','change_id',json_extract(NEW.binding_json,'$.change_id'),'conflict_digest',json_extract(NEW.binding_json,'$.conflict_digest'),'head_sha',json_extract(NEW.binding_json,'$.head_sha')))
 OR (json_extract(NEW.binding_json,'$.arm')='startup_stall' AND NEW.binding_json<>json_object('arm','startup_stall','attempt_no',json_extract(NEW.binding_json,'$.attempt_no'),'generation',json_extract(NEW.binding_json,'$.generation'),'run_id',json_extract(NEW.binding_json,'$.run_id')))
 OR (json_extract(NEW.binding_json,'$.arm')='failure_review_attempt' AND NEW.binding_json<>json_object('arm','failure_review_attempt','attempt_no',json_extract(NEW.binding_json,'$.attempt_no'),'change_id',json_extract(NEW.binding_json,'$.change_id'),'generation',json_extract(NEW.binding_json,'$.generation'),'head_sha',json_extract(NEW.binding_json,'$.head_sha'),'retry_kind',json_extract(NEW.binding_json,'$.retry_kind'),'run_id',json_extract(NEW.binding_json,'$.run_id'),'terminal_attempt_no',json_extract(NEW.binding_json,'$.terminal_attempt_no'),'terminal_generation',json_extract(NEW.binding_json,'$.terminal_generation')))
 OR (json_extract(NEW.binding_json,'$.arm')='report_quota_failure_review' AND NEW.binding_json<>json_object('arm','report_quota_failure_review','daily_bucket_end_ms',json_extract(NEW.binding_json,'$.daily_bucket_end_ms'),'daily_bucket_start_ms',json_extract(NEW.binding_json,'$.daily_bucket_start_ms'),'run_id',json_extract(NEW.binding_json,'$.run_id'),'security_event_id',json_extract(NEW.binding_json,'$.security_event_id')))
BEGIN SELECT RAISE(ABORT,'interrupt binding JSON key order is not canonical'); END;

CREATE TRIGGER interrupt_binding_identity_insert
BEFORE INSERT ON interrupt_command_effect_bindings FOR EACH ROW
WHEN (json_type(NEW.binding_json,'$.run_id')='text' AND NOT EXISTS (SELECT 1 FROM interrupts i WHERE i.id=NEW.interrupt_id AND i.run_id=json_extract(NEW.binding_json,'$.run_id')))
 OR ((json_extract(NEW.binding_json,'$.arm') IN ('startup_stall','failure_review_attempt')) AND NOT EXISTS (SELECT 1 FROM attempts a WHERE a.run_id=json_extract(NEW.binding_json,'$.run_id') AND a.attempt_no=json_extract(NEW.binding_json,'$.attempt_no') AND a.generation=json_extract(NEW.binding_json,'$.generation')))
BEGIN SELECT RAISE(ABORT,'invalid interrupt binding identity'); END;
