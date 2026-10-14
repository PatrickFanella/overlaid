BEGIN;
CREATE TABLE service_bot_catalog (
 id uuid PRIMARY KEY REFERENCES service_bot_accounts(id), provider text NOT NULL CHECK(provider='twitch'),
 provider_user_id text NOT NULL, login text NOT NULL,
 grant_revision bigint NOT NULL CHECK(grant_revision>0), revision bigint NOT NULL DEFAULT 1 CHECK(revision>0),
 available boolean NOT NULL DEFAULT false
);
ALTER TABLE service_bot_catalog ENABLE ROW LEVEL SECURITY;
ALTER TABLE service_bot_catalog FORCE ROW LEVEL SECURITY;
CREATE POLICY catalog_admin ON service_bot_catalog TO overlaid_bot_admin USING(true) WITH CHECK(true);
CREATE POLICY catalog_runtime ON service_bot_catalog FOR SELECT TO overlaid_runtime USING(available);
GRANT SELECT,INSERT,UPDATE ON service_bot_catalog TO overlaid_bot_admin;
GRANT SELECT ON service_bot_catalog TO overlaid_runtime;
REVOKE ALL ON service_bot_catalog FROM PUBLIC,overlaid_worker;
ALTER TABLE service_bot_account_audit ADD COLUMN catalog_revision bigint CHECK(catalog_revision IS NULL OR catalog_revision>0);
ALTER TABLE service_bot_account_audit DROP CONSTRAINT service_bot_account_audit_action_check;
ALTER TABLE service_bot_account_audit ADD CONSTRAINT service_bot_account_audit_action_check
 CHECK(action IN('grant','refresh','revoke','publish','unpublish'));
CREATE TABLE shared_bot_delegations (
 tenant_id uuid NOT NULL DEFAULT nullif(current_setting('app.tenant_id',true),'')::uuid REFERENCES tenants(id) ON DELETE CASCADE,
 provider text NOT NULL CHECK(provider='twitch'), channel_user_id text NOT NULL,
 bot_account_id uuid NOT NULL REFERENCES service_bot_catalog(id),
 bot_grant_revision bigint NOT NULL CHECK(bot_grant_revision>0), catalog_revision bigint NOT NULL CHECK(catalog_revision>0),
 capabilities jsonb NOT NULL CHECK(jsonb_typeof(capabilities)='array'),
 revision bigint NOT NULL DEFAULT 1 CHECK(revision>0), granted_at timestamptz NOT NULL DEFAULT now(), revoked_at timestamptz,
 PRIMARY KEY(tenant_id,provider),
 FOREIGN KEY(tenant_id,provider) REFERENCES platform_connections(tenant_id,provider) ON DELETE CASCADE,
 CHECK(revoked_at IS NULL OR capabilities='[]')
);
ALTER TABLE shared_bot_delegations ENABLE ROW LEVEL SECURITY;
ALTER TABLE shared_bot_delegations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON shared_bot_delegations
 USING(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);
GRANT SELECT,INSERT,UPDATE,DELETE ON shared_bot_delegations TO overlaid_runtime;
INSERT INTO overlaid_migrations(name) VALUES('0012_shared_bot_delegations.sql') ON CONFLICT DO NOTHING;
COMMIT;
