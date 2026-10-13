package bot

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type APIVariable struct {
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	Enabled          bool       `json:"enabled"`
	URL              string     `json:"url"`
	ResponseSelector string     `json:"responseSelector,omitempty"`
	Fallback         string     `json:"fallback,omitempty"`
	CacheTTLSeconds  int        `json:"cacheTtlSeconds"`
	TimeoutMs        int        `json:"timeoutMs"`
	LastStatus       string     `json:"lastStatus,omitempty"`
	LastError        string     `json:"lastError,omitempty"`
	LastCheckedAt    *time.Time `json:"lastCheckedAt,omitempty"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
}

type APIVariableResult struct {
	Name       string `json:"name"`
	Value      string `json:"value"`
	Status     string `json:"status"`
	HTTPStatus int    `json:"httpStatus,omitempty"`
	Error      string `json:"error,omitempty"`
}

type apiVariableScanner interface{ Scan(dest ...any) error }

func (s *Store) ListAPIVariables(ctx context.Context) ([]APIVariable, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, enabled, url, response_selector, fallback, cache_ttl_seconds, timeout_ms, last_status, last_error, last_checked_at, created_at, updated_at FROM bot_api_variables WHERE /*tenant:bot_api_variables*/ ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []APIVariable{}
	for rows.Next() {
		item, err := scanAPIVariable(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) SaveAPIVariable(ctx context.Context, variable APIVariable) (APIVariable, error) {
	variable, err := NormalizeAPIVariable(variable)
	if err != nil {
		return APIVariable{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if strings.TrimSpace(variable.ID) == "" {
		return scanAPIVariable(s.db.QueryRowContext(ctx, `INSERT INTO bot_api_variables (name, enabled, url, response_selector, fallback, cache_ttl_seconds, timeout_ms) VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id, name, enabled, url, response_selector, fallback, cache_ttl_seconds, timeout_ms, last_status, last_error, last_checked_at, created_at, updated_at`, variable.Name, variable.Enabled, variable.URL, variable.ResponseSelector, variable.Fallback, variable.CacheTTLSeconds, variable.TimeoutMs))
	}
	return scanAPIVariable(s.db.QueryRowContext(ctx, `UPDATE bot_api_variables SET name=$2, enabled=$3, url=$4, response_selector=$5, fallback=$6, cache_ttl_seconds=$7, timeout_ms=$8, last_status='', last_error='', last_checked_at=NULL, updated_at=now() WHERE /*tenant:bot_api_variables*/ AND id=$1 RETURNING id, name, enabled, url, response_selector, fallback, cache_ttl_seconds, timeout_ms, last_status, last_error, last_checked_at, created_at, updated_at`, variable.ID, variable.Name, variable.Enabled, variable.URL, variable.ResponseSelector, variable.Fallback, variable.CacheTTLSeconds, variable.TimeoutMs))
}

func (s *Store) DeleteAPIVariable(ctx context.Context, id string) error {
	return s.deleteByID(ctx, "bot_api_variables", id, "api variable id is required")
}

func scanAPIVariable(row apiVariableScanner) (APIVariable, error) {
	var item APIVariable
	var lastChecked sql.NullTime
	if err := row.Scan(&item.ID, &item.Name, &item.Enabled, &item.URL, &item.ResponseSelector, &item.Fallback, &item.CacheTTLSeconds, &item.TimeoutMs, &item.LastStatus, &item.LastError, &lastChecked, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return APIVariable{}, err
	}
	if lastChecked.Valid {
		item.LastCheckedAt = &lastChecked.Time
	}
	return item, nil
}

func NormalizeAPIVariable(variable APIVariable) (APIVariable, error) {
	variable.ID = strings.TrimSpace(variable.ID)
	variable.Name = strings.ToLower(strings.TrimSpace(variable.Name))
	variable.URL = strings.TrimSpace(variable.URL)
	variable.ResponseSelector = strings.TrimSpace(variable.ResponseSelector)
	variable.Fallback = capRunes(strings.TrimSpace(variable.Fallback), 200)
	if variable.Name == "" {
		return APIVariable{}, errors.New("api variable name is required")
	}
	if len(variable.Name) > 32 || !isSafeBotName(variable.Name) {
		return APIVariable{}, errors.New("api variable name must use lowercase letters, numbers, dash, or underscore")
	}
	if err := validateAPIVariableURL(variable.URL); err != nil {
		return APIVariable{}, err
	}
	if variable.CacheTTLSeconds == 0 {
		variable.CacheTTLSeconds = 60
	}
	if variable.CacheTTLSeconds < 15 || variable.CacheTTLSeconds > 3600 {
		return APIVariable{}, errors.New("api variable cache ttl must be between 15 and 3600 seconds")
	}
	if variable.TimeoutMs == 0 {
		variable.TimeoutMs = 1500
	}
	if variable.TimeoutMs < 250 || variable.TimeoutMs > 3000 {
		return APIVariable{}, errors.New("api variable timeout must be between 250 and 3000 ms")
	}
	return variable, nil
}

// Diagnostics keep the configuration revision unchanged. A fetch from an older
// revision cannot overwrite diagnostics after an operator edits the variable.
func (s *Store) updateAPIVariableStatus(ctx context.Context, variable APIVariable, status, detail string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `UPDATE bot_api_variables SET last_status=$2, last_error=$3, last_checked_at=now() WHERE /*tenant:bot_api_variables*/ AND id=$1 AND updated_at=$4`, variable.ID, status, capRunes(detail, 180), variable.UpdatedAt)
	return err
}

// Lookup and diagnostics are storage adapters; render policy belongs to the resolver.
func (s *Store) lookupAPIVariable(ctx context.Context, name string) (APIVariable, error) {
	variable, err := scanAPIVariable(s.db.QueryRowContext(ctx, `SELECT id, name, enabled, url, response_selector, fallback, cache_ttl_seconds, timeout_ms, last_status, last_error, last_checked_at, created_at, updated_at FROM bot_api_variables WHERE /*tenant:bot_api_variables*/ AND name=$1`, name))
	if errors.Is(err, sql.ErrNoRows) {
		return APIVariable{}, errAPIVariableMissing
	}
	return variable, err
}
