package tenantdb

import (
	"context"
	"database/sql"
	"errors"
)

// Exact definitions keep broadened/disabled constraints from silently granting
// a new writer permission. These strings are PostgreSQL's canonical rendering
// of the reviewed migration, not expressions supplied by runtime callers.
var streamConstraints = map[string]struct{ table, definition string }{
	"stream_event_kind":          {"normalized_stream_events", `CHECK ((kind = ANY (ARRAY['stream_online'::text, 'stream_offline'::text, 'channel_update'::text])))`},
	"stream_event_deadline":      {"normalized_stream_events", `CHECK (((dispatch_deadline = ((floor(EXTRACT(epoch FROM occurred_at)))::bigint + 120)) AND (dispatch_deadline > 0)))`},
	"stream_event_connection":    {"normalized_stream_events", `CHECK ((connection_id = ((platform || ':'::text) || channel_id)))`},
	"stream_event_connection_fk": {"normalized_stream_events", "FOREIGN KEY (tenant_id, platform) REFERENCES platform_connections(tenant_id, provider) ON DELETE CASCADE"},
	"stream_payload_event":       {"platform_stream_payloads", "FOREIGN KEY (tenant_id, event_id) REFERENCES normalized_stream_events(tenant_id, id) ON DELETE CASCADE"},
	"overlaid_stream_job":        {"river_job", "FOREIGN KEY (overlaid_stream_tenant_id, overlaid_stream_event_id, overlaid_stream_connection_id, overlaid_stream_deadline) REFERENCES normalized_stream_events(tenant_id, id, connection_id, dispatch_deadline) ON DELETE CASCADE"},
	"overlaid_stream_args":       {"river_job", `CHECK (((kind <> 'overlaid_stream_dispatch'::text) OR ((args IS NOT NULL) AND (jsonb_typeof(args) = 'object'::text) AND (args ?& ARRAY['tenant_id'::text, 'event_id'::text, 'connection_id'::text, 'deadline'::text]) AND ((args - ARRAY['tenant_id'::text, 'event_id'::text, 'connection_id'::text, 'deadline'::text]) = '{}'::jsonb) AND (jsonb_typeof((args -> 'tenant_id'::text)) = 'string'::text) AND (jsonb_typeof((args -> 'event_id'::text)) = 'string'::text) AND ((args ->> 'event_id'::text) ~ '^[0-9a-f]{64}$'::text) AND (jsonb_typeof((args -> 'connection_id'::text)) = 'string'::text) AND (jsonb_typeof((args -> 'deadline'::text)) = 'number'::text) AND ((args ->> 'deadline'::text) ~ '^[1-9][0-9]{0,11}$'::text) AND (queue = 'stream_followup'::text) AND (max_attempts = 3) AND (priority = 4))))`},
}

func validateStreamSchema(ctx context.Context, raw *sql.DB) error {
	invalid := errors.New("stream ingestion migration and exact fenced constraints required")
	for name, constraint := range streamConstraints {
		var valid bool
		err := raw.QueryRowContext(ctx, `SELECT count(*)=1 FROM pg_constraint WHERE conrelid=to_regclass($1) AND conname=$2 AND convalidated AND NOT condeferrable AND pg_get_constraintdef(oid)=$3`, constraint.table, name, constraint.definition).Scan(&valid)
		if err != nil || !valid {
			return invalid
		}
	}
	for column, expression := range map[string]string{
		"overlaid_stream_tenant_id":     "\nCASE\n    WHEN (kind = 'overlaid_stream_dispatch'::text) THEN ((args ->> 'tenant_id'::text))::uuid\n    ELSE NULL::uuid\nEND",
		"overlaid_stream_event_id":      "\nCASE\n    WHEN (kind = 'overlaid_stream_dispatch'::text) THEN (args ->> 'event_id'::text)\n    ELSE NULL::text\nEND",
		"overlaid_stream_connection_id": "\nCASE\n    WHEN (kind = 'overlaid_stream_dispatch'::text) THEN (args ->> 'connection_id'::text)\n    ELSE NULL::text\nEND",
		"overlaid_stream_deadline":      "\nCASE\n    WHEN (kind = 'overlaid_stream_dispatch'::text) THEN ((args ->> 'deadline'::text))::bigint\n    ELSE NULL::bigint\nEND",
	} {
		var valid bool
		err := raw.QueryRowContext(ctx, `SELECT count(*)=1 FROM pg_attribute a JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum WHERE a.attrelid='river_job'::regclass AND a.attname=$1 AND a.attgenerated='s' AND NOT a.attisdropped AND pg_get_expr(d.adbin,d.adrelid)=$2`, column, expression).Scan(&valid)
		if err != nil || !valid {
			return invalid
		}
	}
	var indexed bool
	err := raw.QueryRowContext(ctx, `SELECT count(*)=1 FROM pg_index i WHERE i.indrelid='river_job'::regclass AND i.indexrelid=to_regclass('overlaid_stream_dispatch') AND i.indisunique AND i.indisvalid AND i.indisready AND i.indnatts=2 AND i.indnkeyatts=2 AND pg_get_indexdef(i.indexrelid,1,true)='overlaid_stream_tenant_id' AND pg_get_indexdef(i.indexrelid,2,true)='overlaid_stream_event_id' AND pg_get_expr(i.indpred,i.indrelid)='(kind = ''overlaid_stream_dispatch''::text)'`).Scan(&indexed)
	if err != nil || !indexed {
		return invalid
	}
	for _, table := range []string{"normalized_stream_events", "platform_stream_payloads"} {
		var fenced bool
		err := raw.QueryRowContext(ctx, `SELECT count(*)=1 AND bool_and(p.polname='tenant_isolation' AND p.polcmd='*' AND p.polpermissive AND cardinality(p.polroles)=1 AND NOT 0=ANY(p.polroles) AND pg_get_expr(p.polqual,p.polrelid)=$2 AND pg_get_expr(p.polwithcheck,p.polrelid)=$2) FROM pg_policy p WHERE p.polrelid=to_regclass($1) AND EXISTS(SELECT 1 FROM unnest(p.polroles) role_id WHERE CASE WHEN role_id=0 THEN true ELSE pg_has_role(current_user,role_id,'MEMBER') OR pg_has_role(session_user,role_id,'MEMBER') END)`, table, `(tenant_id = (NULLIF(current_setting('app.tenant_id'::text, true), ''::text))::uuid)`).Scan(&fenced)
		if err != nil || !fenced {
			return invalid
		}
	}
	return nil
}
