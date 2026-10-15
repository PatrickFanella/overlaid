BEGIN;
DO $setup$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='overlaid_outbound_worker') THEN
  CREATE ROLE overlaid_outbound_worker NOLOGIN NOSUPERUSER NOBYPASSRLS;
 END IF;
 EXECUTE format('GRANT USAGE ON SCHEMA %I TO overlaid_outbound_worker',current_schema());
END $setup$;
GRANT SELECT,INSERT,UPDATE,DELETE ON river_job,river_queue,river_notification,river_leader TO overlaid_outbound_worker;
GRANT SELECT ON river_migration TO overlaid_outbound_worker;
GRANT USAGE,SELECT ON SEQUENCE river_job_id_seq,river_notification_id_seq TO overlaid_outbound_worker;
CREATE POLICY outbound_worker_dispatch ON river_job TO overlaid_outbound_worker
 USING(kind='overlaid_chat_send') WITH CHECK(kind='overlaid_chat_send');
INSERT INTO overlaid_migrations(name) VALUES('0016_outbound_worker.sql') ON CONFLICT DO NOTHING;
COMMIT;
