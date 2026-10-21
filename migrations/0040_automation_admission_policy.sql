-- Codec compatibility only. No run/reservation/dispatcher is registered.
BEGIN;
ALTER TABLE engine_automation_versions DROP CONSTRAINT engine_version_header;
ALTER TABLE engine_automation_versions ADD CONSTRAINT engine_version_header CHECK(document::jsonb @> jsonb_build_object('tenantId',tenant_id::text,'automationId',automation_id::text,'versionId',id::text));
ALTER TABLE engine_automation_versions DROP CONSTRAINT IF EXISTS engine_version_schema;
ALTER TABLE engine_automation_versions ADD CONSTRAINT engine_version_schema CHECK(document::jsonb @> '{"schemaVersion":1}'::jsonb OR document::jsonb @> '{"schemaVersion":2}'::jsonb);
INSERT INTO overlaid_migrations(name) VALUES('0040_automation_admission_policy.sql') ON CONFLICT DO NOTHING;
COMMIT;
