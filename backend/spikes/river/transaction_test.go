// Package riverspike exercises ADR 0007's compatibility gate. Nothing in this
// module is linked into the application or starts a River worker.
package riverspike

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"git.subcult.tv/PatrickFanella/over-laid/internal/testpostgres"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
	"github.com/riverqueue/river/rivermigrate"
)

const otherTenant = "00000000-0000-4000-8000-000000000099"

type dispatchArgs struct {
	TenantID     string `json:"tenant_id"`
	ConnectionID string `json:"connection_id"`
	EventID      string `json:"event_id"`
}

func (dispatchArgs) Kind() string { return "overlaid_chat_dispatch_spike" }

type fixture struct {
	admin  *sql.DB
	pool   *sql.DB
	client *river.Client[*sql.Tx]
	schema string
	url    string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	f := testpostgres.NewTenantFixture(t, "")
	ctx := t.Context()
	migrator, err := rivermigrate.New(riverdatabasesql.New(f.Admin), &rivermigrate.Config{Schema: f.Schema})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(f.RuntimeURL)
	if err != nil {
		t.Fatal(err)
	}
	// The domain table and River policy are fixture-only prototypes. The
	// production schema/policy and worker role need a separate migration review.
	ddl := fmt.Sprintf(`
		CREATE TABLE spike_events (
		 tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
		 connection_id text NOT NULL, idempotency_key text NOT NULL,
		 event_id text NOT NULL, envelope jsonb NOT NULL,
		 PRIMARY KEY(tenant_id,connection_id,idempotency_key)
		);
		ALTER TABLE spike_events ENABLE ROW LEVEL SECURITY;
		ALTER TABLE spike_events FORCE ROW LEVEL SECURITY;
		CREATE POLICY tenant_isolation ON spike_events
		 USING(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid)
		 WITH CHECK(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);
		ALTER TABLE river_job ENABLE ROW LEVEL SECURITY;
		ALTER TABLE river_job FORCE ROW LEVEL SECURITY;
		CREATE POLICY tenant_isolation ON river_job
		 USING(args->>'tenant_id'=nullif(current_setting('app.tenant_id',true),''))
		 WITH CHECK(args->>'tenant_id'=nullif(current_setting('app.tenant_id',true),''));
		GRANT SELECT,INSERT,UPDATE,DELETE ON spike_events,river_job TO %s;
		GRANT USAGE,SELECT ON SEQUENCE river_job_id_seq TO %s;
	`, u.User.Username(), u.User.Username())
	if _, err = f.Admin.ExecContext(ctx, ddl); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Admin.ExecContext(ctx, `INSERT INTO tenants(id,slug,name,owner_account_id,plan_id) VALUES($1,'river-other','Other','00000000-0000-4000-8000-000000000002','unlimited')`, otherTenant); err != nil {
		t.Fatal(err)
	}
	// Validate the same role and application schema required by tenantdb.Open.
	validated, err := tenantdb.Open(f.RuntimeURL, tenantdb.BootstrapID)
	if err != nil {
		t.Fatal(err)
	}
	if err = validated.Close(); err != nil {
		t.Fatal(err)
	}
	pool, err := sql.Open("pgx", f.RuntimeURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })
	pool.SetMaxOpenConns(12)
	client, err := river.NewClient(riverdatabasesql.New(pool), &river.Config{Schema: f.Schema})
	if err != nil {
		t.Fatal(err)
	}
	return fixture{admin: f.Admin, pool: pool, client: client, schema: f.Schema, url: f.RuntimeURL}
}

func chatEvent(t *testing.T, tenant, delivery string) platform.ChatEvent {
	t.Helper()
	now := time.Now().UTC()
	event, err := platform.NewChatEvent(platform.Connection{TenantID: tenant, ID: "twitch:channel", Platform: "twitch"}, delivery, now, now,
		platform.ChatMessage{MessageID: delivery, Text: "private fixture chat", Actor: platform.Identity{ID: "viewer"}, Channel: platform.Identity{ID: "channel"}})
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func beginScoped(ctx context.Context, pool *sql.DB, tenant string) (*sql.Tx, error) {
	tx, err := pool.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `SELECT set_config('app.tenant_id',$1,true)`, tenant); err != nil {
		tx.Rollback()
		return nil, err
	}
	return tx, nil
}

func (f fixture) insert(ctx context.Context, tx *sql.Tx, event platform.ChatEvent, args dispatchArgs) (bool, error) {
	raw, err := json.Marshal(event)
	if err != nil {
		return false, err
	}
	var id string
	err = tx.QueryRowContext(ctx, `INSERT INTO spike_events(tenant_id,connection_id,idempotency_key,event_id,envelope)
		VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING RETURNING event_id`, event.TenantID, event.ConnectionID, event.IdempotencyKey, event.ID, raw).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_, err = f.client.InsertTx(ctx, tx, args, &river.InsertOpts{Queue: "ingest_followup", MaxAttempts: 3})
	return err == nil, err
}

func (f fixture) counts(t *testing.T, tenant string) (events, jobs int) {
	t.Helper()
	tx, err := beginScoped(t.Context(), f.pool, tenant)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = tx.QueryRowContext(t.Context(), `SELECT count(*) FROM spike_events`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRowContext(t.Context(), `SELECT count(*) FROM river_job`).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	return
}

func argsFor(event platform.ChatEvent) dispatchArgs {
	return dispatchArgs{TenantID: event.TenantID, ConnectionID: event.ConnectionID, EventID: event.ID}
}

func TestCommitVisibilityRollbackAndEnqueueFailure(t *testing.T) {
	for _, mode := range []string{"commit", "rollback", "enqueue_failure"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			ctx := t.Context()
			event := chatEvent(t, tenantdb.BootstrapID, mode)
			tx, err := beginScoped(ctx, f.pool, event.TenantID)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			args := argsFor(event)
			if mode == "enqueue_failure" {
				args.TenantID = otherTenant
			}
			inserted, err := f.insert(ctx, tx, event, args)
			if mode == "enqueue_failure" {
				if err == nil {
					t.Fatal("cross-tenant enqueue accepted")
				}
			} else if err != nil || !inserted {
				t.Fatalf("insert: %v", err)
			}
			beforeEvents, beforeJobs := f.counts(t, event.TenantID)
			if beforeEvents != 0 || beforeJobs != 0 {
				t.Fatal("uncommitted work visible")
			}
			want := 0
			if mode == "commit" {
				if err := tx.Commit(); err != nil {
					t.Fatal(err)
				}
				want = 1
			} else if err := tx.Rollback(); err != nil {
				t.Fatal(err)
			}
			if events, jobs := f.counts(t, event.TenantID); events != want || jobs != want {
				t.Fatalf("partial commit or leaked rollback: events=%d jobs=%d", events, jobs)
			}
			if mode == "commit" {
				var raw string
				if err := f.admin.QueryRowContext(ctx, `SELECT args::text FROM river_job`).Scan(&raw); err != nil {
					t.Fatal(err)
				}
				var args map[string]any
				if err := json.Unmarshal([]byte(raw), &args); err != nil || len(args) != 3 || args["tenant_id"] != tenantdb.BootstrapID {
					t.Fatalf("unexpected job payload: %s %v", raw, err)
				}
			}
		})
	}
}

func TestConcurrentDuplicatesAndRestart(t *testing.T) {
	f := newFixture(t)
	event := chatEvent(t, tenantdb.BootstrapID, "duplicate")
	ctx := t.Context()
	var accepted atomic.Int32
	var workers sync.WaitGroup
	for range 24 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			tx, err := beginScoped(ctx, f.pool, event.TenantID)
			if err != nil {
				t.Error(err)
				return
			}
			defer tx.Rollback()
			inserted, err := f.insert(ctx, tx, event, argsFor(event))
			if err != nil {
				t.Error(err)
				return
			}
			if err = tx.Commit(); err != nil {
				t.Error(err)
			} else if inserted {
				accepted.Add(1)
			}
		}()
	}
	workers.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("accepted %d duplicate deliveries", accepted.Load())
	}
	if err := f.pool.Close(); err != nil {
		t.Fatal(err)
	}
	pool, err := sql.Open("pgx", f.url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	f.pool = pool
	f.client, err = river.NewClient(riverdatabasesql.New(pool), &river.Config{Schema: f.schema})
	if err != nil {
		t.Fatal(err)
	}
	if events, jobs := f.counts(t, event.TenantID); events != 1 || jobs != 1 {
		t.Fatalf("restart lost atomic work: %d %d", events, jobs)
	}
	tx, err := beginScoped(ctx, pool, event.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	inserted, err := f.insert(ctx, tx, event, argsFor(event))
	if err != nil || inserted {
		t.Fatalf("restart redelivery enqueued again: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestTenantAndMissingContextIsolation(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	for _, tenant := range []string{tenantdb.BootstrapID, otherTenant} {
		event := chatEvent(t, tenant, "same-native-delivery")
		tx, err := beginScoped(ctx, f.pool, tenant)
		if err != nil {
			t.Fatal(err)
		}
		inserted, err := f.insert(ctx, tx, event, argsFor(event))
		if err != nil || !inserted {
			tx.Rollback()
			t.Fatalf("tenant insert: %v", err)
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if events, jobs := f.counts(t, tenant); events != 1 || jobs != 1 {
			t.Fatalf("tenant read crossed scopes: %d %d", events, jobs)
		}
	}
	if events, jobs := f.counts(t, ""); events != 0 || jobs != 0 {
		t.Fatal("missing context exposed rows")
	}
	for _, test := range []struct{ scope, argument string }{{"", tenantdb.BootstrapID}, {tenantdb.BootstrapID, ""}, {tenantdb.BootstrapID, otherTenant}} {
		tx, err := beginScoped(ctx, f.pool, test.scope)
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.client.InsertTx(ctx, tx, dispatchArgs{TenantID: test.argument, ConnectionID: "connection", EventID: "forged"}, nil)
		tx.Rollback()
		if err == nil {
			t.Fatalf("invalid tenant job accepted: %#v", test)
		}
	}
}
