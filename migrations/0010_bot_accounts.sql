BEGIN;
CREATE TABLE bot_accounts (
 tenant_id uuid NOT NULL DEFAULT nullif(current_setting('app.tenant_id',true),'')::uuid REFERENCES tenants(id) ON DELETE CASCADE,
 id uuid NOT NULL DEFAULT gen_random_uuid(),
 provider text NOT NULL CHECK(provider='twitch'),
 provider_user_id text NOT NULL CHECK(length(provider_user_id) BETWEEN 1 AND 128),
 login text NOT NULL CHECK(length(login) BETWEEN 1 AND 64),
 scopes jsonb NOT NULL DEFAULT '[]' CHECK(jsonb_typeof(scopes)='array'),
 ciphertext bytea, key_version text, granted_at timestamptz NOT NULL DEFAULT now(), revoked_at timestamptz,
 PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,provider,provider_user_id),
 CHECK((revoked_at IS NULL AND ciphertext IS NOT NULL AND key_version IS NOT NULL) OR
       (revoked_at IS NOT NULL AND ciphertext IS NULL AND key_version IS NULL AND scopes='[]'))
);
ALTER TABLE bot_accounts ENABLE ROW LEVEL SECURITY;
ALTER TABLE bot_accounts FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON bot_accounts
 USING(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);
GRANT SELECT,INSERT,UPDATE,DELETE ON bot_accounts TO overlaid_runtime;
ALTER TABLE platform_connections ADD COLUMN bot_account_id uuid;
ALTER TABLE platform_connections ADD CONSTRAINT connection_bot_account
 FOREIGN KEY(tenant_id,bot_account_id) REFERENCES bot_accounts(tenant_id,id);
INSERT INTO overlaid_migrations(name) VALUES('0010_bot_accounts.sql') ON CONFLICT DO NOTHING;
COMMIT;
