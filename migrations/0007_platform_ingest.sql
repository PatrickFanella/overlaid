BEGIN;
CREATE TABLE normalized_chat_events (
 tenant_id uuid NOT NULL DEFAULT nullif(current_setting('app.tenant_id',true),'')::uuid REFERENCES tenants(id) ON DELETE CASCADE,
 id text NOT NULL, connection_id text NOT NULL, idempotency_key text NOT NULL,
 platform text NOT NULL, actor_id text NOT NULL, actor_login text NOT NULL,
 channel_id text NOT NULL, occurred_at timestamptz NOT NULL, received_at timestamptz NOT NULL,
 envelope jsonb NOT NULL CHECK(octet_length(envelope::text)<=65536),
 PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,connection_id,idempotency_key), UNIQUE(tenant_id,id,connection_id)
);
CREATE INDEX normalized_chat_retention ON normalized_chat_events(tenant_id,received_at);
CREATE INDEX normalized_chat_actor ON normalized_chat_events(tenant_id,actor_id);
CREATE TABLE platform_event_payloads (
 tenant_id uuid NOT NULL DEFAULT nullif(current_setting('app.tenant_id',true),'')::uuid,
 event_id text NOT NULL, payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object' AND octet_length(payload::text)<=65536),
 PRIMARY KEY(tenant_id,event_id),
 FOREIGN KEY(tenant_id,event_id) REFERENCES normalized_chat_events(tenant_id,id) ON DELETE CASCADE
);
ALTER TABLE river_job ADD COLUMN overlaid_tenant_id uuid GENERATED ALWAYS AS
 (CASE WHEN kind='overlaid_chat_dispatch' THEN (args->>'tenant_id')::uuid END) STORED;
ALTER TABLE river_job ADD COLUMN overlaid_event_id text GENERATED ALWAYS AS
 (CASE WHEN kind='overlaid_chat_dispatch' THEN args->>'event_id' END) STORED;
ALTER TABLE river_job ADD COLUMN overlaid_connection_id text GENERATED ALWAYS AS
 (CASE WHEN kind='overlaid_chat_dispatch' THEN args->>'connection_id' END) STORED;
ALTER TABLE river_job ADD CONSTRAINT overlaid_event_job FOREIGN KEY(overlaid_tenant_id,overlaid_event_id,overlaid_connection_id)
 REFERENCES normalized_chat_events(tenant_id,id,connection_id) ON DELETE CASCADE;
ALTER TABLE river_job ADD CONSTRAINT overlaid_dispatch_args CHECK(kind<>'overlaid_chat_dispatch' OR
 (jsonb_typeof(args)='object' AND args ?& ARRAY['tenant_id','event_id','connection_id','deadline']
 AND args-ARRAY['tenant_id','event_id','connection_id','deadline']='{}'::jsonb
 AND jsonb_typeof(args->'tenant_id')='string' AND jsonb_typeof(args->'event_id')='string'
 AND jsonb_typeof(args->'connection_id')='string'
 AND jsonb_typeof(args->'deadline')='number'));
CREATE UNIQUE INDEX overlaid_event_dispatch ON river_job(overlaid_tenant_id,overlaid_event_id)
 WHERE kind='overlaid_chat_dispatch';
DO $setup$ DECLARE tab text; BEGIN
 FOREACH tab IN ARRAY ARRAY['normalized_chat_events','platform_event_payloads'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',tab);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',tab);
  EXECUTE format('CREATE POLICY tenant_isolation ON %I USING(tenant_id=nullif(current_setting(''app.tenant_id'',true),'''')::uuid) WITH CHECK(tenant_id=nullif(current_setting(''app.tenant_id'',true),'''')::uuid)',tab);
 END LOOP;
END $setup$;
ALTER TABLE river_job ENABLE ROW LEVEL SECURITY;
ALTER TABLE river_job FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON river_job USING
 (kind='overlaid_chat_dispatch' AND overlaid_tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK(kind='overlaid_chat_dispatch' AND overlaid_tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid AND overlaid_event_id IS NOT NULL AND overlaid_connection_id IS NOT NULL);
REVOKE ALL ON river_migration,river_queue,river_notification FROM overlaid_runtime;
REVOKE ALL ON SEQUENCE river_notification_id_seq FROM overlaid_runtime;
REVOKE ALL ON overlaid_migrations FROM overlaid_runtime;
GRANT SELECT,INSERT,DELETE ON normalized_chat_events,platform_event_payloads,river_job TO overlaid_runtime;
-- River's insertion SQL contains an ON CONFLICT update branch even when the
-- domain store provides delivery deduplication. RLS still fences this grant.
GRANT UPDATE ON river_job TO overlaid_runtime;
GRANT USAGE,SELECT ON SEQUENCE river_job_id_seq TO overlaid_runtime;
INSERT INTO overlaid_migrations(name) VALUES('0007_platform_ingest.sql') ON CONFLICT DO NOTHING;
COMMIT;
