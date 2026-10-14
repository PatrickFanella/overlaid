BEGIN;
CREATE TABLE IF NOT EXISTS newsroom_documents (
  tenant_id uuid NOT NULL DEFAULT nullif(current_setting('app.tenant_id',true),'')::uuid REFERENCES tenants(id) ON DELETE CASCADE,
  id text NOT NULL,
  document jsonb NOT NULL,
  ciphertext bytea,
  key_version text,
  token_hash text UNIQUE,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY(tenant_id,id)
);
ALTER TABLE newsroom_documents ENABLE ROW LEVEL SECURITY;
ALTER TABLE newsroom_documents FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON newsroom_documents;
CREATE POLICY tenant_isolation ON newsroom_documents
  USING(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid)
  WITH CHECK(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);
GRANT SELECT,INSERT,UPDATE,DELETE ON newsroom_documents TO overlaid_runtime;
-- Narrow lookup for independently revocable add/share capabilities. The
-- runtime role still sees product rows only through the forced tenant policy.
DO $setup$ BEGIN
  EXECUTE format('GRANT USAGE ON SCHEMA %I TO overlaid_credential_lookup',current_schema());
  GRANT SELECT ON newsroom_documents TO overlaid_credential_lookup;
  DROP POLICY IF EXISTS capability_lookup ON newsroom_documents;
  CREATE POLICY capability_lookup ON newsroom_documents FOR SELECT TO overlaid_credential_lookup USING(true);
  EXECUTE format('CREATE OR REPLACE FUNCTION resolve_newsroom_capability(hash text,kind text) RETURNS uuid LANGUAGE sql SECURITY DEFINER SET search_path=%I,pg_temp AS $fn$ SELECT tenant_id FROM %I.newsroom_documents WHERE token_hash=hash AND ((kind=''add'' AND id=''add'') OR (kind=''share'' AND id LIKE ''share:%%'')) $fn$',current_schema(),current_schema());
  ALTER FUNCTION resolve_newsroom_capability(text,text) OWNER TO overlaid_credential_lookup;
END $setup$;
REVOKE ALL ON FUNCTION resolve_newsroom_capability(text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION resolve_newsroom_capability(text,text) TO overlaid_runtime;
COMMIT;
