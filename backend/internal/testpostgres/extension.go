// Package testpostgres provides setup used by optional Postgres integration tests.
package testpostgres

import (
	"context"
	"database/sql"
	"fmt"
)

// EnsurePGCrypto keeps this database-wide prerequisite outside disposable test
// schemas. The transaction lock serializes installation across package processes.
func EnsurePGCrypto(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('overlaid:postgres-test-pgcrypto'))`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public`); err != nil {
		return err
	}
	var schema string
	if err := tx.QueryRowContext(ctx, `SELECT n.nspname FROM pg_extension e JOIN pg_namespace n ON n.oid=e.extnamespace WHERE e.extname='pgcrypto'`).Scan(&schema); err != nil {
		return err
	}
	if schema != "public" {
		return fmt.Errorf("isolated Postgres tests require pgcrypto in public, found %q", schema)
	}
	return tx.Commit()
}
