package tenantdb

import (
	"context"
	"database/sql"
	"errors"
)

func validateStreamDispatchSchema(ctx context.Context, raw *sql.DB) error {
	var valid bool
	err := raw.QueryRowContext(ctx, `SELECT count(*)=1 FROM pg_constraint WHERE conrelid='platform_stream_dispatch_receipts'::regclass AND conname='stream_dispatch_source' AND convalidated AND NOT condeferrable AND pg_get_constraintdef(oid)='FOREIGN KEY (tenant_id, event_id, connection_id, deadline) REFERENCES normalized_stream_events(tenant_id, id, connection_id, dispatch_deadline) ON DELETE CASCADE'`).Scan(&valid)
	if err != nil || !valid {
		return errors.New("stream dispatch migration and fenced receipt source required")
	}
	err = raw.QueryRowContext(ctx, `SELECT count(*)=1 FROM pg_constraint WHERE conrelid='platform_stream_dispatch_receipts'::regclass AND conname='stream_dispatch_state' AND convalidated AND NOT condeferrable AND pg_get_constraintdef(oid)=$1`, streamDispatchStateConstraint).Scan(&valid)
	if err != nil || !valid {
		return errors.New("stream dispatch receipt vocabulary required")
	}
	for name, definition := range map[string]string{
		"stream_dispatch_claim":                  `CHECK ((claim_hash ~ '^[0-9a-f]{64}$'::text))`,
		"platform_stream_dispatch_receipts_pkey": `PRIMARY KEY (tenant_id, event_id)`,
	} {
		err = raw.QueryRowContext(ctx, `SELECT count(*)=1 FROM pg_constraint WHERE conrelid='platform_stream_dispatch_receipts'::regclass AND conname=$1 AND convalidated AND NOT condeferrable AND pg_get_constraintdef(oid)=$2`, name, definition).Scan(&valid)
		if err != nil || !valid {
			return errors.New("stream dispatch one-use claim required")
		}
	}
	err = raw.QueryRowContext(ctx, `SELECT count(*)=1 FROM pg_constraint WHERE conrelid='normalized_stream_events'::regclass AND conname='stream_dispatch_locked_identity' AND convalidated AND NOT condeferrable AND pg_get_constraintdef(oid)='CHECK (COALESCE((id = (envelope ->> ''id''::text)), false))'`).Scan(&valid)
	if err != nil || !valid {
		return errors.New("stream dispatch immutable source lock required")
	}
	err = raw.QueryRowContext(ctx, `WITH reachable AS (SELECT oid FROM pg_roles WHERE rolname IN(current_user,session_user) OR pg_has_role(current_user,oid,'MEMBER') OR pg_has_role(session_user,oid,'MEMBER')) SELECT has_column_privilege(current_user,'normalized_stream_events','id','UPDATE') AND NOT EXISTS(SELECT 1 FROM reachable r,pg_attribute a WHERE a.attrelid='normalized_stream_events'::regclass AND a.attnum>0 AND NOT a.attisdropped AND a.attname<>'id' AND has_column_privilege(r.oid,a.attrelid,a.attname,'UPDATE'))`).Scan(&valid)
	if err != nil || !valid {
		return errors.New("stream dispatch requires an identity-only source lock grant")
	}
	return nil
}

const streamDispatchStateConstraint = `CHECK ((((state = 'running'::text) AND (code = 'started'::text) AND (finished_at IS NULL)) OR ((state = 'completed'::text) AND (code = 'completed'::text) AND (finished_at IS NOT NULL)) OR ((state = 'failed'::text) AND (code = ANY (ARRAY['handler_failed'::text, 'capability_unavailable'::text, 'connection_unavailable'::text])) AND (finished_at IS NOT NULL)) OR ((state = 'uncertain'::text) AND (code = ANY (ARRAY['handler_cancelled'::text, 'prior_start'::text, 'owner_release_failed'::text])) AND (finished_at IS NOT NULL)) OR ((state = 'skipped'::text) AND (code = ANY (ARRAY['expired'::text, 'connection_unavailable'::text, 'invalid_event'::text, 'capability_unavailable'::text])) AND (finished_at IS NOT NULL))))`
