-- Failure-review gate rechecks must remain bound to the exact persisted Gate
-- snapshot that produced the source Interrupt. This closes forged SQL writers.
CREATE TRIGGER failure_review_gate_recheck_provenance_insert
BEFORE INSERT ON interrupt_command_effect_bindings FOR EACH ROW
WHEN json_extract(NEW.binding_json,'$.arm')='failure_review_attempt'
 AND json_extract(NEW.binding_json,'$.retry_kind')='gate_recheck'
 AND NOT EXISTS (
   SELECT 1
   FROM interrupts i
   JOIN calibration_entries c ON c.id=i.calibration_id
   JOIN gate_evaluations e ON e.id=c.gate_evaluation_id
   JOIN gate_input_snapshots s ON s.id=e.snapshot_id
   JOIN runs r ON r.id=i.run_id
   WHERE i.id=NEW.interrupt_id
     AND json_extract(NEW.binding_json,'$.change_id')=json_extract(s.canonical_json,'$.identity.change_id')
     AND json_extract(NEW.binding_json,'$.head_sha')=s.head_sha
     AND json_extract(NEW.binding_json,'$.change_id')=r.change_id
     AND json_extract(NEW.binding_json,'$.head_sha')=r.change_head_sha
 )
BEGIN SELECT RAISE(ABORT,'failure review provenance mismatch'); END;
