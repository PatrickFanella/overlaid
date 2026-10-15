BEGIN;
CREATE TABLE platform_moderation_actions (
 tenant_id uuid NOT NULL DEFAULT nullif(current_setting('app.tenant_id',true),'')::uuid,
 id text NOT NULL CHECK(id ~ '^[0-9a-f]{64}$'),
 event_id text NOT NULL, connection_id text NOT NULL,
 action_key text NOT NULL CHECK(length(action_key) BETWEEN 1 AND 128),
 channel_id text NOT NULL CHECK(channel_id ~ '^[0-9]{1,128}$'),
 capability text NOT NULL CHECK(capability IN ('message.delete','user.timeout','user.ban','user.unban')),
 user_id text NOT NULL DEFAULT '', message_id text NOT NULL DEFAULT '',
 reason text NOT NULL DEFAULT '' CHECK(length(reason)<=500),
 duration_ns bigint NOT NULL DEFAULT 0,
 target_event_id text, target_actor_id text NOT NULL CHECK(length(target_actor_id) BETWEEN 1 AND 256),
 payload_hash text NOT NULL CHECK(payload_hash ~ '^[0-9a-f]{64}$'),
 deadline timestamptz NOT NULL,
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','running','delivered','rejected','uncertain')),
 code text NOT NULL DEFAULT 'admitted',
 claim_hash text CHECK(claim_hash ~ '^[0-9a-f]{64}$'),
 failure text NOT NULL DEFAULT '',
 retry_after_ns bigint NOT NULL DEFAULT 0 CHECK(retry_after_ns BETWEEN 0 AND 86400000000000),
 created_at timestamptz NOT NULL DEFAULT now(), started_at timestamptz, finished_at timestamptz,
 PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,event_id,connection_id,action_key),
 FOREIGN KEY(tenant_id,event_id,connection_id) REFERENCES normalized_chat_events(tenant_id,id,connection_id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,target_event_id,connection_id) REFERENCES normalized_chat_events(tenant_id,id,connection_id) ON DELETE CASCADE,
 CHECK((capability='message.delete' AND length(message_id) BETWEEN 1 AND 256 AND message_id ~ '^[!-~]+$' AND user_id='' AND duration_ns=0 AND reason='' AND target_event_id IS NOT NULL)
 OR (capability IN ('user.timeout','user.ban','user.unban') AND user_id ~ '^[0-9]{1,128}$' AND message_id='' AND target_event_id IS NULL AND target_actor_id=user_id
 AND ((capability='user.timeout' AND duration_ns BETWEEN 1000000000 AND 1209600000000000 AND duration_ns%1000000000=0)
 OR (capability='user.ban' AND duration_ns=0) OR (capability='user.unban' AND duration_ns=0 AND reason='')))),
 CHECK((state='pending' AND code='admitted' AND started_at IS NULL AND finished_at IS NULL AND claim_hash IS NULL)
 OR (state='running' AND code='started' AND started_at IS NOT NULL AND finished_at IS NULL AND claim_hash IS NOT NULL)
 OR (state='delivered' AND code='delivered' AND started_at IS NOT NULL AND finished_at IS NOT NULL AND claim_hash IS NULL)
 OR (state='uncertain' AND code IN ('uncertain','deadline_elapsed') AND started_at IS NOT NULL AND finished_at IS NOT NULL AND claim_hash IS NULL)
 OR (state='rejected' AND code IN ('rejected','expired','connection_unavailable') AND finished_at IS NOT NULL AND claim_hash IS NULL)),
 CHECK((state<>'rejected' AND failure='' AND retry_after_ns=0)
 OR (state='rejected' AND failure IN ('retryable','quota_limited'))
 OR (state='rejected' AND failure IN ('unauthorized','unsupported','permanent') AND retry_after_ns=0))
);
CREATE INDEX platform_moderation_recent ON platform_moderation_actions(tenant_id,created_at DESC,id);
CREATE INDEX platform_moderation_expiry ON platform_moderation_actions(tenant_id,deadline,id) WHERE state IN ('pending','running');
CREATE INDEX platform_moderation_target ON platform_moderation_actions(tenant_id,target_actor_id);
CREATE INDEX normalized_chat_message ON normalized_chat_events(tenant_id,connection_id,(envelope->'chat'->>'message_id'),id);
ALTER TABLE platform_moderation_actions ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform_moderation_actions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON platform_moderation_actions
 USING(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);
GRANT SELECT,INSERT,UPDATE,DELETE ON platform_moderation_actions TO overlaid_runtime;
INSERT INTO overlaid_migrations(name) VALUES('0019_moderation_actions.sql') ON CONFLICT DO NOTHING;
COMMIT;
