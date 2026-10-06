package bot

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type Store struct {
	db           *sql.DB
	apiVariables *APIVariableResolver
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

type Command struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Enabled         bool      `json:"enabled"`
	Response        string    `json:"response"`
	Action          string    `json:"action"`
	ResponseMode    string    `json:"responseMode"`
	RoleRequired    string    `json:"roleRequired"`
	CooldownSeconds int       `json:"cooldownSeconds"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type Counter struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Value     int       `json:"value"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Quote struct {
	ID        string    `json:"id"`
	Text      string    `json:"text"`
	Author    string    `json:"author,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ModerationRule struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Enabled       bool       `json:"enabled"`
	Enforce       bool       `json:"enforce"`
	Pattern       string     `json:"pattern"`
	MatchTarget   string     `json:"matchTarget"`
	CaseSensitive bool       `json:"caseSensitive"`
	Regex         bool       `json:"regex"`
	Action        string     `json:"action"`
	Reason        string     `json:"reason,omitempty"`
	Source        string     `json:"source,omitempty"`
	ExpiresAt     *time.Time `json:"expiresAt,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

type ModerationMatch struct {
	RuleID      string `json:"ruleId"`
	RuleName    string `json:"ruleName"`
	Pattern     string `json:"pattern"`
	MatchTarget string `json:"matchTarget"`
	Action      string `json:"action"`
	Reason      string `json:"reason,omitempty"`
}

func NewPostgres(databaseURL string) (*Store, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	store := &Store{db: db}
	store.apiVariables = newAPIVariableResolver(store, fetchAPIVariable, time.Now)
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
		if err := rows.Scan(auditDestinations(&item)...); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func auditDestinations(item *AuditEvent) []any {
	return []any{&item.ID, &item.CreatedAt, &item.Source, &item.ActorID, &item.ActorLogin, &item.ActorDisplayName, &item.ActorRole, &item.Trigger, &item.Command, &item.Action, &item.Result, &item.Reason, &item.EventID, &item.StreamSessionID, &item.Data}
}

// AuditForSession filters before limiting so older sessions cannot lose their
// audit rows merely because newer sessions generated more global activity.
func (s *Store) AuditForSession(ctx context.Context, sessionID string, limit int) ([]AuditEvent, int, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, 0, errors.New("session id is required")
	}
	if limit <= 0 || limit > 250 {
		limit = 50
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `
        SELECT id, created_at, source, actor_id, actor_login, actor_display_name, actor_role,
            trigger, command, action, result, reason, COALESCE(event_id, ''), COALESCE(stream_session_id, ''), COALESCE(data, '{}'::jsonb), count(*) OVER ()
        FROM bot_audit_events
        WHERE stream_session_id = $1
        ORDER BY created_at DESC, id DESC
        LIMIT $2
    `, sessionID, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []AuditEvent{}
	total := 0
	for rows.Next() {
		var item AuditEvent
		if err := rows.Scan(append(auditDestinations(&item), &total)...); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
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

func (s *Store) ListCounters(ctx context.Context) ([]Counter, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, value, created_at, updated_at FROM bot_counters ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Counter{}
	for rows.Next() {
		item, err := scanCounter(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) SaveCounter(ctx context.Context, counter Counter) (Counter, error) {
	counter, err := NormalizeCounter(counter)
	if err != nil {
		return Counter{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if strings.TrimSpace(counter.ID) == "" {
		return scanCounter(s.db.QueryRowContext(ctx, `INSERT INTO bot_counters (name, value) VALUES ($1,$2) RETURNING id, name, value, created_at, updated_at`, counter.Name, counter.Value))
	}
	return scanCounter(s.db.QueryRowContext(ctx, `UPDATE bot_counters SET name=$2, value=$3, updated_at=now() WHERE id=$1 RETURNING id, name, value, created_at, updated_at`, counter.ID, counter.Name, counter.Value))
}

func (s *Store) IncrementCounter(ctx context.Context, name string, delta int) (Counter, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return Counter{}, errors.New("counter name is required")
	}
	if !isSafeBotName(name) {
		return Counter{}, errors.New("counter name must use lowercase letters, numbers, dash, or underscore")
	}
	if delta == 0 {
		delta = 1
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return scanCounter(s.db.QueryRowContext(ctx, `
		INSERT INTO bot_counters (name, value) VALUES ($1,$2)
		ON CONFLICT (name) DO UPDATE SET value = bot_counters.value + EXCLUDED.value, updated_at = now()
		RETURNING id, name, value, created_at, updated_at
	`, name, delta))
}

func (s *Store) CounterValue(ctx context.Context, name string) (Counter, bool, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return Counter{}, false, errors.New("counter name is required")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	counter, err := scanCounter(s.db.QueryRowContext(ctx, `SELECT id, name, value, created_at, updated_at FROM bot_counters WHERE name = $1`, name))
	if errors.Is(err, sql.ErrNoRows) {
		return Counter{}, false, nil
	}
	return counter, err == nil, err
}

func (s *Store) DeleteCounter(ctx context.Context, id string) error {
	return s.deleteByID(ctx, "bot_counters", id, "counter id is required")
}

func (s *Store) ListQuotes(ctx context.Context) ([]Quote, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `SELECT id, text, author, created_at, updated_at FROM bot_quotes ORDER BY created_at DESC, id DESC LIMIT 250`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Quote{}
	for rows.Next() {
		item, err := scanQuote(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) ListModerationRules(ctx context.Context) ([]ModerationRule, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, enabled, COALESCE(enforce, false), pattern, match_target, case_sensitive, regex, action, reason, COALESCE(source, ''), expires_at, created_at, updated_at FROM bot_moderation_rules ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ModerationRule{}
	for rows.Next() {
		item, err := scanModerationRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) SaveModerationRule(ctx context.Context, rule ModerationRule) (ModerationRule, error) {
	rule, err := NormalizeModerationRule(rule)
	if err != nil {
		return ModerationRule{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if strings.TrimSpace(rule.ID) == "" {
		return scanModerationRule(s.db.QueryRowContext(ctx, `INSERT INTO bot_moderation_rules (name, enabled, enforce, pattern, match_target, case_sensitive, regex, action, reason, source, expires_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id, name, enabled, COALESCE(enforce, false), pattern, match_target, case_sensitive, regex, action, reason, COALESCE(source, ''), expires_at, created_at, updated_at`, rule.Name, rule.Enabled, rule.Enforce, rule.Pattern, rule.MatchTarget, rule.CaseSensitive, rule.Regex, rule.Action, rule.Reason, rule.Source, rule.ExpiresAt))
	}
	return scanModerationRule(s.db.QueryRowContext(ctx, `UPDATE bot_moderation_rules SET name=$2, enabled=$3, enforce=$4, pattern=$5, match_target=$6, case_sensitive=$7, regex=$8, action=$9, reason=$10, source=CASE WHEN $11 = 'manual' THEN source ELSE $11 END, expires_at=COALESCE($12, expires_at), updated_at=now() WHERE id=$1 RETURNING id, name, enabled, COALESCE(enforce, false), pattern, match_target, case_sensitive, regex, action, reason, COALESCE(source, ''), expires_at, created_at, updated_at`, rule.ID, rule.Name, rule.Enabled, rule.Enforce, rule.Pattern, rule.MatchTarget, rule.CaseSensitive, rule.Regex, rule.Action, rule.Reason, rule.Source, rule.ExpiresAt))
}

func (s *Store) DisableNukeRadiationRule(ctx context.Context, id string) (ModerationRule, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return ModerationRule{}, errors.New("moderation rule id is required")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return scanModerationRule(s.db.QueryRowContext(ctx, `UPDATE bot_moderation_rules SET enabled=false, enforce=false, updated_at=now() WHERE id=$1 AND source='nuke_radiation' RETURNING id, name, enabled, COALESCE(enforce, false), pattern, match_target, case_sensitive, regex, action, reason, COALESCE(source, ''), expires_at, created_at, updated_at`, id))
}

func (s *Store) DeleteModerationRule(ctx context.Context, id string) error {
	return s.deleteByID(ctx, "bot_moderation_rules", id, "moderation rule id is required")
}

func (s *Store) DryRunModeration(ctx context.Context, message, username string) ([]ModerationMatch, error) {
	rules, err := s.ListModerationRules(ctx)
	if err != nil {
		return nil, err
	}
	return MatchModerationRules(rules, message, username)
}

func (s *Store) SaveQuote(ctx context.Context, quote Quote) (Quote, error) {
	quote, err := NormalizeQuote(quote)
	if err != nil {
		return Quote{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if strings.TrimSpace(quote.ID) == "" {
		return scanQuote(s.db.QueryRowContext(ctx, `INSERT INTO bot_quotes (text, author) VALUES ($1,$2) RETURNING id, text, author, created_at, updated_at`, quote.Text, quote.Author))
	}
	return scanQuote(s.db.QueryRowContext(ctx, `UPDATE bot_quotes SET text=$2, author=$3, updated_at=now() WHERE id=$1 RETURNING id, text, author, created_at, updated_at`, quote.ID, quote.Text, quote.Author))
}

func (s *Store) RandomQuote(ctx context.Context) (Quote, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	quote, err := scanQuote(s.db.QueryRowContext(ctx, `SELECT id, text, author, created_at, updated_at FROM bot_quotes ORDER BY random() LIMIT 1`))
	if errors.Is(err, sql.ErrNoRows) {
		return Quote{}, false, nil
	}
	return quote, err == nil, err
}

func (s *Store) DeleteQuote(ctx context.Context, id string) error {
	return s.deleteByID(ctx, "bot_quotes", id, "quote id is required")
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

func (s *Store) ListCommands(ctx context.Context) ([]Command, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, enabled, response, action, COALESCE(response_mode, 'overlay'), role_required, cooldown_seconds, created_at, updated_at
		FROM bot_commands
		ORDER BY name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	commands := []Command{}
	for rows.Next() {
		command, err := scanCommand(rows)
		if err != nil {
			return nil, err
		}
		commands = append(commands, command)
	}
	return commands, rows.Err()
}

func (s *Store) SaveCommand(ctx context.Context, command Command) (Command, error) {
	command, err := NormalizeCommand(command)
	if err != nil {
		return Command{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if strings.TrimSpace(command.ID) == "" {
		return s.insertCommand(ctx, command)
	}
	return s.updateCommand(ctx, command)
}

func (s *Store) SetCommandEnabled(ctx context.Context, id string, enabled bool) (Command, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Command{}, errors.New("command id is required")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	row := s.db.QueryRowContext(ctx, `
		UPDATE bot_commands SET enabled = $2, updated_at = now()
		WHERE id = $1
		RETURNING id, name, enabled, response, action, COALESCE(response_mode, 'overlay'), role_required, cooldown_seconds, created_at, updated_at
	`, id, enabled)
	return scanCommand(row)
}

func (s *Store) DeleteCommand(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("command id is required")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result, err := s.db.ExecContext(ctx, `DELETE FROM bot_commands WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err == nil && affected == 0 {
		return sql.ErrNoRows
	}
	return nil
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

func (s *Store) GetTimer(ctx context.Context, id string) (Timer, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Timer{}, errors.New("timer id is required")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return scanTimer(s.db.QueryRowContext(ctx, `
		SELECT id, name, enabled, interval_seconds, response, action, created_at, updated_at, last_run_at
		FROM bot_timers WHERE id = $1
	`, id))
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

func (s *Store) DueTimers(ctx context.Context, now time.Time, limit int) ([]Timer, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, enabled, interval_seconds, response, action, created_at, updated_at, last_run_at
		FROM bot_timers
		WHERE enabled = true
		  AND (last_run_at IS NULL OR last_run_at + (interval_seconds * interval '1 second') <= $1)
		ORDER BY COALESCE(last_run_at, 'epoch'::timestamptz) ASC, name ASC
		LIMIT $2
	`, now.UTC(), limit)
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

func (s *Store) MarkTimerRun(ctx context.Context, id string, at time.Time) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("timer id is required")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result, err := s.db.ExecContext(ctx, `UPDATE bot_timers SET last_run_at = $2, updated_at = now() WHERE id = $1`, id, at.UTC())
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err == nil && affected == 0 {
		return sql.ErrNoRows
	}
	return nil
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

func (s *Store) insertCommand(ctx context.Context, command Command) (Command, error) {
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO bot_commands (name, enabled, response, action, response_mode, role_required, cooldown_seconds)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, name, enabled, response, action, COALESCE(response_mode, 'overlay'), role_required, cooldown_seconds, created_at, updated_at
	`, command.Name, command.Enabled, command.Response, command.Action, command.ResponseMode, command.RoleRequired, command.CooldownSeconds)
	return scanCommand(row)
}

func (s *Store) updateCommand(ctx context.Context, command Command) (Command, error) {
	row := s.db.QueryRowContext(ctx, `
		UPDATE bot_commands
		SET name = $2, enabled = $3, response = $4, action = $5, response_mode = $6, role_required = $7, cooldown_seconds = $8, updated_at = now()
		WHERE id = $1
		RETURNING id, name, enabled, response, action, COALESCE(response_mode, 'overlay'), role_required, cooldown_seconds, created_at, updated_at
	`, strings.TrimSpace(command.ID), command.Name, command.Enabled, command.Response, command.Action, command.ResponseMode, command.RoleRequired, command.CooldownSeconds)
	return scanCommand(row)
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

type commandScanner interface {
	Scan(dest ...any) error
}

func scanCommand(row commandScanner) (Command, error) {
	var command Command
	if err := row.Scan(&command.ID, &command.Name, &command.Enabled, &command.Response, &command.Action, &command.ResponseMode, &command.RoleRequired, &command.CooldownSeconds, &command.CreatedAt, &command.UpdatedAt); err != nil {
		return Command{}, err
	}
	return command, nil
}

type counterScanner interface{ Scan(dest ...any) error }
type quoteScanner interface{ Scan(dest ...any) error }
type moderationRuleScanner interface{ Scan(dest ...any) error }

func scanCounter(row counterScanner) (Counter, error) {
	var counter Counter
	if err := row.Scan(&counter.ID, &counter.Name, &counter.Value, &counter.CreatedAt, &counter.UpdatedAt); err != nil {
		return Counter{}, err
	}
	return counter, nil
}

func scanQuote(row quoteScanner) (Quote, error) {
	var quote Quote
	if err := row.Scan(&quote.ID, &quote.Text, &quote.Author, &quote.CreatedAt, &quote.UpdatedAt); err != nil {
		return Quote{}, err
	}
	return quote, nil
}

func scanModerationRule(row moderationRuleScanner) (ModerationRule, error) {
	var rule ModerationRule
	var expiresAt sql.NullTime
	if err := row.Scan(&rule.ID, &rule.Name, &rule.Enabled, &rule.Enforce, &rule.Pattern, &rule.MatchTarget, &rule.CaseSensitive, &rule.Regex, &rule.Action, &rule.Reason, &rule.Source, &expiresAt, &rule.CreatedAt, &rule.UpdatedAt); err != nil {
		return ModerationRule{}, err
	}
	if expiresAt.Valid {
		rule.ExpiresAt = &expiresAt.Time
	}
	return rule, nil
}

func NormalizeCounter(counter Counter) (Counter, error) {
	counter.ID = strings.TrimSpace(counter.ID)
	counter.Name = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(counter.Name), "!")))
	if counter.Name == "" {
		return Counter{}, errors.New("counter name is required")
	}
	if !isSafeBotName(counter.Name) {
		return Counter{}, errors.New("counter name must use lowercase letters, numbers, dash, or underscore")
	}
	return counter, nil
}

func NormalizeQuote(quote Quote) (Quote, error) {
	quote.ID = strings.TrimSpace(quote.ID)
	quote.Text = strings.TrimSpace(quote.Text)
	quote.Author = strings.TrimSpace(quote.Author)
	if quote.Text == "" {
		return Quote{}, errors.New("quote text is required")
	}
	if len(quote.Text) > 500 {
		return Quote{}, errors.New("quote text must be 500 characters or less")
	}
	if len(quote.Author) > 80 {
		return Quote{}, errors.New("quote author must be 80 characters or less")
	}
	return quote, nil
}

func NormalizeModerationRule(rule ModerationRule) (ModerationRule, error) {
	rule.ID = strings.TrimSpace(rule.ID)
	rule.Name = strings.ToLower(strings.TrimSpace(rule.Name))
	rule.Pattern = strings.TrimSpace(rule.Pattern)
	rule.MatchTarget = normalizeToken(rule.MatchTarget, "message")
	rule.Action = normalizeToken(rule.Action, "flag")
	rule.Source = normalizeToken(rule.Source, "manual")
	rule.Reason = strings.TrimSpace(rule.Reason)
	if rule.Name == "" {
		return ModerationRule{}, errors.New("moderation rule name is required")
	}
	if !isSafeBotName(rule.Name) {
		return ModerationRule{}, errors.New("moderation rule name must use lowercase letters, numbers, dash, or underscore")
	}
	if rule.Pattern == "" {
		return ModerationRule{}, errors.New("moderation rule pattern is required")
	}
	if len(rule.Pattern) > 200 {
		return ModerationRule{}, errors.New("moderation rule pattern must be 200 characters or less")
	}
	if len(rule.Reason) > 180 {
		return ModerationRule{}, errors.New("moderation rule reason must be 180 characters or less")
	}
	if len(rule.Source) > 40 {
		return ModerationRule{}, errors.New("moderation rule source must be 40 characters or less")
	}
	switch rule.MatchTarget {
	case "message", "username", "both":
	default:
		return ModerationRule{}, fmt.Errorf("unsupported moderation match target %q", rule.MatchTarget)
	}
	switch rule.Action {
	case "flag", "delete", "timeout", "ban":
	default:
		return ModerationRule{}, fmt.Errorf("unsupported moderation action %q", rule.Action)
	}
	if rule.Regex {
		if _, err := regexp.Compile(rule.Pattern); err != nil {
			return ModerationRule{}, fmt.Errorf("invalid moderation regex: %w", err)
		}
	}
	return rule, nil
}

func MatchModerationRules(rules []ModerationRule, message, username string) ([]ModerationMatch, error) {
	matches := []ModerationMatch{}
	for _, rule := range rules {
		rule, err := NormalizeModerationRule(rule)
		if err != nil {
			return nil, err
		}
		if !rule.Enabled {
			continue
		}
		if rule.ExpiresAt != nil && !rule.ExpiresAt.After(time.Now().UTC()) {
			continue
		}
		if moderationRuleMatches(rule, message, username) {
			matches = append(matches, ModerationMatch{RuleID: rule.ID, RuleName: rule.Name, Pattern: rule.Pattern, MatchTarget: rule.MatchTarget, Action: rule.Action, Reason: rule.Reason})
		}
	}
	return matches, nil
}

func moderationRuleMatches(rule ModerationRule, message, username string) bool {
	switch rule.MatchTarget {
	case "username":
		return textMatchesModerationRule(rule, username)
	case "both":
		return textMatchesModerationRule(rule, message) || textMatchesModerationRule(rule, username)
	default:
		return textMatchesModerationRule(rule, message)
	}
}

func textMatchesModerationRule(rule ModerationRule, value string) bool {
	if !rule.CaseSensitive {
		value = strings.ToLower(value)
	}
	pattern := rule.Pattern
	if !rule.CaseSensitive && !rule.Regex {
		pattern = strings.ToLower(pattern)
	}
	if !rule.Regex {
		return strings.Contains(value, pattern)
	}
	compiledPattern := pattern
	if !rule.CaseSensitive {
		compiledPattern = "(?i)" + pattern
	}
	compiled, err := regexp.Compile(compiledPattern)
	if err != nil {
		return false
	}
	return compiled.MatchString(value)
}

func (s *Store) deleteByID(ctx context.Context, table, id, emptyMessage string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New(emptyMessage)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result, err := s.db.ExecContext(ctx, `DELETE FROM `+table+` WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err == nil && affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func NormalizeCommand(command Command) (Command, error) {
	command.ID = strings.TrimSpace(command.ID)
	command.Name = strings.TrimSpace(command.Name)
	command.Name = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(command.Name, "!")))
	command.Response = strings.TrimSpace(command.Response)
	command.Action = normalizeToken(command.Action, "ticker")
	command.ResponseMode = normalizeToken(command.ResponseMode, "overlay")
	command.RoleRequired = normalizeToken(command.RoleRequired, "viewer")
	if command.Name == "" {
		return Command{}, errors.New("command name is required")
	}
	if len(command.Name) > 32 || !isSafeBotName(command.Name) || !isBotNameInitial(command.Name[0]) {
		return Command{}, errors.New("command name must start with a letter or number and use 1-32 lowercase letters, numbers, dashes, or underscores")
	}
	if isReservedCommandName(command.Name) {
		return Command{}, fmt.Errorf("command name %q is reserved", command.Name)
	}
	if command.Response == "" {
		return Command{}, errors.New("command response is required")
	}
	if len(command.Response) > 500 {
		return Command{}, errors.New("command response must be 500 characters or less")
	}
	switch command.Action {
	case "ticker", "alert":
	default:
		return Command{}, fmt.Errorf("unsupported command action %q", command.Action)
	}
	switch command.ResponseMode {
	case "overlay", "chat", "both":
	default:
		return Command{}, fmt.Errorf("unsupported command response mode %q", command.ResponseMode)
	}
	switch command.RoleRequired {
	case "viewer", "moderator", "broadcaster", "operator", "system":
	default:
		return Command{}, fmt.Errorf("unsupported command role %q", command.RoleRequired)
	}
	if command.CooldownSeconds < 0 || command.CooldownSeconds > 3600 {
		return Command{}, errors.New("command cooldown must be between 0 and 3600 seconds")
	}
	return command, nil
}

func NormalizeTimer(timer Timer) (Timer, error) {
	timer.ID = strings.TrimSpace(timer.ID)
	timer.Name = strings.ToLower(strings.TrimSpace(timer.Name))
	timer.Response = strings.TrimSpace(timer.Response)
	timer.Action = normalizeToken(timer.Action, "ticker")
	if timer.Name == "" {
		return Timer{}, errors.New("timer name is required")
	}
	if !isSafeBotName(timer.Name) {
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
	if containsJSONSecretKey(timer.Response) {
		return Timer{}, errors.New("timer JSON response must not contain secret-looking keys")
	}
	switch timer.Action {
	case "ticker", "alert":
	default:
		return Timer{}, fmt.Errorf("unsupported timer action %q", timer.Action)
	}
	return timer, nil
}

// Timer responses are text. When the text is JSON, inspect every key, including
// duplicate object members that would disappear when decoding into a map.
func containsJSONSecretKey(response string) bool {
	if !json.Valid([]byte(response)) {
		return false
	}
	decoder := json.NewDecoder(strings.NewReader(response))
	decoder.UseNumber()
	// k: object key, v: object value, a: array element.
	stack := []byte{}
	for {
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		if delim, ok := token.(json.Delim); ok {
			switch delim {
			case '{', '[':
				if len(stack) > 0 && stack[len(stack)-1] == 'v' {
					stack[len(stack)-1] = 'k'
				}
				if delim == '{' {
					stack = append(stack, 'k')
				} else {
					stack = append(stack, 'a')
				}
			case '}', ']':
				stack = stack[:len(stack)-1]
			}
			continue
		}
		if len(stack) == 0 {
			continue
		}
		switch stack[len(stack)-1] {
		case 'k':
			if key, ok := token.(string); ok && isForbiddenKey(key) {
				return true
			}
			stack[len(stack)-1] = 'v'
		case 'v':
			stack[len(stack)-1] = 'k'
		}
	}
}

func isBotNameInitial(initial byte) bool {
	return (initial >= 'a' && initial <= 'z') || (initial >= '0' && initial <= '9')
}

func isSafeBotName(name string) bool {
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func isReservedCommandName(name string) bool {
	switch name {
	case "alert", "clear", "brb", "back":
		return true
	default:
		return false
	}
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
	compact := strings.ReplaceAll(strings.ReplaceAll(lower, "_", ""), "-", "")
	if strings.Contains(compact, "apikey") || strings.Contains(compact, "privatekey") {
		return true
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
  enforce boolean NOT NULL DEFAULT false,
  pattern text NOT NULL,
  match_target text NOT NULL DEFAULT 'message',
  case_sensitive boolean NOT NULL DEFAULT false,
  regex boolean NOT NULL DEFAULT false,
  action text NOT NULL DEFAULT 'flag',
  reason text NOT NULL DEFAULT '',
  source text NOT NULL DEFAULT 'manual',
  expires_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS bot_moderation_rules_enabled_idx ON bot_moderation_rules (enabled);
ALTER TABLE bot_moderation_rules ADD COLUMN IF NOT EXISTS enforce boolean NOT NULL DEFAULT false;
ALTER TABLE bot_moderation_rules ADD COLUMN IF NOT EXISTS source text NOT NULL DEFAULT 'manual';
ALTER TABLE bot_moderation_rules ADD COLUMN IF NOT EXISTS expires_at timestamptz;
CREATE INDEX IF NOT EXISTS bot_moderation_rules_expires_at_idx ON bot_moderation_rules (expires_at);

CREATE TABLE IF NOT EXISTS bot_api_variables (
  id text PRIMARY KEY DEFAULT encode(gen_random_bytes(16), 'hex'),
  name text NOT NULL UNIQUE,
  enabled boolean NOT NULL DEFAULT true,
  url text NOT NULL,
  response_selector text NOT NULL DEFAULT '',
  fallback text NOT NULL DEFAULT '',
  cache_ttl_seconds integer NOT NULL DEFAULT 60,
  timeout_ms integer NOT NULL DEFAULT 1500,
  last_status text NOT NULL DEFAULT '',
  last_error text NOT NULL DEFAULT '',
  last_checked_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS bot_api_variables_enabled_idx ON bot_api_variables (enabled);
`
