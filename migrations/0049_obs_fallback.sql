-- Managed OBS fallback rules and non-replayable operator proposals.
-- No observer/executor is activated by this additive schema alone.
BEGIN;
CREATE TABLE IF NOT EXISTS obs_fallback_rules (
 tenant_id uuid PRIMARY KEY DEFAULT nullif(current_setting('app.tenant_id',true),'')::uuid REFERENCES tenants(id) ON DELETE CASCADE,
 automation_id uuid NOT NULL,
 version_id uuid NOT NULL,
 connection_id text NOT NULL CONSTRAINT obs_rule_connection CHECK(connection_id ~ '^[0-9a-f]{64}$'),
 configured_by uuid NOT NULL,
 enabled boolean NOT NULL,
 revision bigint NOT NULL DEFAULT 1 CONSTRAINT obs_rule_revision CHECK(revision BETWEEN 1 AND 9007199254740991),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 latest_observation text NOT NULL DEFAULT '' CONSTRAINT obs_latest_bytes CHECK(octet_length(latest_observation)<=8192),
 latched boolean NOT NULL DEFAULT false,
 active_proposal text NOT NULL DEFAULT '' CONSTRAINT obs_active_id CHECK(active_proposal='' OR active_proposal ~ '^[0-9a-f]{64}$'),
 last_proposal_at timestamptz NOT NULL DEFAULT 'epoch',
 last_claim_at timestamptz NOT NULL DEFAULT 'epoch',
 CONSTRAINT obs_rule_automation UNIQUE(tenant_id,automation_id),
 CONSTRAINT obs_rule_version FOREIGN KEY(tenant_id,automation_id,version_id) REFERENCES engine_automation_versions(tenant_id,automation_id,id) ON DELETE CASCADE,
 CONSTRAINT obs_rule_actor FOREIGN KEY(tenant_id,configured_by) REFERENCES tenant_memberships(tenant_id,account_id) ON DELETE CASCADE,
 CONSTRAINT obs_rule_clocks CHECK(isfinite(updated_at) AND isfinite(last_proposal_at) AND isfinite(last_claim_at))
);
CREATE TABLE IF NOT EXISTS obs_fallback_proposals (
 tenant_id uuid NOT NULL DEFAULT nullif(current_setting('app.tenant_id',true),'')::uuid,
 observation_id text NOT NULL CONSTRAINT obs_observation_id CHECK(observation_id ~ '^[0-9a-f]{64}$'),
 id text NOT NULL CONSTRAINT obs_proposal_id CHECK(id ~ '^[0-9a-f]{64}$'),
 automation_id uuid NOT NULL,
 version_id uuid NOT NULL,
 graph_digest text NOT NULL CONSTRAINT obs_proposal_graph CHECK(graph_digest ~ '^[0-9a-f]{64}$'),
 connection_id text NOT NULL CONSTRAINT obs_proposal_connection CHECK(connection_id ~ '^[0-9a-f]{64}$'),
 document text NOT NULL CONSTRAINT obs_proposal_bytes CHECK(octet_length(document) BETWEEN 1 AND 8192),
 observed_at timestamptz NOT NULL,
 deadline timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 state text NOT NULL DEFAULT 'pending',
 approved_by uuid,
 request_id uuid,
 approved_scene text,
 claim_hash bytea,
 started_at timestamptz,
 finished_at timestamptz,
 failure text NOT NULL DEFAULT '',
 PRIMARY KEY(tenant_id,id),
 CONSTRAINT obs_proposal_observation UNIQUE(tenant_id,observation_id),
 CONSTRAINT obs_proposal_rule FOREIGN KEY(tenant_id,automation_id) REFERENCES obs_fallback_rules(tenant_id,automation_id) ON DELETE CASCADE,
 CONSTRAINT obs_proposal_version FOREIGN KEY(tenant_id,automation_id,version_id) REFERENCES engine_automation_versions(tenant_id,automation_id,id) ON DELETE CASCADE,
 CONSTRAINT obs_proposal_actor FOREIGN KEY(tenant_id,approved_by) REFERENCES tenant_memberships(tenant_id,account_id) ON DELETE CASCADE,
 CONSTRAINT obs_proposal_clock CHECK(isfinite(observed_at) AND deadline=observed_at+interval '10 seconds' AND isfinite(created_at)),
 CONSTRAINT obs_proposal_header CHECK(document::jsonb @> jsonb_build_object('ID',observation_id,'RuleVersionID',version_id::text,'GraphDigest',graph_digest,'ConnectionID',connection_id)),
 CONSTRAINT obs_receipt_clocks CHECK((started_at IS NULL OR isfinite(started_at)) AND (finished_at IS NULL OR isfinite(finished_at)) AND (started_at IS NULL OR finished_at IS NULL OR finished_at>=started_at) AND (started_at IS NULL OR (started_at>=observed_at AND started_at<deadline))),
 CONSTRAINT obs_proposal_scene CHECK(approved_scene IS NULL OR (octet_length(approved_scene) BETWEEN 1 AND 255)),
 CONSTRAINT obs_proposal_state CHECK(
  (state='pending' AND approved_by IS NULL AND request_id IS NULL AND approved_scene IS NULL AND claim_hash IS NULL AND started_at IS NULL AND finished_at IS NULL AND failure='') OR
  (state IN ('dismissed','expired','refused') AND approved_by IS NULL AND request_id IS NULL AND approved_scene IS NULL AND claim_hash IS NULL AND started_at IS NULL AND finished_at IS NOT NULL AND ((state='dismissed' AND failure='dismissed') OR (state='expired' AND failure='expired') OR (state='refused' AND failure IN ('rule_changed','condition_cleared')))) OR
  (state='running' AND approved_by IS NOT NULL AND request_id IS NOT NULL AND approved_scene IS NOT NULL AND claim_hash IS NOT NULL AND octet_length(claim_hash)=32 AND started_at IS NOT NULL AND finished_at IS NULL AND failure='') OR
  (state IN ('succeeded','refused','uncertain') AND approved_by IS NOT NULL AND request_id IS NOT NULL AND approved_scene IS NOT NULL AND claim_hash IS NOT NULL AND octet_length(claim_hash)=32 AND started_at IS NOT NULL AND finished_at IS NOT NULL AND ((state='succeeded' AND failure='') OR (state='refused' AND failure='guard_refused') OR (state='uncertain' AND failure IN ('obs_unavailable','interrupted','completion_unknown'))))
 )
);
CREATE UNIQUE INDEX IF NOT EXISTS obs_fallback_pending ON obs_fallback_proposals(tenant_id) WHERE state='pending';
DO $setup$ DECLARE tab text; BEGIN
 FOREACH tab IN ARRAY ARRAY['obs_fallback_rules','obs_fallback_proposals'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',tab);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',tab);
  EXECUTE format('DROP POLICY IF EXISTS tenant_isolation ON %I',tab);
  EXECUTE format('CREATE POLICY tenant_isolation ON %I TO overlaid_runtime USING(tenant_id=nullif(current_setting(''app.tenant_id'',true),'''')::uuid) WITH CHECK(tenant_id=nullif(current_setting(''app.tenant_id'',true),'''')::uuid)',tab);
  EXECUTE format('REVOKE ALL ON %I FROM PUBLIC,overlaid_runtime',tab);
 END LOOP;
END $setup$;
REVOKE ALL(tenant_id,automation_id,version_id,connection_id,configured_by,enabled,revision,updated_at,latest_observation,latched,active_proposal,last_proposal_at,last_claim_at) ON obs_fallback_rules FROM PUBLIC,overlaid_runtime;
REVOKE ALL(tenant_id,id,observation_id,automation_id,version_id,graph_digest,connection_id,document,observed_at,deadline,created_at,state,approved_by,request_id,approved_scene,claim_hash,started_at,finished_at,failure) ON obs_fallback_proposals FROM PUBLIC,overlaid_runtime;
GRANT SELECT,INSERT ON obs_fallback_rules,obs_fallback_proposals TO overlaid_runtime;
GRANT UPDATE(version_id,connection_id,configured_by,enabled,revision,updated_at,latest_observation,latched,active_proposal,last_proposal_at,last_claim_at) ON obs_fallback_rules TO overlaid_runtime;
GRANT UPDATE(state,approved_by,request_id,approved_scene,claim_hash,started_at,finished_at,failure),DELETE ON obs_fallback_proposals TO overlaid_runtime;
INSERT INTO overlaid_migrations(name) VALUES('0049_obs_fallback.sql') ON CONFLICT DO NOTHING;
COMMIT;
