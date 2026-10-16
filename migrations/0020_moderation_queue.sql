BEGIN;
ALTER TABLE river_job ADD COLUMN overlaid_moderation_tenant_id uuid GENERATED ALWAYS AS
 (CASE WHEN kind='overlaid_moderation' THEN (args->>'tenant_id')::uuid END) STORED;
ALTER TABLE river_job ADD COLUMN overlaid_moderation_action_id text GENERATED ALWAYS AS
 (CASE WHEN kind='overlaid_moderation' THEN args->>'action_id' END) STORED;
ALTER TABLE river_job ADD CONSTRAINT overlaid_moderation_job FOREIGN KEY(overlaid_moderation_tenant_id,overlaid_moderation_action_id)
 REFERENCES platform_moderation_actions(tenant_id,id) ON DELETE CASCADE;
ALTER TABLE river_job ADD CONSTRAINT overlaid_moderation_args CHECK(kind<>'overlaid_moderation' OR
 (args IS NOT NULL AND jsonb_typeof(args)='object' AND args ?& ARRAY['tenant_id','action_id','deadline']
 AND args-ARRAY['tenant_id','action_id','deadline']='{}'::jsonb
 AND jsonb_typeof(args->'tenant_id')='string'
 AND jsonb_typeof(args->'action_id')='string' AND (args->>'action_id') ~ '^[0-9a-f]{64}$'
 AND jsonb_typeof(args->'deadline')='number' AND (args->>'deadline') ~ '^[1-9][0-9]{0,11}$'
 AND queue='moderation_outbound' AND max_attempts=3 AND priority=4));
CREATE UNIQUE INDEX overlaid_moderation_dispatch ON river_job(overlaid_moderation_tenant_id,overlaid_moderation_action_id)
 WHERE kind='overlaid_moderation';
ALTER POLICY tenant_isolation ON river_job TO overlaid_runtime USING
 ((kind='overlaid_chat_dispatch' AND overlaid_tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid)
 OR (kind='overlaid_chat_send' AND overlaid_outbound_tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid)
 OR (kind='overlaid_moderation' AND overlaid_moderation_tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid))
 WITH CHECK
 ((kind='overlaid_chat_dispatch' AND overlaid_tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid AND overlaid_event_id IS NOT NULL AND overlaid_connection_id IS NOT NULL)
 OR (kind='overlaid_chat_send' AND overlaid_outbound_tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid AND overlaid_action_id IS NOT NULL)
 OR (kind='overlaid_moderation' AND overlaid_moderation_tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid AND overlaid_moderation_action_id IS NOT NULL));
DO $setup$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='overlaid_moderation_worker') THEN
  CREATE ROLE overlaid_moderation_worker NOLOGIN NOSUPERUSER NOBYPASSRLS;
 END IF;
 EXECUTE format('GRANT USAGE ON SCHEMA %I TO overlaid_moderation_worker',current_schema());
END $setup$;
GRANT SELECT,INSERT,UPDATE,DELETE ON river_job,river_queue,river_notification,river_leader TO overlaid_moderation_worker;
GRANT SELECT ON river_migration TO overlaid_moderation_worker;
GRANT USAGE,SELECT ON SEQUENCE river_job_id_seq,river_notification_id_seq TO overlaid_moderation_worker;
CREATE POLICY moderation_worker_dispatch ON river_job TO overlaid_moderation_worker
 USING(kind='overlaid_moderation') WITH CHECK(kind='overlaid_moderation');
-- Catch up only safe pre-0020 rows. No job is created for a started or terminal
-- action, and conflict handling never renews an existing job or deadline.
INSERT INTO river_job(kind,args,max_attempts,queue,priority,tags)
 SELECT 'overlaid_moderation',jsonb_build_object('tenant_id',tenant_id::text,'action_id',id,'deadline',floor(extract(epoch FROM deadline))::bigint),3,'moderation_outbound',4,ARRAY[]::varchar(255)[]
 FROM platform_moderation_actions WHERE state='pending' AND deadline>clock_timestamp()
 ON CONFLICT(overlaid_moderation_tenant_id,overlaid_moderation_action_id) WHERE kind='overlaid_moderation' DO NOTHING;
INSERT INTO overlaid_migrations(name) VALUES('0020_moderation_queue.sql') ON CONFLICT DO NOTHING;
COMMIT;
