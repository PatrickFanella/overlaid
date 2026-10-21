-- Explicit operator admission only. No executor, HTTP route or queue activation.
BEGIN;
CREATE TABLE IF NOT EXISTS engine_operator_occurrences (
 tenant_id uuid NOT NULL DEFAULT nullif(current_setting('app.tenant_id',true),'')::uuid REFERENCES tenants(id) ON DELETE CASCADE,
 id text NOT NULL CONSTRAINT engine_occurrence_id CHECK(id ~ '^[0-9a-f]{64}$'),
 automation_id uuid NOT NULL, version_id uuid NOT NULL,
 graph_digest text NOT NULL CONSTRAINT engine_occurrence_graph CHECK(graph_digest ~ '^[0-9a-f]{64}$'),
 run_id text NOT NULL CONSTRAINT engine_occurrence_run CHECK(run_id ~ '^[0-9a-f]{64}$'),
 created_at timestamptz NOT NULL, deadline timestamptz NOT NULL,
 CONSTRAINT engine_occurrence_deadline CHECK(isfinite(created_at) AND deadline=created_at+interval '2 minutes'),
 PRIMARY KEY(tenant_id,id),
 CONSTRAINT engine_occurrence_binding UNIQUE(tenant_id,id,automation_id,version_id,graph_digest,run_id)
);
CREATE TABLE IF NOT EXISTS engine_operator_runs (
 tenant_id uuid NOT NULL DEFAULT nullif(current_setting('app.tenant_id',true),'')::uuid,
 occurrence_id text NOT NULL, automation_id uuid NOT NULL, version_id uuid NOT NULL,
 graph_digest text NOT NULL, id text NOT NULL, actor_account_id uuid NOT NULL,
 PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,occurrence_id),
 CONSTRAINT engine_run_occurrence FOREIGN KEY(tenant_id,occurrence_id,automation_id,version_id,graph_digest,id)
  REFERENCES engine_operator_occurrences(tenant_id,id,automation_id,version_id,graph_digest,run_id) ON DELETE CASCADE,
 CONSTRAINT engine_run_version FOREIGN KEY(tenant_id,automation_id,version_id)
  REFERENCES engine_automation_versions(tenant_id,automation_id,id) ON DELETE CASCADE,
 CONSTRAINT engine_run_actor FOREIGN KEY(tenant_id,actor_account_id)
  REFERENCES tenant_memberships(tenant_id,account_id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS engine_run_budgets (
 tenant_id uuid NOT NULL DEFAULT nullif(current_setting('app.tenant_id',true),'')::uuid,
 automation_id uuid NOT NULL, charged_at timestamptz NOT NULL, cooldown_until timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,automation_id),
 CONSTRAINT engine_budget_automation FOREIGN KEY(tenant_id,automation_id) REFERENCES engine_automations(tenant_id,id) ON DELETE CASCADE,
 CONSTRAINT engine_budget_interval CHECK(isfinite(charged_at) AND isfinite(cooldown_until) AND cooldown_until>=charged_at AND cooldown_until<=charged_at+interval '24 hours')
);
DO $setup$ DECLARE tab text; BEGIN
 FOREACH tab IN ARRAY ARRAY['engine_operator_occurrences','engine_operator_runs','engine_run_budgets'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',tab);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',tab);
  EXECUTE format('DROP POLICY IF EXISTS tenant_isolation ON %I',tab);
  EXECUTE format('CREATE POLICY tenant_isolation ON %I TO overlaid_runtime USING(tenant_id=nullif(current_setting(''app.tenant_id'',true),'''')::uuid) WITH CHECK(tenant_id=nullif(current_setting(''app.tenant_id'',true),'''')::uuid)',tab);
  EXECUTE format('REVOKE ALL ON %I FROM PUBLIC,overlaid_runtime',tab);
 END LOOP;
END $setup$;
REVOKE ALL(tenant_id,id,automation_id,version_id,graph_digest,run_id,created_at,deadline) ON engine_operator_occurrences FROM PUBLIC,overlaid_runtime;
REVOKE ALL(tenant_id,occurrence_id,automation_id,version_id,graph_digest,id,actor_account_id) ON engine_operator_runs FROM PUBLIC,overlaid_runtime;
REVOKE ALL(tenant_id,automation_id,charged_at,cooldown_until) ON engine_run_budgets FROM PUBLIC,overlaid_runtime;
GRANT SELECT,INSERT ON engine_operator_occurrences,engine_operator_runs,engine_run_budgets TO overlaid_runtime;
GRANT DELETE ON engine_operator_runs TO overlaid_runtime;
GRANT UPDATE(charged_at,cooldown_until) ON engine_run_budgets TO overlaid_runtime;
INSERT INTO overlaid_migrations(name) VALUES('0041_operator_automation_runs.sql') ON CONFLICT DO NOTHING;
COMMIT;
