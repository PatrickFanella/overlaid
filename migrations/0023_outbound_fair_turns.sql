BEGIN;
-- Monotonic lease turns, not effect identities. Gaps on rollback are harmless.
CREATE SEQUENCE overlaid_queue_turn_seq AS bigint MINVALUE 1 MAXVALUE 999999999999999999 CACHE 1 NO CYCLE;
REVOKE ALL ON SEQUENCE overlaid_queue_turn_seq FROM PUBLIC;
GRANT USAGE,SELECT ON SEQUENCE overlaid_queue_turn_seq TO overlaid_outbound_worker,overlaid_moderation_worker;
-- Match the lease selector's bounded numeric projection. Arbitrary metadata
-- cannot cause cast overflow or become a tenant cursor.
CREATE INDEX overlaid_outbound_tenant_turn ON river_job(kind,queue,((args->>'tenant_id')::uuid),
 (CASE WHEN jsonb_typeof(metadata->'overlaid_fair_turn')='number' AND metadata->>'overlaid_fair_turn' ~ '^[0-9]{1,18}$' THEN (metadata->>'overlaid_fair_turn')::bigint ELSE 0 END) DESC)
 WHERE kind IN('overlaid_chat_send','overlaid_moderation');
INSERT INTO overlaid_migrations(name) VALUES('0023_outbound_fair_turns.sql') ON CONFLICT DO NOTHING;
COMMIT;
