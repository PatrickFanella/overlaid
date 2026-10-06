package sessions

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type Session struct {
	ID         string     `json:"id"`
	Title      string     `json:"title"`
	Kind       string     `json:"kind"`
	Notes      string     `json:"notes,omitempty"`
	StartedAt  time.Time  `json:"startedAt"`
	EndedAt    *time.Time `json:"endedAt,omitempty"`
	EventCount int        `json:"eventCount"`
}

type Report struct {
	Session   Session             `json:"session"`
	Events    []events.Event      `json:"events"`
	Snapshots []ReadinessSnapshot `json:"snapshots"`
}

type ReadinessSnapshot struct {
	ID         string          `json:"id"`
	SessionID  string          `json:"sessionId"`
	Phase      string          `json:"phase"`
	Status     string          `json:"status"`
	Checks     json.RawMessage `json:"checks"`
	CapturedAt time.Time       `json:"capturedAt"`
}

type Store struct {
	db *sql.DB
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
	_, err := s.db.ExecContext(ctx, createTableSQL)
	return err
}

func (s *Store) Current(ctx context.Context) (Session, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, sessionSelectSQL+` WHERE ended_at IS NULL GROUP BY stream_sessions.id ORDER BY started_at DESC LIMIT 1`)
	if err != nil {
		return Session{}, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return Session{}, false, rows.Err()
	}
	session, err := scanSession(rows)
	return session, err == nil, err
}

func (s *Store) Recent(ctx context.Context, limit int) ([]Session, error) {
	if limit <= 0 || limit > 25 {
		limit = 8
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, sessionSelectSQL+` GROUP BY stream_sessions.id ORDER BY started_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Session{}
	for rows.Next() {
		item, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) Start(ctx context.Context, title, kind, notes string) (Session, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "Stream session"
	}
	kind = strings.TrimSpace(strings.ToLower(kind))
	if kind != "live" && kind != "rehearsal" {
		kind = "rehearsal"
	}
	notes = strings.TrimSpace(notes)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Session{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, lifecycleLockSQL); err != nil {
		return Session{}, err
	}
	// Capture time after acquiring the lock; transaction start may precede a
	// session created by another writer while this transaction was waiting.
	var startedAt time.Time
	if err := tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&startedAt); err != nil {
		return Session{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE stream_sessions SET ended_at = $1 WHERE ended_at IS NULL`, startedAt); err != nil {
		return Session{}, err
	}
	var id string
	err = tx.QueryRowContext(ctx, `INSERT INTO stream_sessions (title, kind, notes, started_at) VALUES ($1, $2, $3, $4) RETURNING id`, title, kind, notes, startedAt).Scan(&id)
	if err != nil {
		return Session{}, err
	}
	if err := tx.Commit(); err != nil {
		return Session{}, err
	}
	return s.ByID(context.Background(), id)
}

func (s *Store) End(ctx context.Context) (Session, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Session{}, false, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, lifecycleLockSQL); err != nil {
		return Session{}, false, err
	}
	var id string
	err = tx.QueryRowContext(ctx, `
		UPDATE stream_sessions SET ended_at = clock_timestamp() WHERE id = (
			SELECT id FROM stream_sessions WHERE ended_at IS NULL ORDER BY started_at DESC LIMIT 1
		) RETURNING id
	`).Scan(&id)
	if err != nil {
		if err == sql.ErrNoRows {
			return Session{}, false, nil
		}
		return Session{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Session{}, false, err
	}
	session, err := s.ByID(context.Background(), id)
	return session, err == nil, err
}

func (s *Store) ByID(ctx context.Context, id string) (Session, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, sessionSelectSQL+` WHERE stream_sessions.id = $1 GROUP BY stream_sessions.id`, id)
	if err != nil {
		return Session{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return Session{}, sql.ErrNoRows
	}
	return scanSession(rows)
}

func (s *Store) Report(ctx context.Context, id string, limit int) (Report, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Report{}, sql.ErrNoRows
	}
	if limit <= 0 || limit > 250 {
		limit = 100
	}
	session, err := s.ByID(ctx, id)
	if err != nil {
		return Report{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, type, source, title, message, severity, data, created_at
		FROM stream_events
		WHERE session_id = $1
		ORDER BY created_at DESC, inserted_at DESC
		LIMIT $2
	`, id, limit)
	if err != nil {
		return Report{}, err
	}
	defer rows.Close()
	items := []events.Event{}
	for rows.Next() {
		item, err := scanEvent(rows)
		if err != nil {
			return Report{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return Report{}, err
	}
	snapshots, err := s.ReadinessSnapshots(ctx, id)
	if err != nil {
		return Report{}, err
	}
	return Report{Session: session, Events: items, Snapshots: snapshots}, nil
}

func (s *Store) AddReadinessSnapshot(ctx context.Context, sessionID, phase, status string, checks any) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return sql.ErrNoRows
	}
	phase = strings.TrimSpace(strings.ToLower(phase))
	if phase == "" {
		phase = "manual"
	}
	status = strings.TrimSpace(strings.ToLower(status))
	if status == "" {
		status = "unknown"
	}
	payload, err := json.Marshal(checks)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO stream_session_readiness_snapshots (session_id, phase, status, checks)
		VALUES ($1, $2, $3, $4)
	`, sessionID, phase, status, payload)
	return err
}

func (s *Store) ReadinessSnapshots(ctx context.Context, sessionID string) ([]ReadinessSnapshot, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return []ReadinessSnapshot{}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, session_id, phase, status, checks, captured_at
		FROM stream_session_readiness_snapshots
		WHERE session_id = $1
		ORDER BY captured_at ASC
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ReadinessSnapshot{}
	for rows.Next() {
		var item ReadinessSnapshot
		if err := rows.Scan(&item.ID, &item.SessionID, &item.Phase, &item.Status, &item.Checks, &item.CapturedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

type rowScanner interface{ Scan(dest ...any) error }

func scanSession(row rowScanner) (Session, error) {
	var session Session
	var endedAt sql.NullTime
	if err := row.Scan(&session.ID, &session.Title, &session.Kind, &session.Notes, &session.StartedAt, &endedAt, &session.EventCount); err != nil {
		return Session{}, err
	}
	if endedAt.Valid {
		t := endedAt.Time
		session.EndedAt = &t
	}
	return session, nil
}

func scanEvent(row rowScanner) (events.Event, error) {
	var event events.Event
	var eventType, severity string
	var data []byte
	if err := row.Scan(&event.ID, &eventType, &event.Source, &event.Title, &event.Message, &severity, &data, &event.CreatedAt); err != nil {
		return events.Event{}, err
	}
	event.Type = events.EventType(eventType)
	event.Severity = events.Severity(severity)
	if len(data) > 0 {
		_ = json.Unmarshal(data, &event.Data)
	}
	return event, nil
}

// Table identity scopes the transaction lock to this database/schema. All
// lifecycle writers share it, including separate API processes.
const lifecycleLockSQL = `SELECT pg_advisory_xact_lock('stream_sessions'::regclass::oid::bigint)`

const sessionSelectSQL = `
SELECT stream_sessions.id, stream_sessions.title, stream_sessions.kind, stream_sessions.notes, stream_sessions.started_at, stream_sessions.ended_at, COUNT(stream_events.id)::int AS event_count
FROM stream_sessions
LEFT JOIN stream_events ON stream_events.session_id = stream_sessions.id
`

const createTableSQL = `
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
`
