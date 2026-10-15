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
	Admin             *sql.DB
	RuntimeURL        string
	WorkerURL         string
	BotAdminURL       string
	PacerURL          string
	OutboundWorkerURL string
	Schema            string
	Migrate           func()
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
	worker := "tenant_worker_" + suffix
	botAdmin := "tenant_bot_admin_" + suffix
	pacer := "tenant_pacer_" + suffix
	outbound := "tenant_outbound_" + suffix
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
		if err := admin.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=$1)`, worker).Scan(&exists); err != nil {
			t.Error(err)
		} else if exists {
			if _, err := admin.Exec(`DROP OWNED BY ` + worker + `; DROP ROLE ` + worker); err != nil {
				t.Error(err)
			}
		}
		var botExists bool
		if err := admin.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=$1)`, botAdmin).Scan(&botExists); err != nil {
			t.Error(err)
		} else if botExists {
			if _, err := admin.Exec(`DROP OWNED BY ` + botAdmin + `; DROP ROLE ` + botAdmin); err != nil {
				t.Error(err)
			}
		}
		var pacerExists bool
		if err := admin.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=$1)`, pacer).Scan(&pacerExists); err != nil {
			t.Error(err)
		} else if pacerExists {
			if _, err := admin.Exec(`DROP OWNED BY ` + pacer + `; DROP ROLE ` + pacer); err != nil {
				t.Error(err)
			}
		}
		var outboundExists bool
		if err := admin.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=$1)`, outbound).Scan(&outboundExists); err != nil {
			t.Error(err)
		} else if outboundExists {
			if _, err := admin.Exec(`DROP OWNED BY ` + outbound + `; DROP ROLE ` + outbound); err != nil {
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
	rawNewsroom, err := os.ReadFile(filepath.Join(root, "0004_newsroom.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = owner.Exec(strings.ReplaceAll(strings.ReplaceAll(string(rawNewsroom), "overlaid_runtime", role), "overlaid_credential_lookup", lookup)); err != nil {
		t.Fatal(err)
	}
	rawEpisodes, err := os.ReadFile(filepath.Join(root, "0005_newsroom_episodes.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = owner.Exec(strings.ReplaceAll(strings.ReplaceAll(string(rawEpisodes), "overlaid_runtime", role), "overlaid_credential_lookup", lookup)); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"0006_river.sql", "0007_platform_ingest.sql"} {
		raw, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = owner.Exec(strings.ReplaceAll(string(raw), "overlaid_runtime", role)); err != nil {
			t.Fatal(err)
		}
	}
	rawWorker, err := os.ReadFile(filepath.Join(root, "0008_platform_dispatch.sql"))
	if err != nil {
		t.Fatal(err)
	}
	workerMigration := strings.ReplaceAll(strings.ReplaceAll(string(rawWorker), "overlaid_runtime", role), "overlaid_worker", worker)
	if _, err = owner.Exec(workerMigration); err != nil {
		t.Fatal(err)
	}
	rawEcho, err := os.ReadFile(filepath.Join(root, "0009_twitch_echo.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = owner.Exec(strings.ReplaceAll(strings.ReplaceAll(string(rawEcho), "overlaid_runtime", role), "overlaid_worker", worker)); err != nil {
		t.Fatal(err)
	}
	rawBots, err := os.ReadFile(filepath.Join(root, "0010_bot_accounts.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = owner.Exec(strings.ReplaceAll(string(rawBots), "overlaid_runtime", role)); err != nil {
		t.Fatal(err)
	}
	rawServiceBots, err := os.ReadFile(filepath.Join(root, "0011_service_bot_accounts.sql"))
	if err != nil {
		t.Fatal(err)
	}
	serviceMigration := strings.NewReplacer("overlaid_runtime", role, "overlaid_worker", worker, "overlaid_bot_admin", botAdmin).Replace(string(rawServiceBots))
	if _, err = owner.Exec(serviceMigration); err != nil {
		t.Fatal(err)
	}
	rawDelegations, err := os.ReadFile(filepath.Join(root, "0012_shared_bot_delegations.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = owner.Exec(strings.NewReplacer("overlaid_runtime", role, "overlaid_worker", worker, "overlaid_bot_admin", botAdmin).Replace(string(rawDelegations))); err != nil {
		t.Fatal(err)
	}
	rawOutbound, err := os.ReadFile(filepath.Join(root, "0013_platform_outbound.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = owner.Exec(strings.ReplaceAll(string(rawOutbound), "overlaid_runtime", role)); err != nil {
		t.Fatal(err)
	}
	rawOutboundQueue, err := os.ReadFile(filepath.Join(root, "0014_outbound_queue.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = owner.Exec(strings.ReplaceAll(string(rawOutboundQueue), "overlaid_runtime", role)); err != nil {
		t.Fatal(err)
	}
	rawPacing, err := os.ReadFile(filepath.Join(root, "0015_chat_pacing.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = owner.Exec(strings.NewReplacer("overlaid_runtime", role, "overlaid_worker", worker, "overlaid_bot_admin", botAdmin, "overlaid_pacer", pacer).Replace(string(rawPacing))); err != nil {
		t.Fatal(err)
	}
	rawOutboundWorker, err := os.ReadFile(filepath.Join(root, "0016_outbound_worker.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = owner.Exec(strings.ReplaceAll(string(rawOutboundWorker), "overlaid_outbound_worker", outbound)); err != nil {
		t.Fatal(err)
	}
	if _, err = owner.Exec(`ALTER ROLE ` + outbound + ` LOGIN PASSWORD '` + password + `'`); err != nil {
		t.Fatal(err)
	}
	u.User = url.UserPassword(outbound, password)
	outboundURL := u.String()

	if _, err = owner.Exec(`ALTER ROLE ` + pacer + ` LOGIN PASSWORD '` + password + `'`); err != nil {
		t.Fatal(err)
	}
	u.User = url.UserPassword(pacer, password)
	pacerURL := u.String()
	if _, err = owner.Exec(`ALTER ROLE ` + botAdmin + ` LOGIN PASSWORD '` + password + `'`); err != nil {
		t.Fatal(err)
	}
	u.User = url.UserPassword(botAdmin, password)
	botAdminURL := u.String()
	if _, err = owner.Exec(`ALTER ROLE ` + worker + ` LOGIN PASSWORD '` + password + `'`); err != nil {
		t.Fatal(err)
	}
	u.User = url.UserPassword(worker, password)
	workerURL := u.String()
	u.User = url.UserPassword(role, password)
	return TenantFixture{Admin: owner, RuntimeURL: u.String(), WorkerURL: workerURL, BotAdminURL: botAdminURL, PacerURL: pacerURL, OutboundWorkerURL: outboundURL, Schema: schema, Migrate: migrate}
}
