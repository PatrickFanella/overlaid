BEGIN;
CREATE TABLE normalized_stream_events (
 tenant_id uuid NOT NULL DEFAULT nullif(current_setting('app.tenant_id',true),'')::uuid REFERENCES tenants(id) ON DELETE CASCADE,
 id text NOT NULL, connection_id text NOT NULL, idempotency_key text NOT NULL,
 platform text NOT NULL CHECK(platform='twitch'), kind text NOT NULL,
 channel_id text NOT NULL, channel_login text NOT NULL,
 occurred_at timestamptz NOT NULL, received_at timestamptz NOT NULL,
 dispatch_deadline bigint NOT NULL,
 envelope jsonb NOT NULL CHECK(jsonb_typeof(envelope)='object' AND octet_length(envelope::text)<=65536),
 PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,connection_id,idempotency_key),
 UNIQUE(tenant_id,id,connection_id,dispatch_deadline),
 CONSTRAINT stream_event_kind CHECK(kind IN('stream_online','stream_offline','channel_update')),
 CONSTRAINT stream_event_deadline CHECK(dispatch_deadline=floor(extract(epoch FROM occurred_at))::bigint+120 AND dispatch_deadline>0),
 CONSTRAINT stream_event_connection CHECK(connection_id=platform||':'||channel_id),
 CONSTRAINT stream_event_connection_fk FOREIGN KEY(tenant_id,platform) REFERENCES platform_connections(tenant_id,provider) ON DELETE CASCADE
);
CREATE INDEX normalized_stream_retention ON normalized_stream_events(tenant_id,received_at);
CREATE INDEX normalized_stream_channel ON normalized_stream_events(tenant_id,channel_id);
CREATE TABLE platform_stream_payloads (
 tenant_id uuid NOT NULL DEFAULT nullif(current_setting('app.tenant_id',true),'')::uuid,
 event_id text NOT NULL, payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object' AND octet_length(payload::text)<=65536),
 PRIMARY KEY(tenant_id,event_id),
 CONSTRAINT stream_payload_event FOREIGN KEY(tenant_id,event_id) REFERENCES normalized_stream_events(tenant_id,id) ON DELETE CASCADE
);
DO $setup$ DECLARE tab text; BEGIN
 FOREACH tab IN ARRAY ARRAY['normalized_stream_events','platform_stream_payloads'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',tab);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',tab);
  EXECUTE format('CREATE POLICY tenant_isolation ON %I TO overlaid_runtime USING(tenant_id=nullif(current_setting(''app.tenant_id'',true),'''')::uuid) WITH CHECK(tenant_id=nullif(current_setting(''app.tenant_id'',true),'''')::uuid)',tab);
 END LOOP;
END $setup$;
GRANT SELECT,INSERT,DELETE ON normalized_stream_events,platform_stream_payloads TO overlaid_runtime;
ALTER TABLE river_job ADD COLUMN overlaid_stream_tenant_id uuid GENERATED ALWAYS AS
 (CASE WHEN kind='overlaid_stream_dispatch' THEN (args->>'tenant_id')::uuid END) STORED;
ALTER TABLE river_job ADD COLUMN overlaid_stream_event_id text GENERATED ALWAYS AS
 (CASE WHEN kind='overlaid_stream_dispatch' THEN args->>'event_id' END) STORED;
ALTER TABLE river_job ADD COLUMN overlaid_stream_connection_id text GENERATED ALWAYS AS
 (CASE WHEN kind='overlaid_stream_dispatch' THEN args->>'connection_id' END) STORED;
ALTER TABLE river_job ADD COLUMN overlaid_stream_deadline bigint GENERATED ALWAYS AS
 (CASE WHEN kind='overlaid_stream_dispatch' THEN (args->>'deadline')::bigint END) STORED;
ALTER TABLE river_job ADD CONSTRAINT overlaid_stream_job FOREIGN KEY(overlaid_stream_tenant_id,overlaid_stream_event_id,overlaid_stream_connection_id,overlaid_stream_deadline)
 REFERENCES normalized_stream_events(tenant_id,id,connection_id,dispatch_deadline) ON DELETE CASCADE;
ALTER TABLE river_job ADD CONSTRAINT overlaid_stream_args CHECK(kind<>'overlaid_stream_dispatch' OR
 (args IS NOT NULL AND jsonb_typeof(args)='object' AND args ?& ARRAY['tenant_id','event_id','connection_id','deadline']
 AND args-ARRAY['tenant_id','event_id','connection_id','deadline']='{}'::jsonb
 AND jsonb_typeof(args->'tenant_id')='string' AND jsonb_typeof(args->'event_id')='string'
 AND (args->>'event_id') ~ '^[0-9a-f]{64}$' AND jsonb_typeof(args->'connection_id')='string'
 AND jsonb_typeof(args->'deadline')='number' AND (args->>'deadline') ~ '^[1-9][0-9]{0,11}$'
 AND queue='stream_followup' AND max_attempts=3 AND priority=4));
CREATE UNIQUE INDEX overlaid_stream_dispatch ON river_job(overlaid_stream_tenant_id,overlaid_stream_event_id)
 WHERE kind='overlaid_stream_dispatch';
ALTER POLICY tenant_isolation ON river_job TO overlaid_runtime USING
 ((kind='overlaid_chat_dispatch' AND overlaid_tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid)
 OR (kind='overlaid_chat_send' AND overlaid_outbound_tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid)
 OR (kind='overlaid_moderation' AND overlaid_moderation_tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid)
 OR (kind='overlaid_stream_dispatch' AND overlaid_stream_tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid))
 WITH CHECK
 ((kind='overlaid_chat_dispatch' AND overlaid_tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid AND overlaid_event_id IS NOT NULL AND overlaid_connection_id IS NOT NULL)
 OR (kind='overlaid_chat_send' AND overlaid_outbound_tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid AND overlaid_action_id IS NOT NULL)
 OR (kind='overlaid_moderation' AND overlaid_moderation_tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid AND overlaid_moderation_action_id IS NOT NULL)
 OR (kind='overlaid_stream_dispatch' AND overlaid_stream_tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid AND overlaid_stream_event_id IS NOT NULL AND overlaid_stream_connection_id IS NOT NULL AND overlaid_stream_deadline IS NOT NULL));
-- Existing worker policies remain exact-kind. No stream leasing role or client
-- is installed; stream jobs cannot be consumed by the current chat dispatcher.
INSERT INTO overlaid_migrations(name) VALUES('0031_stream_event_ingest.sql') ON CONFLICT DO NOTHING;
COMMIT;
