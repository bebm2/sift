-- Close collecting-authority retargeting and make every effect-binding arm
-- prove its durable source fact on direct SQL writes.
DROP TRIGGER IF EXISTS attention_batch_member_authority_current_update;

CREATE TRIGGER attention_batch_member_authority_current_update
BEFORE UPDATE ON attention_batch_member_authority FOR EACH ROW
WHEN EXISTS (SELECT 1 FROM attention_batches b WHERE b.id=OLD.batch_id AND b.state='collecting')
 AND NOT EXISTS (
 SELECT 1 FROM attention_batch_members m
 JOIN attention_batches old_batch ON old_batch.id=OLD.batch_id
 JOIN attention_batches new_batch ON new_batch.id=NEW.batch_id
 JOIN interrupts i ON i.id=m.interrupt_id
 WHERE m.batch_id=NEW.batch_id AND m.interrupt_id=NEW.interrupt_id
   AND old_batch.state='collecting' AND new_batch.state='collecting'
   AND i.status='open' AND i.version=NEW.interrupt_version AND i.nonce=NEW.nonce
   AND i.headline=NEW.headline AND i.reason=NEW.reason AND i.severity=NEW.severity
   AND i.links_json=NEW.links_json AND i.options_json=NEW.options_json
)
BEGIN SELECT RAISE(ABORT,'attention batch member authority is not current open Interrupt'); END;

DROP TRIGGER IF EXISTS interrupt_binding_provenance_insert;
CREATE TRIGGER interrupt_binding_provenance_insert
BEFORE INSERT ON interrupt_command_effect_bindings FOR EACH ROW
WHEN
 (json_extract(NEW.binding_json,'$.arm')='agent_blocked' AND NOT EXISTS (
   SELECT 1 FROM report_receipts r JOIN interrupts i ON i.id=NEW.interrupt_id
   WHERE r.id=json_extract(NEW.binding_json,'$.report_id') AND r.run_id=i.run_id
     AND r.attempt_no=json_extract(NEW.binding_json,'$.attempt_no') AND r.report_kind='blocker'))
BEGIN SELECT RAISE(ABORT,'invalid interrupt binding provenance'); END;
