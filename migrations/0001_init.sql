CREATE TABLE IF NOT EXISTS app_metadata (
  key text PRIMARY KEY,
  value text NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS stream_events (
  id text PRIMARY KEY,
  type text NOT NULL,
  source text NOT NULL,
  title text NOT NULL DEFAULT '',
  message text NOT NULL DEFAULT '',
  severity text NOT NULL DEFAULT '',
  data jsonb,
  created_at timestamptz NOT NULL,
  inserted_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS stream_events_created_at_idx ON stream_events (created_at DESC, inserted_at DESC);
CREATE INDEX IF NOT EXISTS stream_events_type_idx ON stream_events (type);
CREATE INDEX IF NOT EXISTS stream_events_source_idx ON stream_events (source);

CREATE TABLE IF NOT EXISTS twitch_interaction_rules (
  id text PRIMARY KEY,
  rules jsonb NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS dashboard_configs (
  id text PRIMARY KEY,
  config jsonb NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS stream_sessions (
  id text PRIMARY KEY DEFAULT encode(gen_random_bytes(16), 'hex'),
  title text NOT NULL,
  kind text NOT NULL DEFAULT 'rehearsal',
  notes text NOT NULL DEFAULT '',
  started_at timestamptz NOT NULL DEFAULT now(),
  ended_at timestamptz
);

ALTER TABLE stream_events ADD COLUMN IF NOT EXISTS session_id text REFERENCES stream_sessions(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS stream_sessions_started_at_idx ON stream_sessions (started_at DESC);
CREATE INDEX IF NOT EXISTS stream_events_session_id_idx ON stream_events (session_id);

CREATE TABLE IF NOT EXISTS stream_session_readiness_snapshots (
  id text PRIMARY KEY DEFAULT encode(gen_random_bytes(16), 'hex'),
  session_id text NOT NULL REFERENCES stream_sessions(id) ON DELETE CASCADE,
  phase text NOT NULL,
  status text NOT NULL,
  checks jsonb NOT NULL,
  captured_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS stream_session_readiness_snapshots_session_idx ON stream_session_readiness_snapshots (session_id, captured_at ASC);

CREATE TABLE IF NOT EXISTS bot_audit_events (
  id text PRIMARY KEY DEFAULT encode(gen_random_bytes(16), 'hex'),
  created_at timestamptz NOT NULL DEFAULT now(),
  source text NOT NULL,
  actor_id text NOT NULL DEFAULT '',
  actor_login text NOT NULL DEFAULT '',
  actor_display_name text NOT NULL DEFAULT '',
  actor_role text NOT NULL DEFAULT 'viewer',
  trigger text NOT NULL DEFAULT '',
  command text NOT NULL DEFAULT '',
  action text NOT NULL DEFAULT '',
  result text NOT NULL,
  reason text NOT NULL DEFAULT '',
  event_id text REFERENCES stream_events(id) ON DELETE SET NULL,
  stream_session_id text REFERENCES stream_sessions(id) ON DELETE SET NULL,
  data jsonb NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS bot_audit_events_created_at_idx ON bot_audit_events (created_at DESC);
CREATE INDEX IF NOT EXISTS bot_audit_events_source_idx ON bot_audit_events (source);
CREATE INDEX IF NOT EXISTS bot_audit_events_command_idx ON bot_audit_events (command);
CREATE INDEX IF NOT EXISTS bot_audit_events_session_idx ON bot_audit_events (stream_session_id);

CREATE TABLE IF NOT EXISTS bot_commands (
  id text PRIMARY KEY DEFAULT encode(gen_random_bytes(16), 'hex'),
  name text NOT NULL UNIQUE,
  enabled boolean NOT NULL DEFAULT true,
  response text NOT NULL DEFAULT '',
  action text NOT NULL DEFAULT 'response',
  response_mode text NOT NULL DEFAULT 'overlay',
  role_required text NOT NULL DEFAULT 'viewer',
  cooldown_seconds integer NOT NULL DEFAULT 10,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE bot_commands ALTER COLUMN action SET DEFAULT 'ticker';
ALTER TABLE bot_commands ADD COLUMN IF NOT EXISTS response_mode text NOT NULL DEFAULT 'overlay';

CREATE TABLE IF NOT EXISTS bot_timers (
  id text PRIMARY KEY DEFAULT encode(gen_random_bytes(16), 'hex'),
  name text NOT NULL UNIQUE,
  enabled boolean NOT NULL DEFAULT true,
  interval_seconds integer NOT NULL DEFAULT 300,
  response text NOT NULL DEFAULT '',
  action text NOT NULL DEFAULT 'ticker',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  last_run_at timestamptz
);

CREATE TABLE IF NOT EXISTS bot_counters (
  id text PRIMARY KEY DEFAULT encode(gen_random_bytes(16), 'hex'),
  name text NOT NULL UNIQUE,
  value integer NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS bot_quotes (
  id text PRIMARY KEY DEFAULT encode(gen_random_bytes(16), 'hex'),
  text text NOT NULL,
  author text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS bot_quotes_created_at_idx ON bot_quotes (created_at DESC);

CREATE TABLE IF NOT EXISTS bot_moderation_rules (
  id text PRIMARY KEY DEFAULT encode(gen_random_bytes(16), 'hex'),
  name text NOT NULL UNIQUE,
  enabled boolean NOT NULL DEFAULT true,
  pattern text NOT NULL,
  match_target text NOT NULL DEFAULT 'message',
  case_sensitive boolean NOT NULL DEFAULT false,
  regex boolean NOT NULL DEFAULT false,
  action text NOT NULL DEFAULT 'flag',
  reason text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS bot_moderation_rules_enabled_idx ON bot_moderation_rules (enabled);
