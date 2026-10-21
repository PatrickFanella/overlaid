BEGIN;
DO $setup$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='overlaid_conduit_admin') THEN
  CREATE ROLE overlaid_conduit_admin NOLOGIN NOSUPERUSER NOBYPASSRLS NOCREATEROLE NOCREATEDB NOREPLICATION;
 END IF;
 EXECUTE format('GRANT USAGE ON SCHEMA %I TO overlaid_conduit_admin',current_schema());
END $setup$;
-- Credential bindings are immutable snapshots, never credential authority.
-- No lifecycle cascade may erase unresolved external conduit ownership.
CREATE TABLE service_conduit_creations (
 id uuid NOT NULL DEFAULT gen_random_uuid(), app_id uuid NOT NULL,
 client_id text COLLATE "C" NOT NULL, app_revision bigint NOT NULL, shard_count integer NOT NULL,
 state text NOT NULL DEFAULT 'prepared', claim_hash text, started_at timestamptz,
 completed_at timestamptz, failure_code text, conduit_id text COLLATE "C",
 principal text NOT NULL DEFAULT session_user,
 created_at timestamptz NOT NULL DEFAULT statement_timestamp(),
 deadline_at timestamptz NOT NULL DEFAULT (statement_timestamp()+interval '5 minutes'),
 CONSTRAINT service_conduit_creation_pk PRIMARY KEY(id),
 CONSTRAINT service_conduit_creation_client CHECK(client_id ~ '^[A-Za-z0-9_-]{1,128}$'),
 CONSTRAINT service_conduit_creation_revision CHECK(app_revision>0),
 CONSTRAINT service_conduit_creation_shards CHECK(shard_count BETWEEN 1 AND 20000),
 CONSTRAINT service_conduit_creation_deadline CHECK(deadline_at=created_at+interval '5 minutes'),
 CONSTRAINT service_conduit_creation_state CHECK(
  ((claim_hash IS NULL AND started_at IS NULL) OR (claim_hash IS NOT NULL AND claim_hash ~ '^[0-9a-f]{64}$' AND started_at IS NOT NULL)) AND
  ((state='prepared' AND claim_hash IS NULL AND completed_at IS NULL AND failure_code IS NULL AND conduit_id IS NULL) OR
   (state='started' AND claim_hash IS NOT NULL AND completed_at IS NULL AND failure_code IS NULL AND conduit_id IS NULL) OR
   (state='created' AND claim_hash IS NOT NULL AND completed_at IS NOT NULL AND failure_code IS NULL AND conduit_id IS NOT NULL AND conduit_id ~ '^[A-Za-z0-9_-]{1,128}$') OR
   (state IN('refused','uncertain') AND completed_at IS NOT NULL AND failure_code IS NOT NULL AND conduit_id IS NULL AND
    ((state='refused' AND failure_code IN('expired','not_started','provider_refused')) OR (state='uncertain' AND claim_hash IS NOT NULL AND failure_code='uncertain')))))
);
CREATE UNIQUE INDEX service_conduit_creation_active ON service_conduit_creations(client_id) WHERE state IN('prepared','started','uncertain','created');
CREATE UNIQUE INDEX service_conduit_creation_provider ON service_conduit_creations(client_id,conduit_id) WHERE conduit_id IS NOT NULL;
CREATE INDEX service_conduit_creation_retention ON service_conduit_creations(created_at,id);
ALTER TABLE service_conduit_creations ENABLE ROW LEVEL SECURITY;
ALTER TABLE service_conduit_creations FORCE ROW LEVEL SECURITY;
CREATE POLICY service_conduit_admin ON service_conduit_creations TO overlaid_conduit_admin USING(true) WITH CHECK(true);
REVOKE ALL ON service_conduit_creations FROM PUBLIC;
GRANT SELECT,DELETE ON service_conduit_creations TO overlaid_conduit_admin;
GRANT INSERT(app_id,client_id,app_revision,shard_count),UPDATE(state,claim_hash,started_at,completed_at,failure_code,conduit_id)
 ON service_conduit_creations TO overlaid_conduit_admin;
INSERT INTO overlaid_migrations(name) VALUES('0037_conduit_creation.sql') ON CONFLICT DO NOTHING;
COMMIT;
