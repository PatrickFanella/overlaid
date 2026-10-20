BEGIN;
-- Stop old administrative clients before adding the separate read policy.
-- This grants no mutation, acquisition, send or startup permission.
DO $setup$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='overlaid_app_execution') THEN
  CREATE ROLE overlaid_app_execution NOLOGIN NOSUPERUSER NOBYPASSRLS NOCREATEROLE NOCREATEDB NOREPLICATION;
 END IF;
 EXECUTE format('GRANT USAGE ON SCHEMA %I TO overlaid_app_execution',current_schema());
END $setup$;
CREATE POLICY service_app_execution ON service_app_credentials FOR SELECT TO overlaid_app_execution USING(revoked_at IS NULL);
GRANT SELECT(id,provider,client_id,revision,token_ciphertext,key_version,validated_at,expires_at,revoked_at,created_at)
 ON service_app_credentials TO overlaid_app_execution;
INSERT INTO overlaid_migrations(name) VALUES('0035_service_app_execution.sql') ON CONFLICT DO NOTHING;
COMMIT;
