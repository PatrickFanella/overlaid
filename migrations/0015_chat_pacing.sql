BEGIN;
DO $setup$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='overlaid_pacer') THEN
  CREATE ROLE overlaid_pacer NOLOGIN NOSUPERUSER NOBYPASSRLS;
 END IF;
 EXECUTE format('GRANT USAGE ON SCHEMA %I TO overlaid_pacer',current_schema());
END $setup$;
CREATE TABLE chat_pacing_accounts (
 account_key text PRIMARY KEY CHECK(account_key ~ '^[0-9a-f]{64}$'),
 attempts jsonb NOT NULL DEFAULT '[]' CHECK(jsonb_typeof(attempts)='array' AND jsonb_array_length(attempts)<=20),
 blocked_until timestamptz NOT NULL DEFAULT '-infinity',
 lease_hash text CHECK(lease_hash ~ '^[0-9a-f]{64}$'), lease_until timestamptz,
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK((lease_hash IS NULL)=(lease_until IS NULL))
);
CREATE TABLE chat_pacing_channels (
 account_key text NOT NULL REFERENCES chat_pacing_accounts(account_key) ON DELETE CASCADE,
 channel_key text NOT NULL CHECK(channel_key ~ '^[0-9a-f]{64}$'),
 next_at timestamptz NOT NULL DEFAULT '-infinity', updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(account_key,channel_key)
);
CREATE INDEX chat_pacing_account_expiry ON chat_pacing_accounts(updated_at);
CREATE INDEX chat_pacing_channel_expiry ON chat_pacing_channels(updated_at);
ALTER TABLE chat_pacing_accounts ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_pacing_accounts FORCE ROW LEVEL SECURITY;
CREATE POLICY pacer_only ON chat_pacing_accounts TO overlaid_pacer USING(true) WITH CHECK(true);
ALTER TABLE chat_pacing_channels ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_pacing_channels FORCE ROW LEVEL SECURITY;
CREATE POLICY pacer_only ON chat_pacing_channels TO overlaid_pacer USING(true) WITH CHECK(true);
REVOKE ALL ON chat_pacing_accounts,chat_pacing_channels FROM PUBLIC,overlaid_runtime,overlaid_worker,overlaid_bot_admin;
GRANT SELECT,INSERT,UPDATE,DELETE ON chat_pacing_accounts,chat_pacing_channels TO overlaid_pacer;
INSERT INTO overlaid_migrations(name) VALUES('0015_chat_pacing.sql') ON CONFLICT DO NOTHING;
COMMIT;
