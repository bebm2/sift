-- Restore the closed effect-binding uniqueness contract removed by 0033, and
-- constrain the mutable collecting-batch authority to the current Interrupt.
-- Immutable member rows remain the admission history; this projection is only
-- the pre-seal current delivery authority.
CREATE UNIQUE INDEX interrupt_command_effect_bindings_digest
ON interrupt_command_effect_bindings(binding_digest);

-- Reinstall the closed binding guards with the report identity required
-- to keep distinct blocker reports distinct under binding_digest uniqueness.
-- Close the EmitInterrupt binding union after the Report owner fix.  The
-- binding row is only valid when its JSON is the exact typed, canonical arm
-- emitted for the source Interrupt; direct SQL must not manufacture privilege.
DROP TRIGGER IF EXISTS interrupt_binding_exact_shape_insert;
DROP TRIGGER IF EXISTS interrupt_binding_canonical_json_insert;
DROP TRIGGER IF EXISTS interrupt_binding_arm_canonical_order_insert;
DROP TRIGGER IF EXISTS interrupt_binding_identity_insert;

CREATE TRIGGER interrupt_binding_exact_shape_insert
BEFORE INSERT ON interrupt_command_effect_bindings FOR EACH ROW
WHEN
  (json_extract(NEW.binding_json,'$.arm')='design_approval' AND ((SELECT count(*) FROM json_each(NEW.binding_json))<>3 OR EXISTS (SELECT 1 FROM json_each(NEW.binding_json) WHERE key NOT IN ('arm','run_id','task_spec_snapshot_id')) OR json_type(NEW.binding_json,'$.run_id')<>'text' OR json_type(NEW.binding_json,'$.task_spec_snapshot_id')<>'text'))
 OR (json_extract(NEW.binding_json,'$.arm')='guardrail_violation' AND ((SELECT count(*) FROM json_each(NEW.binding_json))<>5 OR EXISTS (SELECT 1 FROM json_each(NEW.binding_json) WHERE key NOT IN ('arm','head_sha','matched_paths_digest','rule_id','run_id')) OR json_type(NEW.binding_json,'$.head_sha')<>'text' OR json_type(NEW.binding_json,'$.matched_paths_digest')<>'text' OR json_type(NEW.binding_json,'$.rule_id')<>'text' OR json_type(NEW.binding_json,'$.run_id')<>'text'))
 OR (json_extract(NEW.binding_json,'$.arm')='code_review' AND ((SELECT count(*) FROM json_each(NEW.binding_json))<>4 OR EXISTS (SELECT 1 FROM json_each(NEW.binding_json) WHERE key NOT IN ('arm','change_id','head_sha','review_policy_snapshot_digest')) OR json_type(NEW.binding_json,'$.change_id')<>'text' OR json_type(NEW.binding_json,'$.head_sha')<>'text' OR json_type(NEW.binding_json,'$.review_policy_snapshot_digest')<>'text'))
 OR (json_extract(NEW.binding_json,'$.arm')='agent_blocked' AND ((SELECT count(*) FROM json_each(NEW.binding_json))<>5 OR EXISTS (SELECT 1 FROM json_each(NEW.binding_json) WHERE key NOT IN ('arm','attempt_no','generation','report_id','run_id')) OR json_type(NEW.binding_json,'$.attempt_no')<>'integer' OR json_type(NEW.binding_json,'$.generation')<>'integer' OR json_type(NEW.binding_json,'$.report_id') NOT IN ('text','null') OR json_type(NEW.binding_json,'$.run_id')<>'text'))
 OR (json_extract(NEW.binding_json,'$.arm')='merge_conflict' AND ((SELECT count(*) FROM json_each(NEW.binding_json))<>4 OR EXISTS (SELECT 1 FROM json_each(NEW.binding_json) WHERE key NOT IN ('arm','change_id','conflict_digest','head_sha')) OR json_type(NEW.binding_json,'$.change_id')<>'text' OR json_type(NEW.binding_json,'$.conflict_digest')<>'text' OR json_type(NEW.binding_json,'$.head_sha')<>'text'))
 OR (json_extract(NEW.binding_json,'$.arm')='startup_stall' AND ((SELECT count(*) FROM json_each(NEW.binding_json))<>4 OR EXISTS (SELECT 1 FROM json_each(NEW.binding_json) WHERE key NOT IN ('arm','attempt_no','generation','run_id')) OR json_type(NEW.binding_json,'$.attempt_no')<>'integer' OR json_type(NEW.binding_json,'$.generation')<>'integer' OR json_type(NEW.binding_json,'$.run_id')<>'text'))
 OR (json_extract(NEW.binding_json,'$.arm')='failure_review_attempt' AND ((SELECT count(*) FROM json_each(NEW.binding_json))<>9 OR EXISTS (SELECT 1 FROM json_each(NEW.binding_json) WHERE key NOT IN ('arm','attempt_no','change_id','generation','head_sha','retry_kind','run_id','terminal_attempt_no','terminal_generation')) OR json_type(NEW.binding_json,'$.attempt_no')<>'integer' OR json_type(NEW.binding_json,'$.generation')<>'integer' OR json_type(NEW.binding_json,'$.retry_kind')<>'text' OR json_type(NEW.binding_json,'$.run_id')<>'text' OR (json_extract(NEW.binding_json,'$.retry_kind')='new_attempt' AND (json_type(NEW.binding_json,'$.change_id')<>'null' OR json_type(NEW.binding_json,'$.head_sha')<>'null' OR json_type(NEW.binding_json,'$.terminal_attempt_no')<>'integer' OR json_type(NEW.binding_json,'$.terminal_generation')<>'integer')) OR (json_extract(NEW.binding_json,'$.retry_kind')='gate_recheck' AND (json_type(NEW.binding_json,'$.change_id')<>'text' OR json_type(NEW.binding_json,'$.head_sha')<>'text' OR json_type(NEW.binding_json,'$.terminal_attempt_no')<>'null' OR json_type(NEW.binding_json,'$.terminal_generation')<>'null'))))
 OR (json_extract(NEW.binding_json,'$.arm')='report_quota_failure_review' AND ((SELECT count(*) FROM json_each(NEW.binding_json))<>5 OR EXISTS (SELECT 1 FROM json_each(NEW.binding_json) WHERE key NOT IN ('arm','daily_bucket_end_ms','daily_bucket_start_ms','run_id','security_event_id')) OR json_type(NEW.binding_json,'$.daily_bucket_end_ms')<>'integer' OR json_type(NEW.binding_json,'$.daily_bucket_start_ms')<>'integer' OR json_type(NEW.binding_json,'$.run_id')<>'text' OR json_type(NEW.binding_json,'$.security_event_id')<>'text'))
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
 OR (json_extract(NEW.binding_json,'$.arm')='agent_blocked' AND NEW.binding_json<>json_object('arm','agent_blocked','attempt_no',json_extract(NEW.binding_json,'$.attempt_no'),'generation',json_extract(NEW.binding_json,'$.generation'),'report_id',json_extract(NEW.binding_json,'$.report_id'),'run_id',json_extract(NEW.binding_json,'$.run_id')))
 OR (json_extract(NEW.binding_json,'$.arm')='merge_conflict' AND NEW.binding_json<>json_object('arm','merge_conflict','change_id',json_extract(NEW.binding_json,'$.change_id'),'conflict_digest',json_extract(NEW.binding_json,'$.conflict_digest'),'head_sha',json_extract(NEW.binding_json,'$.head_sha')))
 OR (json_extract(NEW.binding_json,'$.arm')='startup_stall' AND NEW.binding_json<>json_object('arm','startup_stall','attempt_no',json_extract(NEW.binding_json,'$.attempt_no'),'generation',json_extract(NEW.binding_json,'$.generation'),'run_id',json_extract(NEW.binding_json,'$.run_id')))
 OR (json_extract(NEW.binding_json,'$.arm')='failure_review_attempt' AND NEW.binding_json<>json_object('arm','failure_review_attempt','attempt_no',json_extract(NEW.binding_json,'$.attempt_no'),'change_id',json_extract(NEW.binding_json,'$.change_id'),'generation',json_extract(NEW.binding_json,'$.generation'),'head_sha',json_extract(NEW.binding_json,'$.head_sha'),'retry_kind',json_extract(NEW.binding_json,'$.retry_kind'),'run_id',json_extract(NEW.binding_json,'$.run_id'),'terminal_attempt_no',json_extract(NEW.binding_json,'$.terminal_attempt_no'),'terminal_generation',json_extract(NEW.binding_json,'$.terminal_generation')))
 OR (json_extract(NEW.binding_json,'$.arm')='report_quota_failure_review' AND NEW.binding_json<>json_object('arm','report_quota_failure_review','daily_bucket_end_ms',json_extract(NEW.binding_json,'$.daily_bucket_end_ms'),'daily_bucket_start_ms',json_extract(NEW.binding_json,'$.daily_bucket_start_ms'),'run_id',json_extract(NEW.binding_json,'$.run_id'),'security_event_id',json_extract(NEW.binding_json,'$.security_event_id')))
BEGIN SELECT RAISE(ABORT,'interrupt binding JSON key order is not canonical'); END;

-- Bind command effects to the frozen Run and Gate facts, not merely to a
-- canonical JSON shape.  This closes the direct-SQL forged-effect bypass.
DROP TRIGGER IF EXISTS interrupt_binding_identity_insert;
DROP TRIGGER IF EXISTS interrupt_binding_value_types_insert;

CREATE TRIGGER interrupt_binding_value_types_insert
BEFORE INSERT ON interrupt_command_effect_bindings FOR EACH ROW
WHEN
 (json_extract(NEW.binding_json,'$.arm')='design_approval' AND (json_type(NEW.binding_json,'$.run_id')<>'text' OR json_type(NEW.binding_json,'$.task_spec_snapshot_id')<>'text'))
 OR (json_extract(NEW.binding_json,'$.arm')='guardrail_violation' AND (json_type(NEW.binding_json,'$.run_id')<>'text' OR json_type(NEW.binding_json,'$.head_sha')<>'text' OR json_type(NEW.binding_json,'$.rule_id')<>'text' OR json_type(NEW.binding_json,'$.matched_paths_digest')<>'text'))
 OR (json_extract(NEW.binding_json,'$.arm')='code_review' AND (json_type(NEW.binding_json,'$.change_id')<>'text' OR json_type(NEW.binding_json,'$.head_sha')<>'text' OR json_type(NEW.binding_json,'$.review_policy_snapshot_digest')<>'text'))
 OR (json_extract(NEW.binding_json,'$.arm')='agent_blocked' AND (json_type(NEW.binding_json,'$.run_id')<>'text' OR json_type(NEW.binding_json,'$.attempt_no')<>'integer' OR json_type(NEW.binding_json,'$.generation')<>'integer' OR json_type(NEW.binding_json,'$.report_id')<>'text'))
 OR (json_extract(NEW.binding_json,'$.arm')='merge_conflict' AND (json_type(NEW.binding_json,'$.change_id')<>'text' OR json_type(NEW.binding_json,'$.head_sha')<>'text' OR json_type(NEW.binding_json,'$.conflict_digest')<>'text'))
 OR (json_extract(NEW.binding_json,'$.arm')='startup_stall' AND (json_type(NEW.binding_json,'$.run_id')<>'text' OR json_type(NEW.binding_json,'$.attempt_no')<>'integer' OR json_type(NEW.binding_json,'$.generation')<>'integer'))
 OR (json_extract(NEW.binding_json,'$.arm')='failure_review_attempt' AND (json_type(NEW.binding_json,'$.run_id')<>'text' OR json_type(NEW.binding_json,'$.attempt_no')<>'integer' OR json_type(NEW.binding_json,'$.generation')<>'integer' OR json_type(NEW.binding_json,'$.retry_kind')<>'text'))
 OR (json_extract(NEW.binding_json,'$.arm')='report_quota_failure_review' AND (json_type(NEW.binding_json,'$.run_id')<>'text' OR json_type(NEW.binding_json,'$.daily_bucket_start_ms')<>'integer' OR json_type(NEW.binding_json,'$.daily_bucket_end_ms')<>'integer' OR json_type(NEW.binding_json,'$.security_event_id')<>'text'))
BEGIN SELECT RAISE(ABORT,'invalid interrupt binding value types'); END;

CREATE TRIGGER interrupt_binding_identity_insert
BEFORE INSERT ON interrupt_command_effect_bindings FOR EACH ROW
WHEN
 NEW.reason <> (CASE WHEN json_extract(NEW.binding_json,'$.arm') IN ('failure_review_attempt','report_quota_failure_review') THEN 'failure_review' ELSE json_extract(NEW.binding_json,'$.arm') END)
 OR (json_extract(NEW.binding_json,'$.arm')='design_approval' AND NOT EXISTS (
   SELECT 1 FROM task_spec_snapshots s JOIN interrupts i ON i.id=NEW.interrupt_id
   WHERE s.id=json_extract(NEW.binding_json,'$.task_spec_snapshot_id') AND s.run_id=i.run_id AND json_extract(NEW.binding_json,'$.run_id')=i.run_id))
 OR (json_extract(NEW.binding_json,'$.arm')='code_review' AND NOT EXISTS (
   SELECT 1 FROM interrupts i JOIN runs r ON r.id=i.run_id LEFT JOIN calibration_entries c ON c.id=i.calibration_id LEFT JOIN gate_evaluations e ON e.id=c.gate_evaluation_id LEFT JOIN gate_input_snapshots s ON s.id=e.snapshot_id
   WHERE i.id=NEW.interrupt_id AND (i.calibration_id IS NULL OR r.change_id IS NULL OR json_extract(NEW.binding_json,'$.review_policy_snapshot_digest')='' OR (e.run_id=i.run_id AND r.change_id=json_extract(NEW.binding_json,'$.change_id') AND r.change_head_sha=json_extract(NEW.binding_json,'$.head_sha') AND s.head_sha=json_extract(NEW.binding_json,'$.head_sha') AND s.effective_policy_hash=json_extract(NEW.binding_json,'$.review_policy_snapshot_digest')))))
 OR (json_extract(NEW.binding_json,'$.arm')='merge_conflict' AND NOT EXISTS (
   SELECT 1 FROM interrupts i JOIN runs r ON r.id=i.run_id
   WHERE i.id=NEW.interrupt_id AND (r.change_id IS NULL OR (r.change_id=json_extract(NEW.binding_json,'$.change_id') AND r.change_head_sha=json_extract(NEW.binding_json,'$.head_sha')))))
 OR (json_extract(NEW.binding_json,'$.arm')='guardrail_violation' AND NOT EXISTS (
   SELECT 1 FROM interrupts i JOIN calibration_entries c ON c.id=i.calibration_id JOIN gate_evaluations e ON e.id=c.gate_evaluation_id JOIN gate_input_snapshots s ON s.id=e.snapshot_id
   WHERE i.id=NEW.interrupt_id AND e.run_id=i.run_id AND json_extract(NEW.binding_json,'$.run_id')=i.run_id AND s.head_sha=json_extract(NEW.binding_json,'$.head_sha') AND json_extract(e.verdict_json,'$.rule_id')=json_extract(NEW.binding_json,'$.rule_id') AND json_extract(e.verdict_json,'$.matched_paths_digest')=json_extract(NEW.binding_json,'$.matched_paths_digest')))
 OR (json_extract(NEW.binding_json,'$.arm') IN ('agent_blocked','startup_stall','failure_review_attempt') AND NOT EXISTS (
   SELECT 1 FROM interrupts i LEFT JOIN attempts a ON a.run_id=i.run_id AND a.attempt_no=i.attempt_no
   WHERE i.id=NEW.interrupt_id AND (i.attempt_no IS NULL OR (json_extract(NEW.binding_json,'$.run_id')=i.run_id AND a.generation=json_extract(NEW.binding_json,'$.generation') AND a.attempt_no=json_extract(NEW.binding_json,'$.attempt_no')))))
 OR (json_extract(NEW.binding_json,'$.arm')='failure_review_attempt' AND json_extract(NEW.binding_json,'$.retry_kind')='new_attempt' AND NOT EXISTS (
   SELECT 1 FROM attempts a JOIN interrupts i ON i.id=NEW.interrupt_id WHERE a.run_id=i.run_id AND a.attempt_no=json_extract(NEW.binding_json,'$.attempt_no') AND a.generation=json_extract(NEW.binding_json,'$.generation') AND a.phase='finished' AND ((a.result_exit_code IS NOT NULL AND a.result_exit_code<>0) OR a.result_signal IS NOT NULL) AND json_extract(NEW.binding_json,'$.terminal_attempt_no')=json_extract(NEW.binding_json,'$.attempt_no') AND json_extract(NEW.binding_json,'$.terminal_generation')=json_extract(NEW.binding_json,'$.generation'))
 OR (json_extract(NEW.binding_json,'$.arm')='report_quota_failure_review' AND NOT EXISTS (
   SELECT 1 FROM interrupts i JOIN report_quota_exhaustions q ON q.run_id=i.run_id JOIN events e ON e.id=q.security_event_id WHERE i.id=NEW.interrupt_id AND q.daily_bucket_start_ms=json_extract(NEW.binding_json,'$.daily_bucket_start_ms') AND q.daily_bucket_end_ms=json_extract(NEW.binding_json,'$.daily_bucket_end_ms') AND q.security_event_id=json_extract(NEW.binding_json,'$.security_event_id') AND e.source='system')))
BEGIN SELECT RAISE(ABORT,'invalid interrupt binding identity'); END;

CREATE TRIGGER attention_batch_member_authority_current_insert
BEFORE INSERT ON attention_batch_member_authority FOR EACH ROW
WHEN NOT EXISTS (
 SELECT 1
 FROM attention_batch_members m
 JOIN attention_batches b ON b.id=m.batch_id
 JOIN interrupts i ON i.id=m.interrupt_id
 WHERE m.batch_id=NEW.batch_id AND m.interrupt_id=NEW.interrupt_id
   AND b.state='collecting'
   AND i.version=NEW.interrupt_version AND i.nonce=NEW.nonce
   AND i.headline=NEW.headline AND i.reason=NEW.reason AND i.severity=NEW.severity
   AND i.links_json=NEW.links_json AND i.options_json=NEW.options_json
)
BEGIN SELECT RAISE(ABORT,'attention batch member authority is not current'); END;

CREATE TRIGGER attention_batch_member_authority_current_update
BEFORE UPDATE ON attention_batch_member_authority FOR EACH ROW
WHEN EXISTS (SELECT 1 FROM attention_batches b WHERE b.id=OLD.batch_id AND b.state='collecting')
 AND NOT EXISTS (
 SELECT 1
 FROM attention_batch_members m
 JOIN interrupts i ON i.id=m.interrupt_id
 WHERE m.batch_id=NEW.batch_id AND m.interrupt_id=NEW.interrupt_id
   AND i.version=NEW.interrupt_version AND i.nonce=NEW.nonce
   AND i.headline=NEW.headline AND i.reason=NEW.reason AND i.severity=NEW.severity
   AND i.links_json=NEW.links_json AND i.options_json=NEW.options_json
)
BEGIN SELECT RAISE(ABORT,'attention batch member authority is not current'); END;
