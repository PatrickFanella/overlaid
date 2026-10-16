BEGIN;
-- Additive receipt vocabulary only. No existing rows, claims or permissions change.
ALTER TABLE platform_outbound_actions DROP CONSTRAINT platform_outbound_actions_failure_check;
ALTER TABLE platform_outbound_actions ADD CONSTRAINT platform_outbound_actions_failure_check
 CHECK(failure IN ('','retryable','unauthorized','unsupported','quota_limited','permanent','plan_limited'));
INSERT INTO overlaid_migrations(name) VALUES('0027_outbound_plan_limit.sql') ON CONFLICT DO NOTHING;
COMMIT;
