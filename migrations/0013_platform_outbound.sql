BEGIN;
CREATE TABLE platform_outbound_actions (
 tenant_id uuid NOT NULL DEFAULT nullif(current_setting('app.tenant_id',true),'')::uuid,
 id text NOT NULL CHECK(id ~ '^[0-9a-f]{64}$'),
 event_id text NOT NULL, connection_id text NOT NULL,
 action_key text NOT NULL CHECK(length(action_key) BETWEEN 1 AND 128),
 channel_id text NOT NULL CHECK(length(channel_id) BETWEEN 1 AND 256),
 message text NOT NULL CHECK(length(message) BETWEEN 1 AND 500),
 reply_to_id text NOT NULL CHECK(length(reply_to_id)<=256),
 capability text NOT NULL CHECK(capability IN ('chat.send','chat.reply')),
 payload_hash text NOT NULL CHECK(payload_hash ~ '^[0-9a-f]{64}$'),
 deadline timestamptz NOT NULL,
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','running','delivered','rejected','uncertain')),
 code text NOT NULL DEFAULT 'admitted' CHECK(code IN ('admitted','started','delivered','rejected','uncertain','expired','connection_unavailable','deadline_elapsed')),
 claim_hash text CHECK(claim_hash ~ '^[0-9a-f]{64}$'),
 provider_message_id text NOT NULL DEFAULT '' CHECK(length(provider_message_id)<=256),
 failure text NOT NULL DEFAULT '' CHECK(failure IN ('','retryable','unauthorized','unsupported','quota_limited','permanent')),
 retry_after_ns bigint NOT NULL DEFAULT 0 CHECK(retry_after_ns>=0),
 created_at timestamptz NOT NULL DEFAULT now(), started_at timestamptz, finished_at timestamptz,
 PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,event_id,connection_id,action_key),
 FOREIGN KEY(tenant_id,event_id,connection_id) REFERENCES normalized_chat_events(tenant_id,id,connection_id) ON DELETE CASCADE,
 CHECK((state='pending' AND started_at IS NULL AND finished_at IS NULL AND claim_hash IS NULL)
    OR (state='running' AND started_at IS NOT NULL AND finished_at IS NULL AND claim_hash IS NOT NULL)
    OR (state IN ('delivered','rejected','uncertain') AND finished_at IS NOT NULL AND claim_hash IS NULL)),
 CHECK((state='delivered' AND provider_message_id<>'' AND failure='' AND retry_after_ns=0)
    OR (state<>'delivered' AND provider_message_id='')),
 CHECK((capability='chat.send' AND reply_to_id='') OR (capability='chat.reply' AND reply_to_id<>''))
);
CREATE INDEX platform_outbound_recent ON platform_outbound_actions(tenant_id,created_at DESC,id);
CREATE INDEX platform_outbound_expiry ON platform_outbound_actions(tenant_id,deadline,id) WHERE state IN ('pending','running');
ALTER TABLE platform_outbound_actions ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform_outbound_actions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON platform_outbound_actions
 USING(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);
GRANT SELECT,INSERT,UPDATE,DELETE ON platform_outbound_actions TO overlaid_runtime;
INSERT INTO overlaid_migrations(name) VALUES('0013_platform_outbound.sql') ON CONFLICT DO NOTHING;
COMMIT;
