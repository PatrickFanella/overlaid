package history

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type PostgresStore struct {
	db              *sql.DB
	limit           int
	mu              sync.RWMutex
	activeSessionID string
}

func NewPostgres(databaseURL string, limit int) (*PostgresStore, error) {
	if limit <= 0 {
		limit = defaultLimit
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	store := &PostgresStore{db: db, limit: limit}
	if err := store.Load(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *PostgresStore) Load() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.db.PingContext(ctx); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, createStreamEventsTableSQL)
	return err
}

func (s *PostgresStore) Append(event events.Event) error {
	sanitized := Sanitize(event)
	sessionID := s.ActiveSessionID()
	if sessionID != "" {
		if sanitized.Data == nil {
			sanitized.Data = map[string]any{}
		}
		sanitized.Data["sessionId"] = sessionID
	}
	payload, err := json.Marshal(sanitized.Data)
	if err != nil {
		return err
	}
	if len(sanitized.Data) == 0 {
		payload = nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO stream_events (id, type, source, title, message, severity, data, created_at, session_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULLIF($9, ''))
		ON CONFLICT (id) DO NOTHING
	`, sanitized.ID, string(sanitized.Type), sanitized.Source, sanitized.Title, sanitized.Message, string(sanitized.Severity), payload, sanitized.CreatedAt, sessionID)
	return err
}

func (s *PostgresStore) SetActiveSessionID(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeSessionID = strings.TrimSpace(sessionID)
}

func (s *PostgresStore) ActiveSessionID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeSessionID
}

func (s *PostgresStore) List(limit int) []events.Event {
	if limit <= 0 || limit > s.limit {
		limit = s.limit
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, type, source, title, message, severity, data, created_at
		FROM stream_events
		ORDER BY created_at DESC, inserted_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return []events.Event{}
	}
	defer rows.Close()

	items := []events.Event{}
	for rows.Next() {
		var event events.Event
		var eventType, severity string
		var data []byte
		if err := rows.Scan(&event.ID, &eventType, &event.Source, &event.Title, &event.Message, &severity, &data, &event.CreatedAt); err != nil {
			return []events.Event{}
		}
		event.Type = events.EventType(eventType)
		event.Severity = events.Severity(severity)
		if len(data) > 0 {
			_ = json.Unmarshal(data, &event.Data)
		}
		items = append(items, event)
	}
	return items
}

func (s *PostgresStore) Close() error {
	return s.db.Close()
}

const createStreamEventsTableSQL = `
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

ALTER TABLE stream_events ADD COLUMN IF NOT EXISTS session_id text;

CREATE INDEX IF NOT EXISTS stream_events_created_at_idx ON stream_events (created_at DESC, inserted_at DESC);
CREATE INDEX IF NOT EXISTS stream_events_type_idx ON stream_events (type);
CREATE INDEX IF NOT EXISTS stream_events_source_idx ON stream_events (source);
CREATE INDEX IF NOT EXISTS stream_events_session_id_idx ON stream_events (session_id);
`
