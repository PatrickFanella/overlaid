package conduitownership

import (
	"context"
	"database/sql"
	_ "embed"
)

// PostgreSQL 17 canonical columns, constraints and indexes of migration 0037.
// Role names are checked independently for isolated fixtures.
//
//go:embed schema.txt
var schemaText string

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func schemaDefinition(ctx context.Context, q queryer) (string, error) {
	var definition string
	err := q.QueryRowContext(ctx, `WITH tables AS(SELECT c.oid,c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relname='service_conduit_creations' AND c.relkind='r'), lines AS(
 SELECT t.relname||':column:'||lpad(a.attnum::text,5,'0') AS ordering,t.relname||':'||a.attname||':'||format_type(a.atttypid,a.atttypmod)||':'||a.attnotnull::text||':'||coalesce(pg_get_expr(d.adbin,d.adrelid),'')||':'||coalesce((SELECT quote_ident(n.nspname)||'.'||quote_ident(c.collname) FROM pg_collation c JOIN pg_namespace n ON n.oid=c.collnamespace WHERE c.oid=a.attcollation),'') AS line FROM tables t JOIN pg_attribute a ON a.attrelid=t.oid AND a.attnum>0 AND NOT a.attisdropped LEFT JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum
 UNION ALL SELECT t.relname||':constraint:'||c.conname,t.relname||':'||c.conname||':'||c.convalidated::text||':'||pg_get_constraintdef(c.oid) FROM tables t JOIN pg_constraint c ON c.conrelid=t.oid
 UNION ALL SELECT t.relname||':index:'||c.relname,t.relname||':index:'||c.relname||':'||i.indisvalid::text||':'||i.indisready::text||':'||replace(pg_get_indexdef(i.indexrelid),quote_ident(current_schema())||'.','') FROM tables t JOIN pg_index i ON i.indrelid=t.oid JOIN pg_class c ON c.oid=i.indexrelid)
 SELECT coalesce(string_agg(line,E'\n' ORDER BY ordering),'') FROM lines`).Scan(&definition)
	return definition, err
}

func validate(ctx context.Context, q queryer) error {
	var safe bool
	err := q.QueryRowContext(ctx, `WITH tables AS(SELECT c.* FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relname='service_conduit_creations' AND c.relkind='r'), reachable AS(SELECT r.* FROM pg_roles r WHERE r.rolname IN(current_user,session_user) OR pg_has_role(current_user,r.oid,'MEMBER') OR pg_has_role(session_user,r.oid,'MEMBER')), policies AS(SELECT p.* FROM pg_policy p JOIN tables t ON t.oid=p.polrelid), allowed AS(SELECT DISTINCT unnest(polroles) AS oid FROM policies)
 SELECT (SELECT count(*)=1 AND bool_and(relrowsecurity AND relforcerowsecurity AND NOT relhasrules AND NOT EXISTS(SELECT 1 FROM pg_trigger tr WHERE tr.tgrelid=t.oid AND NOT tr.tgisinternal)) FROM tables t)
 AND (SELECT count(*)=1 AND bool_and(polname='service_conduit_admin' AND polpermissive AND polcmd='*' AND cardinality(polroles)=1 AND polroles[1]<>0 AND pg_get_expr(polqual,polrelid)='true' AND pg_get_expr(polwithcheck,polrelid)='true') FROM policies)
 AND EXISTS(SELECT 1 FROM reachable r WHERE r.oid IN(SELECT oid FROM allowed))
 AND NOT EXISTS(SELECT 1 FROM reachable r WHERE r.rolsuper OR r.rolbypassrls OR r.rolcreaterole OR r.rolcreatedb OR r.rolreplication OR has_database_privilege(r.oid,current_database(),'CREATE') OR has_schema_privilege(r.oid,current_schema(),'CREATE') OR EXISTS(SELECT 1 FROM tables t WHERE t.relowner=r.oid) OR (r.rolname NOT IN(current_user,session_user) AND r.oid NOT IN(SELECT oid FROM allowed)))
 AND NOT EXISTS(SELECT 1 FROM pg_auth_members m JOIN reachable r ON r.oid=m.member WHERE m.admin_option)
 AND NOT EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace,reachable r WHERE n.nspname NOT LIKE 'pg_%' AND n.nspname<>'information_schema' AND c.relkind IN('r','p','v','m','f') AND has_schema_privilege(r.oid,n.oid,'USAGE') AND c.oid NOT IN(SELECT oid FROM tables) AND (has_table_privilege(r.oid,c.oid,'SELECT,INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER') OR has_any_column_privilege(r.oid,c.oid,'SELECT,INSERT,UPDATE,REFERENCES')))
 AND NOT EXISTS(SELECT 1 FROM pg_proc f JOIN pg_namespace n ON n.oid=f.pronamespace,reachable r WHERE f.prosecdef AND n.nspname NOT LIKE 'pg_%' AND n.nspname<>'information_schema' AND has_schema_privilege(r.oid,n.oid,'USAGE') AND has_function_privilege(r.oid,f.oid,'EXECUTE'))
 AND NOT EXISTS(SELECT 1 FROM tables t,reachable r WHERE has_table_privilege(r.oid,t.oid,'TRUNCATE,REFERENCES,TRIGGER') OR has_table_privilege(r.oid,t.oid,'SELECT WITH GRANT OPTION,INSERT WITH GRANT OPTION,UPDATE WITH GRANT OPTION,DELETE WITH GRANT OPTION,TRUNCATE WITH GRANT OPTION,REFERENCES WITH GRANT OPTION,TRIGGER WITH GRANT OPTION') OR has_any_column_privilege(r.oid,t.oid,'SELECT WITH GRANT OPTION,INSERT WITH GRANT OPTION,UPDATE WITH GRANT OPTION,REFERENCES WITH GRANT OPTION') OR EXISTS(SELECT 1 FROM pg_attribute a WHERE a.attrelid=t.oid AND a.attnum>0 AND NOT a.attisdropped AND (has_column_privilege(r.oid,t.oid,a.attnum,'REFERENCES') OR (a.attname NOT IN('app_id','client_id','app_revision','shard_count') AND has_column_privilege(r.oid,t.oid,a.attnum,'INSERT')) OR (a.attname NOT IN('state','claim_hash','started_at','completed_at','failure_code','conduit_id') AND has_column_privilege(r.oid,t.oid,a.attnum,'UPDATE')))))
 AND (SELECT bool_and(has_table_privilege(current_user,t.oid,'SELECT') AND has_table_privilege(current_user,t.oid,'DELETE') AND (SELECT bool_and(has_column_privilege(current_user,t.oid,a.attnum,'INSERT')) FROM pg_attribute a WHERE a.attrelid=t.oid AND a.attname IN('app_id','client_id','app_revision','shard_count')) AND (SELECT bool_and(has_column_privilege(current_user,t.oid,a.attnum,'UPDATE')) FROM pg_attribute a WHERE a.attrelid=t.oid AND a.attname IN('state','claim_hash','started_at','completed_at','failure_code','conduit_id'))) FROM tables t)`).Scan(&safe)
	if err != nil || !safe {
		return ErrBoundary
	}
	definition, err := schemaDefinition(ctx, q)
	if err != nil || definition+"\n" != schemaText {
		return ErrBoundary
	}
	return nil
}
