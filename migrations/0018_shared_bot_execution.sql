BEGIN;
DO $setup$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='overlaid_bot_executor') THEN
  CREATE ROLE overlaid_bot_executor NOLOGIN NOSUPERUSER NOBYPASSRLS;
 END IF;
 EXECUTE format('GRANT USAGE ON SCHEMA %I TO overlaid_bot_executor',current_schema());
END $setup$;
-- No tenant, queue, pacing, grant-management or publication privileges.
GRANT SELECT ON service_bot_accounts,service_bot_catalog TO overlaid_bot_executor;
GRANT UPDATE(ciphertext,key_version,login,scopes) ON service_bot_accounts TO overlaid_bot_executor;
CREATE POLICY catalog_executor ON service_bot_catalog FOR SELECT TO overlaid_bot_executor USING(available);
CREATE POLICY service_bot_executor ON service_bot_accounts TO overlaid_bot_executor
 USING(revoked_at IS NULL AND id IN(SELECT id FROM service_bot_catalog))
 WITH CHECK(revoked_at IS NULL AND id IN(SELECT id FROM service_bot_catalog));
-- One fixed-field rolling receipt per bounded service account, not an
-- unbounded provider-traffic log. Operator grant/publication audit is separate.
CREATE TABLE service_bot_execution_receipts (
 bot_account_id uuid PRIMARY KEY REFERENCES service_bot_accounts(id) ON DELETE CASCADE,
 revision bigint NOT NULL CHECK(revision>0), catalog_revision bigint NOT NULL CHECK(catalog_revision>0),
 refreshes bigint NOT NULL DEFAULT 1 CHECK(refreshes>0),
 principal text NOT NULL DEFAULT session_user, refreshed_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE FUNCTION stamp_service_bot_execution_receipt() RETURNS trigger LANGUAGE plpgsql SET search_path FROM CURRENT AS $body$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM service_bot_accounts a JOIN service_bot_catalog c ON c.id=a.id
 WHERE a.id=NEW.bot_account_id AND a.revoked_at IS NULL AND c.available
 AND a.revision=NEW.revision AND c.grant_revision=NEW.revision AND c.revision=NEW.catalog_revision) THEN
  RAISE EXCEPTION 'service bot refresh receipt fenced';
 END IF;
 NEW.principal := session_user;
 NEW.refreshed_at := clock_timestamp();
 IF TG_OP='UPDATE' THEN NEW.refreshes := OLD.refreshes+1; ELSE NEW.refreshes := 1; END IF;
 RETURN NEW;
END $body$;
DO $path$ BEGIN
 EXECUTE format('ALTER FUNCTION stamp_service_bot_execution_receipt() SET search_path = %I, pg_catalog, pg_temp',current_schema());
END $path$;
CREATE TRIGGER service_bot_execution_receipt_stamp BEFORE INSERT OR UPDATE ON service_bot_execution_receipts
 FOR EACH ROW EXECUTE FUNCTION stamp_service_bot_execution_receipt();
ALTER TABLE service_bot_execution_receipts ENABLE ROW LEVEL SECURITY;
ALTER TABLE service_bot_execution_receipts FORCE ROW LEVEL SECURITY;
CREATE POLICY execution_receipt_executor ON service_bot_execution_receipts TO overlaid_bot_executor USING(true) WITH CHECK(true);
CREATE POLICY execution_receipt_admin ON service_bot_execution_receipts FOR SELECT TO overlaid_bot_admin USING(true);
GRANT SELECT ON service_bot_execution_receipts TO overlaid_bot_executor,overlaid_bot_admin;
GRANT INSERT(bot_account_id,revision,catalog_revision),UPDATE(revision,catalog_revision) ON service_bot_execution_receipts TO overlaid_bot_executor;
INSERT INTO overlaid_migrations(name) VALUES('0018_shared_bot_execution.sql') ON CONFLICT DO NOTHING;
COMMIT;
