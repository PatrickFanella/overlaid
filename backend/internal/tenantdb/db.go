// Package tenantdb gives product stores an immutable scope and transaction-local
// PostgreSQL tenant context. Legacy development stores keep their old schema.
package tenantdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const BootstrapID = "00000000-0000-4000-8000-000000000001"

var uuid = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
var predicate = regexp.MustCompile(`/\*tenant:([a-z_]+)\*/`)
var legacyAnd = regexp.MustCompile(`WHERE TRUE\s+AND\s+`)
var legacyClause = regexp.MustCompile(`WHERE TRUE\s+(ORDER BY|GROUP BY|LIMIT|RETURNING)`)
var legacyEnd = regexp.MustCompile(`\s+WHERE TRUE\s*(\)|$)`)

type Row interface{ Scan(...any) error }
type Rows interface {
	Next() bool
	Scan(...any) error
	Close() error
	Err() error
}

type DB struct {
	raw    *sql.DB
	tenant string
}
type Tx struct {
	raw *sql.Tx
	db  *DB
}

func Legacy(db *sql.DB) *DB { return &DB{raw: db} }

// Scoped shares an already validated runtime pool with another immutable scope.
func (d *DB) Scoped(tenantID string) (*DB, error) {
	if !d.IsScoped() || !ValidID(tenantID) {
		return nil, errors.New("validated tenant database required")
	}
	return &DB{raw: d.raw, tenant: strings.ToLower(tenantID)}, nil
}
func ValidID(id string) bool                        { return uuid.MatchString(id) }
func (d *DB) IsScoped() bool                        { return d.tenant != "" }
func (d *DB) TenantID() string                      { return d.tenant }
func (d *DB) Close() error                          { return d.raw.Close() }
func (d *DB) PingContext(ctx context.Context) error { return d.raw.PingContext(ctx) }

func Open(databaseURL, tenantID string) (*DB, error) {
	if !ValidID(tenantID) {
		return nil, errors.New("valid tenant context required")
	}
	raw, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	d := &DB{raw: raw, tenant: strings.ToLower(tenantID)}
	raw.SetMaxOpenConns(16)
	raw.SetMaxIdleConns(4)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var unsafe bool
	err = raw.QueryRowContext(ctx, `SELECT EXISTS (
        SELECT 1 FROM pg_roles r WHERE r.rolname IN (current_user, session_user)
        AND (r.rolsuper OR r.rolbypassrls OR EXISTS (
            SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
            WHERE n.nspname=current_schema() AND c.relkind IN ('r','p')
            AND c.relowner=r.oid
        ))
    )`).Scan(&unsafe)
	if err != nil || unsafe {
		_ = raw.Close()
		return nil, errors.New("tenant runtime requires a non-owner PostgreSQL role without RLS bypass")
	}
	var migrated bool
	err = raw.QueryRowContext(ctx, `SELECT count(*)=40 AND bool_and(relrowsecurity AND relforcerowsecurity)
		FROM pg_class WHERE oid IN (
		'app_metadata'::regclass,'stream_events'::regclass,'twitch_interaction_rules'::regclass,
		'dashboard_configs'::regclass,'stream_sessions'::regclass,'stream_session_readiness_snapshots'::regclass,
		'bot_audit_events'::regclass,'bot_commands'::regclass,'bot_timers'::regclass,'bot_counters'::regclass,
		'bot_quotes'::regclass,'bot_moderation_rules'::regclass,'bot_api_variables'::regclass,
'platform_connections'::regclass,'tenant_credentials'::regclass,'tenant_imports'::regclass,'tenant_usage'::regclass,'overlay_state'::regclass,'newsroom_documents'::regclass,
'normalized_chat_events'::regclass,'platform_event_payloads'::regclass,'normalized_stream_events'::regclass,'platform_stream_payloads'::regclass,'platform_stream_dispatch_receipts'::regclass,'river_job'::regclass,'platform_dispatch_receipts'::regclass,'twitch_sent_messages'::regclass,'bot_accounts'::regclass,'shared_bot_delegations'::regclass,'platform_outbound_actions'::regclass,'platform_moderation_actions'::regclass,'platform_manual_sources'::regclass,'platform_moderation_sources'::regclass,'engine_automations'::regclass,'engine_automation_versions'::regclass,'engine_automation_presets'::regclass,'engine_operator_occurrences'::regclass,'engine_operator_runs'::regclass,'engine_run_budgets'::regclass,'engine_operator_rehearsals'::regclass)`).Scan(&migrated)
	if err == nil && migrated {
		err = raw.QueryRowContext(ctx, `SELECT count(*)=2 FROM pg_constraint WHERE conrelid='river_job'::regclass AND contype='f' AND convalidated AND ((confrelid='platform_outbound_actions'::regclass AND conname='overlaid_outbound_job') OR (confrelid='platform_moderation_actions'::regclass AND conname='overlaid_moderation_job'))`).Scan(&migrated)
	}
	if err == nil && migrated {
		err = raw.QueryRowContext(ctx, `SELECT count(*)=1 FROM pg_constraint WHERE conrelid='river_job'::regclass AND conname='overlaid_moderation_priority' AND contype='c' AND convalidated AND pg_get_expr(conbin,conrelid)='((kind <> ''overlaid_moderation''::text) OR (priority = 1))'`).Scan(&migrated)
	}
	if err == nil && migrated {
		err = raw.QueryRowContext(ctx, `SELECT count(*)=2 AND bool_and(convalidated) FROM pg_constraint WHERE conrelid='platform_outbound_actions'::regclass AND conname IN('platform_outbound_source_kind','platform_outbound_manual_source')`).Scan(&migrated)
	}
	if err == nil && migrated {
		err = raw.QueryRowContext(ctx, `SELECT count(*)=2 AND bool_and(convalidated) FROM pg_constraint WHERE conrelid='platform_moderation_actions'::regclass AND ((conname='platform_moderation_source_kind' AND contype='c' AND pg_get_expr(conbin,conrelid)='((event_id IS NULL) <> (manual_source_id IS NULL))') OR (conname='platform_moderation_manual_source' AND contype='f' AND confrelid='platform_moderation_sources'::regclass))`).Scan(&migrated)
	}
	if err == nil && migrated {
		err = raw.QueryRowContext(ctx, `SELECT count(*)=2 AND bool_and(convalidated) FROM pg_constraint WHERE conrelid='platform_moderation_actions'::regclass AND ((conname='platform_moderation_nuke_target_kind' AND contype='c' AND pg_get_expr(conbin,conrelid)='((nuke_target_event_id IS NULL) OR ((manual_source_id IS NOT NULL) AND (capability = ''user.timeout''::text) AND (target_event_id IS NULL)))') OR (conname='platform_moderation_nuke_target' AND contype='f' AND pg_get_constraintdef(oid)='FOREIGN KEY (tenant_id, nuke_target_event_id, connection_id) REFERENCES normalized_chat_events(tenant_id, id, connection_id) ON DELETE CASCADE'))`).Scan(&migrated)
	}
	if err == nil && migrated {
		err = raw.QueryRowContext(ctx, `SELECT count(*)=1 AND bool_and(convalidated) FROM pg_constraint WHERE conrelid='platform_moderation_sources'::regclass AND conname='platform_moderation_source_action_count' AND contype='c' AND pg_get_expr(conbin,conrelid)='(((kind = ''single''::text) AND (action_count = 1)) OR ((kind = ''batch''::text) AND ((action_count >= 1) AND (action_count <= 50))) OR ((kind = ''radiation''::text) AND (action_count = 0)))'`).Scan(&migrated)
	}
	if err == nil && migrated {
		err = raw.QueryRowContext(ctx, `SELECT position('episode:' in pg_get_functiondef('resolve_newsroom_capability(text,text)'::regprocedure))>0`).Scan(&migrated)
	}
	if err == nil && migrated {
		err = validateStreamSchema(ctx, raw)
	}
	if err == nil && migrated {
		err = validateStreamDispatchSchema(ctx, raw)
	}
	if err == nil && migrated {
		err = d.ValidateStreamMode(ctx)
	}
	if err != nil || !migrated {
		_ = raw.Close()
		return nil, errors.New("tenant schema migration and forced RLS required")
	}
	return d, nil
}

// Predicates are part of each store query, independent of the RLS policies.
// Transaction-local context also supplies insertion defaults; caller input is
// never interpolated into SQL, and a missing context cannot match a product row.
func (d *DB) SQL(query string) string {
	lock := `'stream_sessions'::regclass::oid::bigint`
	if d.IsScoped() {
		lock = `hashtextextended('stream_sessions:' || current_setting('app.tenant_id'),'stream_sessions'::regclass::oid::bigint)`
	}
	query = strings.ReplaceAll(query, "/*tenant-session-lock*/", lock)
	query = predicate.ReplaceAllStringFunc(query, func(marker string) string {
		if !d.IsScoped() {
			return "TRUE"
		}
		table := predicate.FindStringSubmatch(marker)[1]
		return table + `.tenant_id = nullif(current_setting('app.tenant_id', true),'')::uuid`
	})
	conflict := ""
	if d.IsScoped() {
		conflict = "tenant_id, "
	}
	query = strings.ReplaceAll(query, "/*tenant-conflict*/", conflict)
	if !d.IsScoped() {
		query = legacyAnd.ReplaceAllString(query, "WHERE ")
		query = legacyClause.ReplaceAllString(query, "$1")
		query = legacyEnd.ReplaceAllString(query, "$1")
		query = strings.ReplaceAll(query, " AND TRUE", "")
	}
	return query
}

func (d *DB) BeginTx(ctx context.Context, options *sql.TxOptions) (*Tx, error) {
	tx, err := d.raw.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	if d.IsScoped() {
		if _, err = tx.ExecContext(ctx, `SELECT set_config('app.tenant_id', $1, true)`, d.tenant); err != nil {
			_ = tx.Rollback()
			return nil, err
		}
	}
	return &Tx{raw: tx, db: d}, nil
}
func (t *Tx) Commit() error   { return t.raw.Commit() }
func (t *Tx) Rollback() error { return t.raw.Rollback() }
func (t *Tx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if err := t.checkInsert(ctx, query); err != nil {
		return nil, err
	}
	result, err := t.raw.ExecContext(ctx, t.db.SQL(query), args...)
	if err == nil {
		err = t.trimHistory(ctx, query)
	}
	return result, err
}
func (t *Tx) QueryRowContext(ctx context.Context, query string, args ...any) Row {
	if err := t.checkInsert(ctx, query); err != nil {
		return scanRow{scan: func(...any) error { return err }}
	}
	return t.raw.QueryRowContext(ctx, t.db.SQL(query), args...)
}
func (t *Tx) QueryContext(ctx context.Context, query string, args ...any) (Rows, error) {
	return t.raw.QueryContext(ctx, t.db.SQL(query), args...)
}

func (d *DB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if !d.IsScoped() {
		return d.raw.ExecContext(ctx, d.SQL(query), args...)
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return result, tx.Commit()
}

type scanRow struct{ scan func(...any) error }

func (r scanRow) Scan(dest ...any) error { return r.scan(dest...) }

func (d *DB) QueryRowContext(ctx context.Context, query string, args ...any) Row {
	if !d.IsScoped() {
		return d.raw.QueryRowContext(ctx, d.SQL(query), args...)
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return scanRow{scan: func(...any) error { return err }}
	}
	return scanRow{scan: func(dest ...any) error {
		defer tx.Rollback()
		if err := tx.QueryRowContext(ctx, query, args...).Scan(dest...); err != nil {
			return err
		}
		return tx.Commit()
	}}
}

type ownedRows struct {
	raw    *sql.Rows
	tx     *Tx
	closed bool
	err    error
}

func (r *ownedRows) Next() bool {
	if r.closed {
		return false
	}
	if r.raw.Next() {
		return true
	}
	_ = r.Close()
	return false
}
func (r *ownedRows) Scan(dest ...any) error {
	err := r.raw.Scan(dest...)
	if err != nil {
		r.err = errors.Join(r.err, err)
	}
	return err
}
func (r *ownedRows) Err() error { return errors.Join(r.err, r.raw.Err()) }
func (r *ownedRows) Close() error {
	if r.closed {
		return r.err
	}
	r.closed = true
	r.err = errors.Join(r.err, r.raw.Err(), r.raw.Close())
	if r.err != nil {
		_ = r.tx.Rollback()
	} else {
		r.err = r.tx.Commit()
	}
	return r.err
}

func (d *DB) QueryContext(ctx context.Context, query string, args ...any) (Rows, error) {
	if !d.IsScoped() {
		return d.raw.QueryContext(ctx, d.SQL(query), args...)
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	rows, err := tx.raw.QueryContext(ctx, d.SQL(query), args...)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	return &ownedRows{raw: rows, tx: tx}, nil
}

func (d *DB) String() string { return fmt.Sprintf("tenant database (%s)", d.tenant) }
