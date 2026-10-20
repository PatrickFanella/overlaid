package appvault

import (
	"context"
	"database/sql"
	_ "embed"
)

// schema.txt is the reviewed PostgreSQL 17 canonical shape of migration 0034.
// Roles are checked separately so disposable fixture role names remain valid.
//
//go:embed schema.txt
var schemaText string

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func schemaDefinition(ctx context.Context, q queryer) (string, error) {
	var definition string
	err := q.QueryRowContext(ctx, `WITH tables AS(SELECT c.oid,c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relname IN('service_app_credentials','service_app_credential_audit') AND c.relkind='r'), lines AS(
 SELECT t.relname||':column:'||lpad(a.attnum::text,5,'0') AS ordering,t.relname||':'||a.attname||':'||format_type(a.atttypid,a.atttypmod)||':'||a.attnotnull::text||':'||coalesce(pg_get_expr(d.adbin,d.adrelid),'')||':'||coalesce((SELECT quote_ident(n.nspname)||'.'||quote_ident(c.collname) FROM pg_collation c JOIN pg_namespace n ON n.oid=c.collnamespace WHERE c.oid=a.attcollation),'') AS line FROM tables t JOIN pg_attribute a ON a.attrelid=t.oid AND a.attnum>0 AND NOT a.attisdropped LEFT JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum
 UNION ALL SELECT t.relname||':constraint:'||c.conname,t.relname||':'||c.conname||':'||c.convalidated::text||':'||pg_get_constraintdef(c.oid) FROM tables t JOIN pg_constraint c ON c.conrelid=t.oid)
 SELECT coalesce(string_agg(line,E'\n' ORDER BY ordering),'') FROM lines`).Scan(&definition)
	return definition, err
}

func validate(ctx context.Context, q queryer) error {
	var safe bool
	err := q.QueryRowContext(ctx, `WITH tables AS(SELECT c.* FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relname IN('service_app_credentials','service_app_credential_audit') AND c.relkind='r'), reachable AS(
 SELECT r.* FROM pg_roles r WHERE r.rolname IN(current_user,session_user) OR pg_has_role(current_user,r.oid,'MEMBER') OR pg_has_role(session_user,r.oid,'MEMBER')),
 allowed AS(SELECT DISTINCT unnest(p.polroles) AS oid FROM pg_policy p JOIN tables t ON t.oid=p.polrelid WHERE p.polname='service_app_admin')
 SELECT
 (SELECT count(*)=2 FROM tables) AND (SELECT count(*)=1 AND bool_and(oid<>0) FROM allowed) AND
 NOT EXISTS(SELECT 1 FROM reachable r WHERE r.rolsuper OR r.rolbypassrls OR r.rolcreaterole OR r.rolcreatedb OR r.rolreplication OR has_database_privilege(r.oid,current_database(),'CREATE') OR has_schema_privilege(r.oid,current_schema(),'CREATE') OR EXISTS(SELECT 1 FROM tables t WHERE t.relowner=r.oid) OR (r.rolname NOT IN(current_user,session_user) AND r.oid NOT IN(SELECT oid FROM allowed))) AND
 NOT EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace,reachable r WHERE n.nspname NOT LIKE 'pg_%' AND n.nspname<>'information_schema' AND c.relkind IN('r','p','v','m','f') AND has_schema_privilege(r.oid,n.oid,'USAGE') AND c.oid NOT IN(SELECT oid FROM tables) AND (has_table_privilege(r.oid,c.oid,'SELECT,INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER') OR has_any_column_privilege(r.oid,c.oid,'SELECT,INSERT,UPDATE,REFERENCES'))) AND
 NOT EXISTS(SELECT 1 FROM pg_proc f JOIN pg_namespace n ON n.oid=f.pronamespace,reachable r WHERE f.prosecdef AND n.nspname NOT LIKE 'pg_%' AND n.nspname<>'information_schema' AND has_schema_privilege(r.oid,n.oid,'USAGE') AND has_function_privilege(r.oid,f.oid,'EXECUTE')) AND
 NOT EXISTS(SELECT 1 FROM tables t,reachable r WHERE has_table_privilege(r.oid,t.oid,'TRUNCATE,REFERENCES,TRIGGER') OR (t.relname='service_app_credentials' AND has_table_privilege(r.oid,t.oid,'DELETE')) OR
 EXISTS(SELECT 1 FROM pg_attribute a WHERE a.attrelid=t.oid AND a.attnum>0 AND NOT a.attisdropped AND (
 has_column_privilege(r.oid,t.oid,a.attnum,'REFERENCES') OR
 (t.relname='service_app_credentials' AND ((a.attname NOT IN('id','provider','client_id','revision','secret_ciphertext','token_ciphertext','key_version','validated_at','expires_at') AND has_column_privilege(r.oid,t.oid,a.attnum,'INSERT')) OR (a.attname NOT IN('revision','secret_ciphertext','token_ciphertext','key_version','validated_at','expires_at','revoked_at') AND has_column_privilege(r.oid,t.oid,a.attnum,'UPDATE')))) OR
 (t.relname='service_app_credential_audit' AND ((a.attname NOT IN('app_id','revision','action') AND has_column_privilege(r.oid,t.oid,a.attnum,'INSERT')) OR has_column_privilege(r.oid,t.oid,a.attnum,'UPDATE')))))) AND
 (SELECT bool_and(t.relrowsecurity AND t.relforcerowsecurity AND NOT t.relhasrules AND has_table_privilege(current_user,t.oid,'SELECT') AND
 NOT EXISTS(SELECT 1 FROM pg_trigger tr WHERE tr.tgrelid=t.oid AND NOT tr.tgisinternal) AND
 (SELECT count(*)=1 AND bool_and(p.polname='service_app_admin' AND p.polcmd='*' AND p.polpermissive AND cardinality(p.polroles)=1 AND NOT 0=ANY(p.polroles) AND EXISTS(SELECT 1 FROM reachable r WHERE r.oid=ANY(p.polroles)) AND pg_get_expr(p.polqual,p.polrelid)='true' AND pg_get_expr(p.polwithcheck,p.polrelid)='true') FROM pg_policy p WHERE p.polrelid=t.oid) AND
 CASE t.relname WHEN 'service_app_credentials' THEN
 (SELECT bool_and(has_column_privilege(current_user,t.oid,a.attname,'INSERT')) FROM pg_attribute a WHERE a.attrelid=t.oid AND a.attname IN('id','provider','client_id','revision','secret_ciphertext','token_ciphertext','key_version','validated_at','expires_at')) AND
 (SELECT bool_and(has_column_privilege(current_user,t.oid,a.attname,'UPDATE')) FROM pg_attribute a WHERE a.attrelid=t.oid AND a.attname IN('revision','secret_ciphertext','token_ciphertext','key_version','validated_at','expires_at','revoked_at'))
 ELSE has_table_privilege(current_user,t.oid,'DELETE') AND has_column_privilege(current_user,t.oid,'app_id','INSERT') AND has_column_privilege(current_user,t.oid,'revision','INSERT') AND has_column_privilege(current_user,t.oid,'action','INSERT') END) FROM tables t)`).Scan(&safe)
	if err != nil || !safe {
		return ErrBoundary
	}
	definition, err := schemaDefinition(ctx, q)
	if err != nil || definition+"\n" != schemaText {
		return ErrBoundary
	}
	return nil
}
