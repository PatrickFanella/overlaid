BEGIN;
-- Stop moderation admission writers during this administrator migration and
-- deploy the priority-one writer before resuming. Older priority-four inserts
-- fail closed. No job identity, deadline, state, attempt or claim is renewed.
ALTER TABLE river_job DROP CONSTRAINT overlaid_moderation_args;
UPDATE river_job SET priority=1 WHERE kind='overlaid_moderation';
ALTER TABLE river_job ADD CONSTRAINT overlaid_moderation_args CHECK(kind<>'overlaid_moderation' OR
 (args IS NOT NULL AND jsonb_typeof(args)='object' AND args ?& ARRAY['tenant_id','action_id','deadline']
 AND args-ARRAY['tenant_id','action_id','deadline']='{}'::jsonb
 AND jsonb_typeof(args->'tenant_id')='string'
 AND jsonb_typeof(args->'action_id')='string' AND (args->>'action_id') ~ '^[0-9a-f]{64}$'
 AND jsonb_typeof(args->'deadline')='number' AND (args->>'deadline') ~ '^[1-9][0-9]{0,11}$'
 AND queue='moderation_outbound' AND max_attempts=3 AND priority=1));
-- A named compatibility gate lets runtime startup reject pre-policy schemas.
ALTER TABLE river_job ADD CONSTRAINT overlaid_moderation_priority
 CHECK(kind<>'overlaid_moderation' OR priority=1);
INSERT INTO overlaid_migrations(name) VALUES('0022_moderation_priority.sql') ON CONFLICT DO NOTHING;
COMMIT;
