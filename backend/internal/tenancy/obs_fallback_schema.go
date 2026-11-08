package tenancy

import (
	"context"
	"encoding/json"
)

func (s *OBSFallbackStore) validate(ctx context.Context) error {
	if ctx == nil || s == nil || s.db == nil || s.automation == nil {
		return ErrAutomationInvalid
	}
	if err := s.automation.validate(ctx); err != nil {
		return err
	}
	if len(obsFallbackConstraints) < 20 {
		return ErrAutomationSchema
	}
	for table, columns := range obsFallbackColumns {
		raw, _ := json.Marshal(columns)
		var valid bool
		err := s.db.QueryRowContext(ctx, `SELECT count(*)=$2 AND jsonb_object_agg(attname,jsonb_build_array(format_type(atttypid,atttypmod),attnotnull))=$3::jsonb FROM pg_attribute WHERE attrelid=to_regclass($1) AND attnum>0 AND NOT attisdropped`, table, len(columns), string(raw)).Scan(&valid)
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
		updates, _ := json.Marshal(obsFallbackUpdates[table])
		err = s.db.QueryRowContext(ctx, `SELECT has_table_privilege(current_user,$1,'SELECT') AND has_table_privilege(current_user,$1,'INSERT') AND ($1<>'obs_fallback_proposals' OR has_table_privilege(current_user,$1,'DELETE')) AND NOT EXISTS(SELECT 1 FROM pg_attribute a WHERE a.attrelid=to_regclass($1) AND a.attnum>0 AND NOT a.attisdropped AND a.attname IN(SELECT jsonb_array_elements_text($2::jsonb)) AND NOT has_column_privilege(current_user,a.attrelid,a.attnum,'UPDATE'))`, table, string(updates)).Scan(&valid)
		if err != nil || !valid {
			return automationSchemaError(ctx)
		}
		err = s.db.QueryRowContext(ctx, `SELECT NOT EXISTS(SELECT 1 FROM pg_roles r WHERE (r.rolname IN(current_user,session_user) OR pg_has_role(current_user,r.oid,'MEMBER') OR pg_has_role(session_user,r.oid,'MEMBER')) AND
   (r.rolsuper OR r.rolbypassrls OR r.rolcreaterole OR r.rolcreatedb OR has_schema_privilege(r.oid,current_schema(),'CREATE') OR EXISTS(SELECT 1 FROM pg_class c WHERE c.oid=to_regclass($1) AND c.relowner=r.oid) OR has_table_privilege(r.oid,$1,'TRUNCATE,REFERENCES,TRIGGER') OR ($1<>'obs_fallback_proposals' AND has_table_privilege(r.oid,$1,'DELETE')) OR
    EXISTS(SELECT 1 FROM pg_attribute a WHERE a.attrelid=to_regclass($1) AND a.attnum>0 AND NOT a.attisdropped AND (has_column_privilege(r.oid,a.attrelid,a.attnum,'REFERENCES') OR (a.attname NOT IN(SELECT jsonb_array_elements_text($2::jsonb)) AND has_column_privilege(r.oid,a.attrelid,a.attnum,'UPDATE'))))))`, table, string(updates)).Scan(&valid)
		if err != nil || !valid {
			return automationSchemaError(ctx)
		}
		err = s.db.QueryRowContext(ctx, `SELECT NOT EXISTS(SELECT 1 FROM pg_class c CROSS JOIN LATERAL aclexplode(coalesce(c.relacl,acldefault('r',c.relowner))) a WHERE c.oid=to_regclass($1) AND (a.grantee=0 OR (a.is_grantable AND EXISTS(SELECT 1 FROM pg_roles r WHERE r.oid=a.grantee AND (r.rolname IN(current_user,session_user) OR pg_has_role(current_user,r.oid,'MEMBER') OR pg_has_role(session_user,r.oid,'MEMBER')))))) AND NOT EXISTS(SELECT 1 FROM pg_attribute col CROSS JOIN LATERAL aclexplode(col.attacl) a WHERE col.attrelid=to_regclass($1) AND (a.grantee=0 OR (a.is_grantable AND EXISTS(SELECT 1 FROM pg_roles r WHERE r.oid=a.grantee AND (r.rolname IN(current_user,session_user) OR pg_has_role(current_user,r.oid,'MEMBER') OR pg_has_role(session_user,r.oid,'MEMBER'))))))`, table).Scan(&valid)
		if err != nil || !valid {
			return automationSchemaError(ctx)
		}
	}
	for name, c := range obsFallbackConstraints {
		var valid bool
		err := s.db.QueryRowContext(ctx, `SELECT count(*)=1 FROM pg_constraint WHERE conrelid=to_regclass($1) AND conname=$2 AND convalidated AND NOT condeferrable AND pg_get_constraintdef(oid)=$3`, c.table, name, c.definition).Scan(&valid)
		if err != nil || !valid {
			return automationSchemaError(ctx)
		}
	}
	var valid bool
	err := s.db.QueryRowContext(ctx, `SELECT count(*)=1 AND bool_and(i.indisunique AND i.indisvalid AND i.indisready AND i.indrelid=to_regclass('obs_fallback_proposals') AND i.indnkeyatts=1 AND i.indnatts=1 AND i.indkey[0]=(SELECT attnum FROM pg_attribute WHERE attrelid=i.indrelid AND attname='tenant_id') AND pg_get_expr(i.indpred,i.indrelid)=$1) FROM pg_index i WHERE i.indexrelid=to_regclass('obs_fallback_pending')`, `(state = 'pending'::text)`).Scan(&valid)
	if err != nil || !valid {
		return automationSchemaError(ctx)
	}
	return nil
}

var obsFallbackColumns = map[string]map[string]any{
	"obs_fallback_rules":     {"tenant_id": []any{"uuid", true}, "automation_id": []any{"uuid", true}, "version_id": []any{"uuid", true}, "connection_id": []any{"text", true}, "configured_by": []any{"uuid", true}, "enabled": []any{"boolean", true}, "revision": []any{"bigint", true}, "updated_at": []any{"timestamp with time zone", true}, "latest_observation": []any{"text", true}, "latched": []any{"boolean", true}, "active_proposal": []any{"text", true}, "last_proposal_at": []any{"timestamp with time zone", true}, "last_claim_at": []any{"timestamp with time zone", true}},
	"obs_fallback_proposals": {"tenant_id": []any{"uuid", true}, "id": []any{"text", true}, "observation_id": []any{"text", true}, "automation_id": []any{"uuid", true}, "version_id": []any{"uuid", true}, "graph_digest": []any{"text", true}, "connection_id": []any{"text", true}, "document": []any{"text", true}, "observed_at": []any{"timestamp with time zone", true}, "deadline": []any{"timestamp with time zone", true}, "created_at": []any{"timestamp with time zone", true}, "state": []any{"text", true}, "approved_by": []any{"uuid", false}, "request_id": []any{"uuid", false}, "approved_scene": []any{"text", false}, "claim_hash": []any{"bytea", false}, "started_at": []any{"timestamp with time zone", false}, "finished_at": []any{"timestamp with time zone", false}, "failure": []any{"text", true}},
}
var obsFallbackUpdates = map[string][]string{
	"obs_fallback_rules":     {"version_id", "connection_id", "configured_by", "enabled", "revision", "updated_at", "latest_observation", "latched", "active_proposal", "last_proposal_at", "last_claim_at"},
	"obs_fallback_proposals": {"state", "approved_by", "request_id", "approved_scene", "claim_hash", "started_at", "finished_at", "failure"},
}
var obsFallbackConstraints = map[string]struct{ table, definition string }{
	"obs_active_id":                     {"obs_fallback_rules", "CHECK (((active_proposal = ''::text) OR (active_proposal ~ '^[0-9a-f]{64}$'::text)))"},
	"obs_fallback_proposals_pkey":       {"obs_fallback_proposals", "PRIMARY KEY (tenant_id, id)"},
	"obs_fallback_rules_pkey":           {"obs_fallback_rules", "PRIMARY KEY (tenant_id)"},
	"obs_fallback_rules_tenant_id_fkey": {"obs_fallback_rules", "FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE"},
	"obs_latest_bytes":                  {"obs_fallback_rules", "CHECK ((octet_length(latest_observation) <= 8192))"},
	"obs_observation_id":                {"obs_fallback_proposals", "CHECK ((observation_id ~ '^[0-9a-f]{64}$'::text))"},
	"obs_proposal_actor":                {"obs_fallback_proposals", "FOREIGN KEY (tenant_id, approved_by) REFERENCES tenant_memberships(tenant_id, account_id) ON DELETE CASCADE"},
	"obs_proposal_bytes":                {"obs_fallback_proposals", "CHECK (((octet_length(document) >= 1) AND (octet_length(document) <= 8192)))"},
	"obs_proposal_clock":                {"obs_fallback_proposals", "CHECK ((isfinite(observed_at) AND (deadline = (observed_at + '00:00:10'::interval)) AND isfinite(created_at)))"},
	"obs_proposal_connection":           {"obs_fallback_proposals", "CHECK ((connection_id ~ '^[0-9a-f]{64}$'::text))"},
	"obs_proposal_graph":                {"obs_fallback_proposals", "CHECK ((graph_digest ~ '^[0-9a-f]{64}$'::text))"},
	"obs_proposal_header":               {"obs_fallback_proposals", "CHECK (((document)::jsonb @> jsonb_build_object('ID', observation_id, 'RuleVersionID', (version_id)::text, 'GraphDigest', graph_digest, 'ConnectionID', connection_id)))"},
	"obs_proposal_id":                   {"obs_fallback_proposals", "CHECK ((id ~ '^[0-9a-f]{64}$'::text))"},
	"obs_proposal_observation":          {"obs_fallback_proposals", "UNIQUE (tenant_id, observation_id)"},
	"obs_proposal_rule":                 {"obs_fallback_proposals", "FOREIGN KEY (tenant_id, automation_id) REFERENCES obs_fallback_rules(tenant_id, automation_id) ON DELETE CASCADE"},
	"obs_proposal_scene":                {"obs_fallback_proposals", "CHECK (((approved_scene IS NULL) OR ((octet_length(approved_scene) >= 1) AND (octet_length(approved_scene) <= 255))))"},
	"obs_proposal_state":                {"obs_fallback_proposals", "CHECK ((((state = 'pending'::text) AND (approved_by IS NULL) AND (request_id IS NULL) AND (approved_scene IS NULL) AND (claim_hash IS NULL) AND (started_at IS NULL) AND (finished_at IS NULL) AND (failure = ''::text)) OR ((state = ANY (ARRAY['dismissed'::text, 'expired'::text, 'refused'::text])) AND (approved_by IS NULL) AND (request_id IS NULL) AND (approved_scene IS NULL) AND (claim_hash IS NULL) AND (started_at IS NULL) AND (finished_at IS NOT NULL) AND (((state = 'dismissed'::text) AND (failure = 'dismissed'::text)) OR ((state = 'expired'::text) AND (failure = 'expired'::text)) OR ((state = 'refused'::text) AND (failure = ANY (ARRAY['rule_changed'::text, 'condition_cleared'::text]))))) OR ((state = 'running'::text) AND (approved_by IS NOT NULL) AND (request_id IS NOT NULL) AND (approved_scene IS NOT NULL) AND (claim_hash IS NOT NULL) AND (octet_length(claim_hash) = 32) AND (started_at IS NOT NULL) AND (finished_at IS NULL) AND (failure = ''::text)) OR ((state = ANY (ARRAY['succeeded'::text, 'refused'::text, 'uncertain'::text])) AND (approved_by IS NOT NULL) AND (request_id IS NOT NULL) AND (approved_scene IS NOT NULL) AND (claim_hash IS NOT NULL) AND (octet_length(claim_hash) = 32) AND (started_at IS NOT NULL) AND (finished_at IS NOT NULL) AND (((state = 'succeeded'::text) AND (failure = ''::text)) OR ((state = 'refused'::text) AND (failure = 'guard_refused'::text)) OR ((state = 'uncertain'::text) AND (failure = ANY (ARRAY['obs_unavailable'::text, 'interrupted'::text, 'completion_unknown'::text])))))))"},
	"obs_proposal_version":              {"obs_fallback_proposals", "FOREIGN KEY (tenant_id, automation_id, version_id) REFERENCES engine_automation_versions(tenant_id, automation_id, id) ON DELETE CASCADE"},
	"obs_receipt_clocks":                {"obs_fallback_proposals", "CHECK ((((started_at IS NULL) OR isfinite(started_at)) AND ((finished_at IS NULL) OR isfinite(finished_at)) AND ((started_at IS NULL) OR (finished_at IS NULL) OR (finished_at >= started_at)) AND ((started_at IS NULL) OR ((started_at >= observed_at) AND (started_at < deadline)))))"},
	"obs_rule_actor":                    {"obs_fallback_rules", "FOREIGN KEY (tenant_id, configured_by) REFERENCES tenant_memberships(tenant_id, account_id) ON DELETE CASCADE"},
	"obs_rule_automation":               {"obs_fallback_rules", "UNIQUE (tenant_id, automation_id)"},
	"obs_rule_clocks":                   {"obs_fallback_rules", "CHECK ((isfinite(updated_at) AND isfinite(last_proposal_at) AND isfinite(last_claim_at)))"},
	"obs_rule_connection":               {"obs_fallback_rules", "CHECK ((connection_id ~ '^[0-9a-f]{64}$'::text))"},
	"obs_rule_revision":                 {"obs_fallback_rules", "CHECK (((revision >= 1) AND (revision <= '9007199254740991'::bigint)))"},
	"obs_rule_version":                  {"obs_fallback_rules", "FOREIGN KEY (tenant_id, automation_id, version_id) REFERENCES engine_automation_versions(tenant_id, automation_id, id) ON DELETE CASCADE"},
}
