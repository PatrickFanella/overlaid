// overlaid-admin changes operator-owned plan assignments without a deployment.
// It is intentionally not exposed through tenant-facing HTTP routes.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	tenant := flag.String("tenant", "", "exact tenant UUID")
	plan := flag.String("plan", "", "free, paid, or unlimited")
	flag.Parse()
	if flag.NArg() != 0 || !tenantdb.ValidID(*tenant) || (*plan != "free" && *plan != "paid" && *plan != "unlimited") {
		fmt.Fprintln(os.Stderr, "Usage: overlaid-admin --tenant UUID --plan free|paid|unlimited")
		os.Exit(2)
	}
	if *tenant == tenantdb.BootstrapID && *plan != "unlimited" {
		fmt.Fprintln(os.Stderr, "The self-host bootstrap plan must remain unlimited.")
		os.Exit(2)
	}
	dsn := os.Getenv("OVERLAID_ADMIN_DATABASE_URL")
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "OVERLAID_ADMIN_DATABASE_URL is required.")
		os.Exit(2)
	}
	if err := assign(dsn, *tenant, *plan); err != nil {
		// Driver errors can contain connection details. Keep the public receipt
		// bounded; inspect database logs privately if assignment fails.
		fmt.Fprintln(os.Stderr, "Plan assignment failed; no change was committed.")
		os.Exit(1)
	}
	fmt.Printf("Plan assigned and audited: tenant=%s plan=%s\n", *tenant, *plan)
}

func assign(dsn, tenant, plan string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var before string
	if err = tx.QueryRowContext(ctx, `SELECT plan_id FROM tenants WHERE id=$1 FOR UPDATE`, tenant).Scan(&before); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tenants SET plan_id=$2 WHERE id=$1`, tenant, plan); err != nil {
		return err
	}
	diff, _ := json.Marshal(map[string]string{"before": before, "after": plan, "source": "operator-cli"})
	if _, err = tx.ExecContext(ctx, `INSERT INTO tenant_change_audit(tenant_id,action,diff) VALUES($1,'plan.assignment',$2)`, tenant, diff); err != nil {
		return err
	}
	return tx.Commit()
}
