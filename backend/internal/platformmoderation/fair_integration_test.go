package platformmoderation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platformoutbound"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformqueue"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"github.com/riverqueue/river/riverdriver"
	"github.com/riverqueue/river/rivertype"
)

func fairJobs(t *testing.T, a, b *tenantdb.DB, kind string, count int) {
	t.Helper()
	for index, db := range []*tenantdb.DB{a, b} {
		event := source(t, db, fmt.Sprintf("fair-%d", index), "111")
		for i := range count {
			key := fmt.Sprintf("job-%d", i)
			if kind == "overlaid_moderation" {
				s, _ := New(db)
				admit(t, s, intent(t, event, key))
			} else {
				s, _ := platformoutbound.New(db)
				if _, ok, err := s.Admit(t.Context(), platformoutbound.Request{EventID: event.ID, ConnectionID: event.ConnectionID, Key: key, Text: "PRIVATE_CHAT"}); err != nil || !ok {
					t.Fatal("chat admission", ok, err)
				}
			}
		}
	}
}
func fairParams(schema, kind string, n int) *riverdriver.JobGetAvailableParams {
	queue := "chat_outbound"
	if kind == "overlaid_moderation" {
		queue = "moderation_outbound"
	}
	return &riverdriver.JobGetAvailableParams{Schema: schema, Queue: queue, ClientID: "fair-fixture", MaxAttemptedBy: 2, MaxToLock: n}
}
func jobTenant(t *testing.T, j *rivertype.JobRow) string {
	t.Helper()
	var args struct {
		Tenant string `json:"tenant_id"`
	}
	if json.Unmarshal(j.EncodedArgs, &args) != nil || args.Tenant == "" {
		t.Error("invalid payload-free job identity")
		return ""
	}
	return args.Tenant
}

func TestFairLeaseTurnsSurviveClientsAndClockChanges(t *testing.T) {
	for _, kind := range []string{"overlaid_chat_send", "overlaid_moderation"} {
		t.Run(kind, func(t *testing.T) {
			f, a, b := fixture(t)
			fairJobs(t, a, b, kind, 6)
			dsn := f.OutboundWorkerURL
			if kind == "overlaid_moderation" {
				dsn = f.ModerationWorkerURL
			}
			service := pool(t, dsn)
			driver, schema, err := platformqueue.NewFairDriver(t.Context(), service, a, kind)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.Admin.Exec(`UPDATE river_job SET metadata='{"keep":"value"}',attempted_by=ARRAY['old1','old2','old3'] WHERE kind=$1`, kind); err != nil {
				t.Fatal(err)
			}
			p := fairParams(schema, kind, 1)
			for i := range 7 {
				jobs, err := driver.GetExecutor().JobGetAvailable(t.Context(), p)
				if err != nil || len(jobs) != 1 {
					t.Fatal("lease", len(jobs), err)
				}
				want := a.TenantID()
				if i%2 == 1 {
					want = b.TenantID()
				}
				job := jobs[0]
				var metadata struct {
					Keep string `json:"keep"`
					Turn int64  `json:"overlaid_fair_turn"`
				}
				if err := json.Unmarshal(job.Metadata, &metadata); err != nil || metadata.Keep != "value" || metadata.Turn <= 0 {
					t.Fatal("lease metadata changed")
				}
				if jobTenant(t, job) != want || job.Attempt != 1 || job.State != rivertype.JobStateRunning || job.AttemptedAt == nil || len(job.AttemptedBy) != 2 || job.AttemptedBy[0] != "old3" || job.AttemptedBy[1] != p.ClientID {
					t.Fatal("fairness or River bookkeeping changed", i)
				}
			}
			// A has the newest durable turn. Even deliberately reversed wall-clock
			// attempt timestamps cannot move it ahead of B in a fresh client/pool.
			if _, err = f.Admin.Exec(`UPDATE river_job SET attempted_at=CASE WHEN args->>'tenant_id'=$1 THEN '1900-01-01'::timestamptz ELSE '2100-01-01'::timestamptz END WHERE kind=$2 AND state='running'`, a.TenantID(), kind); err != nil {
				t.Fatal(err)
			}
			if err = service.Close(); err != nil {
				t.Fatal(err)
			}
			fresh, _, err := platformqueue.NewFairDriver(t.Context(), pool(t, dsn), a, kind)
			if err != nil {
				t.Fatal(err)
			}
			p.ClientID = "restarted-client"
			jobs, err := fresh.GetExecutor().JobGetAvailable(t.Context(), p)
			if err != nil || len(jobs) != 1 || jobTenant(t, jobs[0]) != b.TenantID() {
				t.Fatal("restart lost tenant turn", err)
			}
			table := "platform_outbound_actions"
			if kind == "overlaid_moderation" {
				table = "platform_moderation_actions"
			}
			var claimed int
			if err = f.Admin.QueryRow(`SELECT count(*) FROM ` + table + ` WHERE state<>'pending'`).Scan(&claimed); err != nil || claimed != 0 {
				t.Fatal("lease created an effect claim", claimed, err)
			}
		})
	}
}

func TestFairLeaseBatchesAcrossConcurrentClients(t *testing.T) {
	for _, kind := range []string{"overlaid_chat_send", "overlaid_moderation"} {
		t.Run(kind, func(t *testing.T) {
			f, a, b := fixture(t)
			fairJobs(t, a, b, kind, 8)
			dsn := f.OutboundWorkerURL
			if kind == "overlaid_moderation" {
				dsn = f.ModerationWorkerURL
			}
			first, schema, err := platformqueue.NewFairDriver(t.Context(), pool(t, dsn), a, kind)
			if err != nil {
				t.Fatal(err)
			}
			second, _, err := platformqueue.NewFairDriver(t.Context(), pool(t, dsn), a, kind)
			if err != nil {
				t.Fatal(err)
			}
			var wg sync.WaitGroup
			var mu sync.Mutex
			seen := map[int64]bool{}
			counts := map[string]int{}
			for _, driver := range []riverdriver.Driver[*sql.Tx]{first, second} {
				wg.Go(func() {
					jobs, err := driver.GetExecutor().JobGetAvailable(t.Context(), fairParams(schema, kind, 4))
					if err != nil || len(jobs) != 4 {
						t.Error("concurrent lease", len(jobs), err)
						return
					}
					for i, job := range jobs {
						want := a.TenantID()
						if i%2 == 1 {
							want = b.TenantID()
						}
						tenant := jobTenant(t, job)
						if tenant != want {
							t.Error("batch filled one tenant before the next round")
						}
						mu.Lock()
						if seen[job.ID] {
							t.Error("duplicate lease")
						}
						seen[job.ID] = true
						counts[tenant]++
						mu.Unlock()
					}
				})
			}
			wg.Wait()
			if len(seen) != 8 || counts[a.TenantID()] != 4 || counts[b.TenantID()] != 4 {
				t.Fatal("unequal batch turns", len(seen), counts)
			}
		})
	}
}

func TestFairLeaseRollbackContentionAndLockedHeads(t *testing.T) {
	f, a, b := fixture(t)
	kind := "overlaid_moderation"
	fairJobs(t, a, b, kind, 3)
	driver, schema, err := platformqueue.NewFairDriver(t.Context(), pool(t, f.ModerationWorkerURL), a, kind)
	if err != nil {
		t.Fatal(err)
	}
	p := fairParams(schema, kind, 1)
	lock, err := f.Admin.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Rollback() }()
	if _, err = lock.ExecContext(t.Context(), `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "outbound_fair:"+schema+":"+kind); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if _, err = driver.GetExecutor().JobGetAvailable(ctx, p); !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, platformqueue.ErrFairLeasing) {
		t.Fatal("blocked lease ignored context", err)
	}
	if err = lock.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Admin.Exec(`CREATE FUNCTION fail_fair_lease() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'PRIVATE_LEASE_ERROR'; END $$; CREATE TRIGGER fail_fair_lease BEFORE UPDATE ON river_job FOR EACH ROW EXECUTE FUNCTION fail_fair_lease()`); err != nil {
		t.Fatal(err)
	}
	if _, err = driver.GetExecutor().JobGetAvailable(t.Context(), p); !errors.Is(err, platformqueue.ErrFairLeasing) || strings.Contains(err.Error(), "PRIVATE") {
		t.Fatal("lease failure leaked or escaped", err)
	}
	var changed int
	if err = f.Admin.QueryRow(`SELECT count(*) FROM river_job WHERE kind=$1 AND (state<>'available' OR attempt<>0 OR metadata ? 'overlaid_fair_turn')`, kind).Scan(&changed); err != nil || changed != 0 {
		t.Fatal("uncommitted lease escaped", changed, err)
	}
	if _, err = f.Admin.Exec(`DROP TRIGGER fail_fair_lease ON river_job; DROP FUNCTION fail_fair_lease()`); err != nil {
		t.Fatal(err)
	}
	head, err := f.Admin.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = head.Rollback() }()
	var lockedID int64
	if err = head.QueryRowContext(t.Context(), `SELECT id FROM river_job WHERE kind=$1 AND args->>'tenant_id'=$2 ORDER BY scheduled_at,id LIMIT 1 FOR UPDATE`, kind, a.TenantID()).Scan(&lockedID); err != nil {
		t.Fatal(err)
	}
	jobs, err := driver.GetExecutor().JobGetAvailable(t.Context(), p)
	if err != nil || len(jobs) != 1 || jobTenant(t, jobs[0]) != b.TenantID() {
		t.Fatal("locked head stalled another tenant", err)
	}
	if err = head.Rollback(); err != nil {
		t.Fatal(err)
	}
	jobs, err = driver.GetExecutor().JobGetAvailable(t.Context(), p)
	if err != nil || len(jobs) != 1 || jobs[0].ID != lockedID {
		t.Fatal("released head lost schedule ordering", err)
	}
}

func TestFairDriverMigrationAndInputBoundaries(t *testing.T) {
	f, a, b := fixture(t)
	fairJobs(t, a, b, "overlaid_chat_send", 1)
	service := pool(t, f.OutboundWorkerURL)
	if _, err := f.Admin.Exec(`DROP SEQUENCE overlaid_queue_turn_seq; DROP INDEX overlaid_outbound_tenant_turn; DELETE FROM overlaid_migrations WHERE name='0023_outbound_fair_turns.sql'`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := platformqueue.NewFairDriver(t.Context(), service, a, "overlaid_chat_send"); !errors.Is(err, platformqueue.ErrFairLeasing) {
		t.Fatal("unmigrated fair driver accepted", err)
	}
	raw, err := os.ReadFile("../../../migrations/0023_outbound_fair_turns.sql")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(f.OutboundWorkerURL)
	m, _ := url.Parse(f.ModerationWorkerURL)
	text := strings.NewReplacer("overlaid_outbound_worker", u.User.Username(), "overlaid_moderation_worker", m.User.Username()).Replace(string(raw))
	tx, err := f.Admin.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	body := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(text), "BEGIN;"), "COMMIT;")
	if _, err = tx.ExecContext(t.Context(), body); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(t.Context(), `SELECT 1/0`); err == nil {
		t.Fatal("fixture rollback did not fail")
	}
	_ = tx.Rollback()
	if _, _, err = platformqueue.NewFairDriver(t.Context(), service, a, "overlaid_chat_send"); !errors.Is(err, platformqueue.ErrFairLeasing) {
		t.Fatal("partial fair migration escaped", err)
	}
	if _, err = f.Admin.Exec(text); err != nil {
		t.Fatal(err)
	}
	driver, schema, err := platformqueue.NewFairDriver(t.Context(), service, a, "overlaid_chat_send")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = platformqueue.NewFairDriver(t.Context(), service, a, "overlaid_chat_dispatch"); !errors.Is(err, platformqueue.ErrFairLeasing) {
		t.Fatal("ingest policy accepted", err)
	}
	for _, change := range []func(*riverdriver.JobGetAvailableParams){func(p *riverdriver.JobGetAvailableParams) { p.Schema = "other" }, func(p *riverdriver.JobGetAvailableParams) { p.Queue = "moderation_outbound" }, func(p *riverdriver.JobGetAvailableParams) { p.MaxToLock = 1001 }, func(p *riverdriver.JobGetAvailableParams) { p.MaxAttemptedBy = 0 }, func(p *riverdriver.JobGetAvailableParams) { p.ClientID = "" }} {
		p := fairParams(schema, "overlaid_chat_send", 1)
		change(p)
		if _, err = driver.GetExecutor().JobGetAvailable(t.Context(), p); !errors.Is(err, platformqueue.ErrFairLeasing) {
			t.Fatal("invalid lease parameters accepted", err)
		}
	}
	p := fairParams(schema, "overlaid_chat_send", 0)
	if jobs, err := driver.GetExecutor().JobGetAvailable(t.Context(), p); err != nil || len(jobs) != 0 {
		t.Fatal("zero batch leased", err)
	}
	// A numeric string is not a cursor, and oversized JSON numbers must never
	// overflow the index/selector cast or let caller metadata choose a turn.
	if _, err = f.Admin.Exec(`UPDATE river_job SET metadata=jsonb_build_object('overlaid_fair_turn',CASE WHEN args->>'tenant_id'=$1 THEN '"999999999999999999"'::jsonb ELSE '999999999999999999999999'::jsonb END) WHERE kind='overlaid_chat_send'`, a.TenantID()); err != nil {
		t.Fatal(err)
	}
	jobs, err := driver.GetExecutor().JobGetAvailable(t.Context(), fairParams(schema, "overlaid_chat_send", 1))
	if err != nil || len(jobs) != 1 || jobTenant(t, jobs[0]) != a.TenantID() {
		t.Fatal("untrusted cursor metadata affected ordering or overflowed", err)
	}
	if _, err = f.Admin.Exec(`ALTER SEQUENCE overlaid_queue_turn_seq CYCLE`); err != nil {
		t.Fatal(err)
	}
	if _, _, err = platformqueue.NewFairDriver(t.Context(), service, a, "overlaid_chat_send"); !errors.Is(err, platformqueue.ErrFairLeasing) {
		t.Fatal("cycling fairness cursor accepted", err)
	}
	if _, err = f.Admin.Exec(`ALTER SEQUENCE overlaid_queue_turn_seq NO CYCLE CACHE 16`); err != nil {
		t.Fatal(err)
	}
	if _, _, err = platformqueue.NewFairDriver(t.Context(), service, a, "overlaid_chat_send"); !errors.Is(err, platformqueue.ErrFairLeasing) {
		t.Fatal("session-cached cursor accepted", err)
	}
	if _, err = f.Admin.Exec(`ALTER SEQUENCE overlaid_queue_turn_seq CACHE 1; GRANT UPDATE ON SEQUENCE overlaid_queue_turn_seq TO ` + u.User.Username()); err != nil {
		t.Fatal(err)
	}
	if _, _, err = platformqueue.NewFairDriver(t.Context(), service, a, "overlaid_chat_send"); !errors.Is(err, platformqueue.ErrFairLeasing) {
		t.Fatal("resettable cursor accepted", err)
	}
	if _, err = f.Admin.Exec(`REVOKE UPDATE ON SEQUENCE overlaid_queue_turn_seq FROM ` + u.User.Username()); err != nil {
		t.Fatal(err)
	}
	if _, _, err = platformqueue.NewFairDriver(t.Context(), service, a, "overlaid_chat_send"); err != nil {
		t.Fatal("restricted cursor not restored", err)
	}
}
