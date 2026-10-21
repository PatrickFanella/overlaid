package tenancy

import (
	"context"
	"encoding/json"
)

func (s *AutomationStore) validateRuns(ctx context.Context) error {
	if ctx == nil || s == nil || s.db == nil {
		return ErrAutomationInvalid
	}
	for _, table := range []string{"engine_operator_occurrences", "engine_operator_runs", "engine_run_budgets"} {
		var valid bool
		types, _ := json.Marshal(automationRunColumns[table])
		err := s.db.QueryRowContext(ctx, `SELECT count(*)=$2 AND bool_and(attnotnull) AND jsonb_object_agg(attname,format_type(atttypid,atttypmod))=$3::jsonb FROM pg_attribute WHERE attrelid=to_regclass($1) AND attnum>0 AND NOT attisdropped`, table, len(automationRunColumns[table]), string(types)).Scan(&valid)
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
	for name, c := range automationRunConstraints {
		var valid bool
		err := s.db.QueryRowContext(ctx, `SELECT count(*)=1 FROM pg_constraint WHERE conrelid=to_regclass($1) AND conname=$2 AND convalidated AND pg_get_constraintdef(oid)=$3`, c.table, name, c.definition).Scan(&valid)
		if err != nil || !valid {
			return automationSchemaError(ctx)
		}
	}
	return nil
}

var automationRunConstraints = map[string]struct{ table, definition string }{
	"engine_operator_occurrences_pkey":                 {"engine_operator_occurrences", "PRIMARY KEY (tenant_id, id)"},
	"engine_occurrence_id":                             {"engine_operator_occurrences", `CHECK ((id ~ '^[0-9a-f]{64}$'::text))`},
	"engine_occurrence_graph":                          {"engine_operator_occurrences", `CHECK ((graph_digest ~ '^[0-9a-f]{64}$'::text))`},
	"engine_occurrence_run":                            {"engine_operator_occurrences", `CHECK ((run_id ~ '^[0-9a-f]{64}$'::text))`},
	"engine_occurrence_deadline":                       {"engine_operator_occurrences", `CHECK ((isfinite(created_at) AND (deadline = (created_at + '00:02:00'::interval))))`},
	"engine_occurrence_binding":                        {"engine_operator_occurrences", "UNIQUE (tenant_id, id, automation_id, version_id, graph_digest, run_id)"},
	"engine_operator_occurrences_tenant_id_fkey":       {"engine_operator_occurrences", "FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE"},
	"engine_operator_runs_pkey":                        {"engine_operator_runs", "PRIMARY KEY (tenant_id, id)"},
	"engine_operator_runs_tenant_id_occurrence_id_key": {"engine_operator_runs", "UNIQUE (tenant_id, occurrence_id)"},
	"engine_run_occurrence":                            {"engine_operator_runs", "FOREIGN KEY (tenant_id, occurrence_id, automation_id, version_id, graph_digest, id) REFERENCES engine_operator_occurrences(tenant_id, id, automation_id, version_id, graph_digest, run_id) ON DELETE CASCADE"},
	"engine_run_version":                               {"engine_operator_runs", "FOREIGN KEY (tenant_id, automation_id, version_id) REFERENCES engine_automation_versions(tenant_id, automation_id, id) ON DELETE CASCADE"},
	"engine_run_actor":                                 {"engine_operator_runs", "FOREIGN KEY (tenant_id, actor_account_id) REFERENCES tenant_memberships(tenant_id, account_id) ON DELETE CASCADE"},
	"engine_run_budgets_pkey":                          {"engine_run_budgets", "PRIMARY KEY (tenant_id, automation_id)"},
	"engine_budget_automation":                         {"engine_run_budgets", "FOREIGN KEY (tenant_id, automation_id) REFERENCES engine_automations(tenant_id, id) ON DELETE CASCADE"},
	"engine_budget_interval":                           {"engine_run_budgets", `CHECK ((isfinite(charged_at) AND isfinite(cooldown_until) AND (cooldown_until >= charged_at) AND (cooldown_until <= (charged_at + '24:00:00'::interval))))`},
}

var automationRunColumns = map[string]map[string]string{
	"engine_operator_occurrences": {"tenant_id": "uuid", "id": "text", "automation_id": "uuid", "version_id": "uuid", "graph_digest": "text", "run_id": "text", "created_at": "timestamp with time zone", "deadline": "timestamp with time zone"},
	"engine_operator_runs":        {"tenant_id": "uuid", "occurrence_id": "text", "automation_id": "uuid", "version_id": "uuid", "graph_digest": "text", "id": "text", "actor_account_id": "uuid"},
	"engine_run_budgets":          {"tenant_id": "uuid", "automation_id": "uuid", "charged_at": "timestamp with time zone", "cooldown_until": "timestamp with time zone"},
}
