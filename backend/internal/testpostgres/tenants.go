package testpostgres

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type TenantFixture struct {
	Admin      *sql.DB
	RuntimeURL string
	Schema     string
	Migrate    func()
}

// Every fixture owns its schema and login/lookup roles, retains only shared
// pgcrypto, and never uses preloaded application tables.
func NewTenantFixture(t *testing.T, legacySQL string) TenantFixture {
	t.Helper()
	base := os.Getenv("OVERLAID_TEST_DATABASE_URL")
	if base == "" {
		t.Skip("set OVERLAID_TEST_DATABASE_URL to a disposable Postgres database")
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "postgres" && u.Scheme != "postgresql" {
		t.Fatal("disposable Postgres URL required")
	}
	admin, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatal(err)
	}
	if err = EnsurePGCrypto(context.Background(), admin); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	var nonce [8]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	suffix := hex.EncodeToString(nonce[:])
	schema := "tenant_test_" + suffix
	role := "tenant_runtime_" + suffix
	lookup := "tenant_lookup_" + suffix
	password := hex.EncodeToString(nonce[:]) + "-fixture"
	if _, err = admin.Exec(`CREATE SCHEMA ` + schema + `; CREATE ROLE ` + role + ` LOGIN PASSWORD '` + password + `' NOSUPERUSER NOBYPASSRLS;`); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(`DROP SCHEMA IF EXISTS ` + schema + ` CASCADE; DROP OWNED BY ` + role + `; DROP ROLE IF EXISTS ` + role + `;`); err != nil {
			t.Errorf("tenant fixture cleanup: %v", err)
		}
		var exists bool
		if err := admin.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=$1)`, lookup).Scan(&exists); err != nil {
			t.Error(err)
		} else if exists {
			if _, err = admin.Exec(`DROP OWNED BY ` + lookup + `; DROP ROLE ` + lookup); err != nil {
				t.Error(err)
			}
		}
		admin.Close()
	})
	q := u.Query()
	q.Set("search_path", schema+",public")
	u.RawQuery = q.Encode()
	owner, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { owner.Close() })
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..", "..", "migrations")
	for _, name := range []string{"0001_init.sql", "0002_moderation_policy.sql"} {
		raw, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = owner.Exec(string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	if legacySQL != "" {
		if _, err = owner.Exec(legacySQL); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(filepath.Join(root, "0003_tenancy.sql"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(raw), "overlaid_runtime", role)
	text = strings.ReplaceAll(text, "overlaid_credential_lookup", lookup)
	text = strings.ReplaceAll(text, "GRANT USAGE ON SCHEMA public TO ", "GRANT USAGE ON SCHEMA "+schema+" TO ")
	text = strings.ReplaceAll(text, "ON ALL TABLES IN SCHEMA public TO ", "ON ALL TABLES IN SCHEMA "+schema+" TO ")
	migrate := func() {
		t.Helper()
		if _, err := owner.Exec(text); err != nil {
			t.Fatal(err)
		}
	}
	migrate()
	u.User = url.UserPassword(role, password)
	return TenantFixture{Admin: owner, RuntimeURL: u.String(), Schema: schema, Migrate: migrate}
}
