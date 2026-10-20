BEGIN;
DO $setup$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='overlaid_app_admin') THEN
  CREATE ROLE overlaid_app_admin NOLOGIN NOSUPERUSER NOBYPASSRLS NOCREATEROLE NOCREATEDB NOREPLICATION;
 END IF;
 EXECUTE format('GRANT USAGE ON SCHEMA %I TO overlaid_app_admin',current_schema());
END $setup$;
-- Administrative app custody is separate from every tenant/bot/worker pool.
-- This additive foundation registers no execution role, provider call or owner.
CREATE TABLE service_app_credentials (
 id uuid NOT NULL DEFAULT gen_random_uuid(),
 provider text NOT NULL DEFAULT 'twitch', client_id text COLLATE "C" NOT NULL,
 revision bigint NOT NULL DEFAULT 1, secret_ciphertext bytea, token_ciphertext bytea, key_version text,
 validated_at timestamptz, expires_at timestamptz, revoked_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CONSTRAINT service_app_credentials_pk PRIMARY KEY(id),
 CONSTRAINT service_app_credentials_client UNIQUE(provider,client_id),
 CONSTRAINT service_app_provider CHECK(provider='twitch'),
 CONSTRAINT service_app_client CHECK(client_id ~ '^[A-Za-z0-9_-]{1,128}$'),
 CONSTRAINT service_app_revision CHECK(revision>0),
 CONSTRAINT service_app_state CHECK(
  (revoked_at IS NULL AND secret_ciphertext IS NOT NULL AND token_ciphertext IS NOT NULL AND
   octet_length(secret_ciphertext) BETWEEN 28 AND 32768 AND octet_length(token_ciphertext) BETWEEN 28 AND 32768 AND
   key_version IS NOT NULL AND length(key_version) BETWEEN 1 AND 40 AND validated_at IS NOT NULL AND expires_at IS NOT NULL AND expires_at>validated_at) OR
  (revoked_at IS NOT NULL AND secret_ciphertext IS NULL AND token_ciphertext IS NULL AND key_version IS NULL AND validated_at IS NULL AND expires_at IS NULL))
);
CREATE TABLE service_app_credential_audit (
 id uuid NOT NULL DEFAULT gen_random_uuid(), app_id uuid NOT NULL, revision bigint NOT NULL, action text NOT NULL,
 principal text NOT NULL DEFAULT session_user, created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CONSTRAINT service_app_audit_pk PRIMARY KEY(id),
 CONSTRAINT service_app_audit_app FOREIGN KEY(app_id) REFERENCES service_app_credentials(id) ON DELETE CASCADE,
 CONSTRAINT service_app_audit_unique UNIQUE(app_id,revision),
 CONSTRAINT service_app_audit_revision CHECK(revision>0),
 CONSTRAINT service_app_audit_action CHECK(action IN('install','revoke'))
);
CREATE INDEX service_app_audit_recent ON service_app_credential_audit(app_id,created_at DESC,id DESC);
ALTER TABLE service_app_credentials ENABLE ROW LEVEL SECURITY;
ALTER TABLE service_app_credentials FORCE ROW LEVEL SECURITY;
ALTER TABLE service_app_credential_audit ENABLE ROW LEVEL SECURITY;
ALTER TABLE service_app_credential_audit FORCE ROW LEVEL SECURITY;
CREATE POLICY service_app_admin ON service_app_credentials TO overlaid_app_admin USING(true) WITH CHECK(true);
CREATE POLICY service_app_admin ON service_app_credential_audit TO overlaid_app_admin USING(true) WITH CHECK(true);
REVOKE ALL ON service_app_credentials,service_app_credential_audit FROM PUBLIC;
GRANT SELECT ON service_app_credentials,service_app_credential_audit TO overlaid_app_admin;
GRANT INSERT(id,provider,client_id,revision,secret_ciphertext,token_ciphertext,key_version,validated_at,expires_at),
 UPDATE(revision,secret_ciphertext,token_ciphertext,key_version,validated_at,expires_at,revoked_at)
 ON service_app_credentials TO overlaid_app_admin;
GRANT INSERT(app_id,revision,action),DELETE ON service_app_credential_audit TO overlaid_app_admin;
INSERT INTO overlaid_migrations(name) VALUES('0034_service_app_custody.sql') ON CONFLICT DO NOTHING;
COMMIT;
