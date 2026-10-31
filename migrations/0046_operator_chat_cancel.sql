-- Operator cancellation of unclaimed engine replies. A canceled action is a
-- terminal local rejection that never started; the code is engine-only.
BEGIN;
ALTER TABLE platform_outbound_actions DROP CONSTRAINT IF EXISTS platform_outbound_actions_code_check;
ALTER TABLE platform_outbound_actions ADD CONSTRAINT platform_outbound_actions_code_check
 CHECK(code IN ('admitted','started','delivered','rejected','uncertain','expired','connection_unavailable','deadline_elapsed','canceled'));
ALTER TABLE platform_outbound_actions DROP CONSTRAINT IF EXISTS platform_outbound_engine_cancel;
ALTER TABLE platform_outbound_actions ADD CONSTRAINT platform_outbound_engine_cancel
 CHECK(code<>'canceled' OR (engine_source_id IS NOT NULL AND state='rejected' AND failure='' AND retry_after_ns=0 AND started_at IS NULL AND finished_at IS NOT NULL));
INSERT INTO overlaid_migrations(name) VALUES('0046_operator_chat_cancel.sql') ON CONFLICT DO NOTHING;
COMMIT;
