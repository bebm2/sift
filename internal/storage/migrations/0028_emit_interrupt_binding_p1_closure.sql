-- Close the remaining direct-SQL binding bypasses left by 0025/0027. The
-- agent_blocked arm includes report_id, which keeps separate Report receipts
-- from colliding on the immutable binding digest.
DROP TRIGGER IF EXISTS interrupt_binding_exact_shape_insert;
CREATE TRIGGER interrupt_binding_exact_shape_insert
BEFORE INSERT ON interrupt_command_effect_bindings FOR EACH ROW
WHEN
 (json_extract(NEW.binding_json,'$.arm')='design_approval' AND ((SELECT count(*) FROM json_each(NEW.binding_json))<>3 OR EXISTS (SELECT 1 FROM json_each(NEW.binding_json) WHERE key NOT IN ('arm','run_id','task_spec_snapshot_id'))))
 OR (json_extract(NEW.binding_json,'$.arm')='guardrail_violation' AND ((SELECT count(*) FROM json_each(NEW.binding_json))<>5 OR EXISTS (SELECT 1 FROM json_each(NEW.binding_json) WHERE key NOT IN ('arm','run_id','head_sha','rule_id','matched_paths_digest'))))
 OR (json_extract(NEW.binding_json,'$.arm')='code_review' AND ((SELECT count(*) FROM json_each(NEW.binding_json))<>4 OR EXISTS (SELECT 1 FROM json_each(NEW.binding_json) WHERE key NOT IN ('arm','change_id','head_sha','review_policy_snapshot_digest'))))
 OR (json_extract(NEW.binding_json,'$.arm')='agent_blocked' AND ((SELECT count(*) FROM json_each(NEW.binding_json))<>5 OR EXISTS (SELECT 1 FROM json_each(NEW.binding_json) WHERE key NOT IN ('arm','run_id','attempt_no','generation','report_id')) OR json_type(NEW.binding_json,'$.report_id')<>'text'))
 OR (json_extract(NEW.binding_json,'$.arm')='merge_conflict' AND ((SELECT count(*) FROM json_each(NEW.binding_json))<>4 OR EXISTS (SELECT 1 FROM json_each(NEW.binding_json) WHERE key NOT IN ('arm','change_id','head_sha','conflict_digest'))))
 OR (json_extract(NEW.binding_json,'$.arm')='startup_stall' AND ((SELECT count(*) FROM json_each(NEW.binding_json))<>4 OR EXISTS (SELECT 1 FROM json_each(NEW.binding_json) WHERE key NOT IN ('arm','run_id','attempt_no','generation'))))
 OR (json_extract(NEW.binding_json,'$.arm')='failure_review_attempt' AND ((SELECT count(*) FROM json_each(NEW.binding_json))<>9 OR EXISTS (SELECT 1 FROM json_each(NEW.binding_json) WHERE key NOT IN ('arm','run_id','attempt_no','generation','retry_kind','change_id','head_sha','terminal_attempt_no','terminal_generation'))))
 OR (json_extract(NEW.binding_json,'$.arm')='report_quota_failure_review' AND ((SELECT count(*) FROM json_each(NEW.binding_json))<>5 OR EXISTS (SELECT 1 FROM json_each(NEW.binding_json) WHERE key NOT IN ('arm','run_id','daily_bucket_start_ms','daily_bucket_end_ms','security_event_id'))))
BEGIN SELECT RAISE(ABORT,'invalid interrupt binding shape'); END;

CREATE TRIGGER interrupt_binding_canonical_json_insert
BEFORE INSERT ON interrupt_command_effect_bindings FOR EACH ROW
WHEN NEW.binding_json <> json(NEW.binding_json)
BEGIN SELECT RAISE(ABORT,'interrupt binding JSON is not canonical'); END;

CREATE TRIGGER interrupt_binding_arm_canonical_order_insert
BEFORE INSERT ON interrupt_command_effect_bindings FOR EACH ROW
WHEN
  (json_extract(NEW.binding_json,'$.arm')='agent_blocked' AND NEW.binding_json<>json_object('arm','agent_blocked','attempt_no',json_extract(NEW.binding_json,'$.attempt_no'),'generation',json_extract(NEW.binding_json,'$.generation'),'report_id',json_extract(NEW.binding_json,'$.report_id'),'run_id',json_extract(NEW.binding_json,'$.run_id')))
  OR (json_extract(NEW.binding_json,'$.arm')='startup_stall' AND NEW.binding_json<>json_object('arm','startup_stall','attempt_no',json_extract(NEW.binding_json,'$.attempt_no'),'generation',json_extract(NEW.binding_json,'$.generation'),'run_id',json_extract(NEW.binding_json,'$.run_id')))
  OR (json_extract(NEW.binding_json,'$.arm')='failure_review_attempt' AND NEW.binding_json<>json_object('arm','failure_review_attempt','attempt_no',json_extract(NEW.binding_json,'$.attempt_no'),'change_id',json_extract(NEW.binding_json,'$.change_id'),'generation',json_extract(NEW.binding_json,'$.generation'),'head_sha',json_extract(NEW.binding_json,'$.head_sha'),'retry_kind',json_extract(NEW.binding_json,'$.retry_kind'),'run_id',json_extract(NEW.binding_json,'$.run_id'),'terminal_attempt_no',json_extract(NEW.binding_json,'$.terminal_attempt_no'),'terminal_generation',json_extract(NEW.binding_json,'$.terminal_generation')))
  OR (json_extract(NEW.binding_json,'$.arm')='report_quota_failure_review' AND NEW.binding_json<>json_object('arm','report_quota_failure_review','daily_bucket_end_ms',json_extract(NEW.binding_json,'$.daily_bucket_end_ms'),'daily_bucket_start_ms',json_extract(NEW.binding_json,'$.daily_bucket_start_ms'),'run_id',json_extract(NEW.binding_json,'$.run_id'),'security_event_id',json_extract(NEW.binding_json,'$.security_event_id')))
BEGIN SELECT RAISE(ABORT,'interrupt binding JSON key order is not canonical'); END;

CREATE TRIGGER interrupt_binding_identity_insert
BEFORE INSERT ON interrupt_command_effect_bindings FOR EACH ROW
WHEN
  (json_extract(NEW.binding_json,'$.arm')='design_approval' AND NOT EXISTS (
    SELECT 1 FROM task_spec_snapshots s JOIN interrupts i ON i.id=NEW.interrupt_id
    WHERE s.id=json_extract(NEW.binding_json,'$.task_spec_snapshot_id') AND s.run_id=i.run_id))
  OR (json_extract(NEW.binding_json,'$.arm')='agent_blocked'
      AND (SELECT attempt_no FROM interrupts WHERE id=NEW.interrupt_id) IS NOT NULL
      AND NOT EXISTS (
        SELECT 1 FROM attempts a JOIN interrupts i ON i.id=NEW.interrupt_id
        WHERE a.run_id=json_extract(NEW.binding_json,'$.run_id')
          AND a.attempt_no=json_extract(NEW.binding_json,'$.attempt_no')
          AND a.generation=json_extract(NEW.binding_json,'$.generation')
          AND i.run_id=a.run_id AND i.attempt_no=a.attempt_no))
  OR (json_extract(NEW.binding_json,'$.arm')='startup_stall' AND NOT EXISTS (
    SELECT 1 FROM attempts a JOIN interrupts i ON i.id=NEW.interrupt_id
    WHERE a.run_id=json_extract(NEW.binding_json,'$.run_id')
      AND a.attempt_no=json_extract(NEW.binding_json,'$.attempt_no')
      AND a.generation=json_extract(NEW.binding_json,'$.generation')
      AND i.run_id=a.run_id AND i.attempt_no=a.attempt_no))
  OR (json_extract(NEW.binding_json,'$.arm')='failure_review_attempt'
      AND NOT EXISTS (SELECT 1 FROM attempts a WHERE a.run_id=json_extract(NEW.binding_json,'$.run_id') AND a.attempt_no=json_extract(NEW.binding_json,'$.attempt_no') AND a.generation=json_extract(NEW.binding_json,'$.generation')))
  OR (json_extract(NEW.binding_json,'$.arm')='failure_review_attempt' AND json_extract(NEW.binding_json,'$.retry_kind')='new_attempt' AND (
      json_type(NEW.binding_json,'$.change_id')<>'null' OR json_type(NEW.binding_json,'$.head_sha')<>'null'
      OR json_extract(NEW.binding_json,'$.terminal_attempt_no')<>json_extract(NEW.binding_json,'$.attempt_no')
      OR json_extract(NEW.binding_json,'$.terminal_generation')<>json_extract(NEW.binding_json,'$.generation')
      OR NOT EXISTS (SELECT 1 FROM attempts a WHERE a.run_id=json_extract(NEW.binding_json,'$.run_id') AND a.attempt_no=json_extract(NEW.binding_json,'$.attempt_no') AND a.generation=json_extract(NEW.binding_json,'$.generation') AND a.phase='finished' AND ((a.result_exit_code IS NOT NULL AND a.result_exit_code<>0) OR a.result_signal IS NOT NULL))))
  OR (json_extract(NEW.binding_json,'$.arm')='failure_review_attempt' AND json_extract(NEW.binding_json,'$.retry_kind')='gate_recheck' AND (
      json_type(NEW.binding_json,'$.change_id')<>'text' OR json_type(NEW.binding_json,'$.head_sha')<>'text'
      OR json_type(NEW.binding_json,'$.terminal_attempt_no')<>'null' OR json_type(NEW.binding_json,'$.terminal_generation')<>'null'))
BEGIN SELECT RAISE(ABORT,'invalid interrupt binding identity'); END;

CREATE TRIGGER interrupt_binding_failure_options_exact_insert
BEFORE INSERT ON interrupt_command_effect_bindings FOR EACH ROW
WHEN
  (json_extract(NEW.binding_json,'$.arm')='failure_review_attempt' AND
   (SELECT options_json FROM interrupts WHERE id=NEW.interrupt_id) <> '[{"effect":"再次执行","id":"retry","label":"重试失败步骤","risk":"相同故障可能再次发生"},{"effect":"Run 停止","id":"reject","label":"停止 Run","risk":"需人工重新发起"},{"effect":"保持等待","id":"hold","label":"暂缓决定","risk":"Run 继续占用待处理项"}]')
  OR (json_extract(NEW.binding_json,'$.arm')='report_quota_failure_review' AND
   (SELECT options_json FROM interrupts WHERE id=NEW.interrupt_id) <> '[{"effect":"Run 停止","id":"reject","label":"停止 Run","risk":"需人工重新发起"},{"effect":"保持 Interrupt 人工 held","id":"hold","label":"暂缓决定","risk":"Run 继续运行"}]')
BEGIN SELECT RAISE(ABORT,'binding options are not canonical'); END;
