-- Connect the Report production write port to its durable receipt links.
ALTER TABLE report_receipts ADD COLUMN direct_interrupt_id TEXT REFERENCES interrupts(id);
ALTER TABLE report_receipts ADD COLUMN report_interrupt_charge_entry_id TEXT REFERENCES budget_entries(id);
CREATE UNIQUE INDEX report_receipts_direct_interrupt ON report_receipts(direct_interrupt_id) WHERE direct_interrupt_id IS NOT NULL;
CREATE UNIQUE INDEX report_receipts_report_charge ON report_receipts(report_interrupt_charge_entry_id) WHERE report_interrupt_charge_entry_id IS NOT NULL;

-- Closed binding guards. The application also verifies the canonical digest; these
-- guards make malformed/cross-arm rows fail closed for every SQL writer.
CREATE TRIGGER interrupt_binding_closed_union_insert
BEFORE INSERT ON interrupt_command_effect_bindings FOR EACH ROW
WHEN (json_extract(NEW.binding_json,'$.arm')='report_quota_failure_review' AND
      (NEW.reason <> 'failure_review' OR json_type(NEW.binding_json,'$.run_id') <> 'text' OR
       json_type(NEW.binding_json,'$.daily_bucket_start_ms') <> 'integer' OR
       json_type(NEW.binding_json,'$.daily_bucket_end_ms') <> 'integer' OR
       json_type(NEW.binding_json,'$.security_event_id') <> 'text'))
  OR (json_extract(NEW.binding_json,'$.arm')='failure_review_attempt' AND
      (NEW.reason <> 'failure_review' OR json_type(NEW.binding_json,'$.run_id') <> 'text' OR
       json_type(NEW.binding_json,'$.attempt_no') <> 'integer' OR
       json_type(NEW.binding_json,'$.generation') <> 'integer' OR
       json_type(NEW.binding_json,'$.retry_kind') NOT IN ('text')))
  OR (json_extract(NEW.binding_json,'$.arm') NOT IN ('report_quota_failure_review','failure_review_attempt','design_approval','guardrail_violation','code_review','agent_blocked','merge_conflict','startup_stall'))
BEGIN SELECT RAISE(ABORT, 'invalid closed interrupt binding'); END;

CREATE TRIGGER interrupt_binding_report_shape_insert
BEFORE INSERT ON interrupt_command_effect_bindings FOR EACH ROW
WHEN json_extract(NEW.binding_json,'$.arm')='report_quota_failure_review'
 AND (json_type(NEW.binding_json,'$.attempt_no') IS NOT NULL OR json_type(NEW.binding_json,'$.generation') IS NOT NULL OR json_type(NEW.binding_json,'$.retry_kind') IS NOT NULL OR json_type(NEW.binding_json,'$.change_id') IS NOT NULL OR json_type(NEW.binding_json,'$.head_sha') IS NOT NULL)
BEGIN SELECT RAISE(ABORT, 'report quota binding contains attempt fields'); END;
