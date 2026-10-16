BEGIN;
-- Explicit operator announcements are not normalized viewer chat events.
CREATE TABLE platform_manual_sources (
 tenant_id uuid NOT NULL DEFAULT nullif(current_setting('app.tenant_id',true),'')::uuid REFERENCES tenants(id) ON DELETE CASCADE,
 id text NOT NULL CHECK(id ~ '^[0-9a-f]{64}$'),
 actor_account_id uuid NOT NULL,
 provider text NOT NULL DEFAULT 'twitch' CHECK(provider='twitch'),
 connection_id text NOT NULL, channel_id text NOT NULL CHECK(length(channel_id) BETWEEN 1 AND 256),
 kind text NOT NULL CHECK(kind IN('newsroom_current','newsroom_raffle')),
 payload_hash text NOT NULL CHECK(payload_hash ~ '^[0-9a-f]{64}$'),
 viewer_id text NOT NULL DEFAULT '' CHECK(length(viewer_id)<=128),
 viewer_login text NOT NULL DEFAULT '' CHECK(length(viewer_login)<=128 AND viewer_login=lower(viewer_login)),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(), deadline timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,id,connection_id),
 FOREIGN KEY(tenant_id,actor_account_id) REFERENCES tenant_memberships(tenant_id,account_id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,provider) REFERENCES platform_connections(tenant_id,provider) ON DELETE CASCADE,
 CHECK(connection_id='twitch:'||channel_id), CHECK(deadline=created_at+interval '2 minutes')
);
CREATE INDEX platform_manual_retention ON platform_manual_sources(tenant_id,created_at,id);
CREATE INDEX platform_manual_viewer ON platform_manual_sources(tenant_id,viewer_id,viewer_login);
ALTER TABLE platform_manual_sources ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform_manual_sources FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON platform_manual_sources
 USING(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);
GRANT SELECT,INSERT,UPDATE,DELETE ON platform_manual_sources TO overlaid_runtime;

ALTER TABLE platform_outbound_actions ALTER COLUMN event_id DROP NOT NULL;
ALTER TABLE platform_outbound_actions ADD COLUMN manual_source_id text;
ALTER TABLE platform_outbound_actions ADD CONSTRAINT platform_outbound_source_kind CHECK((event_id IS NULL)<>(manual_source_id IS NULL));
ALTER TABLE platform_outbound_actions ADD CONSTRAINT platform_outbound_manual_source
 FOREIGN KEY(tenant_id,manual_source_id,connection_id) REFERENCES platform_manual_sources(tenant_id,id,connection_id) ON DELETE CASCADE;
CREATE UNIQUE INDEX platform_outbound_one_manual ON platform_outbound_actions(tenant_id,manual_source_id) WHERE manual_source_id IS NOT NULL;
-- Existing chat identities, original deadlines, claims and River jobs are intact.
INSERT INTO overlaid_migrations(name) VALUES('0026_manual_outbound_sources.sql') ON CONFLICT DO NOTHING;
COMMIT;
