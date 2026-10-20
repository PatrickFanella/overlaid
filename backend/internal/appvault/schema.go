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
 EXISTS(SELECT 1 FROM reachable r WHERE r.oid IN(SELECT oid FROM allowed)) AND NOT EXISTS(SELECT 1 FROM reachable r JOIN pg_policy p ON r.oid=ANY(p.polroles) JOIN tables t ON t.oid=p.polrelid WHERE p.polname='service_app_execution') AND
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

 CASE t.relname WHEN 'service_app_credentials' THEN
 (SELECT bool_and(has_column_privilege(current_user,t.oid,a.attname,'INSERT')) FROM pg_attribute a WHERE a.attrelid=t.oid AND a.attname IN('id','provider','client_id','revision','secret_ciphertext','token_ciphertext','key_version','validated_at','expires_at')) AND
 (SELECT bool_and(has_column_privilege(current_user,t.oid,a.attname,'UPDATE')) FROM pg_attribute a WHERE a.attrelid=t.oid AND a.attname IN('revision','secret_ciphertext','token_ciphertext','key_version','validated_at','expires_at','revoked_at'))
 ELSE has_table_privilege(current_user,t.oid,'DELETE') AND has_column_privilege(current_user,t.oid,'app_id','INSERT') AND has_column_privilege(current_user,t.oid,'revision','INSERT') AND has_column_privilege(current_user,t.oid,'action','INSERT') END) FROM tables t)`).Scan(&safe)
	if err != nil || !safe {
		return ErrBoundary
	}
	return validateLayout(ctx, q)
}

func validateLayout(ctx context.Context, q queryer) error {
	if err := validatePolicies(ctx, q); err != nil {
		return err
	}
	definition, err := schemaDefinition(ctx, q)
	if err != nil || definition+"\n" != schemaText {
		return ErrBoundary
	}
	return nil
}

func validatePolicies(ctx context.Context, q queryer) error {
	var safe bool
	err := q.QueryRowContext(ctx, `WITH tables AS(SELECT c.* FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relname IN('service_app_credentials','service_app_credential_audit') AND c.relkind='r'), policies AS(SELECT p.*,t.relname FROM pg_policy p JOIN tables t ON t.oid=p.polrelid), admins AS(SELECT DISTINCT unnest(polroles) AS oid FROM policies WHERE polname='service_app_admin'), executors AS(SELECT DISTINCT unnest(polroles) AS oid FROM policies WHERE polname='service_app_execution')
 SELECT (SELECT count(*)=2 AND bool_and(relrowsecurity AND relforcerowsecurity AND NOT relhasrules AND NOT EXISTS(SELECT 1 FROM pg_trigger tr WHERE tr.tgrelid=t.oid AND NOT tr.tgisinternal)) FROM tables t)
 AND NOT EXISTS(SELECT 1 FROM pg_auth_members m JOIN pg_roles r ON r.oid=m.member WHERE m.admin_option AND (r.rolname IN(current_user,session_user) OR pg_has_role(current_user,r.oid,'MEMBER') OR pg_has_role(session_user,r.oid,'MEMBER')))
 AND NOT EXISTS(SELECT 1 FROM tables t,pg_roles r WHERE (r.rolname IN(current_user,session_user) OR pg_has_role(current_user,r.oid,'MEMBER') OR pg_has_role(session_user,r.oid,'MEMBER')) AND (has_table_privilege(r.oid,t.oid,'SELECT WITH GRANT OPTION,INSERT WITH GRANT OPTION,UPDATE WITH GRANT OPTION,DELETE WITH GRANT OPTION,TRUNCATE WITH GRANT OPTION,REFERENCES WITH GRANT OPTION,TRIGGER WITH GRANT OPTION') OR has_any_column_privilege(r.oid,t.oid,'SELECT WITH GRANT OPTION,INSERT WITH GRANT OPTION,UPDATE WITH GRANT OPTION,REFERENCES WITH GRANT OPTION')))
 AND (SELECT count(*)=1 AND bool_and(oid<>0) FROM admins) AND (SELECT count(*)=1 AND bool_and(oid<>0) FROM executors) AND NOT EXISTS(SELECT 1 FROM admins a JOIN executors e USING(oid))
 AND (SELECT count(*)=3 AND bool_and(polpermissive AND cardinality(polroles)=1 AND NOT 0=ANY(polroles) AND CASE WHEN polname='service_app_admin' THEN polcmd='*' AND polroles[1] IN(SELECT oid FROM admins) AND pg_get_expr(polqual,polrelid)='true' AND pg_get_expr(polwithcheck,polrelid)='true'
 WHEN polname='service_app_execution' AND relname='service_app_credentials' THEN polcmd='r' AND polroles[1] IN(SELECT oid FROM executors) AND pg_get_expr(polqual,polrelid)='(revoked_at IS NULL)' AND polwithcheck IS NULL ELSE false END) FROM policies)
 AND (SELECT count(*)=2 FROM policies WHERE polname='service_app_admin') AND (SELECT count(*)=1 FROM policies WHERE polname='service_app_execution')`).Scan(&safe)
	if err != nil || !safe {
		return ErrBoundary
	}
	return nil
}

func validateExecution(ctx context.Context, q queryer) error {
	var safe bool
	err := q.QueryRowContext(ctx, `WITH tables AS(SELECT c.* FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relname='service_app_credentials' AND c.relkind='r'), reachable AS(SELECT r.* FROM pg_roles r WHERE r.rolname IN(current_user,session_user) OR pg_has_role(current_user,r.oid,'MEMBER') OR pg_has_role(session_user,r.oid,'MEMBER')), allowed AS(SELECT DISTINCT unnest(p.polroles) AS oid FROM pg_policy p JOIN tables t ON t.oid=p.polrelid WHERE p.polname='service_app_execution')
 SELECT (SELECT count(*)=1 FROM tables) AND (SELECT count(*)=1 AND bool_and(oid<>0) FROM allowed)
 AND EXISTS(SELECT 1 FROM reachable r WHERE r.oid IN(SELECT oid FROM allowed))
 AND NOT EXISTS(SELECT 1 FROM reachable r JOIN pg_policy p ON r.oid=ANY(p.polroles) JOIN tables t ON t.oid=p.polrelid WHERE p.polname='service_app_admin')
 AND NOT EXISTS(SELECT 1 FROM reachable r WHERE r.rolsuper OR r.rolbypassrls OR r.rolcreaterole OR r.rolcreatedb OR r.rolreplication OR has_database_privilege(r.oid,current_database(),'CREATE') OR has_schema_privilege(r.oid,current_schema(),'CREATE') OR EXISTS(SELECT 1 FROM tables t WHERE t.relowner=r.oid) OR (r.rolname NOT IN(current_user,session_user) AND r.oid NOT IN(SELECT oid FROM allowed)))
 AND NOT EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace,reachable r WHERE n.nspname NOT LIKE 'pg_%' AND n.nspname<>'information_schema' AND c.relkind IN('r','p','v','m','f') AND has_schema_privilege(r.oid,n.oid,'USAGE') AND c.oid NOT IN(SELECT oid FROM tables) AND (has_table_privilege(r.oid,c.oid,'SELECT,INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER') OR has_any_column_privilege(r.oid,c.oid,'SELECT,INSERT,UPDATE,REFERENCES')))
 AND NOT EXISTS(SELECT 1 FROM pg_proc f JOIN pg_namespace n ON n.oid=f.pronamespace,reachable r WHERE f.prosecdef AND n.nspname NOT LIKE 'pg_%' AND n.nspname<>'information_schema' AND has_schema_privilege(r.oid,n.oid,'USAGE') AND has_function_privilege(r.oid,f.oid,'EXECUTE'))
 AND NOT EXISTS(SELECT 1 FROM tables t,reachable r WHERE has_table_privilege(r.oid,t.oid,'INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER') OR has_any_column_privilege(r.oid,t.oid,'INSERT,UPDATE,REFERENCES') OR EXISTS(SELECT 1 FROM pg_attribute a WHERE a.attrelid=t.oid AND a.attnum>0 AND NOT a.attisdropped AND a.attname NOT IN('id','provider','client_id','revision','token_ciphertext','key_version','validated_at','expires_at','revoked_at','created_at') AND has_column_privilege(r.oid,t.oid,a.attnum,'SELECT')))
 AND (SELECT count(*)=10 AND bool_and(has_column_privilege(current_user,t.oid,a.attnum,'SELECT')) FROM tables t JOIN pg_attribute a ON a.attrelid=t.oid WHERE a.attname IN('id','provider','client_id','revision','token_ciphertext','key_version','validated_at','expires_at','revoked_at','created_at'))`).Scan(&safe)
	if err != nil || !safe {
		return ErrBoundary
	}
	return validateLayout(ctx, q)
}
