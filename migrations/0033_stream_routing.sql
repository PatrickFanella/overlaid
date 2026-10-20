BEGIN;
-- Stop all writers before applying. No sources/jobs/receipts are rewritten.
CREATE FUNCTION fence_stream_dispatch_mode() RETURNS trigger LANGUAGE plpgsql SET search_path FROM CURRENT AS $body$
DECLARE
 protected_old boolean := false;
 protected_new boolean := false;
 enabled boolean;
BEGIN
 IF TG_OP <> 'INSERT' THEN
  protected_old := OLD.tenant_id='00000000-0000-4000-8000-000000000001'::uuid AND OLD.key IN('stream_dispatch_mode','chat_dispatch_mode');
 END IF;
 IF TG_OP <> 'DELETE' THEN
  protected_new := NEW.tenant_id='00000000-0000-4000-8000-000000000001'::uuid AND NEW.key IN('stream_dispatch_mode','chat_dispatch_mode');
 END IF;
 IF NOT protected_old AND NOT protected_new THEN
  IF TG_OP='DELETE' THEN RETURN OLD; END IF;
  RETURN NEW;
 END IF;
 SELECT EXISTS(SELECT 1 FROM app_metadata WHERE tenant_id='00000000-0000-4000-8000-000000000001'::uuid AND key='stream_dispatch_mode') INTO enabled;
 IF (protected_old AND OLD.key='stream_dispatch_mode') OR (protected_new AND NEW.key='stream_dispatch_mode') OR enabled THEN
  IF TG_OP='DELETE' OR (protected_old AND NOT protected_new) OR
   (TG_OP='UPDATE' AND (OLD.tenant_id IS DISTINCT FROM NEW.tenant_id OR OLD.key IS DISTINCT FROM NEW.key)) THEN
   RAISE EXCEPTION 'retained stream compatibility fence';
  END IF;
  IF current_setting('app.stream_dispatch_mode',true) IS DISTINCT FROM 'retained-v1' OR
   (NEW.key='stream_dispatch_mode' AND NEW.value IS DISTINCT FROM 'retained-v1') OR
   (NEW.key='chat_dispatch_mode' AND NEW.value IS DISTINCT FROM 'retained') THEN
   RAISE EXCEPTION 'retained stream compatibility fence';
  END IF;
 END IF;
 RETURN NEW;
END $body$;
DO $pin$ BEGIN
 EXECUTE format('ALTER FUNCTION fence_stream_dispatch_mode() SET search_path = %I, pg_catalog, pg_temp',current_schema());
END $pin$;
CREATE TRIGGER retained_stream_mode BEFORE INSERT OR UPDATE OR DELETE ON app_metadata
 FOR EACH ROW EXECUTE FUNCTION fence_stream_dispatch_mode();
INSERT INTO overlaid_migrations(name) VALUES('0033_stream_routing.sql') ON CONFLICT DO NOTHING;
COMMIT;
