BEGIN;
-- The running River client also elects a maintenance leader. This table holds
-- service metadata, not tenant event payloads.
GRANT SELECT,INSERT,UPDATE,DELETE ON river_leader TO overlaid_worker;
CREATE TABLE twitch_sent_messages (
 tenant_id uuid NOT NULL DEFAULT nullif(current_setting('app.tenant_id',true),'')::uuid REFERENCES tenants(id) ON DELETE CASCADE,
 message_hash text NOT NULL, expires_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,message_hash)
);
CREATE INDEX twitch_sent_expiry ON twitch_sent_messages(tenant_id,expires_at);
ALTER TABLE twitch_sent_messages ENABLE ROW LEVEL SECURITY;
ALTER TABLE twitch_sent_messages FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON twitch_sent_messages
 USING(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);
GRANT SELECT,INSERT,UPDATE,DELETE ON twitch_sent_messages TO overlaid_runtime;
INSERT INTO overlaid_migrations(name) VALUES('0009_twitch_echo.sql') ON CONFLICT DO NOTHING;
COMMIT;
