-- Close Report receipt linking and the interrupt binding union for all SQL writers.
-- 0019 guarded only two partial arms; this migration replaces it with exact
-- tagged-object checks and option-set checks against the bound Interrupt.
DROP TRIGGER IF EXISTS report_receipts_append_only_update;
CREATE TRIGGER report_receipts_append_only_update
BEFORE UPDATE ON report_receipts FOR EACH ROW
WHEN NOT (OLD.direct_interrupt_id IS NULL AND NEW.direct_interrupt_id IS NOT NULL
          AND NEW.id IS OLD.id AND NEW.run_id IS OLD.run_id AND NEW.attempt_no IS OLD.attempt_no
          AND NEW.report_key IS OLD.report_key AND NEW.report_kind IS OLD.report_kind
          AND NEW.payload_digest IS OLD.payload_digest AND NEW.event_id IS OLD.event_id
          AND NEW.received_at_ms IS OLD.received_at_ms
          AND NEW.report_interrupt_charge_entry_id IS OLD.report_interrupt_charge_entry_id)
BEGIN SELECT RAISE(ABORT, 'append-only table'); END;

DROP TRIGGER IF EXISTS interrupt_binding_closed_union_insert;
DROP TRIGGER IF EXISTS interrupt_binding_report_shape_insert;
CREATE TRIGGER interrupt_binding_closed_union_insert
BEFORE INSERT ON interrupt_command_effect_bindings FOR EACH ROW
WHEN json_valid(NEW.binding_json) = 0
  OR NEW.binding_schema_version <> 1
  OR length(NEW.binding_digest) <> 64
  OR NEW.binding_digest GLOB '*[^0-9a-f]*'
  OR NEW.reason <> json_extract(NEW.binding_json, '$.arm')
     AND json_extract(NEW.binding_json, '$.arm') NOT IN ('failure_review_attempt', 'report_quota_failure_review')
  OR (json_extract(NEW.binding_json, '$.arm') IN ('failure_review_attempt', 'report_quota_failure_review') AND NEW.reason <> 'failure_review')
  OR json_extract(NEW.binding_json, '$.arm') NOT IN ('design_approval','guardrail_violation','code_review','agent_blocked','merge_conflict','startup_stall','failure_review_attempt','report_quota_failure_review')
  OR NOT EXISTS (SELECT 1 FROM interrupts i WHERE i.id=NEW.interrupt_id AND i.reason=NEW.reason)
BEGIN SELECT RAISE(ABORT, 'invalid closed interrupt binding'); END;

CREATE TRIGGER interrupt_binding_exact_shape_insert
BEFORE INSERT ON interrupt_command_effect_bindings FOR EACH ROW
WHEN
 (json_extract(NEW.binding_json,'$.arm')='design_approval' AND (SELECT count(*) FROM json_each(NEW.binding_json))<>3)
 OR (json_extract(NEW.binding_json,'$.arm')='guardrail_violation' AND (SELECT count(*) FROM json_each(NEW.binding_json))<>4)
 OR (json_extract(NEW.binding_json,'$.arm')='code_review' AND (SELECT count(*) FROM json_each(NEW.binding_json))<>4)
 OR (json_extract(NEW.binding_json,'$.arm')='agent_blocked' AND (SELECT count(*) FROM json_each(NEW.binding_json))<>5)
 OR (json_extract(NEW.binding_json,'$.arm')='merge_conflict' AND (SELECT count(*) FROM json_each(NEW.binding_json))<>5)
 OR (json_extract(NEW.binding_json,'$.arm')='startup_stall' AND (SELECT count(*) FROM json_each(NEW.binding_json))<>4)
 OR (json_extract(NEW.binding_json,'$.arm')='failure_review_attempt' AND (SELECT count(*) FROM json_each(NEW.binding_json))<>9)
 OR (json_extract(NEW.binding_json,'$.arm')='report_quota_failure_review' AND (SELECT count(*) FROM json_each(NEW.binding_json))<>5)
 OR json_type(NEW.binding_json,'$.run_id') <> 'text'
 OR (json_extract(NEW.binding_json,'$.arm')='failure_review_attempt' AND (json_type(NEW.binding_json,'$.attempt_no') <> 'integer' OR json_type(NEW.binding_json,'$.generation') <> 'integer' OR json_extract(NEW.binding_json,'$.retry_kind') NOT IN ('new_attempt','gate_recheck')
      OR (json_extract(NEW.binding_json,'$.retry_kind')='new_attempt' AND (json_extract(NEW.binding_json,'$.terminal_attempt_no') <> json_extract(NEW.binding_json,'$.attempt_no') OR json_extract(NEW.binding_json,'$.terminal_generation') <> json_extract(NEW.binding_json,'$.generation') OR json_type(NEW.binding_json,'$.change_id') <> 'null' OR json_type(NEW.binding_json,'$.head_sha') <> 'null'))
      OR (json_extract(NEW.binding_json,'$.retry_kind')='gate_recheck' AND (json_type(NEW.binding_json,'$.change_id') <> 'text' OR json_type(NEW.binding_json,'$.head_sha') <> 'text' OR json_type(NEW.binding_json,'$.terminal_attempt_no') <> 'null' OR json_type(NEW.binding_json,'$.terminal_generation') <> 'null'))))
 OR (json_extract(NEW.binding_json,'$.arm')='report_quota_failure_review' AND (json_type(NEW.binding_json,'$.daily_bucket_start_ms') <> 'integer' OR json_type(NEW.binding_json,'$.daily_bucket_end_ms') <> 'integer' OR json_type(NEW.binding_json,'$.security_event_id') <> 'text' OR NOT EXISTS (SELECT 1 FROM report_quota_exhaustions q WHERE q.run_id=json_extract(NEW.binding_json,'$.run_id') AND q.daily_bucket_start_ms=json_extract(NEW.binding_json,'$.daily_bucket_start_ms') AND q.daily_bucket_end_ms=json_extract(NEW.binding_json,'$.daily_bucket_end_ms') AND q.security_event_id=json_extract(NEW.binding_json,'$.security_event_id'))))
BEGIN SELECT RAISE(ABORT, 'invalid interrupt binding shape'); END;

CREATE TRIGGER interrupt_binding_variant_options_insert
BEFORE INSERT ON interrupt_command_effect_bindings FOR EACH ROW
WHEN (json_extract(NEW.binding_json,'$.arm')='report_quota_failure_review' AND EXISTS (SELECT 1 FROM interrupts i, json_each(i.options_json) o WHERE i.id=NEW.interrupt_id AND json_extract(o.value,'$.id') NOT IN ('reject','hold')))
  OR (json_extract(NEW.binding_json,'$.arm')='failure_review_attempt' AND EXISTS (SELECT 1 FROM interrupts i, json_each(i.options_json) o WHERE i.id=NEW.interrupt_id AND json_extract(o.value,'$.id') NOT IN ('retry','reject','hold')))
BEGIN SELECT RAISE(ABORT, 'binding options cross variant'); END;
