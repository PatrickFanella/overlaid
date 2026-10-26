-- Immutable engine source plans only. No outbound action/job or sender cutover.
BEGIN;
DO $setup$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='engine_operator_occurrences'::regclass AND conname='engine_occurrence_chat_plan_binding') THEN
  ALTER TABLE engine_operator_occurrences ADD CONSTRAINT engine_occurrence_chat_plan_binding UNIQUE(tenant_id,id,automation_id,version_id,graph_digest,run_id,created_at,deadline);
 END IF;
 IF NOT EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='engine_operator_occurrences'::regclass AND conname='engine_occurrence_chat_marker_binding') THEN
  ALTER TABLE engine_operator_occurrences ADD CONSTRAINT engine_occurrence_chat_marker_binding UNIQUE(tenant_id,id,run_id);
 END IF;
END $setup$;
CREATE TABLE IF NOT EXISTS engine_operator_chat_plan_markers (
 tenant_id uuid NOT NULL DEFAULT nullif(current_setting('app.tenant_id',true),'')::uuid,
 id text NOT NULL, run_id text NOT NULL, occurrence_id text NOT NULL, prepared_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,id),
 CONSTRAINT engine_chat_marker_run_once UNIQUE(tenant_id,run_id),
 CONSTRAINT engine_chat_marker_binding UNIQUE(tenant_id,id,run_id,occurrence_id,prepared_at),
 CONSTRAINT engine_chat_marker_source FOREIGN KEY(tenant_id,occurrence_id,run_id) REFERENCES engine_operator_occurrences(tenant_id,id,run_id) ON DELETE CASCADE,
 CONSTRAINT engine_chat_marker_id CHECK(id ~ '^[0-9a-f]{64}$'),
 CONSTRAINT engine_chat_marker_time CHECK(isfinite(prepared_at))
);
ALTER TABLE engine_operator_chat_plan_markers ENABLE ROW LEVEL SECURITY;
ALTER TABLE engine_operator_chat_plan_markers FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON engine_operator_chat_plan_markers;
CREATE POLICY tenant_isolation ON engine_operator_chat_plan_markers TO overlaid_runtime USING(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid) WITH CHECK(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);
REVOKE ALL ON engine_operator_chat_plan_markers FROM PUBLIC,overlaid_runtime;
REVOKE ALL(tenant_id,id,run_id,occurrence_id,prepared_at) ON engine_operator_chat_plan_markers FROM PUBLIC,overlaid_runtime;
GRANT SELECT,INSERT ON engine_operator_chat_plan_markers TO overlaid_runtime;
CREATE TABLE IF NOT EXISTS engine_operator_chat_plans (
 tenant_id uuid NOT NULL DEFAULT nullif(current_setting('app.tenant_id',true),'')::uuid,
 id text NOT NULL, run_id text NOT NULL, occurrence_id text NOT NULL,
 automation_id uuid NOT NULL, version_id uuid NOT NULL, graph_digest text NOT NULL,
 provider text NOT NULL DEFAULT 'twitch', connection_id text NOT NULL, channel_id text NOT NULL,
 document text NOT NULL, digest text NOT NULL, effect_count integer NOT NULL,
 source_created_at timestamptz NOT NULL, deadline timestamptz NOT NULL, prepared_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,id),
 CONSTRAINT engine_chat_plan_once UNIQUE(tenant_id,run_id),
 CONSTRAINT engine_chat_plan_binding UNIQUE(tenant_id,id,connection_id,deadline),
 CONSTRAINT engine_chat_plan_marker FOREIGN KEY(tenant_id,id,run_id,occurrence_id,prepared_at) REFERENCES engine_operator_chat_plan_markers(tenant_id,id,run_id,occurrence_id,prepared_at) ON DELETE CASCADE,
 CONSTRAINT engine_chat_plan_run FOREIGN KEY(tenant_id,run_id,automation_id) REFERENCES engine_operator_runs(tenant_id,id,automation_id) ON DELETE CASCADE,
 CONSTRAINT engine_chat_plan_source FOREIGN KEY(tenant_id,occurrence_id,automation_id,version_id,graph_digest,run_id,source_created_at,deadline) REFERENCES engine_operator_occurrences(tenant_id,id,automation_id,version_id,graph_digest,run_id,created_at,deadline) ON DELETE CASCADE,
 CONSTRAINT engine_chat_plan_connection FOREIGN KEY(tenant_id,provider) REFERENCES platform_connections(tenant_id,provider) ON DELETE CASCADE,
 CONSTRAINT engine_chat_plan_target CHECK(provider='twitch' AND length(channel_id) BETWEEN 1 AND 256 AND connection_id='twitch:'||channel_id),
 CONSTRAINT engine_chat_plan_id CHECK(id ~ '^[0-9a-f]{64}$'),
 CONSTRAINT engine_chat_plan_time CHECK(isfinite(prepared_at) AND source_created_at<=prepared_at AND prepared_at<deadline),
 CONSTRAINT engine_chat_plan_count CHECK(effect_count BETWEEN 1 AND 16),
 CONSTRAINT engine_chat_plan_bytes CHECK(octet_length(document) BETWEEN 1 AND 65536),
 CONSTRAINT engine_chat_plan_header CHECK(document::jsonb @> jsonb_build_object('formatVersion',1,'syntaxVersion',2,'captureVersion',1,'tenantId',tenant_id::text,'id',id,'runId',run_id,'occurrenceId',occurrence_id,'automationId',automation_id::text,'versionId',version_id::text,'graphDigest',graph_digest,'connectionId',connection_id,'channelId',channel_id,'effectCount',effect_count)),
 CONSTRAINT engine_chat_plan_effects CHECK(document::jsonb ? 'effects' AND jsonb_typeof(document::jsonb->'effects')='array' AND jsonb_array_length(document::jsonb->'effects')=effect_count),
 CONSTRAINT engine_chat_plan_digest CHECK(digest=encode(public.digest(convert_to('overlaid.engine.operator-chat-plan.v1','UTF8')||decode('00','hex')||convert_to(document,'UTF8'),'sha256'),'hex'))
);
ALTER TABLE engine_operator_chat_plans ENABLE ROW LEVEL SECURITY;
ALTER TABLE engine_operator_chat_plans FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON engine_operator_chat_plans;
CREATE POLICY tenant_isolation ON engine_operator_chat_plans TO overlaid_runtime USING(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid) WITH CHECK(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);
REVOKE ALL ON engine_operator_chat_plans FROM PUBLIC,overlaid_runtime;
REVOKE ALL(tenant_id,id,run_id,occurrence_id,automation_id,version_id,graph_digest,provider,connection_id,channel_id,document,digest,effect_count,source_created_at,deadline,prepared_at) ON engine_operator_chat_plans FROM PUBLIC,overlaid_runtime;
GRANT SELECT,INSERT ON engine_operator_chat_plans TO overlaid_runtime;
INSERT INTO overlaid_migrations(name) VALUES('0043_operator_chat_plans.sql') ON CONFLICT DO NOTHING;
COMMIT;
