BEGIN;
-- Keep the existing typed delete/user-action constraint unchanged. Nuke
-- timeouts additionally bind their immutable preview event for lifecycle deletion.
ALTER TABLE platform_moderation_actions ADD COLUMN nuke_target_event_id text;
ALTER TABLE platform_moderation_actions ADD CONSTRAINT platform_moderation_nuke_target_kind
 CHECK(nuke_target_event_id IS NULL OR (manual_source_id IS NOT NULL AND capability='user.timeout' AND target_event_id IS NULL));
ALTER TABLE platform_moderation_actions ADD CONSTRAINT platform_moderation_nuke_target
 FOREIGN KEY(tenant_id,nuke_target_event_id,connection_id) REFERENCES normalized_chat_events(tenant_id,id,connection_id) ON DELETE CASCADE;
CREATE INDEX platform_moderation_nuke_target_lookup ON platform_moderation_actions(tenant_id,nuke_target_event_id,connection_id) WHERE nuke_target_event_id IS NOT NULL;
INSERT INTO overlaid_migrations(name) VALUES('0029_nuke_timeout_targets.sql') ON CONFLICT DO NOTHING;
COMMIT;
