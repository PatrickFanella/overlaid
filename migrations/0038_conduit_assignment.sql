BEGIN;
DO $setup$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='overlaid_conduit_assignment_admin') THEN
  CREATE ROLE overlaid_conduit_assignment_admin NOLOGIN NOSUPERUSER NOBYPASSRLS NOCREATEROLE NOCREATEDB NOREPLICATION;
 END IF;
 EXECUTE format('GRANT USAGE ON SCHEMA %I TO overlaid_conduit_assignment_admin',current_schema());
END $setup$;
-- Credential bindings are immutable snapshots, never credential authority.
-- No lifecycle cascade may erase unresolved external conduit ownership.
CREATE TABLE service_conduit_assignments (
 id uuid NOT NULL DEFAULT gen_random_uuid(), app_id uuid NOT NULL,
 client_id text COLLATE "C" NOT NULL, app_revision bigint NOT NULL, shard_count integer NOT NULL,
 creation_id uuid NOT NULL, conduit_id text COLLATE "C" NOT NULL, shard_id integer NOT NULL, session_hash text COLLATE "C" NOT NULL,
 state text NOT NULL DEFAULT 'prepared', claim_hash text, started_at timestamptz,
 completed_at timestamptz, failure_code text,
 principal text NOT NULL DEFAULT session_user,
 created_at timestamptz NOT NULL DEFAULT statement_timestamp(),
 deadline_at timestamptz NOT NULL DEFAULT (statement_timestamp()+interval '10 seconds'),
 CONSTRAINT service_conduit_assignment_pk PRIMARY KEY(id),
 CONSTRAINT service_conduit_assignment_client CHECK(client_id ~ '^[A-Za-z0-9_-]{1,128}$'),
 CONSTRAINT service_conduit_assignment_revision CHECK(app_revision>0),
 CONSTRAINT service_conduit_assignment_shards CHECK(shard_count BETWEEN 1 AND 20000),
 CONSTRAINT service_conduit_assignment_conduit CHECK(conduit_id ~ '^[A-Za-z0-9_-]{1,128}$'),
 CONSTRAINT service_conduit_assignment_shard CHECK(shard_id>=0 AND shard_id<shard_count),
 CONSTRAINT service_conduit_assignment_session CHECK(session_hash ~ '^[0-9a-f]{64}$'),
 CONSTRAINT service_conduit_assignment_deadline CHECK(deadline_at=created_at+interval '10 seconds'),
 CONSTRAINT service_conduit_assignment_state CHECK(
  ((claim_hash IS NULL AND started_at IS NULL) OR (claim_hash IS NOT NULL AND claim_hash ~ '^[0-9a-f]{64}$' AND started_at IS NOT NULL)) AND
  ((state='prepared' AND claim_hash IS NULL AND completed_at IS NULL AND failure_code IS NULL) OR
   (state='started' AND claim_hash IS NOT NULL AND completed_at IS NULL AND failure_code IS NULL) OR
   (state='assigned' AND claim_hash IS NOT NULL AND completed_at IS NOT NULL AND failure_code IS NULL) OR
   (state IN('refused','uncertain') AND completed_at IS NOT NULL AND failure_code IS NOT NULL AND
    ((state='refused' AND failure_code IN('expired','not_started','provider_refused')) OR (state='uncertain' AND claim_hash IS NOT NULL AND failure_code='uncertain')))))
);
CREATE UNIQUE INDEX service_conduit_assignment_active ON service_conduit_assignments(client_id,conduit_id,shard_id) WHERE state IN('prepared','started','uncertain','assigned');
CREATE UNIQUE INDEX service_conduit_assignment_identity ON service_conduit_assignments(session_hash);
CREATE INDEX service_conduit_assignment_retention ON service_conduit_assignments(created_at,id);
ALTER TABLE service_conduit_assignments ENABLE ROW LEVEL SECURITY;
ALTER TABLE service_conduit_assignments FORCE ROW LEVEL SECURITY;
CREATE POLICY service_conduit_assignment_admin ON service_conduit_assignments TO overlaid_conduit_assignment_admin USING(true) WITH CHECK(true);
REVOKE ALL ON service_conduit_assignments FROM PUBLIC;
GRANT SELECT,DELETE ON service_conduit_assignments TO overlaid_conduit_assignment_admin;
GRANT INSERT(app_id,client_id,app_revision,shard_count,creation_id,conduit_id,shard_id,session_hash),UPDATE(state,claim_hash,started_at,completed_at,failure_code)
 ON service_conduit_assignments TO overlaid_conduit_assignment_admin;
INSERT INTO overlaid_migrations(name) VALUES('0038_conduit_assignment.sql') ON CONFLICT DO NOTHING;
COMMIT;
