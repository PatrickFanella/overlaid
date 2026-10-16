package platformqueue_test

import (
	"context"
	"database/sql"
	"io"
	"net/url"
	"testing"

	"git.subcult.tv/PatrickFanella/over-laid/internal/chatpacing"
	"git.subcult.tv/PatrickFanella/over-laid/internal/moderationpacing"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"git.subcult.tv/PatrickFanella/over-laid/internal/testpostgres"
)

func pacingOpen(ctx context.Context, kind, dsn string) (io.Closer, error) {
	if kind == "chat" {
		return chatpacing.Open(ctx, dsn)
	}
	return moderationpacing.Open(ctx, dsn)
}
func pacingConfig(f testpostgres.TenantFixture, kind string) (string, []string) {
	if kind == "chat" {
		return f.PacerURL, []string{"chat_pacing_accounts", "chat_pacing_channels"}
	}
	return f.ModerationPacerURL, []string{"moderation_pacing_buckets"}
}
func rejectPacing(t *testing.T, kind, dsn string) {
	t.Helper()
	if store, err := pacingOpen(t.Context(), kind, dsn); err == nil {
		_ = store.Close()
		t.Error("unsafe pacing pool was accepted")
	}
}
func acceptPacing(t *testing.T, kind, dsn string) {
	t.Helper()
	store, err := pacingOpen(t.Context(), kind, dsn)
	if err != nil {
		t.Fatal("restored pacing pool rejected", err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPacingPoolsRejectDestructiveStateGrants(t *testing.T) {
	for _, kind := range []string{"chat", "moderation"} {
		t.Run(kind, func(t *testing.T) {
			f := testpostgres.NewTenantFixture(t, "")
			dsn, tables := pacingConfig(f, kind)
			u, _ := url.Parse(dsn)
			role := u.User.Username()
			for _, table := range tables {
				for _, privilege := range []string{"TRUNCATE", "REFERENCES", "TRIGGER"} {
					t.Run(table+"/"+privilege, func(t *testing.T) {
						if _, err := f.Admin.Exec(`GRANT ` + privilege + ` ON ` + table + ` TO ` + role); err != nil {
							t.Fatal(err)
						}
						rejectPacing(t, kind, dsn)
						if _, err := f.Admin.Exec(`REVOKE ` + privilege + ` ON ` + table + ` FROM ` + role); err != nil {
							t.Fatal(err)
						}
						acceptPacing(t, kind, dsn)
					})
				}
			}
			alias := f.Schema + "_dangerous"
			if _, err := f.Admin.Exec(`CREATE ROLE ` + alias + ` NOLOGIN; GRANT USAGE ON SCHEMA ` + f.Schema + ` TO ` + alias + `; GRANT TRUNCATE ON ` + tables[0] + ` TO ` + alias + `; GRANT ` + alias + ` TO ` + role + ` WITH INHERIT FALSE`); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := f.Admin.Exec(`DROP OWNED BY ` + alias + `; DROP ROLE ` + alias); err != nil {
					t.Error(err)
				}
			})
			rejectPacing(t, kind, dsn)
		})
	}
}

func TestPacingPoolsRequireExactPermissivePolicies(t *testing.T) {
	for _, kind := range []string{"chat", "moderation"} {
		t.Run(kind, func(t *testing.T) {
			f := testpostgres.NewTenantFixture(t, "")
			dsn, tables := pacingConfig(f, kind)
			u, _ := url.Parse(dsn)
			role := u.User.Username()
			for _, table := range tables {
				for _, mode := range []string{"hidden-rows", "denied-writes", "restrictive"} {
					t.Run(table+"/"+mode, func(t *testing.T) {
						permissive, using, check := "PERMISSIVE", "true", "true"
						switch mode {
						case "hidden-rows":
							using = "false"
						case "denied-writes":
							check = "false"
						case "restrictive":
							permissive = "RESTRICTIVE"
						}
						if _, err := f.Admin.Exec(`DROP POLICY pacer_only ON ` + table + `; CREATE POLICY pacer_only ON ` + table + ` AS ` + permissive + ` FOR ALL TO ` + role + ` USING(` + using + `) WITH CHECK(` + check + `)`); err != nil {
							t.Fatal(err)
						}
						rejectPacing(t, kind, dsn)
						if _, err := f.Admin.Exec(`DROP POLICY pacer_only ON ` + table + `; CREATE POLICY pacer_only ON ` + table + ` AS PERMISSIVE FOR ALL TO ` + role + ` USING(true) WITH CHECK(true)`); err != nil {
							t.Fatal(err)
						}
						acceptPacing(t, kind, dsn)
					})
				}
			}
		})
	}
}

func TestPacingPoolsRejectAdministrativeAndSchemaPrivileges(t *testing.T) {
	for _, kind := range []string{"chat", "moderation"} {
		t.Run(kind, func(t *testing.T) {
			f := testpostgres.NewTenantFixture(t, "")
			dsn, _ := pacingConfig(f, kind)
			u, _ := url.Parse(dsn)
			role := u.User.Username()
			for _, attribute := range []string{"CREATEROLE", "CREATEDB", "REPLICATION"} {
				t.Run(attribute, func(t *testing.T) {
					if _, err := f.Admin.Exec(`ALTER ROLE ` + role + ` ` + attribute); err != nil {
						t.Fatal(err)
					}
					rejectPacing(t, kind, dsn)
					if _, err := f.Admin.Exec(`ALTER ROLE ` + role + ` NO` + attribute); err != nil {
						t.Fatal(err)
					}
					acceptPacing(t, kind, dsn)
				})
			}
			if _, err := f.Admin.Exec(`GRANT CREATE ON SCHEMA ` + f.Schema + ` TO ` + role); err != nil {
				t.Fatal(err)
			}
			rejectPacing(t, kind, dsn)
			if _, err := f.Admin.Exec(`REVOKE CREATE ON SCHEMA ` + f.Schema + ` FROM ` + role); err != nil {
				t.Fatal(err)
			}
			acceptPacing(t, kind, dsn)
		})
	}
}

func TestPacingPoolsRejectOriginalSessionRoleEscape(t *testing.T) {
	for _, kind := range []string{"chat", "moderation"} {
		t.Run(kind, func(t *testing.T) {
			f := testpostgres.NewTenantFixture(t, "")
			dsn, _ := pacingConfig(f, kind)
			safe, _ := url.Parse(dsn)
			runtime, _ := url.Parse(f.RuntimeURL)
			role := f.Schema + "_session"
			password, _ := safe.User.Password()
			if _, err := f.Admin.Exec(`CREATE ROLE ` + role + ` LOGIN NOINHERIT PASSWORD '` + password + `'; GRANT ` + safe.User.Username() + `,` + runtime.User.Username() + ` TO ` + role); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := f.Admin.Exec(`DROP ROLE ` + role); err != nil {
					t.Error(err)
				}
			})
			u := *safe
			u.User = url.UserPassword(role, password)
			q := u.Query()
			q.Set("role", safe.User.Username())
			u.RawQuery = q.Encode()
			raw, err := sql.Open("pgx", u.String())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = raw.Close() }()
			conn, err := raw.Conn(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = conn.Close() }()
			var current, original string
			if err = conn.QueryRowContext(t.Context(), `SELECT current_user,session_user`).Scan(&current, &original); err != nil || current != safe.User.Username() || original != role {
				t.Fatal("startup-role fixture did not switch identity", err)
			}
			// Prove that the original login really can escape to the private role; no
			// submitted/provider data or production identity participates in this test.
			if _, err = conn.ExecContext(t.Context(), `RESET ROLE; SET ROLE `+runtime.User.Username()+`; SELECT set_config('app.tenant_id','`+tenantdb.BootstrapID+`',false); SELECT count(*) FROM platform_connections`); err != nil {
				t.Fatal("fixture could not reach original session membership", err)
			}
			rejectPacing(t, kind, u.String())
			acceptPacing(t, kind, dsn)
		})
	}
}
