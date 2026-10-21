-- Immutable metadata only; private proposals and raw facts are not stored.
BEGIN;
DO $setup$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='engine_operator_runs'::regclass AND conname='engine_run_trace_binding') THEN
  ALTER TABLE engine_operator_runs ADD CONSTRAINT engine_run_trace_binding UNIQUE(tenant_id,id,automation_id);
 END IF;
END $setup$;
CREATE TABLE IF NOT EXISTS engine_operator_rehearsals (
 tenant_id uuid NOT NULL DEFAULT nullif(current_setting('app.tenant_id',true),'')::uuid,
 id text NOT NULL, run_id text NOT NULL, automation_id uuid NOT NULL, version_id uuid NOT NULL,
 mode text NOT NULL CONSTRAINT engine_trace_mode CHECK(mode IN('reproduce','current_config')),
 document text NOT NULL, digest text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,id),
 CONSTRAINT engine_trace_selection UNIQUE(tenant_id,run_id,version_id,mode),
 CONSTRAINT engine_trace_run FOREIGN KEY(tenant_id,run_id,automation_id) REFERENCES engine_operator_runs(tenant_id,id,automation_id) ON DELETE CASCADE,
 CONSTRAINT engine_trace_version FOREIGN KEY(tenant_id,automation_id,version_id) REFERENCES engine_automation_versions(tenant_id,automation_id,id) ON DELETE CASCADE,
 CONSTRAINT engine_trace_id CHECK(id ~ '^[0-9a-f]{64}$'),
 CONSTRAINT engine_trace_bytes CHECK(octet_length(document) BETWEEN 1 AND 65536),
 CONSTRAINT engine_trace_header CHECK(document::jsonb @> jsonb_build_object('formatVersion',1,'tenantId',tenant_id::text,'id',id,'runId',run_id,'automationId',automation_id::text,'versionId',version_id::text,'mode',mode)),
 CONSTRAINT engine_trace_digest CHECK(digest=encode(public.digest(convert_to('overlaid.engine.operator-trace.v1','UTF8')||decode('00','hex')||convert_to(document,'UTF8'),'sha256'),'hex'))
);
ALTER TABLE engine_operator_rehearsals ENABLE ROW LEVEL SECURITY;
ALTER TABLE engine_operator_rehearsals FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON engine_operator_rehearsals;
DO $setup$ DECLARE tab text:='engine_operator_rehearsals'; BEGIN
  EXECUTE format('CREATE POLICY tenant_isolation ON %I TO overlaid_runtime USING(tenant_id=nullif(current_setting(''app.tenant_id'',true),'''')::uuid) WITH CHECK(tenant_id=nullif(current_setting(''app.tenant_id'',true),'''')::uuid)',tab);
END $setup$;
REVOKE ALL ON engine_operator_rehearsals FROM PUBLIC,overlaid_runtime;
REVOKE ALL(tenant_id,id,run_id,automation_id,version_id,mode,document,digest,created_at) ON engine_operator_rehearsals FROM PUBLIC,overlaid_runtime;
GRANT SELECT,INSERT ON engine_operator_rehearsals TO overlaid_runtime;
INSERT INTO overlaid_migrations(name) VALUES('0042_operator_rehearsal_traces.sql') ON CONFLICT DO NOTHING;
COMMIT;
