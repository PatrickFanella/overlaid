BEGIN;
-- Operator occurrences are not viewer chat. Old chat IDs/jobs/claims stay intact.
CREATE TABLE platform_moderation_sources (
 tenant_id uuid NOT NULL DEFAULT nullif(current_setting('app.tenant_id',true),'')::uuid REFERENCES tenants(id) ON DELETE CASCADE,
 id text NOT NULL CHECK(id ~ '^[0-9a-f]{64}$'),
 actor_account_id uuid NOT NULL,
 provider text NOT NULL DEFAULT 'twitch' CHECK(provider='twitch'),
 connection_id text NOT NULL, channel_id text NOT NULL CHECK(channel_id ~ '^[0-9]{1,128}$'),
 kind text NOT NULL CHECK(kind IN('single','batch')),
 payload_hash text NOT NULL CHECK(payload_hash ~ '^[0-9a-f]{64}$'),
 action_count integer NOT NULL CHECK(action_count BETWEEN 1 AND 50),
 created_at timestamptz NOT NULL, deadline timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,id,connection_id),
 FOREIGN KEY(tenant_id,actor_account_id) REFERENCES tenant_memberships(tenant_id,account_id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,provider) REFERENCES platform_connections(tenant_id,provider) ON DELETE CASCADE,
 CHECK(connection_id='twitch:'||channel_id), CHECK(deadline=created_at+interval '2 minutes'),
 CHECK(kind='batch' OR action_count=1)
);
CREATE INDEX platform_moderation_source_retention ON platform_moderation_sources(tenant_id,created_at,id);
ALTER TABLE platform_moderation_sources ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform_moderation_sources FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON platform_moderation_sources
 USING(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);
GRANT SELECT,INSERT,UPDATE,DELETE ON platform_moderation_sources TO overlaid_runtime;
ALTER TABLE platform_moderation_actions ALTER COLUMN event_id DROP NOT NULL;
ALTER TABLE platform_moderation_actions ADD COLUMN manual_source_id text;
ALTER TABLE platform_moderation_actions ADD CONSTRAINT platform_moderation_source_kind CHECK((event_id IS NULL)<>(manual_source_id IS NULL));
ALTER TABLE platform_moderation_actions ADD CONSTRAINT platform_moderation_manual_source
 FOREIGN KEY(tenant_id,manual_source_id,connection_id) REFERENCES platform_moderation_sources(tenant_id,id,connection_id) ON DELETE CASCADE;
CREATE UNIQUE INDEX platform_moderation_manual_key ON platform_moderation_actions(tenant_id,manual_source_id,action_key) WHERE manual_source_id IS NOT NULL;
INSERT INTO overlaid_migrations(name) VALUES('0028_manual_moderation_sources.sql') ON CONFLICT DO NOTHING;
COMMIT;
