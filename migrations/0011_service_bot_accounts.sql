BEGIN;
DO $setup$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='overlaid_bot_admin') THEN
  CREATE ROLE overlaid_bot_admin NOLOGIN NOSUPERUSER NOBYPASSRLS;
 END IF;
 EXECUTE format('GRANT USAGE ON SCHEMA %I TO overlaid_bot_admin',current_schema());
END $setup$;
-- Service identities have no tenant owner. Only the dedicated administrative
-- role may access them; product/worker roles have neither grants nor policies.
CREATE TABLE service_bot_accounts (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), provider text NOT NULL CHECK(provider='twitch'),
 provider_user_id text NOT NULL CHECK(length(provider_user_id) BETWEEN 1 AND 128),
 login text NOT NULL CHECK(length(login) BETWEEN 1 AND 64),
 scopes jsonb NOT NULL DEFAULT '[]' CHECK(jsonb_typeof(scopes)='array'),
 revision bigint NOT NULL DEFAULT 1 CHECK(revision>0),
 ciphertext bytea, key_version text, granted_at timestamptz NOT NULL DEFAULT now(), revoked_at timestamptz,
 UNIQUE(provider,provider_user_id),
 CHECK((revoked_at IS NULL AND ciphertext IS NOT NULL AND key_version IS NOT NULL) OR
       (revoked_at IS NOT NULL AND ciphertext IS NULL AND key_version IS NULL AND scopes='[]'))
);
CREATE TABLE service_bot_account_audit (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), bot_account_id uuid NOT NULL REFERENCES service_bot_accounts(id),
 revision bigint NOT NULL CHECK(revision>0), action text NOT NULL CHECK(action IN ('grant','refresh','revoke')),
 principal text NOT NULL DEFAULT session_user, created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX service_bot_audit_recent ON service_bot_account_audit(created_at DESC,id DESC);
ALTER TABLE service_bot_accounts ENABLE ROW LEVEL SECURITY;
ALTER TABLE service_bot_accounts FORCE ROW LEVEL SECURITY;
CREATE POLICY service_bot_admin ON service_bot_accounts TO overlaid_bot_admin USING(true) WITH CHECK(true);
ALTER TABLE service_bot_account_audit ENABLE ROW LEVEL SECURITY;
ALTER TABLE service_bot_account_audit FORCE ROW LEVEL SECURITY;
CREATE POLICY service_bot_admin ON service_bot_account_audit TO overlaid_bot_admin USING(true) WITH CHECK(true);
REVOKE ALL ON service_bot_accounts,service_bot_account_audit FROM PUBLIC,overlaid_runtime,overlaid_worker;
GRANT SELECT,INSERT,UPDATE ON service_bot_accounts TO overlaid_bot_admin;
GRANT SELECT,INSERT,DELETE ON service_bot_account_audit TO overlaid_bot_admin;
INSERT INTO overlaid_migrations(name) VALUES('0011_service_bot_accounts.sql') ON CONFLICT DO NOTHING;
COMMIT;
