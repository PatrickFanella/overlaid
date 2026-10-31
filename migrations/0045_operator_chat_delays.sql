-- Per-effect delayed operator replies. Each engine action keeps its own fixed
-- not-before time and deadline, both derived once from the original occurrence.
-- Replacing the engine source key deliberately refuses older runtime binaries.
BEGIN;
ALTER TABLE platform_outbound_actions ADD COLUMN IF NOT EXISTS not_before timestamptz;
-- Actions admitted before this migration were immediate replies: offset zero.
UPDATE platform_outbound_actions a SET not_before=p.source_created_at
 FROM engine_operator_chat_plans p
 WHERE a.engine_source_id IS NOT NULL AND a.not_before IS NULL
 AND p.tenant_id=a.tenant_id AND p.id=a.engine_source_id;
DO $setup$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='engine_operator_chat_plans'::regclass AND conname='engine_chat_plan_effect_binding') THEN
  ALTER TABLE engine_operator_chat_plans ADD CONSTRAINT engine_chat_plan_effect_binding UNIQUE(tenant_id,id,connection_id);
 END IF;
END $setup$;
ALTER TABLE platform_outbound_actions DROP CONSTRAINT IF EXISTS platform_outbound_engine_source;
ALTER TABLE platform_outbound_actions DROP CONSTRAINT IF EXISTS platform_outbound_engine_source_v2;
ALTER TABLE platform_outbound_actions ADD CONSTRAINT platform_outbound_engine_source_v2
 FOREIGN KEY(tenant_id,engine_source_id,connection_id)
 REFERENCES engine_operator_chat_plans(tenant_id,id,connection_id) ON DELETE CASCADE;
ALTER TABLE platform_outbound_actions DROP CONSTRAINT IF EXISTS platform_outbound_not_before;
ALTER TABLE platform_outbound_actions ADD CONSTRAINT platform_outbound_not_before
 CHECK((engine_source_id IS NULL AND not_before IS NULL)
  OR (engine_source_id IS NOT NULL AND not_before IS NOT NULL AND isfinite(not_before) AND not_before<deadline));
INSERT INTO overlaid_migrations(name) VALUES('0045_operator_chat_delays.sql') ON CONFLICT DO NOTHING;
COMMIT;
