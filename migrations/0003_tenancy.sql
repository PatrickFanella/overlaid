-- Explicit, transactional cutover. Stop old/global application writers first;
-- retain a verified backup. Runtime connections use a different non-owner role.
BEGIN;
SELECT pg_advisory_xact_lock(735619023);
CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public;

CREATE TABLE IF NOT EXISTS accounts (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), display_name text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS account_identities (
  account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  provider text NOT NULL, provider_user_id text NOT NULL,
  PRIMARY KEY(provider, provider_user_id)
);
CREATE TABLE IF NOT EXISTS plans (
  id text PRIMARY KEY CHECK(id IN ('free','paid','unlimited')),
  limits jsonb NOT NULL CHECK(jsonb_typeof(limits)='object')
);
INSERT INTO plans(id,limits) VALUES
 ('free','{"commands":25,"timers":5,"apiVariables":5,"httpCallsPerHour":120,"storedEvents":250,"mediaBytes":0}'),
 ('paid','{"commands":250,"timers":50,"apiVariables":50,"httpCallsPerHour":1200,"storedEvents":5000,"mediaBytes":1073741824}'),
 ('unlimited','{"commands":-1,"timers":-1,"apiVariables":-1,"httpCallsPerHour":-1,"storedEvents":-1,"mediaBytes":-1}')
ON CONFLICT(id) DO NOTHING;
CREATE TABLE IF NOT EXISTS tenants (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), slug text NOT NULL UNIQUE,
  name text NOT NULL, owner_account_id uuid NOT NULL REFERENCES accounts(id),
  plan_id text NOT NULL REFERENCES plans(id), retention_days integer NOT NULL DEFAULT 30 CHECK(retention_days BETWEEN 1 AND 365),
  bootstrap_key text UNIQUE, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS tenant_memberships (
  tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  role text NOT NULL CHECK(role IN ('owner','manager','editor','moderator')),
  PRIMARY KEY(tenant_id,account_id)
);
CREATE UNIQUE INDEX IF NOT EXISTS tenant_one_owner ON tenant_memberships(tenant_id) WHERE role='owner';
CREATE TABLE IF NOT EXISTS account_sessions (
  token_hash text PRIMARY KEY, account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  active_tenant_id uuid REFERENCES tenants(id) ON DELETE SET NULL, csrf_hash text NOT NULL,
  expires_at timestamptz NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS account_oauth_states (
  state_hash text PRIMARY KEY, browser_hash text NOT NULL, purpose text NOT NULL,
  tenant_id uuid REFERENCES tenants(id) ON DELETE CASCADE, expires_at timestamptz NOT NULL
);
CREATE TABLE IF NOT EXISTS tenant_change_audit (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id uuid,
  actor_account_id uuid, action text NOT NULL, diff jsonb NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS platform_connections (
  tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  provider text NOT NULL, provider_user_id text NOT NULL, login text NOT NULL,
  ciphertext bytea, key_version text, scopes jsonb NOT NULL DEFAULT '[]',
  granted_at timestamptz NOT NULL DEFAULT now(), revoked_at timestamptz,
  PRIMARY KEY(tenant_id,provider), UNIQUE(tenant_id,provider,provider_user_id)
);
CREATE TABLE IF NOT EXISTS tenant_credentials (
  tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  purpose text NOT NULL CHECK(purpose IN ('overlay','webhook')), token_hash text NOT NULL UNIQUE,
  version integer NOT NULL DEFAULT 1, PRIMARY KEY(tenant_id,purpose)
);
CREATE TABLE IF NOT EXISTS tenant_imports (
  tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  kind text NOT NULL, imported_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(tenant_id,kind)
);
CREATE TABLE IF NOT EXISTS tenant_usage (
  tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  kind text NOT NULL, window_start timestamptz NOT NULL, used bigint NOT NULL DEFAULT 0,
  PRIMARY KEY(tenant_id,kind,window_start)
);
CREATE TABLE IF NOT EXISTS overlay_state (
  tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  key text NOT NULL, event jsonb NOT NULL, PRIMARY KEY(tenant_id,key)
);

INSERT INTO accounts(id,display_name) VALUES ('00000000-0000-4000-8000-000000000002','Self-host operator') ON CONFLICT(id) DO NOTHING;
INSERT INTO tenants(id,slug,name,owner_account_id,plan_id,bootstrap_key) VALUES
 ('00000000-0000-4000-8000-000000000001','selfhost','Self-host','00000000-0000-4000-8000-000000000002','unlimited','selfhost-v1')
ON CONFLICT(bootstrap_key) DO NOTHING;
INSERT INTO tenant_memberships(tenant_id,account_id,role) VALUES
 ('00000000-0000-4000-8000-000000000001','00000000-0000-4000-8000-000000000002','owner') ON CONFLICT DO NOTHING;

DO $$ DECLARE tab text; BEGIN
  FOREACH tab IN ARRAY ARRAY['app_metadata','stream_events','twitch_interaction_rules','dashboard_configs',
    'stream_sessions','stream_session_readiness_snapshots','bot_audit_events','bot_commands','bot_timers',
    'bot_counters','bot_quotes','bot_moderation_rules','bot_api_variables'] LOOP
    EXECUTE format('ALTER TABLE %I ADD COLUMN IF NOT EXISTS tenant_id uuid',tab);
    EXECUTE format('UPDATE %I SET tenant_id=$1 WHERE tenant_id IS NULL',tab) USING '00000000-0000-4000-8000-000000000001'::uuid;
    EXECUTE format('ALTER TABLE %I ALTER COLUMN tenant_id SET DEFAULT nullif(current_setting(''app.tenant_id'',true),'''')::uuid',tab);
    EXECUTE format('ALTER TABLE %I ALTER COLUMN tenant_id SET NOT NULL',tab);
    IF NOT EXISTS(SELECT 1 FROM pg_constraint WHERE conname=tab||'_tenant_fk' AND conrelid=to_regclass(tab)) THEN
      EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I FOREIGN KEY(tenant_id) REFERENCES tenants(id) ON DELETE CASCADE',tab,tab||'_tenant_fk');
    END IF;
    EXECUTE format('CREATE INDEX IF NOT EXISTS %I ON %I(tenant_id)',tab||'_tenant_idx',tab);
  END LOOP;
END $$;

-- Singleton IDs and names are unique inside a tenant, never across creators.
ALTER TABLE dashboard_configs DROP CONSTRAINT IF EXISTS dashboard_configs_pkey;
ALTER TABLE dashboard_configs ADD PRIMARY KEY(tenant_id,id);
ALTER TABLE twitch_interaction_rules DROP CONSTRAINT IF EXISTS twitch_interaction_rules_pkey;
ALTER TABLE twitch_interaction_rules ADD PRIMARY KEY(tenant_id,id);
ALTER TABLE app_metadata DROP CONSTRAINT IF EXISTS app_metadata_pkey;
ALTER TABLE app_metadata ADD PRIMARY KEY(tenant_id,key);
DO $$ DECLARE tab text; BEGIN
  FOREACH tab IN ARRAY ARRAY['bot_commands','bot_timers','bot_counters','bot_moderation_rules','bot_api_variables'] LOOP
    EXECUTE format('ALTER TABLE %I DROP CONSTRAINT IF EXISTS %I',tab,tab||'_name_key');
    EXECUTE format('CREATE UNIQUE INDEX IF NOT EXISTS %I ON %I(tenant_id,name)',tab||'_tenant_name_key',tab);
  END LOOP;
END $$;
CREATE UNIQUE INDEX IF NOT EXISTS stream_sessions_tenant_id_key ON stream_sessions(tenant_id,id);
CREATE UNIQUE INDEX IF NOT EXISTS stream_events_tenant_id_key ON stream_events(tenant_id,id);
ALTER TABLE stream_events DROP CONSTRAINT IF EXISTS stream_events_session_id_fkey;
ALTER TABLE stream_events DROP CONSTRAINT IF EXISTS stream_events_tenant_session_fk;
ALTER TABLE stream_events ADD CONSTRAINT stream_events_tenant_session_fk FOREIGN KEY(tenant_id,session_id) REFERENCES stream_sessions(tenant_id,id) ON DELETE SET NULL(session_id);
ALTER TABLE stream_session_readiness_snapshots DROP CONSTRAINT IF EXISTS stream_session_readiness_snapshots_session_id_fkey;
ALTER TABLE stream_session_readiness_snapshots DROP CONSTRAINT IF EXISTS snapshots_tenant_session_fk;
ALTER TABLE stream_session_readiness_snapshots ADD CONSTRAINT snapshots_tenant_session_fk FOREIGN KEY(tenant_id,session_id) REFERENCES stream_sessions(tenant_id,id) ON DELETE CASCADE;
ALTER TABLE bot_audit_events DROP CONSTRAINT IF EXISTS bot_audit_events_event_id_fkey;
ALTER TABLE bot_audit_events DROP CONSTRAINT IF EXISTS bot_audit_events_stream_session_id_fkey;
ALTER TABLE bot_audit_events DROP CONSTRAINT IF EXISTS audit_tenant_event_fk;
ALTER TABLE bot_audit_events DROP CONSTRAINT IF EXISTS audit_tenant_session_fk;
ALTER TABLE bot_audit_events ADD CONSTRAINT audit_tenant_event_fk FOREIGN KEY(tenant_id,event_id) REFERENCES stream_events(tenant_id,id) ON DELETE SET NULL(event_id);
ALTER TABLE bot_audit_events ADD CONSTRAINT audit_tenant_session_fk FOREIGN KEY(tenant_id,stream_session_id) REFERENCES stream_sessions(tenant_id,id) ON DELETE SET NULL(stream_session_id);

DO $$ DECLARE tab text; BEGIN
  FOREACH tab IN ARRAY ARRAY['app_metadata','stream_events','twitch_interaction_rules','dashboard_configs',
    'stream_sessions','stream_session_readiness_snapshots','bot_audit_events','bot_commands','bot_timers',
    'bot_counters','bot_quotes','bot_moderation_rules','bot_api_variables','platform_connections',
    'tenant_credentials','tenant_imports','tenant_usage','overlay_state'] LOOP
    EXECUTE format('ALTER TABLE %I ALTER COLUMN tenant_id SET DEFAULT nullif(current_setting(''app.tenant_id'',true),'''')::uuid',tab);
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',tab);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',tab);
    EXECUTE format('DROP POLICY IF EXISTS tenant_isolation ON %I',tab);
    EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id=nullif(current_setting(''app.tenant_id'',true),'''')::uuid) WITH CHECK (tenant_id=nullif(current_setting(''app.tenant_id'',true),'''')::uuid)',tab);
  END LOOP;
  IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='overlaid_runtime') THEN CREATE ROLE overlaid_runtime NOLOGIN NOSUPERUSER NOBYPASSRLS; END IF;
END $$;
GRANT USAGE ON SCHEMA public TO overlaid_runtime;
GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA public TO overlaid_runtime;
DO $setup$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='overlaid_credential_lookup') THEN CREATE ROLE overlaid_credential_lookup NOLOGIN NOSUPERUSER NOBYPASSRLS; END IF;
 EXECUTE format('GRANT USAGE ON SCHEMA %I TO overlaid_credential_lookup',current_schema());
 GRANT SELECT ON tenant_credentials TO overlaid_credential_lookup;
 DROP POLICY IF EXISTS credential_lookup ON tenant_credentials;
 CREATE POLICY credential_lookup ON tenant_credentials FOR SELECT TO overlaid_credential_lookup USING(true);
 EXECUTE format('CREATE OR REPLACE FUNCTION resolve_tenant_credential(hash text,kind text) RETURNS uuid LANGUAGE sql SECURITY DEFINER SET search_path=%I,pg_temp AS $fn$ SELECT tenant_id FROM %I.tenant_credentials WHERE token_hash=hash AND purpose=kind $fn$',current_schema(),current_schema());
 ALTER FUNCTION resolve_tenant_credential(text,text) OWNER TO overlaid_credential_lookup;
END $setup$;
REVOKE ALL ON FUNCTION resolve_tenant_credential(text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION resolve_tenant_credential(text,text) TO overlaid_runtime;
COMMIT;
