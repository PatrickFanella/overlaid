package tenancy

import "context"

// Validation is repeated before each explicit operation. It is a snapshot, not
// permission to broaden grants or alter policy while a transaction is active.
func (s *AutomationStore) validate(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, table := range []string{"engine_automations", "engine_automation_versions", "engine_automation_presets"} {
		var valid bool
		err := s.db.QueryRowContext(ctx, `SELECT count(*)=1 AND bool_and(c.relrowsecurity AND c.relforcerowsecurity AND c.relkind='r') FROM pg_class c WHERE c.oid=to_regclass($1)`, table).Scan(&valid)
		if err != nil || !valid {
			return automationSchemaError(ctx)
		}
		err = s.db.QueryRowContext(ctx, `SELECT count(*)=1 AND bool_and(p.polname='tenant_isolation' AND p.polcmd='*' AND p.polpermissive AND cardinality(p.polroles)=1 AND NOT 0=ANY(p.polroles) AND pg_has_role(current_user,p.polroles[1],'MEMBER') AND pg_get_expr(p.polqual,p.polrelid)=$2 AND pg_get_expr(p.polwithcheck,p.polrelid)=$2) FROM pg_policy p WHERE p.polrelid=to_regclass($1)`, table, `(tenant_id = (NULLIF(current_setting('app.tenant_id'::text, true), ''::text))::uuid)`).Scan(&valid)
		if err != nil || !valid {
			return automationSchemaError(ctx)
		}
		err = s.db.QueryRowContext(ctx, `SELECT has_table_privilege(current_user,$1,'SELECT') AND has_table_privilege(current_user,$1,'INSERT')`, table).Scan(&valid)
		if err != nil || !valid {
			return automationSchemaError(ctx)
		}
		// Check original/current identities and every reachable SET ROLE identity,
		// including non-inherited roles and column-only privilege escalation.
		err = s.db.QueryRowContext(ctx, `SELECT NOT EXISTS(SELECT 1 FROM pg_roles r WHERE (r.rolname IN(current_user,session_user) OR pg_has_role(current_user,r.oid,'MEMBER') OR pg_has_role(session_user,r.oid,'MEMBER')) AND
		 (r.rolsuper OR r.rolbypassrls OR r.rolcreaterole OR r.rolcreatedb OR has_schema_privilege(r.oid,current_schema(),'CREATE') OR EXISTS(SELECT 1 FROM pg_class c WHERE c.oid=to_regclass($1) AND c.relowner=r.oid) OR has_table_privilege(r.oid,$1,'DELETE,TRUNCATE,REFERENCES,TRIGGER') OR
		 EXISTS(SELECT 1 FROM pg_attribute a WHERE a.attrelid=to_regclass($1) AND a.attnum>0 AND NOT a.attisdropped AND
		  (has_column_privilege(r.oid,a.attrelid,a.attnum,'REFERENCES') OR (($1<>'engine_automations' OR a.attname NOT IN('current_version','updated_at')) AND has_column_privilege(r.oid,a.attrelid,a.attnum,'UPDATE'))))))`, table).Scan(&valid)
		if err != nil || !valid {
			return automationSchemaError(ctx)
		}
	}
	for name, c := range automationConstraints {
		var valid bool
		err := s.db.QueryRowContext(ctx, `SELECT count(*)=1 FROM pg_constraint WHERE conrelid=to_regclass($1) AND conname=$2 AND convalidated AND pg_get_constraintdef(oid)=$3`, c.table, name, c.definition).Scan(&valid)
		if err != nil || !valid {
			return automationSchemaError(ctx)
		}
	}
	return nil
}

func automationSchemaError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return ErrAutomationSchema
}

var automationConstraints = map[string]struct{ table, definition string }{
	"engine_version_bytes":                             {"engine_automation_versions", "CHECK (((octet_length(document) >= 1) AND (octet_length(document) <= 65536)))"},
	"engine_version_header":                            {"engine_automation_versions", "CHECK (((document)::jsonb @> jsonb_build_object('schemaVersion', 1, 'tenantId', (tenant_id)::text, 'automationId', (automation_id)::text, 'versionId', (id)::text)))"},
	"engine_version_digest":                            {"engine_automation_versions", "CHECK ((digest = encode(digest(((convert_to('overlaid.engine.document.v1'::text, 'UTF8'::name) || decode('00'::text, 'hex'::text)) || convert_to(document, 'UTF8'::name)), 'sha256'::text), 'hex'::text)))"},
	"engine_automations_tenant_id_fkey":                {"engine_automations", "FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE"},
	"engine_automation_versions_actor_account_id_fkey": {"engine_automation_versions", "FOREIGN KEY (actor_account_id) REFERENCES accounts(id) ON DELETE SET NULL"},

	"engine_version_automation": {"engine_automation_versions", "FOREIGN KEY (tenant_id, automation_id) REFERENCES engine_automations(tenant_id, id) ON DELETE CASCADE"},
	"engine_current_version":    {"engine_automations", "FOREIGN KEY (tenant_id, id, current_version) REFERENCES engine_automation_versions(tenant_id, automation_id, id) DEFERRABLE INITIALLY DEFERRED"},
	"engine_preset_source":      {"engine_automation_presets", "FOREIGN KEY (tenant_id, automation_id, version_id) REFERENCES engine_automation_versions(tenant_id, automation_id, id) ON DELETE CASCADE"},
	"engine_preset_target":      {"engine_automation_presets", "FOREIGN KEY (tenant_id, target_automation_id, target_version_id) REFERENCES engine_automation_versions(tenant_id, automation_id, id) DEFERRABLE INITIALLY DEFERRED"},
}
