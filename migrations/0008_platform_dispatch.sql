BEGIN;
DO $setup$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='overlaid_worker') THEN
  CREATE ROLE overlaid_worker NOLOGIN NOSUPERUSER NOBYPASSRLS;
 END IF;
 EXECUTE format('GRANT USAGE ON SCHEMA %I TO overlaid_worker',current_schema());
END $setup$;
GRANT SELECT,INSERT,UPDATE,DELETE ON river_job,river_queue,river_notification TO overlaid_worker;
GRANT SELECT ON river_migration TO overlaid_worker;
GRANT USAGE,SELECT ON SEQUENCE river_job_id_seq,river_notification_id_seq TO overlaid_worker;
CREATE POLICY worker_dispatch ON river_job TO overlaid_worker
 USING(kind='overlaid_chat_dispatch') WITH CHECK(kind='overlaid_chat_dispatch');
CREATE TABLE platform_dispatch_receipts (
 tenant_id uuid NOT NULL DEFAULT nullif(current_setting('app.tenant_id',true),'')::uuid,
 event_id text NOT NULL, connection_id text NOT NULL,
 state text NOT NULL CHECK(state IN ('running','completed','failed','uncertain','skipped')),
 code text NOT NULL CHECK(code IN ('started','completed','handler_failed','handler_cancelled','prior_start','expired','connection_unavailable','invalid_event')),
 claim_token text NOT NULL,
 started_at timestamptz NOT NULL DEFAULT now(), finished_at timestamptz,
 PRIMARY KEY(tenant_id,event_id),
 FOREIGN KEY(tenant_id,event_id,connection_id) REFERENCES normalized_chat_events(tenant_id,id,connection_id) ON DELETE CASCADE
);
CREATE INDEX platform_dispatch_recent ON platform_dispatch_receipts(tenant_id,started_at DESC,event_id);
ALTER TABLE platform_dispatch_receipts ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform_dispatch_receipts FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON platform_dispatch_receipts
 USING(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);
GRANT SELECT,INSERT,UPDATE,DELETE ON platform_dispatch_receipts TO overlaid_runtime;
INSERT INTO overlaid_migrations(name) VALUES('0008_platform_dispatch.sql') ON CONFLICT DO NOTHING;
COMMIT;
