-- Capture no-repeat memory before each operator run. JSON null preserves the
-- unsupported behavior of older captures; new captures always store an object.
-- Stop older runtimes before migrating; preserve these rows on rollback.
BEGIN;
ALTER TABLE engine_operator_occurrences ADD COLUMN IF NOT EXISTS recent jsonb NOT NULL DEFAULT 'null'::jsonb;
ALTER TABLE engine_run_budgets ADD COLUMN IF NOT EXISTS recent jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE engine_operator_occurrences DROP CONSTRAINT IF EXISTS engine_occurrence_recent;
ALTER TABLE engine_operator_occurrences ADD CONSTRAINT engine_occurrence_recent CHECK(recent='null'::jsonb OR (jsonb_typeof(recent)='object' AND octet_length(recent::text)<=300000 AND seed>0));
ALTER TABLE engine_run_budgets DROP CONSTRAINT IF EXISTS engine_budget_recent;
ALTER TABLE engine_run_budgets ADD CONSTRAINT engine_budget_recent CHECK(jsonb_typeof(recent)='object' AND octet_length(recent::text)<=300000);
REVOKE ALL(recent) ON engine_operator_occurrences,engine_run_budgets FROM PUBLIC,overlaid_runtime;
GRANT SELECT,INSERT ON engine_operator_occurrences,engine_run_budgets TO overlaid_runtime;
GRANT UPDATE(recent) ON engine_run_budgets TO overlaid_runtime;
ALTER TABLE engine_operator_chat_plans DROP CONSTRAINT IF EXISTS engine_chat_plan_header;
ALTER TABLE engine_operator_chat_plans ADD CONSTRAINT engine_chat_plan_header CHECK(document::jsonb @> jsonb_build_object('formatVersion',1,'syntaxVersion',2,'tenantId',tenant_id::text,'id',id,'runId',run_id,'occurrenceId',occurrence_id,'automationId',automation_id::text,'versionId',version_id::text,'graphDigest',graph_digest,'connectionId',connection_id,'channelId',channel_id,'effectCount',effect_count) AND document::jsonb->'captureVersion' IN ('1'::jsonb,'2'::jsonb,'3'::jsonb));
INSERT INTO overlaid_migrations(name) VALUES('0048_operator_no_repeat.sql') ON CONFLICT DO NOTHING;
COMMIT;
