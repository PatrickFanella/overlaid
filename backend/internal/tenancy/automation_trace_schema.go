package tenancy

import (
	"context"
	"encoding/json"
)

func (s *AutomationStore) validateTraces(ctx context.Context) error {
	if ctx == nil || s == nil || s.db == nil {
		return ErrAutomationInvalid
	}
	for _, table := range []string{"engine_operator_rehearsals"} {
		var valid bool
		types, _ := json.Marshal(automationTraceColumns[table])
		err := s.db.QueryRowContext(ctx, `SELECT count(*)=$2 AND bool_and(attnotnull) AND jsonb_object_agg(attname,format_type(atttypid,atttypmod))=$3::jsonb FROM pg_attribute WHERE attrelid=to_regclass($1) AND attnum>0 AND NOT attisdropped`, table, len(automationTraceColumns[table]), string(types)).Scan(&valid)
		if err != nil || !valid {
			return automationSchemaError(ctx)
		}
		err = s.db.QueryRowContext(ctx, `SELECT count(*)=1 AND bool_and(c.relrowsecurity AND c.relforcerowsecurity AND c.relkind='r') FROM pg_class c WHERE c.oid=to_regclass($1)`, table).Scan(&valid)
		if err != nil || !valid {
			return automationSchemaError(ctx)
		}
		err = s.db.QueryRowContext(ctx, `SELECT count(*)=1 AND bool_and(p.polname='tenant_isolation' AND p.polcmd='*' AND p.polpermissive AND cardinality(p.polroles)=1 AND NOT 0=ANY(p.polroles) AND pg_has_role(current_user,p.polroles[1],'MEMBER') AND pg_get_expr(p.polqual,p.polrelid)=$2 AND pg_get_expr(p.polwithcheck,p.polrelid)=$2) FROM pg_policy p WHERE p.polrelid=to_regclass($1)`, table, `(tenant_id = (NULLIF(current_setting('app.tenant_id'::text, true), ''::text))::uuid)`).Scan(&valid)
		if err != nil || !valid {
			return automationSchemaError(ctx)
		}
		err = s.db.QueryRowContext(ctx, `SELECT has_table_privilege(current_user,$1,'SELECT') AND has_table_privilege(current_user,$1,'INSERT') AND ($1<>'engine_operator_runs' OR has_table_privilege(current_user,$1,'DELETE')) AND ($1<>'engine_run_budgets' OR (has_column_privilege(current_user,$1,'charged_at','UPDATE') AND has_column_privilege(current_user,$1,'cooldown_until','UPDATE')))`, table).Scan(&valid)
		if err != nil || !valid {
			return automationSchemaError(ctx)
		}
		err = s.db.QueryRowContext(ctx, `SELECT NOT EXISTS(SELECT 1 FROM pg_roles r WHERE (r.rolname IN(current_user,session_user) OR pg_has_role(current_user,r.oid,'MEMBER') OR pg_has_role(session_user,r.oid,'MEMBER')) AND
   (r.rolsuper OR r.rolbypassrls OR r.rolcreaterole OR r.rolcreatedb OR has_schema_privilege(r.oid,current_schema(),'CREATE') OR EXISTS(SELECT 1 FROM pg_class c WHERE c.oid=to_regclass($1) AND c.relowner=r.oid) OR has_table_privilege(r.oid,$1,'TRUNCATE,REFERENCES,TRIGGER') OR ($1<>'engine_operator_runs' AND has_table_privilege(r.oid,$1,'DELETE')) OR
   EXISTS(SELECT 1 FROM pg_attribute a WHERE a.attrelid=to_regclass($1) AND a.attnum>0 AND NOT a.attisdropped AND
    (has_column_privilege(r.oid,a.attrelid,a.attnum,'REFERENCES') OR (($1<>'engine_run_budgets' OR a.attname NOT IN('charged_at','cooldown_until')) AND has_column_privilege(r.oid,a.attrelid,a.attnum,'UPDATE'))))))`, table).Scan(&valid)
		if err != nil || !valid {
			return automationSchemaError(ctx)
		}
	}
	for name, c := range automationTraceConstraints {
		var valid bool
		err := s.db.QueryRowContext(ctx, `SELECT count(*)=1 FROM pg_constraint WHERE conrelid=to_regclass($1) AND conname=$2 AND convalidated AND pg_get_constraintdef(oid)=$3`, c.table, name, c.definition).Scan(&valid)
		if err != nil || !valid {
			return automationSchemaError(ctx)
		}
	}
	return nil
}

var automationTraceColumns = map[string]map[string]string{"engine_operator_rehearsals": {"tenant_id": "uuid", "id": "text", "run_id": "text", "automation_id": "uuid", "version_id": "uuid", "mode": "text", "document": "text", "digest": "text", "created_at": "timestamp with time zone"}}
var automationTraceConstraints = map[string]struct{ table, definition string }{
	"engine_trace_mode":               {"engine_operator_rehearsals", `CHECK ((mode = ANY (ARRAY['reproduce'::text, 'current_config'::text])))`},
	"engine_trace_selection":          {"engine_operator_rehearsals", "UNIQUE (tenant_id, run_id, version_id, mode)"},
	"engine_run_trace_binding":        {"engine_operator_runs", "UNIQUE (tenant_id, id, automation_id)"},
	"engine_operator_rehearsals_pkey": {"engine_operator_rehearsals", "PRIMARY KEY (tenant_id, id)"},
	"engine_trace_id":                 {"engine_operator_rehearsals", `CHECK ((id ~ '^[0-9a-f]{64}$'::text))`},
	"engine_trace_bytes":              {"engine_operator_rehearsals", "CHECK (((octet_length(document) >= 1) AND (octet_length(document) <= 65536)))"},
	"engine_trace_header":             {"engine_operator_rehearsals", "CHECK (((document)::jsonb @> jsonb_build_object('formatVersion', 1, 'tenantId', (tenant_id)::text, 'id', id, 'runId', run_id, 'automationId', (automation_id)::text, 'versionId', (version_id)::text, 'mode', mode)))"},
	"engine_trace_digest":             {"engine_operator_rehearsals", "CHECK ((digest = encode(digest(((convert_to('overlaid.engine.operator-trace.v1'::text, 'UTF8'::name) || decode('00'::text, 'hex'::text)) || convert_to(document, 'UTF8'::name)), 'sha256'::text), 'hex'::text)))"},
	"engine_trace_run":                {"engine_operator_rehearsals", "FOREIGN KEY (tenant_id, run_id, automation_id) REFERENCES engine_operator_runs(tenant_id, id, automation_id) ON DELETE CASCADE"},
	"engine_trace_version":            {"engine_operator_rehearsals", "FOREIGN KEY (tenant_id, automation_id, version_id) REFERENCES engine_automation_versions(tenant_id, automation_id, id) ON DELETE CASCADE"},
}
