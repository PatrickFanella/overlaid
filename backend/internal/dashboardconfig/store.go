package dashboardconfig

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const defaultID = "default"

type Store struct {
	db *sql.DB
}

type Document struct {
	Config    json.RawMessage `json:"config"`
	UpdatedAt time.Time       `json:"updatedAt"`
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

func (s *Store) Get(ctx context.Context) (Document, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var doc Document
	err := s.db.QueryRowContext(ctx, `SELECT config, updated_at FROM dashboard_configs WHERE id = $1`, defaultID).Scan(&doc.Config, &doc.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Document{}, false, nil
		}
		return Document{}, false, err
	}
	return doc, true, nil
}

func (s *Store) Replace(ctx context.Context, config json.RawMessage) (Document, error) {
	if err := Validate(config); err != nil {
		return Document{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var doc Document
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO dashboard_configs (id, config, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT (id) DO UPDATE SET config = EXCLUDED.config, updated_at = now()
		RETURNING config, updated_at
	`, defaultID, config).Scan(&doc.Config, &doc.UpdatedAt)
	return doc, err
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func Validate(payload json.RawMessage) error {
	var decoded any
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	if !isObject(decoded) {
		return errors.New("dashboard config must be a JSON object")
	}
	if containsSecretKey(decoded) {
		return errors.New("dashboard config must not contain secrets")
	}
	return nil
}

func isObject(value any) bool {
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

const createTableSQL = `
CREATE TABLE IF NOT EXISTS dashboard_configs (
  id text PRIMARY KEY,
  config jsonb NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now()
);
`
