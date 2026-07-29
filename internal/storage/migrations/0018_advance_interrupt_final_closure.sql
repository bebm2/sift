-- Preserve historical frozen values. 0013 is already released and must not be
-- rewritten; only missing nonce issue times can be safely completed here.
UPDATE interrupts
SET nonce_issued_at_ms = created_at_ms
WHERE nonce_issued_at_ms IS NULL;

-- Replace the pre-contract run_transition placeholder with a durable, explicit
-- historical arm. It is deliberately per Interrupt so the legacy digest key
-- cannot collapse several bindings from the same Run.
DROP TRIGGER IF EXISTS interrupt_command_effect_bindings_append_only_update;
DROP TRIGGER IF EXISTS interrupt_command_effect_bindings_append_only_delete;
UPDATE interrupt_command_effect_bindings
SET reason = (SELECT reason FROM interrupts WHERE interrupts.id = interrupt_command_effect_bindings.interrupt_id),
    binding_json = json_object(
      'arm', (SELECT reason FROM interrupts WHERE interrupts.id = interrupt_command_effect_bindings.interrupt_id),
      'run_id', (SELECT run_id FROM interrupts WHERE interrupts.id = interrupt_command_effect_bindings.interrupt_id),
      'legacy_interrupt_id', interrupt_id
    ),
    binding_digest = 'legacy:' || interrupt_id
WHERE binding_json = '{"arm":"run_transition"}';
CREATE TRIGGER interrupt_command_effect_bindings_append_only_update
BEFORE UPDATE ON interrupt_command_effect_bindings FOR EACH ROW
BEGIN SELECT RAISE(ABORT, 'append-only table'); END;
CREATE TRIGGER interrupt_command_effect_bindings_append_only_delete
BEFORE DELETE ON interrupt_command_effect_bindings FOR EACH ROW
BEGIN SELECT RAISE(ABORT, 'append-only table'); END;

-- SQLite cannot add NOT NULL/CHECK constraints to existing columns. These
-- guards apply to every subsequent write while preserving readable history.
CREATE TRIGGER interrupts_nonce_issued_required_insert
BEFORE INSERT ON interrupts
WHEN NEW.nonce_issued_at_ms IS NULL
BEGIN SELECT RAISE(ABORT, 'interrupt nonce_issued_at_ms is required'); END;
CREATE TRIGGER interrupts_startup_stall_max_reject_insert
BEFORE INSERT ON interrupts
WHEN NEW.reason = 'startup_stall' AND NEW.on_max_escalations = 'auto_reject'
BEGIN SELECT RAISE(ABORT, 'startup_stall cannot auto reject at max escalations'); END;
