// SPDX-License-Identifier: MPL-2.0
// Lease bookkeeping adapts River v0.47.0 JobGetAvailable; see
// docs/vendor/river/PROVENANCE.md and its retained upstream license.
package platformqueue

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river/riverdriver"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
	"github.com/riverqueue/river/rivertype"
)

var ErrFairLeasing = errors.New("fair queue leasing unavailable")

// NewFairDriver preserves the pinned driver's methods except outbound leasing.
// It validates the dedicated role and explicit migration, opens no pool and
// registers no worker. Each lane retains its own kind and capacity boundary.
func NewFairDriver(ctx context.Context, service *sql.DB, product *tenantdb.DB, kind string) (riverdriver.Driver[*sql.Tx], string, error) {
	queue := ""
	switch kind {
	case "overlaid_chat_send":
		queue = "chat_outbound"
	case "overlaid_moderation":
		queue = "moderation_outbound"
	default:
		return nil, "", ErrFairLeasing
	}
	schema, err := Validate(ctx, service, product, kind)
	if err != nil {
		return nil, "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var ready bool
	err = service.QueryRowContext(ctx, `SELECT count(*)=1 AND coalesce(bool_and(s.seqtypid='int8'::regtype AND s.seqincrement=1 AND s.seqstart=1 AND s.seqcache=1 AND s.seqmin=1 AND s.seqmax=999999999999999999 AND NOT s.seqcycle AND has_sequence_privilege(current_user,c.oid,'USAGE') AND has_sequence_privilege(current_user,c.oid,'SELECT') AND NOT EXISTS(SELECT 1 FROM pg_roles r WHERE (r.rolname IN(current_user,session_user) OR pg_has_role(current_user,r.oid,'MEMBER') OR pg_has_role(session_user,r.oid,'MEMBER')) AND has_sequence_privilege(r.oid,c.oid,'UPDATE'))),false) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace JOIN pg_sequence s ON s.seqrelid=c.oid WHERE n.nspname=current_schema() AND c.relname='overlaid_queue_turn_seq'`).Scan(&ready)
	if err != nil || !ready {
		return nil, "", validationFailure(ctx, ErrFairLeasing, err)
	}
	return &fairDriver{Driver: riverdatabasesql.New(service), service: service, kind: kind, queue: queue, schema: schema}, schema, nil
}

type fairDriver struct {
	*riverdatabasesql.Driver
	service             *sql.DB
	kind, queue, schema string
}

func (d *fairDriver) GetExecutor() riverdriver.Executor {
	return &fairExecutor{Executor: d.Driver.GetExecutor(), driver: d}
}

type fairExecutor struct {
	riverdriver.Executor
	driver *fairDriver
}

// A lease batch visits each ready tenant before its next job. The least
// recently served tenant wins the next round; existing schedule/ID breaks ties.
// Turns commit in job metadata with the lease, independent of wall-clock jumps.
// No provider call, product claim, pacing permit or future send slot occurs here.
func (e *fairExecutor) JobGetAvailable(ctx context.Context, p *riverdriver.JobGetAvailableParams) ([]*rivertype.JobRow, error) {
	d := e.driver
	if p == nil || p.Schema != d.schema || p.Queue != d.queue || p.MaxToLock < 0 || p.MaxToLock > 1000 || p.MaxAttemptedBy < 1 || p.MaxAttemptedBy > 1000 || p.ClientID == "" || len(p.ClientID) > 1024 {
		return nil, ErrFairLeasing
	}
	if p.MaxToLock == 0 {
		return []*rivertype.JobRow{}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	fail := func() error {
		if ctx.Err() != nil {
			return errors.Join(ErrFairLeasing, ctx.Err())
		}
		return ErrFairLeasing
	}
	tx, err := d.service.BeginTx(ctx, nil)
	if err != nil {
		return nil, fail()
	}
	defer func() { _ = tx.Rollback() }()
	// Serialize only this short kind/schema lease transaction across clients.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "outbound_fair:"+d.schema+":"+d.kind); err != nil {
		return nil, fail()
	}
	table := pgx.Identifier{d.schema, "river_job"}.Sanitize()
	sequence := pgx.Identifier{d.schema, "overlaid_queue_turn_seq"}.Sanitize()
	query := `WITH ready AS MATERIALIZED (
 SELECT id,(args->>'tenant_id')::uuid AS tenant,priority,scheduled_at,
 row_number() OVER(PARTITION BY (args->>'tenant_id')::uuid ORDER BY priority,scheduled_at,id) AS round
 FROM ` + table + ` WHERE kind=$1 AND queue=$2 AND state='available' AND scheduled_at<=coalesce($3::timestamptz,clock_timestamp())
 ), tenants AS MATERIALIZED (SELECT DISTINCT tenant FROM ready), turns AS MATERIALIZED (
 SELECT tenant,coalesce((SELECT max(CASE WHEN jsonb_typeof(metadata->'overlaid_fair_turn')='number' AND metadata->>'overlaid_fair_turn' ~ '^[0-9]{1,18}$' THEN (metadata->>'overlaid_fair_turn')::bigint ELSE 0 END)
 FROM ` + table + ` history WHERE history.kind=$1 AND history.queue=$2 AND (history.args->>'tenant_id')::uuid=tenants.tenant AND history.kind IN('overlaid_chat_send','overlaid_moderation')),0) AS last_turn FROM tenants
 ) SELECT job.id FROM ready JOIN turns USING(tenant) JOIN ` + table + ` job ON job.id=ready.id
 ORDER BY ready.priority,ready.round,turns.last_turn,ready.scheduled_at,ready.id
 LIMIT $4 FOR UPDATE OF job SKIP LOCKED`
	rows, err := tx.QueryContext(ctx, query, d.kind, d.queue, p.Now, p.MaxToLock)
	if err != nil {
		return nil, fail()
	}
	ids := make([]int64, 0, p.MaxToLock)
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, fail()
		}
		ids = append(ids, id)
	}
	readErr := rows.Err()
	closeErr := rows.Close()
	if readErr != nil || closeErr != nil {
		return nil, fail()
	}
	if len(ids) == 0 {
		if err = tx.Commit(); err != nil {
			return nil, fail()
		}
		return []*rivertype.JobRow{}, nil
	}
	var turn int64
	if err = tx.QueryRowContext(ctx, `SELECT nextval($1::regclass)`, sequence).Scan(&turn); err != nil {
		return nil, fail()
	}
	// Preserve River's attempt and bounded attempted_by semantics. Only the
	// application-owned metadata key is added; other metadata remains unchanged.
	result, err := tx.ExecContext(ctx, `UPDATE `+table+` SET state='running',attempt=attempt+1,
 attempted_at=coalesce($1::timestamptz,clock_timestamp()),
 attempted_by=array_append(CASE WHEN array_length(attempted_by,1)>=$2 THEN attempted_by[array_length(attempted_by,1)+2-$2:] ELSE attempted_by END,$3::text),
 metadata=jsonb_set(metadata,'{overlaid_fair_turn}',to_jsonb($4::bigint),true)
 WHERE id=ANY($5::bigint[]) AND kind=$6 AND queue=$7 AND state='available'`, p.Now, p.MaxAttemptedBy, p.ClientID, turn, ids, d.kind, d.queue)
	if err != nil {
		return nil, fail()
	}
	count, err := result.RowsAffected()
	if err != nil || count != int64(len(ids)) {
		return nil, fail()
	}
	// Reuse the pinned driver's full row decoder; return deterministic lease order.
	out := make([]*rivertype.JobRow, 0, len(ids))
	reader := d.UnwrapExecutor(tx)
	for _, id := range ids {
		job, err := reader.JobGetByID(ctx, &riverdriver.JobGetByIDParams{ID: id, Schema: d.schema})
		if err != nil {
			return nil, fail()
		}
		out = append(out, job)
	}
	if err = tx.Commit(); err != nil {
		return nil, fail()
	}
	return out, nil
}
