BEGIN;
-- Refreshing ciphertext does not change sender selection. Binding, connection
-- reauthorization and revocation do, including an A -> B -> A binding cycle.
ALTER TABLE platform_connections ADD COLUMN sender_revision bigint NOT NULL DEFAULT 1 CHECK(sender_revision>0);
CREATE FUNCTION advance_sender_revision() RETURNS trigger LANGUAGE plpgsql AS $body$
BEGIN
 NEW.sender_revision := OLD.sender_revision;
 IF NEW.bot_account_id IS DISTINCT FROM OLD.bot_account_id
 OR NEW.provider_user_id IS DISTINCT FROM OLD.provider_user_id
 OR NEW.granted_at IS DISTINCT FROM OLD.granted_at
 OR NEW.revoked_at IS DISTINCT FROM OLD.revoked_at THEN
  NEW.sender_revision := OLD.sender_revision+1;
 END IF;
 RETURN NEW;
END $body$;
CREATE TRIGGER platform_sender_revision BEFORE UPDATE ON platform_connections
 FOR EACH ROW EXECUTE FUNCTION advance_sender_revision();
INSERT INTO overlaid_migrations(name) VALUES('0017_sender_selection.sql') ON CONFLICT DO NOTHING;
COMMIT;
