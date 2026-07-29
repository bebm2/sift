-- Close Channel authority gaps left by the additive 0012/0014 projections.
-- Historical nullable columns remain readable, but no new Channel authority row may
-- omit its frozen identity and sealed rows cannot be retargeted.
CREATE TRIGGER IF NOT EXISTS channel_delivery_authority_required
BEFORE INSERT ON interrupt_deliveries
WHEN NEW.surface='channel' AND (
  NEW.delivery_id IS NULL OR NEW.channel_id IS NULL OR NEW.channel_snapshot_json IS NULL OR
  NEW.interrupt_version IS NULL OR NEW.nonce IS NULL OR NEW.escalation_no IS NULL OR
  NEW.forge_kind IS NULL OR NEW.forge_host IS NULL OR NEW.forge_project_key IS NULL OR
  NEW.forge_alert_target_kind IS NULL OR NEW.forge_alert_target_id IS NULL
)
BEGIN SELECT RAISE(ABORT,'channel delivery requires complete frozen authority'); END;

CREATE TRIGGER IF NOT EXISTS attention_batch_authority_required
BEFORE INSERT ON attention_batches
WHEN NEW.project_id IS NULL OR NEW.channel_id IS NULL OR NEW.channel_snapshot_json IS NULL OR
     NEW.forge_kind IS NULL OR NEW.forge_host IS NULL OR NEW.forge_project_key IS NULL OR
     NEW.target_kind IS NULL OR NEW.target_id IS NULL OR NEW.kind IS NULL OR
     NEW.delivery_id IS NULL OR NEW.scope IS NULL OR NEW.scope_id IS NULL OR
     NEW.due_at_ms IS NULL OR NEW.created_at_ms IS NULL
BEGIN SELECT RAISE(ABORT,'attention batch requires complete frozen authority'); END;

CREATE TRIGGER IF NOT EXISTS attention_batch_no_failed_state
BEFORE INSERT ON attention_batches
WHEN NEW.state='failed'
BEGIN SELECT RAISE(ABORT,'attention batch has no failed state'); END;
CREATE TRIGGER IF NOT EXISTS attention_batch_no_failed_transition
BEFORE UPDATE ON attention_batches
WHEN NEW.state='failed'
BEGIN SELECT RAISE(ABORT,'attention batch has no failed state'); END;

CREATE TRIGGER IF NOT EXISTS attention_batch_sealed_authority_immutable
BEFORE UPDATE ON attention_batches
WHEN OLD.state IN ('sealed','delivered','cancelled') AND (
  NEW.project_id IS NOT OLD.project_id OR NEW.channel_id IS NOT OLD.channel_id OR
  NEW.channel_snapshot_json IS NOT OLD.channel_snapshot_json OR NEW.forge_kind IS NOT OLD.forge_kind OR
  NEW.forge_host IS NOT OLD.forge_host OR NEW.forge_project_key IS NOT OLD.forge_project_key OR
  NEW.target_kind IS NOT OLD.target_kind OR NEW.target_id IS NOT OLD.target_id OR
  NEW.kind IS NOT OLD.kind OR NEW.delivery_id IS NOT OLD.delivery_id OR NEW.scope IS NOT OLD.scope OR
  NEW.scope_id IS NOT OLD.scope_id OR NEW.due_at_ms IS NOT OLD.due_at_ms OR
  NEW.payload_json IS NOT OLD.payload_json OR NEW.payload_digest IS NOT OLD.payload_digest OR
  NEW.operation_key IS NOT OLD.operation_key
)
BEGIN SELECT RAISE(ABORT,'sealed attention batch authority is immutable'); END;

CREATE TRIGGER IF NOT EXISTS attention_batch_member_matches_authority
BEFORE INSERT ON attention_batch_members
WHEN NOT EXISTS (
  SELECT 1 FROM attention_batches b
  WHERE b.id=NEW.batch_id AND b.channel_id=NEW.channel_id AND b.channel_snapshot_json=NEW.channel_snapshot_json
)
BEGIN SELECT RAISE(ABORT,'batch member does not match frozen authority'); END;
