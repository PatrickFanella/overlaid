BEGIN;
-- Episode tokens authorize only the allowlisted public episode projection.
DO $setup$ BEGIN
  EXECUTE format('CREATE OR REPLACE FUNCTION resolve_newsroom_capability(hash text,kind text) RETURNS uuid LANGUAGE sql SECURITY DEFINER SET search_path=%I,pg_temp AS $fn$ SELECT tenant_id FROM %I.newsroom_documents WHERE token_hash=hash AND ((kind=''add'' AND id=''add'') OR (kind=''share'' AND id LIKE ''share:%%'') OR (kind=''episode'' AND id LIKE ''episode:%%'')) $fn$',current_schema(),current_schema());
  ALTER FUNCTION resolve_newsroom_capability(text,text) OWNER TO overlaid_credential_lookup;
END $setup$;
REVOKE ALL ON FUNCTION resolve_newsroom_capability(text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION resolve_newsroom_capability(text,text) TO overlaid_runtime;
COMMIT;
