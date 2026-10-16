BEGIN;
-- One bounded turn list per client chooses work eligible for both resources.
-- Independent per-resource turn lists could elect incompatible winners.
ALTER TABLE moderation_pacing_buckets DROP CONSTRAINT moderation_pacing_buckets_kind_check;
ALTER TABLE moderation_pacing_buckets ADD CONSTRAINT moderation_pacing_buckets_kind_check
 CHECK(kind IN('sender','channel','client'));
ALTER TABLE moderation_pacing_buckets ADD COLUMN fair_state jsonb NOT NULL DEFAULT '{}';
ALTER TABLE moderation_pacing_buckets ADD CONSTRAINT moderation_pacing_fair_state_bound
 CHECK(jsonb_typeof(fair_state)='object' AND octet_length(fair_state::text)<=262144 AND (kind='client' OR fair_state='{}'::jsonb));
INSERT INTO overlaid_migrations(name) VALUES('0025_moderation_permit_turns.sql') ON CONFLICT DO NOTHING;
COMMIT;
