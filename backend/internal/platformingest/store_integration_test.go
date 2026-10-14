package platformingest

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"git.subcult.tv/PatrickFanella/over-laid/internal/testpostgres"
	"github.com/jackc/pgx/v5/pgconn"
)

const otherTenant = "00000000-0000-4000-8000-000000000099"
const owner = "00000000-0000-4000-8000-000000000002"

func fixture(t *testing.T) (testpostgres.TenantFixture, *tenantdb.DB, *tenantdb.DB) {
	t.Helper()
	f := testpostgres.NewTenantFixture(t, "")
	if _, err := f.Admin.Exec(`INSERT INTO tenants(id,slug,name,owner_account_id,plan_id) VALUES($1,'ingest-other','Other',$2,'unlimited');`, otherTenant, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Admin.Exec(`INSERT INTO tenant_memberships(tenant_id,account_id,role) VALUES($1,$2,'owner')`, otherTenant, owner); err != nil {
		t.Fatal(err)
	}
	for _, tenant := range []string{tenantdb.BootstrapID, otherTenant} {
		if _, err := f.Admin.Exec(`INSERT INTO platform_connections(tenant_id,provider,provider_user_id,login) VALUES($1,'twitch','channel','channel')`, tenant); err != nil {
			t.Fatal(err)
		}
	}
	db, err := tenantdb.Open(f.RuntimeURL, tenantdb.BootstrapID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	b, err := db.Scoped(otherTenant)
	if err != nil {
		t.Fatal(err)
	}
	return f, db, b
}

func event(t *testing.T, tenant, delivery string) platform.ChatEvent {
	t.Helper()
	now := time.Now().UTC()
	e, err := platform.NewChatEvent(platform.Connection{TenantID: tenant, ID: "twitch:channel", Platform: "twitch"}, delivery, now, now, platform.ChatMessage{MessageID: delivery, Text: "private text", Actor: platform.Identity{ID: "viewer", Login: "Viewer"}, Channel: platform.Identity{ID: "channel"}})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func counts(t *testing.T, db *tenantdb.DB, want int) {
	t.Helper()
	for _, table := range []string{"normalized_chat_events", "platform_event_payloads", "river_job"} {
		var count int
		if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM `+table).Scan(&count); err != nil || count != want {
			t.Fatalf("%s count=%d want=%d err=%v", table, count, want, err)
		}
	}
}

func TestAtomicDuplicatesIsolationAndRestart(t *testing.T) {
	f, a, b := fixture(t)
	s, _ := New(a)
	e := event(t, a.TenantID(), "same-delivery")
	native := json.RawMessage(`{"privateNative":"payload"}`)
	var accepted atomic.Int32
	var workers sync.WaitGroup
	for range 24 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			added, err := s.IngestChat(t.Context(), e, native)
			if err != nil {
				t.Error(err)
			} else if added {
				accepted.Add(1)
			}
		}()
	}
	workers.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("accepted %d duplicates", accepted.Load())
	}
	counts(t, a, 1)
	counts(t, b, 0)
	other, _ := New(b)
	if _, err := other.IngestChat(t.Context(), e, native); !errors.Is(err, ErrInvalid) {
		t.Fatal("foreign envelope accepted", err)
	}
	if added, err := other.IngestChat(t.Context(), event(t, b.TenantID(), "same-delivery"), native); err != nil || !added {
		t.Fatal(added, err)
	}
	counts(t, b, 1)
	reopened, err := tenantdb.Open(f.RuntimeURL, a.TenantID())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restarted, _ := New(reopened)
	if added, err := restarted.IngestChat(t.Context(), e, json.RawMessage(`{"changed":true}`)); err != nil || added {
		t.Fatal("restart duplicated delivery", added, err)
	}
	var raw string
	if err := a.QueryRowContext(t.Context(), `SELECT payload::text FROM platform_event_payloads`).Scan(&raw); err != nil || !strings.Contains(raw, "privateNative") {
		t.Fatal("duplicate overwrote raw payload", raw, err)
	}
	var args []byte
	if err := a.QueryRowContext(t.Context(), `SELECT args FROM river_job`).Scan(&args); err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(args, &decoded); err != nil || len(decoded) != 4 || strings.Contains(string(args), "private") {
		t.Fatal("unsafe job arguments", string(args), err)
	}
	if decoded["deadline"] != float64(e.OccurredAt.Add(2*time.Minute).Unix()) {
		t.Fatal("dispatch deadline detached from event", decoded)
	}
	// Isolate the connection FK from the one-job-per-event unique index.
	if _, err := a.ExecContext(t.Context(), `DELETE FROM river_job WHERE overlaid_event_id=$1`, e.ID); err != nil {
		t.Fatal(err)
	}
	forged, _ := json.Marshal(map[string]any{"tenant_id": e.TenantID, "event_id": e.ID, "connection_id": "wrong-connection", "deadline": time.Now().Unix()})
	_, err = a.ExecContext(t.Context(), `INSERT INTO river_job(kind,args,max_attempts,tags) VALUES('overlaid_chat_dispatch',$1,3,'{}')`, forged)
	var pgerr *pgconn.PgError
	if !errors.As(err, &pgerr) || pgerr.Code != "23503" || pgerr.ConstraintName != "overlaid_event_job" {
		t.Fatal("connection FK not enforced", err)
	}
	tx, err := a.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.EnqueueChatDispatch(t.Context(), e.ConnectionID, e.ID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	counts(t, a, 1)
	if _, err := b.ExecContext(t.Context(), `INSERT INTO platform_event_payloads(tenant_id,event_id,payload) VALUES($1,$2,'{}')`, a.TenantID(), e.ID); err == nil {
		t.Fatal("forged tenant insert accepted")
	}
}

func TestEnqueueFailureRollsBackAllData(t *testing.T) {
	f, a, _ := fixture(t)
	var migrationCount, riverCount int
	if err := f.Admin.QueryRow(`SELECT count(*) FROM overlaid_migrations WHERE name IN ('0006_river.sql','0007_platform_ingest.sql')`).Scan(&migrationCount); err != nil || migrationCount != 2 {
		t.Fatal("initialization omitted migration receipts", migrationCount, err)
	}
	if err := f.Admin.QueryRow(`SELECT count(*) FROM river_migration WHERE line='main'`).Scan(&riverCount); err != nil || riverCount != 7 {
		t.Fatal("pinned River migration ledger incomplete", riverCount, err)
	}
	u, _ := url.Parse(f.RuntimeURL)
	if _, err := f.Admin.Exec(`REVOKE INSERT ON river_job FROM ` + u.User.Username()); err != nil {
		t.Fatal(err)
	}
	s, _ := New(a)
	if added, err := s.IngestChat(t.Context(), event(t, a.TenantID(), "failed"), json.RawMessage(`{"private":true}`)); err == nil || added {
		t.Fatal("failed enqueue committed", added, err)
	}
	counts(t, a, 0)
}

func TestInvalidBindingAndBoundedAdmission(t *testing.T) {
	f, a, _ := fixture(t)
	s, _ := New(a)
	for _, mode := range []string{"id", "kind", "trace", "channel", "connection", "old", "native_array", "native_size"} {
		t.Run(mode, func(t *testing.T) {
			e := event(t, a.TenantID(), mode)
			native := json.RawMessage(`{}`)
			switch mode {
			case "id":
				e.ID = "forged"
			case "kind":
				e.Kind = "follow"
			case "trace":
				e.TraceID = "forged"
			case "channel":
				e.Chat.Channel.ID = "foreign-channel"
			case "connection":
				e.ConnectionID = "unbound"
				e, _ = platform.NewChatEvent(platform.Connection{TenantID: e.TenantID, ID: e.ConnectionID, Platform: e.Platform}, e.NativeEventID, e.OccurredAt, e.ReceivedAt, e.Chat)
			case "old":
				e.OccurredAt = time.Now().Add(-400 * 24 * time.Hour)
			case "native_array":
				native = json.RawMessage(`[]`)
			case "native_size":
				native = json.RawMessage(`{"text":"` + strings.Repeat("x", 65536) + `"}`)
			}
			if added, err := s.IngestChat(t.Context(), e, native); !errors.Is(err, ErrInvalid) || added {
				t.Fatal("invalid event accepted", added, err)
			}
		})
	}
	counts(t, a, 0)
	if _, err := f.Admin.Exec(`UPDATE platform_connections SET revoked_at=now() WHERE tenant_id=$1`, a.TenantID()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.IngestChat(t.Context(), event(t, a.TenantID(), "revoked"), nil); !errors.Is(err, ErrInvalid) {
		t.Fatal("revoked connection accepted", err)
	}
	if _, err := f.Admin.Exec(`UPDATE platform_connections SET revoked_at=NULL WHERE tenant_id=$1;`, a.TenantID()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Admin.Exec(`UPDATE plans SET limits=jsonb_set(limits,'{storedEvents}','2') WHERE id='unlimited'`); err != nil {
		t.Fatal(err)
	}
	var accepted atomic.Int32
	var workers sync.WaitGroup
	for i := range 12 {
		e := event(t, a.TenantID(), fmt.Sprint("bounded-", i))
		workers.Add(1)
		go func() {
			defer workers.Done()
			added, err := s.IngestChat(t.Context(), e, json.RawMessage(`{}`))
			if err != nil && !errors.Is(err, tenantdb.ErrLimit) {
				t.Error(err)
			} else if added {
				accepted.Add(1)
			}
		}()
	}
	workers.Wait()
	if accepted.Load() != 2 {
		t.Fatal("storage admission exceeded limit", accepted.Load())
	}
	counts(t, a, 2)
}

func TestRetentionExportAndViewerTenantDeletion(t *testing.T) {
	f, a, b := fixture(t)
	for _, db := range []*tenantdb.DB{a, b} {
		s, _ := New(db)
		if _, err := s.IngestChat(t.Context(), event(t, db.TenantID(), "retained"), json.RawMessage(`{"privateNative":"payload"}`)); err != nil {
			t.Fatal(err)
		}
	}
	control, err := tenancy.Open(f.RuntimeURL)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	exported, err := control.Export(t.Context(), owner, a.TenantID())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(exported)
	if !strings.Contains(string(raw), "normalized_chat_events") || strings.Contains(string(raw), "privateNative") || strings.Contains(string(raw), "platform_event_payloads") || strings.Contains(string(raw), "river_job") {
		t.Fatal("export leaked raw/job storage", string(raw))
	}
	if err := control.DeleteViewer(t.Context(), "00000000-0000-4000-8000-000000000003", a.TenantID(), "viewer", "Viewer"); !errors.Is(err, tenancy.ErrDenied) {
		t.Fatal("foreign viewer deletion accepted", err)
	}
	if err := control.DeleteViewer(t.Context(), owner, a.TenantID(), "viewer", "Viewer"); err != nil {
		t.Fatal(err)
	}
	counts(t, a, 0)
	counts(t, b, 1)
	if _, err := f.Admin.Exec(`UPDATE normalized_chat_events SET received_at=now()-interval '400 days' WHERE tenant_id=$1`, b.TenantID()); err != nil {
		t.Fatal(err)
	}
	if err := control.Expire(t.Context()); err != nil {
		t.Fatal(err)
	}
	counts(t, b, 0)
	s, _ := New(b)
	if _, err := s.IngestChat(t.Context(), event(t, b.TenantID(), "tenant-delete"), json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if err := control.DeleteTenant(t.Context(), owner, b.TenantID()); err != nil {
		t.Fatal(err)
	}
	counts(t, b, 0)
}
