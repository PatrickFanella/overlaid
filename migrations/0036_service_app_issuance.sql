BEGIN;
CREATE TABLE service_app_issuance (
 id uuid NOT NULL DEFAULT gen_random_uuid(), client_id text COLLATE "C" NOT NULL,
 expected_revision bigint NOT NULL, secret_fingerprint text NOT NULL,
 state text NOT NULL DEFAULT 'prepared', claim_hash text, started_at timestamptz,
 completed_at timestamptz, failure_code text, installed_app_id uuid, installed_revision bigint,
 principal text NOT NULL DEFAULT session_user,
 created_at timestamptz NOT NULL DEFAULT statement_timestamp(),
 deadline_at timestamptz NOT NULL DEFAULT (statement_timestamp()+interval '5 minutes'),
 CONSTRAINT service_app_issuance_pk PRIMARY KEY(id),
 CONSTRAINT service_app_issuance_client CHECK(client_id ~ '^[A-Za-z0-9_-]{1,128}$'),
 CONSTRAINT service_app_issuance_revision CHECK(expected_revision>=0 AND expected_revision<9223372036854775807),
 CONSTRAINT service_app_issuance_secret CHECK(secret_fingerprint ~ '^[0-9a-f]{64}$'),
 CONSTRAINT service_app_issuance_deadline CHECK(deadline_at=created_at+interval '5 minutes'),
 CONSTRAINT service_app_issuance_app FOREIGN KEY(installed_app_id) REFERENCES service_app_credentials(id) ON DELETE CASCADE,
 CONSTRAINT service_app_issuance_state CHECK(
  ((claim_hash IS NULL AND started_at IS NULL) OR (claim_hash IS NOT NULL AND claim_hash ~ '^[0-9a-f]{64}$' AND started_at IS NOT NULL)) AND
  ((state='prepared' AND claim_hash IS NULL AND completed_at IS NULL AND failure_code IS NULL AND installed_app_id IS NULL AND installed_revision IS NULL) OR
   (state='started' AND claim_hash IS NOT NULL AND completed_at IS NULL AND failure_code IS NULL AND installed_app_id IS NULL AND installed_revision IS NULL) OR
   (state='issued' AND claim_hash IS NOT NULL AND completed_at IS NOT NULL AND failure_code IS NULL AND installed_app_id IS NOT NULL AND installed_revision IS NOT NULL AND installed_revision=expected_revision+1) OR
   (state IN('refused','uncertain') AND completed_at IS NOT NULL AND failure_code IS NOT NULL AND ((state='refused' AND failure_code IN('expired','not_started','provider_refused')) OR (state='uncertain' AND failure_code='uncertain')) AND installed_app_id IS NULL AND installed_revision IS NULL)))
);
CREATE UNIQUE INDEX service_app_issuance_active ON service_app_issuance(client_id) WHERE state IN('prepared','started','uncertain');
CREATE INDEX service_app_issuance_retention ON service_app_issuance(created_at,id);
ALTER TABLE service_app_issuance ENABLE ROW LEVEL SECURITY;
ALTER TABLE service_app_issuance FORCE ROW LEVEL SECURITY;
CREATE POLICY service_app_admin ON service_app_issuance TO overlaid_app_admin USING(true) WITH CHECK(true);
REVOKE ALL ON service_app_issuance FROM PUBLIC;
GRANT SELECT,DELETE ON service_app_issuance TO overlaid_app_admin;
GRANT INSERT(client_id,expected_revision,secret_fingerprint),
 UPDATE(state,claim_hash,started_at,completed_at,failure_code,installed_app_id,installed_revision)
 ON service_app_issuance TO overlaid_app_admin;
INSERT INTO overlaid_migrations(name) VALUES('0036_service_app_issuance.sql') ON CONFLICT DO NOTHING;
COMMIT;
