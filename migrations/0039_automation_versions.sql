-- Additive, unused version storage. No dispatcher or run writer is activated.
BEGIN;
CREATE TABLE IF NOT EXISTS engine_automations (
 tenant_id uuid NOT NULL DEFAULT nullif(current_setting('app.tenant_id',true),'')::uuid REFERENCES tenants(id) ON DELETE CASCADE,
 id uuid NOT NULL, current_version uuid NOT NULL,
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,id)
);
CREATE TABLE IF NOT EXISTS engine_automation_versions (
 tenant_id uuid NOT NULL DEFAULT nullif(current_setting('app.tenant_id',true),'')::uuid,
 automation_id uuid NOT NULL, id uuid NOT NULL,
 document text NOT NULL CONSTRAINT engine_version_bytes CHECK(octet_length(document) BETWEEN 1 AND 65536),
 digest text NOT NULL CHECK(digest ~ '^[0-9a-f]{64}$'),
 actor_account_id uuid REFERENCES accounts(id) ON DELETE SET NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,automation_id,id),
 CONSTRAINT engine_version_automation FOREIGN KEY(tenant_id,automation_id) REFERENCES engine_automations(tenant_id,id) ON DELETE CASCADE,
 CONSTRAINT engine_version_header CHECK(document::jsonb @> jsonb_build_object('schemaVersion',1,'tenantId',tenant_id::text,'automationId',automation_id::text,'versionId',id::text)),
 CONSTRAINT engine_version_digest CHECK(digest=encode(public.digest(convert_to('overlaid.engine.document.v1','UTF8')||decode('00','hex')||convert_to(document,'UTF8'),'sha256'),'hex'))
);
DO $setup$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='engine_automations'::regclass AND conname='engine_current_version') THEN
  ALTER TABLE engine_automations ADD CONSTRAINT engine_current_version FOREIGN KEY(tenant_id,id,current_version)
   REFERENCES engine_automation_versions(tenant_id,automation_id,id) DEFERRABLE INITIALLY DEFERRED;
 END IF;
END $setup$;
CREATE TABLE IF NOT EXISTS engine_automation_presets (
 tenant_id uuid NOT NULL DEFAULT nullif(current_setting('app.tenant_id',true),'')::uuid,
 automation_id uuid NOT NULL, version_id uuid NOT NULL,
 target_automation_id uuid NOT NULL, target_version_id uuid NOT NULL,
 PRIMARY KEY(tenant_id,automation_id,version_id,target_automation_id,target_version_id),
 CONSTRAINT engine_preset_source FOREIGN KEY(tenant_id,automation_id,version_id) REFERENCES engine_automation_versions(tenant_id,automation_id,id) ON DELETE CASCADE,
 CONSTRAINT engine_preset_target FOREIGN KEY(tenant_id,target_automation_id,target_version_id) REFERENCES engine_automation_versions(tenant_id,automation_id,id) DEFERRABLE INITIALLY DEFERRED
);
DO $setup$ DECLARE tab text; BEGIN
 FOREACH tab IN ARRAY ARRAY['engine_automations','engine_automation_versions','engine_automation_presets'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',tab);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',tab);
  EXECUTE format('DROP POLICY IF EXISTS tenant_isolation ON %I',tab);
  EXECUTE format('CREATE POLICY tenant_isolation ON %I TO overlaid_runtime USING(tenant_id=nullif(current_setting(''app.tenant_id'',true),'''')::uuid) WITH CHECK(tenant_id=nullif(current_setting(''app.tenant_id'',true),'''')::uuid)',tab);
  EXECUTE format('REVOKE ALL ON %I FROM PUBLIC,overlaid_runtime',tab);
 END LOOP;
END $setup$;
REVOKE ALL(tenant_id,id,current_version,updated_at) ON engine_automations FROM PUBLIC,overlaid_runtime;
REVOKE ALL(tenant_id,automation_id,id,document,digest,actor_account_id,created_at) ON engine_automation_versions FROM PUBLIC,overlaid_runtime;
REVOKE ALL(tenant_id,automation_id,version_id,target_automation_id,target_version_id) ON engine_automation_presets FROM PUBLIC,overlaid_runtime;
GRANT SELECT,INSERT ON engine_automations,engine_automation_versions,engine_automation_presets TO overlaid_runtime;
GRANT UPDATE(current_version,updated_at) ON engine_automations TO overlaid_runtime;
INSERT INTO overlaid_migrations(name) VALUES('0039_automation_versions.sql') ON CONFLICT DO NOTHING;
COMMIT;
