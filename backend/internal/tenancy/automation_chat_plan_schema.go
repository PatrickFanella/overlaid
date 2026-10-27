package tenancy

import (
	"context"
	"encoding/json"
)

func (s *AutomationStore) validateChatPlanStore(ctx context.Context) error {
	if err := s.validateRuns(ctx); err != nil {
		return err
	}
	return s.validateImmutableAutomationTables(ctx, operatorChatPlanColumns, operatorChatPlanConstraints)
}

func (s *AutomationStore) validateImmutableAutomationTables(ctx context.Context, tables map[string]map[string]string, constraints map[string]struct{ table, definition string }) error {
	for table, columns := range tables {
		var valid bool
		types, _ := json.Marshal(columns)
		err := s.db.QueryRowContext(ctx, `SELECT count(*)=$2 AND bool_and(attnotnull) AND jsonb_object_agg(attname,format_type(atttypid,atttypmod))=$3::jsonb FROM pg_attribute WHERE attrelid=to_regclass($1) AND attnum>0 AND NOT attisdropped`, table, len(columns), string(types)).Scan(&valid)
		if err != nil || !valid {
			return automationSchemaError(ctx)
		}
		err = s.db.QueryRowContext(ctx, `SELECT count(*)=1 AND bool_and(relrowsecurity AND relforcerowsecurity AND relkind='r') FROM pg_class WHERE oid=to_regclass($1)`, table).Scan(&valid)
		if err != nil || !valid {
			return automationSchemaError(ctx)
		}
		err = s.db.QueryRowContext(ctx, `SELECT count(*)=1 AND bool_and(p.polname='tenant_isolation' AND p.polcmd='*' AND p.polpermissive AND cardinality(p.polroles)=1 AND NOT 0=ANY(p.polroles) AND pg_has_role(current_user,p.polroles[1],'MEMBER') AND pg_get_expr(p.polqual,p.polrelid)=$2 AND pg_get_expr(p.polwithcheck,p.polrelid)=$2) FROM pg_policy p WHERE p.polrelid=to_regclass($1)`, table, `(tenant_id = (NULLIF(current_setting('app.tenant_id'::text, true), ''::text))::uuid)`).Scan(&valid)
		if err != nil || !valid {
			return automationSchemaError(ctx)
		}
		err = s.db.QueryRowContext(ctx, `SELECT has_table_privilege(current_user,$1,'SELECT') AND has_table_privilege(current_user,$1,'INSERT') AND NOT EXISTS(SELECT 1 FROM pg_roles r WHERE (r.rolname IN(current_user,session_user) OR pg_has_role(current_user,r.oid,'MEMBER') OR pg_has_role(session_user,r.oid,'MEMBER')) AND
 (r.rolsuper OR r.rolbypassrls OR r.rolcreaterole OR r.rolcreatedb OR has_schema_privilege(r.oid,current_schema(),'CREATE') OR EXISTS(SELECT 1 FROM pg_class c WHERE c.oid=to_regclass($1) AND c.relowner=r.oid) OR has_table_privilege(r.oid,$1,'UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER') OR EXISTS(SELECT 1 FROM pg_attribute a WHERE a.attrelid=to_regclass($1) AND a.attnum>0 AND NOT a.attisdropped AND (has_column_privilege(r.oid,a.attrelid,a.attnum,'UPDATE') OR has_column_privilege(r.oid,a.attrelid,a.attnum,'REFERENCES')))))`, table).Scan(&valid)
		if err != nil || !valid {
			return automationSchemaError(ctx)
		}
	}
	for name, c := range constraints {
		var valid bool
		err := s.db.QueryRowContext(ctx, `SELECT count(*)=1 FROM pg_constraint WHERE conrelid=to_regclass($1) AND conname=$2 AND convalidated AND NOT condeferrable AND pg_get_constraintdef(oid)=$3`, c.table, name, c.definition).Scan(&valid)
		if err != nil || !valid {
			return automationSchemaError(ctx)
		}
	}
	return nil
}

var operatorChatPlanColumns = map[string]map[string]string{
	"engine_operator_chat_plan_markers": {"tenant_id": "uuid", "id": "text", "run_id": "text", "occurrence_id": "text", "prepared_at": "timestamp with time zone"},
	"engine_operator_chat_plans":        {"tenant_id": "uuid", "id": "text", "run_id": "text", "occurrence_id": "text", "automation_id": "uuid", "version_id": "uuid", "graph_digest": "text", "provider": "text", "connection_id": "text", "channel_id": "text", "document": "text", "digest": "text", "effect_count": "integer", "source_created_at": "timestamp with time zone", "deadline": "timestamp with time zone", "prepared_at": "timestamp with time zone"},
}

var operatorChatPlanConstraints = map[string]struct{ table, definition string }{
	"engine_occurrence_chat_plan_binding":    {"engine_operator_occurrences", "UNIQUE (tenant_id, id, automation_id, version_id, graph_digest, run_id, created_at, deadline)"},
	"engine_occurrence_chat_marker_binding":  {"engine_operator_occurrences", "UNIQUE (tenant_id, id, run_id)"},
	"engine_operator_chat_plan_markers_pkey": {"engine_operator_chat_plan_markers", "PRIMARY KEY (tenant_id, id)"},
	"engine_chat_marker_run_once":            {"engine_operator_chat_plan_markers", "UNIQUE (tenant_id, run_id)"},
	"engine_chat_marker_binding":             {"engine_operator_chat_plan_markers", "UNIQUE (tenant_id, id, run_id, occurrence_id, prepared_at)"},
	"engine_chat_marker_source":              {"engine_operator_chat_plan_markers", "FOREIGN KEY (tenant_id, occurrence_id, run_id) REFERENCES engine_operator_occurrences(tenant_id, id, run_id) ON DELETE CASCADE"},
	"engine_chat_marker_id":                  {"engine_operator_chat_plan_markers", `CHECK ((id ~ '^[0-9a-f]{64}$'::text))`},
	"engine_chat_marker_time":                {"engine_operator_chat_plan_markers", "CHECK (isfinite(prepared_at))"},
	"engine_operator_chat_plans_pkey":        {"engine_operator_chat_plans", "PRIMARY KEY (tenant_id, id)"},
	"engine_chat_plan_once":                  {"engine_operator_chat_plans", "UNIQUE (tenant_id, run_id)"},
	"engine_chat_plan_binding":               {"engine_operator_chat_plans", "UNIQUE (tenant_id, id, connection_id, deadline)"},
	"engine_chat_plan_marker":                {"engine_operator_chat_plans", "FOREIGN KEY (tenant_id, id, run_id, occurrence_id, prepared_at) REFERENCES engine_operator_chat_plan_markers(tenant_id, id, run_id, occurrence_id, prepared_at) ON DELETE CASCADE"},
	"engine_chat_plan_run":                   {"engine_operator_chat_plans", "FOREIGN KEY (tenant_id, run_id, automation_id) REFERENCES engine_operator_runs(tenant_id, id, automation_id) ON DELETE CASCADE"},
	"engine_chat_plan_source":                {"engine_operator_chat_plans", "FOREIGN KEY (tenant_id, occurrence_id, automation_id, version_id, graph_digest, run_id, source_created_at, deadline) REFERENCES engine_operator_occurrences(tenant_id, id, automation_id, version_id, graph_digest, run_id, created_at, deadline) ON DELETE CASCADE"},
	"engine_chat_plan_connection":            {"engine_operator_chat_plans", "FOREIGN KEY (tenant_id, provider) REFERENCES platform_connections(tenant_id, provider) ON DELETE CASCADE"},
	"engine_chat_plan_target":                {"engine_operator_chat_plans", "CHECK (((provider = 'twitch'::text) AND ((length(channel_id) >= 1) AND (length(channel_id) <= 256)) AND (connection_id = ('twitch:'::text || channel_id))))"},
	"engine_chat_plan_id":                    {"engine_operator_chat_plans", `CHECK ((id ~ '^[0-9a-f]{64}$'::text))`},
	"engine_chat_plan_time":                  {"engine_operator_chat_plans", "CHECK ((isfinite(prepared_at) AND (source_created_at <= prepared_at) AND (prepared_at < deadline)))"},
	"engine_chat_plan_count":                 {"engine_operator_chat_plans", "CHECK (((effect_count >= 1) AND (effect_count <= 16)))"},
	"engine_chat_plan_bytes":                 {"engine_operator_chat_plans", "CHECK (((octet_length(document) >= 1) AND (octet_length(document) <= 65536)))"},
	"engine_chat_plan_header":                {"engine_operator_chat_plans", "CHECK (((document)::jsonb @> jsonb_build_object('formatVersion', 1, 'syntaxVersion', 2, 'captureVersion', 1, 'tenantId', (tenant_id)::text, 'id', id, 'runId', run_id, 'occurrenceId', occurrence_id, 'automationId', (automation_id)::text, 'versionId', (version_id)::text, 'graphDigest', graph_digest, 'connectionId', connection_id, 'channelId', channel_id, 'effectCount', effect_count)))"},
	"engine_chat_plan_effects":               {"engine_operator_chat_plans", "CHECK ((((document)::jsonb ? 'effects'::text) AND (jsonb_typeof(((document)::jsonb -> 'effects'::text)) = 'array'::text) AND (jsonb_array_length(((document)::jsonb -> 'effects'::text)) = effect_count)))"},
	"engine_chat_plan_digest":                {"engine_operator_chat_plans", "CHECK ((digest = encode(digest(((convert_to('overlaid.engine.operator-chat-plan.v1'::text, 'UTF8'::name) || decode('00'::text, 'hex'::text)) || convert_to(document, 'UTF8'::name)), 'sha256'::text), 'hex'::text)))"},
}
