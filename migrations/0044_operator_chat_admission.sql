BEGIN;
-- Removing the old guard name deliberately refuses older runtime binaries.
ALTER TABLE platform_outbound_actions ADD COLUMN IF NOT EXISTS engine_source_id text;
ALTER TABLE platform_outbound_actions DROP CONSTRAINT IF EXISTS platform_outbound_source_kind;
ALTER TABLE platform_outbound_actions DROP CONSTRAINT IF EXISTS platform_outbound_source_v2;
ALTER TABLE platform_outbound_actions ADD CONSTRAINT platform_outbound_source_v2
 CHECK(num_nonnulls(event_id,manual_source_id,engine_source_id)=1);
ALTER TABLE platform_outbound_actions DROP CONSTRAINT IF EXISTS platform_outbound_engine_source;
ALTER TABLE platform_outbound_actions ADD CONSTRAINT platform_outbound_engine_source
 FOREIGN KEY(tenant_id,engine_source_id,connection_id,deadline)
 REFERENCES engine_operator_chat_plans(tenant_id,id,connection_id,deadline) ON DELETE CASCADE;
ALTER TABLE platform_outbound_actions DROP CONSTRAINT IF EXISTS platform_outbound_engine_text;
ALTER TABLE platform_outbound_actions ADD CONSTRAINT platform_outbound_engine_text
 CHECK(engine_source_id IS NULL OR (capability='chat.send' AND reply_to_id='' AND id=action_key));
CREATE UNIQUE INDEX IF NOT EXISTS platform_outbound_engine_effect ON platform_outbound_actions(tenant_id,engine_source_id,action_key) WHERE engine_source_id IS NOT NULL;

-- Retain this metadata after action/plan deletion. Admission never recreates
-- missing or terminal work, even while the original private source survives.
CREATE TABLE IF NOT EXISTS engine_operator_chat_admissions (
 tenant_id uuid NOT NULL DEFAULT nullif(current_setting('app.tenant_id',true),'')::uuid,
 id text NOT NULL,
 admitted_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CONSTRAINT engine_operator_chat_admissions_pkey PRIMARY KEY(tenant_id,id),
 CONSTRAINT engine_chat_admission_source FOREIGN KEY(tenant_id,id)
 REFERENCES engine_operator_chat_plan_markers(tenant_id,id) ON DELETE CASCADE,
 CONSTRAINT engine_chat_admission_time CHECK(isfinite(admitted_at))
);
ALTER TABLE engine_operator_chat_admissions ENABLE ROW LEVEL SECURITY;
ALTER TABLE engine_operator_chat_admissions FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON engine_operator_chat_admissions;
CREATE POLICY tenant_isolation ON engine_operator_chat_admissions TO overlaid_runtime
 USING(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);
REVOKE ALL ON engine_operator_chat_admissions FROM PUBLIC,overlaid_runtime;
REVOKE ALL(tenant_id,id,admitted_at) ON engine_operator_chat_admissions FROM PUBLIC,overlaid_runtime;
GRANT SELECT,INSERT ON engine_operator_chat_admissions TO overlaid_runtime;
INSERT INTO overlaid_migrations(name) VALUES('0044_operator_chat_admission.sql') ON CONFLICT DO NOTHING;
COMMIT;
