BEGIN;
-- A distinct kind admits radiation-only occurrences without weakening the
-- existing single/batch action bounds or manufacturing a provider job.
ALTER TABLE platform_moderation_sources DROP CONSTRAINT platform_moderation_sources_kind_check;
ALTER TABLE platform_moderation_sources DROP CONSTRAINT platform_moderation_sources_action_count_check;
ALTER TABLE platform_moderation_sources DROP CONSTRAINT platform_moderation_sources_check2;
ALTER TABLE platform_moderation_sources ADD CONSTRAINT platform_moderation_source_action_count
 CHECK((kind='single' AND action_count=1) OR (kind='batch' AND action_count BETWEEN 1 AND 50) OR (kind='radiation' AND action_count=0));
INSERT INTO overlaid_migrations(name) VALUES('0030_radiation_only_sources.sql') ON CONFLICT DO NOTHING;
COMMIT;
