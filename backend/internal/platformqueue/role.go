// Package platformqueue validates the separate River leasing pools.
package platformqueue

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

// Validate requires exactly the intended job-kind policy, including every role
// reachable through SET ROLE. Combining two worker memberships is not safe:
// PostgreSQL permissive policies would otherwise expose both job kinds.
func Validate(ctx context.Context, service *sql.DB, product *tenantdb.DB, kind string) (string, error) {
	policy := ""
	switch kind {
	case "overlaid_chat_dispatch":
		policy = "worker_dispatch"
	case "overlaid_chat_send":
		policy = "outbound_worker_dispatch"
	default:
		return "", errors.New("invalid queue role kind")
	}
	if service == nil || product == nil || !product.IsScoped() {
		return "", errors.New("separate restricted queue role required")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var unsafe, private, ready, fenced bool
	err := service.QueryRowContext(ctx, `WITH reachable AS (
 SELECT r.* FROM pg_roles r WHERE r.rolname IN(current_user,session_user)
 OR pg_has_role(current_user,r.oid,'MEMBER') OR pg_has_role(session_user,r.oid,'MEMBER')
 ), tables AS (
 SELECT c.* FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname=current_schema() AND c.relkind IN('r','p')
 ) SELECT
 EXISTS(SELECT 1 FROM reachable r WHERE r.rolsuper OR r.rolbypassrls OR r.rolcreaterole OR EXISTS(SELECT 1 FROM tables c WHERE c.relowner=r.oid)),
 EXISTS(SELECT 1 FROM tables c,reachable r WHERE
 (c.relname NOT IN('river_job','river_queue','river_notification','river_migration','river_leader') AND has_table_privilege(r.oid,c.oid,'SELECT,INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER'))
 OR (c.relname IN('river_job','river_queue','river_notification','river_leader') AND has_table_privilege(r.oid,c.oid,'TRUNCATE,REFERENCES,TRIGGER'))
 OR (c.relname='river_migration' AND has_table_privilege(r.oid,c.oid,'INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER'))),
 (SELECT count(*)=5 AND bool_and(has_table_privilege(current_user,c.oid,'SELECT') AND
 (c.relname='river_migration' OR (has_table_privilege(current_user,c.oid,'INSERT') AND has_table_privilege(current_user,c.oid,'UPDATE') AND has_table_privilege(current_user,c.oid,'DELETE')))) FROM tables c WHERE c.relname IN('river_job','river_queue','river_notification','river_migration','river_leader')),
 (SELECT c.relrowsecurity AND c.relforcerowsecurity AND
 (SELECT count(*)=1 AND bool_and(p.polname=$1 AND p.polcmd='*' AND p.polpermissive AND cardinality(p.polroles)=1 AND NOT 0=ANY(p.polroles) AND pg_get_expr(p.polqual,p.polrelid)=$2 AND pg_get_expr(p.polwithcheck,p.polrelid)=$2)
 FROM pg_policy p WHERE p.polrelid=c.oid AND EXISTS(SELECT 1 FROM unnest(p.polroles) role_id WHERE CASE WHEN role_id=0 THEN true ELSE EXISTS(SELECT 1 FROM reachable r WHERE r.oid=role_id) END))
 FROM tables c WHERE c.relname='river_job')`, policy, "(kind = '"+kind+"'::text)").Scan(&unsafe, &private, &ready, &fenced)
	if err != nil || unsafe || private || !ready || !fenced {
		return "", errors.New("separate restricted queue role and exact job policy required")
	}
	const identity = `SELECT current_database(),current_schema(),coalesce(inet_server_addr()::text,'local'),coalesce(inet_server_port(),0)::text`
	var serviceID, productID [4]string
	if err = service.QueryRowContext(ctx, identity).Scan(&serviceID[0], &serviceID[1], &serviceID[2], &serviceID[3]); err != nil {
		return "", errors.New("queue database unavailable")
	}
	if err = product.QueryRowContext(ctx, identity).Scan(&productID[0], &productID[1], &productID[2], &productID[3]); err != nil || serviceID != productID {
		return "", errors.New("queue and product database must match")
	}
	return serviceID[1], nil
}
