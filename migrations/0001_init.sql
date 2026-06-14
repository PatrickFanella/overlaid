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
