BEGIN;
-- Hash-only bounded fair admission state shares the existing restricted account
-- row and admission lock. Existing budgets, fences and role grants are retained.
ALTER TABLE chat_pacing_accounts ADD COLUMN fair_state jsonb NOT NULL DEFAULT '{}';
ALTER TABLE chat_pacing_accounts ADD CONSTRAINT chat_pacing_fair_state_bound
 CHECK(jsonb_typeof(fair_state)='object' AND octet_length(fair_state::text)<=131072);
INSERT INTO overlaid_migrations(name) VALUES('0024_chat_permit_turns.sql') ON CONFLICT DO NOTHING;
COMMIT;
