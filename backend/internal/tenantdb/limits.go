package tenantdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var ErrLimit = errors.New("plan limit reached")
var resources = map[string]string{"commands": "bot_commands", "timers": "bot_timers", "apiVariables": "bot_api_variables"}

func (d *DB) Limits(ctx context.Context) (map[string]int64, error) {
	if !d.IsScoped() {
		return nil, nil
	}
	var raw []byte
	err := d.raw.QueryRowContext(ctx, `SELECT p.limits FROM tenants t JOIN plans p ON p.id=t.plan_id WHERE t.id=$1`, d.tenant).Scan(&raw)
	if err != nil {
		return nil, err
	}
	limits := map[string]int64{}
	if err = json.Unmarshal(raw, &limits); err != nil {
		return nil, errors.New("invalid plan limits")
	}
	for _, name := range []string{"commands", "timers", "apiVariables", "httpCallsPerHour", "storedEvents", "mediaBytes"} {
		value, ok := limits[name]
		if !ok || value < -1 {
			return nil, errors.New("invalid plan limits")
		}
	}
	return limits, nil
}

func (t *Tx) checkInsert(ctx context.Context, query string) error {
	if !t.db.IsScoped() {
		return nil
	}
	var feature, table string
	for f, tab := range resources {
		if strings.HasPrefix(strings.TrimSpace(query), "INSERT INTO "+tab+" ") {
			feature, table = f, tab
			break
		}
	}
	if table == "" {
		return nil
	}
	var raw []byte
	if err := t.raw.QueryRowContext(ctx, `SELECT p.limits FROM tenants t JOIN plans p ON p.id=t.plan_id WHERE t.id=$1 FOR UPDATE OF t`, t.db.tenant).Scan(&raw); err != nil {
		return err
	}
	limits := map[string]int64{}
	if err := json.Unmarshal(raw, &limits); err != nil {
		return err
	}
	limit, ok := limits[feature]
	if !ok || limit < -1 {
		return errors.New("invalid plan limits")
	}
	if limit == -1 {
		return nil
	}
	var count int64
	if err := t.raw.QueryRowContext(ctx, `SELECT count(*) FROM `+table+` WHERE tenant_id=$1`, t.db.tenant).Scan(&count); err != nil {
		return err
	}
	if count >= limit {
		return fmt.Errorf("%w: %s limit is %d", ErrLimit, feature, limit)
	}
	return nil
}

func (d *DB) AllowItem(ctx context.Context, feature, id string) error {
	if !d.IsScoped() {
		return nil
	}
	table, ok := resources[feature]
	if !ok {
		return errors.New("unknown entitlement")
	}
	limits, err := d.Limits(ctx)
	if err != nil {
		return err
	}
	limit := limits[feature]
	if limit == -1 {
		return nil
	}
	column := "id"
	if feature == "commands" {
		column = "name"
	}
	var allowed bool
	err = d.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM (SELECT `+column+` FROM `+table+` WHERE /*tenant:`+table+`*/ ORDER BY created_at,id LIMIT $1) available WHERE `+column+`=$2)`, limit, id).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return fmt.Errorf("%w: %s execution exceeds current plan", ErrLimit, feature)
	}
	return nil
}

func (d *DB) ReserveHTTP(ctx context.Context) error {
	if !d.IsScoped() {
		return nil
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw []byte
	if err = tx.raw.QueryRowContext(ctx, `SELECT p.limits FROM tenants t JOIN plans p ON p.id=t.plan_id WHERE t.id=$1 FOR UPDATE OF t`, d.tenant).Scan(&raw); err != nil {
		return err
	}
	limits := map[string]int64{}
	if err = json.Unmarshal(raw, &limits); err != nil {
		return err
	}
	limit, ok := limits["httpCallsPerHour"]
	if !ok || limit < -1 {
		return errors.New("invalid plan limits")
	}
	if limit == -1 {
		return tx.Commit()
	}
	var used int64
	err = tx.raw.QueryRowContext(ctx, `INSERT INTO tenant_usage(tenant_id,kind,window_start,used) VALUES($1,'http',date_trunc('hour',now()),1)
	 ON CONFLICT(tenant_id,kind,window_start) DO UPDATE SET used=tenant_usage.used+1 WHERE tenant_usage.used<$2 RETURNING used`, d.tenant, limit).Scan(&used)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrLimit
	}
	if err != nil {
		return err
	}
	if used > limit {
		return ErrLimit
	}
	return tx.Commit()
}

func (t *Tx) trimHistory(ctx context.Context, query string) error {
	if !t.db.IsScoped() || !strings.HasPrefix(strings.TrimSpace(query), "INSERT INTO stream_events ") {
		return nil
	}
	var raw []byte
	if err := t.raw.QueryRowContext(ctx, `SELECT p.limits FROM tenants t JOIN plans p ON p.id=t.plan_id WHERE t.id=$1 FOR UPDATE OF t`, t.db.tenant).Scan(&raw); err != nil {
		return err
	}
	limits := map[string]int64{}
	if err := json.Unmarshal(raw, &limits); err != nil {
		return err
	}
	limit, ok := limits["storedEvents"]
	if !ok || limit < -1 {
		return errors.New("invalid plan limits")
	}
	if limit == -1 || limit > 250000 {
		limit = 250000
	}
	_, err := t.raw.ExecContext(ctx, `DELETE FROM stream_events WHERE tenant_id=$1 AND id IN (SELECT id FROM stream_events WHERE tenant_id=$1 ORDER BY created_at DESC,inserted_at DESC,id DESC OFFSET $2)`, t.db.tenant, limit)
	return err
}
