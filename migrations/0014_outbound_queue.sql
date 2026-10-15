BEGIN;
ALTER TABLE river_job ADD COLUMN overlaid_outbound_tenant_id uuid GENERATED ALWAYS AS
 (CASE WHEN kind='overlaid_chat_send' THEN (args->>'tenant_id')::uuid END) STORED;
ALTER TABLE river_job ADD COLUMN overlaid_action_id text GENERATED ALWAYS AS
 (CASE WHEN kind='overlaid_chat_send' THEN args->>'action_id' END) STORED;
ALTER TABLE river_job ADD CONSTRAINT overlaid_outbound_job FOREIGN KEY(overlaid_outbound_tenant_id,overlaid_action_id)
 REFERENCES platform_outbound_actions(tenant_id,id) ON DELETE CASCADE;
ALTER TABLE river_job ADD CONSTRAINT overlaid_outbound_args CHECK(kind<>'overlaid_chat_send' OR
 (args IS NOT NULL AND jsonb_typeof(args)='object' AND args ?& ARRAY['tenant_id','action_id','deadline']
 AND args-ARRAY['tenant_id','action_id','deadline']='{}'::jsonb
 AND jsonb_typeof(args->'tenant_id')='string'
 AND jsonb_typeof(args->'action_id')='string' AND (args->>'action_id') ~ '^[0-9a-f]{64}$'
 AND jsonb_typeof(args->'deadline')='number' AND (args->>'deadline') ~ '^[1-9][0-9]{0,11}$'
 AND queue='chat_outbound' AND max_attempts=3 AND priority=4));
CREATE UNIQUE INDEX overlaid_action_dispatch ON river_job(overlaid_outbound_tenant_id,overlaid_action_id)
 WHERE kind='overlaid_chat_send';
ALTER POLICY tenant_isolation ON river_job TO overlaid_runtime USING
 ((kind='overlaid_chat_dispatch' AND overlaid_tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid)
 OR (kind='overlaid_chat_send' AND overlaid_outbound_tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid))
 WITH CHECK
 ((kind='overlaid_chat_dispatch' AND overlaid_tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid AND overlaid_event_id IS NOT NULL AND overlaid_connection_id IS NOT NULL)
 OR (kind='overlaid_chat_send' AND overlaid_outbound_tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid AND overlaid_action_id IS NOT NULL));
-- Existing chat workers keep their chat-only policy. No outbound lease role or
-- executor is enabled by this migration.
-- Catch up only unexpired, unstarted actions from migration 0013. Running and
-- terminal rows must never receive a new execution job during an upgrade.
INSERT INTO river_job(kind,args,max_attempts,queue,priority,tags)
 SELECT 'overlaid_chat_send',jsonb_build_object('tenant_id',tenant_id::text,'action_id',id,'deadline',floor(extract(epoch FROM deadline))::bigint),3,'chat_outbound',4,ARRAY[]::varchar(255)[]
 FROM platform_outbound_actions WHERE state='pending' AND deadline>now()
 ON CONFLICT (overlaid_outbound_tenant_id,overlaid_action_id) WHERE kind='overlaid_chat_send' DO NOTHING;
INSERT INTO overlaid_migrations(name) VALUES('0014_outbound_queue.sql') ON CONFLICT DO NOTHING;
COMMIT;
