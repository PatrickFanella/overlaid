package tenantdb

import (
	"context"
	"errors"
)

// Keep the reviewed trigger body exact. It is a startup compatibility fence,
// not an authorization credential; runtime code alone sets the local mode flag.
const streamModeGuardBody = `
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
END `

// ValidateStreamMode repeats the pinned invoker-trigger contract without writes.
func (d *DB) ValidateStreamMode(ctx context.Context) error {
	if !d.IsScoped() {
		return errors.New("retained stream compatibility fence unavailable")
	}
	var valid bool
	err := d.raw.QueryRowContext(ctx, `SELECT count(*)=1 FROM pg_trigger t JOIN pg_proc p ON p.oid=t.tgfoid JOIN pg_namespace n ON n.oid=p.pronamespace JOIN pg_language l ON l.oid=p.prolang JOIN pg_class c ON c.oid=t.tgrelid WHERE t.tgrelid='app_metadata'::regclass AND t.tgname='retained_stream_mode' AND t.tgtype=31 AND t.tgenabled='O' AND NOT t.tgisinternal AND t.tgqual IS NULL AND t.tgnargs=0 AND n.nspname=current_schema() AND p.proname='fence_stream_dispatch_mode' AND p.pronargs=0 AND p.prorettype='trigger'::regtype AND NOT p.prosecdef AND p.provolatile='v' AND p.proowner=c.relowner AND l.lanname='plpgsql' AND p.prosrc=$1 AND p.proconfig=ARRAY[format('search_path=%I, pg_catalog, pg_temp',current_schema())]`, streamModeGuardBody).Scan(&valid)
	if err != nil || !valid {
		return errors.New("retained stream compatibility fence unavailable")
	}
	return nil
}
