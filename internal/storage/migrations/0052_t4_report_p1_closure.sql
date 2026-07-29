-- Gate failure-review bindings must be a closed provenance chain for the same
-- Run, including the Run identity frozen inside the canonical Gate input.
DROP TRIGGER IF EXISTS failure_review_gate_recheck_provenance_insert;
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
     AND c.run_id=i.run_id
     AND e.run_id=i.run_id
     AND json_extract(s.canonical_json,'$.identity.run_id')=i.run_id
     AND json_extract(NEW.binding_json,'$.change_id')=json_extract(s.canonical_json,'$.identity.change_id')
     AND json_extract(NEW.binding_json,'$.head_sha')=s.head_sha
     AND json_extract(NEW.binding_json,'$.change_id')=r.change_id
     AND json_extract(NEW.binding_json,'$.head_sha')=r.change_head_sha
 )
BEGIN SELECT RAISE(ABORT,'failure review provenance mismatch'); END;

-- A quota exhaustion is durable before its best-effort failure_review. Keep a
-- single audit identity for a structural rejection so replay cannot emit a
-- second diagnostic or conceal the failed generation.
CREATE TABLE report_emission_diagnostics (
    generation_key TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES runs(id),
    security_event_id TEXT NOT NULL REFERENCES events(id),
    event_id TEXT NOT NULL UNIQUE REFERENCES events(id),
    disposition TEXT NOT NULL CHECK (disposition IN ('structural_rejected')),
    created_at_ms INTEGER NOT NULL
);
CREATE TRIGGER report_emission_diagnostics_append_only_update
BEFORE UPDATE ON report_emission_diagnostics FOR EACH ROW
BEGIN SELECT RAISE(ABORT,'report_emission_diagnostics is append-only'); END;
CREATE TRIGGER report_emission_diagnostics_append_only_delete
BEFORE DELETE ON report_emission_diagnostics FOR EACH ROW
BEGIN SELECT RAISE(ABORT,'report_emission_diagnostics is append-only'); END;
