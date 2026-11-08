-- Captured randomness for operator occurrences. New admissions store one
-- random seed chosen once; zero marks older capture-1 rows, which have none.
-- The seed is bounded to JavaScript's safe integers so browsers keep it exact.
BEGIN;
ALTER TABLE engine_operator_occurrences ADD COLUMN IF NOT EXISTS seed bigint NOT NULL DEFAULT 0;
ALTER TABLE engine_operator_occurrences DROP CONSTRAINT IF EXISTS engine_occurrence_seed;
ALTER TABLE engine_operator_occurrences ADD CONSTRAINT engine_occurrence_seed CHECK(seed BETWEEN 0 AND 9007199254740991);
REVOKE ALL(seed) ON engine_operator_occurrences FROM PUBLIC,overlaid_runtime;
GRANT SELECT,INSERT ON engine_operator_occurrences TO overlaid_runtime;
-- Plans record capture 1 (no seed) or capture 2 (captured seed).
-- Do not downgrade a newer capture contract when migrations are rerun.
DO $$ BEGIN
IF NOT EXISTS (SELECT 1 FROM pg_attribute WHERE attrelid='engine_operator_occurrences'::regclass AND attname='recent' AND NOT attisdropped) THEN
ALTER TABLE engine_operator_chat_plans DROP CONSTRAINT IF EXISTS engine_chat_plan_header;
ALTER TABLE engine_operator_chat_plans ADD CONSTRAINT engine_chat_plan_header CHECK(document::jsonb @> jsonb_build_object('formatVersion',1,'syntaxVersion',2,'tenantId',tenant_id::text,'id',id,'runId',run_id,'occurrenceId',occurrence_id,'automationId',automation_id::text,'versionId',version_id::text,'graphDigest',graph_digest,'connectionId',connection_id,'channelId',channel_id,'effectCount',effect_count) AND document::jsonb->'captureVersion' IN ('1'::jsonb,'2'::jsonb));
END IF;
END $$;
INSERT INTO overlaid_migrations(name) VALUES('0047_operator_random_seed.sql') ON CONFLICT DO NOTHING;
COMMIT;
