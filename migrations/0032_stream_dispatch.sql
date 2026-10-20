BEGIN;
-- PostgreSQL row-locking SELECT requires UPDATE on at least one column. Bind
-- the only granted column to its immutable envelope, allowing id=id but no
-- identity or payload mutation. Do not grant table-wide UPDATE on sources.
ALTER TABLE normalized_stream_events ADD CONSTRAINT stream_dispatch_locked_identity
 CHECK(COALESCE(id=envelope->>'id',false));
GRANT UPDATE(id) ON normalized_stream_events TO overlaid_runtime;
DO $setup$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='overlaid_stream_worker') THEN
  CREATE ROLE overlaid_stream_worker NOLOGIN NOSUPERUSER NOBYPASSRLS;
 END IF;
 EXECUTE format('GRANT USAGE ON SCHEMA %I TO overlaid_stream_worker',current_schema());
END $setup$;
GRANT SELECT,INSERT,UPDATE,DELETE ON river_job,river_queue,river_notification,river_leader TO overlaid_stream_worker;
GRANT SELECT ON river_migration TO overlaid_stream_worker;
GRANT USAGE,SELECT ON SEQUENCE river_job_id_seq,river_notification_id_seq TO overlaid_stream_worker;
CREATE POLICY stream_worker_dispatch ON river_job TO overlaid_stream_worker
 USING(kind='overlaid_stream_dispatch') WITH CHECK(kind='overlaid_stream_dispatch');
CREATE TABLE platform_stream_dispatch_receipts (
 tenant_id uuid NOT NULL DEFAULT nullif(current_setting('app.tenant_id',true),'')::uuid,
 event_id text NOT NULL, connection_id text NOT NULL, deadline bigint NOT NULL,
 state text NOT NULL, code text NOT NULL, claim_hash text NOT NULL CONSTRAINT stream_dispatch_claim CHECK(claim_hash ~ '^[0-9a-f]{64}$'),
 started_at timestamptz NOT NULL DEFAULT clock_timestamp(), finished_at timestamptz,
 PRIMARY KEY(tenant_id,event_id),
 CONSTRAINT stream_dispatch_source FOREIGN KEY(tenant_id,event_id,connection_id,deadline)
 REFERENCES normalized_stream_events(tenant_id,id,connection_id,dispatch_deadline) ON DELETE CASCADE,
 CONSTRAINT stream_dispatch_state CHECK(
 (state='running' AND code='started' AND finished_at IS NULL)
 OR (state='completed' AND code='completed' AND finished_at IS NOT NULL)
 OR (state='failed' AND code IN('handler_failed','capability_unavailable','connection_unavailable') AND finished_at IS NOT NULL)
 OR (state='uncertain' AND code IN('handler_cancelled','prior_start','owner_release_failed') AND finished_at IS NOT NULL)
 OR (state='skipped' AND code IN('expired','connection_unavailable','invalid_event','capability_unavailable') AND finished_at IS NOT NULL))
);
CREATE INDEX stream_dispatch_recent ON platform_stream_dispatch_receipts(tenant_id,started_at DESC,event_id);
ALTER TABLE platform_stream_dispatch_receipts ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform_stream_dispatch_receipts FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON platform_stream_dispatch_receipts TO overlaid_runtime
 USING(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);
GRANT SELECT,INSERT,UPDATE,DELETE ON platform_stream_dispatch_receipts TO overlaid_runtime;
INSERT INTO overlaid_migrations(name) VALUES('0032_stream_dispatch.sql') ON CONFLICT DO NOTHING;
COMMIT;
