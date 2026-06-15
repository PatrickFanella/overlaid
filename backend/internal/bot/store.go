package bot

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type Store struct {
	db *sql.DB
}

type AuditEvent struct {
	ID               string          `json:"id"`
	CreatedAt        time.Time       `json:"createdAt"`
	Source           string          `json:"source"`
	ActorID          string          `json:"actorId,omitempty"`
	ActorLogin       string          `json:"actorLogin,omitempty"`
	ActorDisplayName string          `json:"actorDisplayName,omitempty"`
	ActorRole        string          `json:"actorRole,omitempty"`
	Trigger          string          `json:"trigger,omitempty"`
	Command          string          `json:"command,omitempty"`
	Action           string          `json:"action,omitempty"`
	Result           string          `json:"result"`
	Reason           string          `json:"reason,omitempty"`
	EventID          string          `json:"eventId,omitempty"`
	StreamSessionID  string          `json:"streamSessionId,omitempty"`
	Data             json.RawMessage `json:"data,omitempty"`
}

type Timer struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Enabled         bool       `json:"enabled"`
	IntervalSeconds int        `json:"intervalSeconds"`
	Response        string     `json:"response"`
	Action          string     `json:"action"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
	LastRunAt       *time.Time `json:"lastRunAt,omitempty"`
}

func NewPostgres(databaseURL string) (*Store, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	store := &Store{db: db}
	if err := store.Load(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Load() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.db.PingContext(ctx); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, createTablesSQL)
	return err
}

func (s *Store) RecordAudit(ctx context.Context, event AuditEvent) (AuditEvent, error) {
	event = normalizeAuditEvent(event)
	if err := ValidateAuditData(event.Data); err != nil {
		return AuditEvent{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO bot_audit_events (
			source, actor_id, actor_login, actor_display_name, actor_role,
			trigger, command, action, result, reason, event_id, stream_session_id, data
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULLIF($11,''),NULLIF($12,''),$13)
		RETURNING id, created_at
	`, event.Source, event.ActorID, event.ActorLogin, event.ActorDisplayName, event.ActorRole, event.Trigger, event.Command, event.Action, event.Result, event.Reason, event.EventID, event.StreamSessionID, nullableJSON(event.Data)).Scan(&event.ID, &event.CreatedAt)
	return event, err
}

func (s *Store) RecentAudit(ctx context.Context, limit int) ([]AuditEvent, error) {
	if limit <= 0 || limit > 250 {
		limit = 50
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, created_at, source, actor_id, actor_login, actor_display_name, actor_role,
			trigger, command, action, result, reason, COALESCE(event_id, ''), COALESCE(stream_session_id, ''), COALESCE(data, '{}'::jsonb)
		FROM bot_audit_events
		ORDER BY created_at DESC, id DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []AuditEvent{}
	for rows.Next() {
		var item AuditEvent
		if err := rows.Scan(&item.ID, &item.CreatedAt, &item.Source, &item.ActorID, &item.ActorLogin, &item.ActorDisplayName, &item.ActorRole, &item.Trigger, &item.Command, &item.Action, &item.Result, &item.Reason, &item.EventID, &item.StreamSessionID, &item.Data); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) Counts(ctx context.Context) (commandCount, timerCount, auditCount int, err error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err = s.db.QueryRowContext(ctx, `
		SELECT
			(SELECT count(*) FROM bot_commands),
			(SELECT count(*) FROM bot_timers),
			(SELECT count(*) FROM bot_audit_events)
	`).Scan(&commandCount, &timerCount, &auditCount)
	return commandCount, timerCount, auditCount, err
}

func (s *Store) ListTimers(ctx context.Context) ([]Timer, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, enabled, interval_seconds, response, action, created_at, updated_at, last_run_at
		FROM bot_timers
		ORDER BY name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	timers := []Timer{}
	for rows.Next() {
		timer, err := scanTimer(rows)
		if err != nil {
			return nil, err
		}
		timers = append(timers, timer)
	}
	return timers, rows.Err()
}

func (s *Store) SaveTimer(ctx context.Context, timer Timer) (Timer, error) {
	timer, err := NormalizeTimer(timer)
	if err != nil {
		return Timer{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if strings.TrimSpace(timer.ID) == "" {
		return s.insertTimer(ctx, timer)
	}
	return s.updateTimer(ctx, timer)
}

func (s *Store) SetTimerEnabled(ctx context.Context, id string, enabled bool) (Timer, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Timer{}, errors.New("timer id is required")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	row := s.db.QueryRowContext(ctx, `
		UPDATE bot_timers SET enabled = $2, updated_at = now()
		WHERE id = $1
		RETURNING id, name, enabled, interval_seconds, response, action, created_at, updated_at, last_run_at
	`, id, enabled)
	return scanTimer(row)
}

func (s *Store) DeleteTimer(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("timer id is required")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result, err := s.db.ExecContext(ctx, `DELETE FROM bot_timers WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err == nil && affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) insertTimer(ctx context.Context, timer Timer) (Timer, error) {
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO bot_timers (name, enabled, interval_seconds, response, action)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, name, enabled, interval_seconds, response, action, created_at, updated_at, last_run_at
	`, timer.Name, timer.Enabled, timer.IntervalSeconds, timer.Response, timer.Action)
	return scanTimer(row)
}

func (s *Store) updateTimer(ctx context.Context, timer Timer) (Timer, error) {
	row := s.db.QueryRowContext(ctx, `
		UPDATE bot_timers
		SET name = $2, enabled = $3, interval_seconds = $4, response = $5, action = $6, updated_at = now()
		WHERE id = $1
		RETURNING id, name, enabled, interval_seconds, response, action, created_at, updated_at, last_run_at
	`, strings.TrimSpace(timer.ID), timer.Name, timer.Enabled, timer.IntervalSeconds, timer.Response, timer.Action)
	return scanTimer(row)
}

type timerScanner interface {
	Scan(dest ...any) error
}

func scanTimer(row timerScanner) (Timer, error) {
	var timer Timer
	var lastRunAt sql.NullTime
	if err := row.Scan(&timer.ID, &timer.Name, &timer.Enabled, &timer.IntervalSeconds, &timer.Response, &timer.Action, &timer.CreatedAt, &timer.UpdatedAt, &lastRunAt); err != nil {
		return Timer{}, err
	}
	if lastRunAt.Valid {
		timer.LastRunAt = &lastRunAt.Time
	}
	return timer, nil
}

func NormalizeTimer(timer Timer) (Timer, error) {
	timer.ID = strings.TrimSpace(timer.ID)
	timer.Name = strings.ToLower(strings.TrimSpace(timer.Name))
	timer.Response = strings.TrimSpace(timer.Response)
	timer.Action = normalizeToken(timer.Action, "ticker")
	if timer.Name == "" {
		return Timer{}, errors.New("timer name is required")
	}
	if !isSafeTimerName(timer.Name) {
		return Timer{}, errors.New("timer name must use lowercase letters, numbers, dash, or underscore")
	}
	if timer.IntervalSeconds < 30 || timer.IntervalSeconds > 86400 {
		return Timer{}, errors.New("timer interval must be between 30 and 86400 seconds")
	}
	if timer.Response == "" {
		return Timer{}, errors.New("timer response is required")
	}
	if len(timer.Response) > 500 {
		return Timer{}, errors.New("timer response must be 500 characters or less")
	}
	switch timer.Action {
	case "ticker", "alert":
	default:
		return Timer{}, fmt.Errorf("unsupported timer action %q", timer.Action)
	}
	return timer, nil
}

func isSafeTimerName(name string) bool {
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func normalizeAuditEvent(event AuditEvent) AuditEvent {
	event.Source = normalizeToken(event.Source, "unknown")
	event.ActorID = strings.TrimSpace(event.ActorID)
	event.ActorLogin = strings.TrimSpace(event.ActorLogin)
	event.ActorDisplayName = strings.TrimSpace(event.ActorDisplayName)
	event.ActorRole = normalizeToken(event.ActorRole, "viewer")
	event.Trigger = strings.TrimSpace(event.Trigger)
	event.Command = strings.TrimSpace(strings.TrimPrefix(event.Command, "!"))
	event.Action = normalizeToken(event.Action, "none")
	event.Result = normalizeToken(event.Result, "unknown")
	event.Reason = strings.TrimSpace(event.Reason)
	event.EventID = strings.TrimSpace(event.EventID)
	event.StreamSessionID = strings.TrimSpace(event.StreamSessionID)
	if len(event.Data) == 0 {
		event.Data = json.RawMessage(`{}`)
	}
	return event
}

func normalizeToken(value, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return fallback
	}
	return value
}

func nullableJSON(payload json.RawMessage) any {
	if len(payload) == 0 || string(payload) == "null" {
		return json.RawMessage(`{}`)
	}
	return payload
}

func ValidateAuditData(payload json.RawMessage) error {
	if len(payload) == 0 || string(payload) == "null" {
		return nil
	}
	var decoded any
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	if !isJSONObject(decoded) {
		return errors.New("bot audit data must be a JSON object")
	}
	if containsSecretKey(decoded) {
		return errors.New("bot audit data must not contain secrets")
	}
	return nil
}

func isJSONObject(value any) bool {
	_, ok := value.(map[string]any)
	return ok
}

func containsSecretKey(value any) bool {
	switch v := value.(type) {
	case map[string]any:
		for key, nested := range v {
			if isForbiddenKey(key) || containsSecretKey(nested) {
				return true
			}
		}
	case []any:
		for _, nested := range v {
			if containsSecretKey(nested) {
				return true
			}
		}
	}
	return false
}

func isForbiddenKey(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	if lower == "" {
		return false
	}
	for _, needle := range []string{"secret", "token", "password", "authorization", "cookie"} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

const createTablesSQL = `
CREATE EXTENSION IF NOT EXISTS pgcrypto;

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
  role_required text NOT NULL DEFAULT 'viewer',
  cooldown_seconds integer NOT NULL DEFAULT 10,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

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
`
