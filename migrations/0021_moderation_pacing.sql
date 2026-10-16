BEGIN;
DO $setup$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='overlaid_moderation_pacer') THEN
  CREATE ROLE overlaid_moderation_pacer NOLOGIN NOSUPERUSER NOBYPASSRLS;
 END IF;
 EXECUTE format('GRANT USAGE ON SCHEMA %I TO overlaid_moderation_pacer',current_schema());
END $setup$;
CREATE TABLE moderation_pacing_buckets (
 kind text NOT NULL CHECK(kind IN('sender','channel')),
 key text NOT NULL CHECK(key ~ '^[0-9a-f]{64}$'),
 next_at timestamptz NOT NULL DEFAULT '-infinity',
 lease_hash text CHECK(lease_hash ~ '^[0-9a-f]{64}$'),
 lease_until timestamptz,
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(kind,key),
 CHECK((lease_hash IS NULL)=(lease_until IS NULL))
);
CREATE INDEX moderation_pacing_expiry ON moderation_pacing_buckets(updated_at);
ALTER TABLE moderation_pacing_buckets ENABLE ROW LEVEL SECURITY;
ALTER TABLE moderation_pacing_buckets FORCE ROW LEVEL SECURITY;
CREATE POLICY pacer_only ON moderation_pacing_buckets TO overlaid_moderation_pacer USING(true) WITH CHECK(true);
REVOKE ALL ON moderation_pacing_buckets FROM PUBLIC,overlaid_runtime,overlaid_worker,overlaid_outbound_worker,overlaid_moderation_worker,overlaid_pacer,overlaid_bot_admin,overlaid_bot_executor;
GRANT SELECT,INSERT,UPDATE,DELETE ON moderation_pacing_buckets TO overlaid_moderation_pacer;
INSERT INTO overlaid_migrations(name) VALUES('0021_moderation_pacing.sql') ON CONFLICT DO NOTHING;
COMMIT;
