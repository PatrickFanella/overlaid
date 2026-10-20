package platformingest

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformqueue"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"github.com/jackc/pgx/v5/pgconn"
)

func streamEvent(t *testing.T, tenant, native string, kind platform.StreamEventKind) platform.StreamEvent {
	t.Helper()
	now := time.Now().UTC()
	var online *platform.StreamOnline
	var offline *platform.StreamOffline
	var update *platform.ChannelUpdate
	switch kind {
	case platform.StreamOnlineKind:
		online = &platform.StreamOnline{StreamID: "native-stream", Type: "live", StartedAt: now.Add(-time.Minute)}
	case platform.StreamOfflineKind:
		offline = &platform.StreamOffline{}
	case platform.ChannelUpdateKind:
		update = &platform.ChannelUpdate{Title: "private stream title", Language: "en", ContentClassificationLabels: []string{"MatureGame"}}
	}
	e, err := platform.NewStreamEvent(platform.Connection{TenantID: tenant, ID: "twitch:channel", Platform: "twitch"}, native, kind, now, now, platform.Identity{ID: "channel", Login: "Channel"}, online, offline, update)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func streamCounts(t *testing.T, db *tenantdb.DB, want int) {
	t.Helper()
	for _, table := range []string{"normalized_stream_events", "platform_stream_payloads"} {
		var count int
		if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM `+table).Scan(&count); err != nil || count != want {
			t.Fatalf("%s count=%d want=%d err=%v", table, count, want, err)
		}
	}
	var count int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM river_job WHERE kind='overlaid_stream_dispatch'`).Scan(&count); err != nil || count != want {
		t.Fatalf("stream jobs count=%d want=%d err=%v", count, want, err)
	}
}

func TestStreamAtomicDuplicatesConflictsAndRestart(t *testing.T) {
	f, a, b := fixture(t)
	s, _ := New(a)
	e := streamEvent(t, a.TenantID(), "same-delivery", platform.ChannelUpdateKind)
	var accepted atomic.Int32
	var group sync.WaitGroup
	for range 20 {
		group.Add(1)
		go func() {
			defer group.Done()
			added, err := s.IngestStream(t.Context(), e, json.RawMessage(`{"privateNative":"original"}`))
			if err != nil {
				t.Error(err)
			} else if added {
				accepted.Add(1)
			}
		}()
	}
	group.Wait()
	if accepted.Load() != 1 {
		t.Fatal("duplicate admitted", accepted.Load())
	}
	streamCounts(t, a, 1)
	streamCounts(t, b, 0)
	other, _ := New(b)
	if _, err := other.IngestStream(t.Context(), e, nil); !errors.Is(err, ErrInvalid) {
		t.Fatal("foreign scope accepted", err)
	}
	if added, err := other.IngestStream(t.Context(), streamEvent(t, b.TenantID(), "same-delivery", platform.ChannelUpdateKind), json.RawMessage(`{}`)); err != nil || !added {
		t.Fatal(added, err)
	}
	streamCounts(t, b, 1)
	if _, err := b.ExecContext(t.Context(), `INSERT INTO platform_stream_payloads(tenant_id,event_id,payload) VALUES($1,$2,'{}')`, a.TenantID(), e.ID); err == nil {
		t.Fatal("forged native tenant accepted")
	}
	reopened, err := tenantdb.Open(f.RuntimeURL, a.TenantID())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	restarted, _ := New(reopened)
	e.ReceivedAt = e.ReceivedAt.Add(time.Second)
	if added, err := restarted.IngestStream(t.Context(), e, json.RawMessage(`{"changedTransport":true}`)); err != nil || added {
		t.Fatal("restart duplicated delivery", added, err)
	}
	changed := e
	data := *e.Update
	data.Title = "conflicting normalized title"
	changed.Update = &data
	if added, err := restarted.IngestStream(t.Context(), changed, nil); !errors.Is(err, ErrConflict) || added {
		t.Fatal("conflicting duplicate accepted", added, err)
	}
	var native, envelope string
	var args []byte
	var id, deadline int64
	if err := a.QueryRowContext(t.Context(), `SELECT p.payload::text,e.envelope::text,j.args,j.id,e.dispatch_deadline FROM normalized_stream_events e JOIN platform_stream_payloads p ON p.tenant_id=e.tenant_id AND p.event_id=e.id JOIN river_job j ON j.overlaid_stream_tenant_id=e.tenant_id AND j.overlaid_stream_event_id=e.id`).Scan(&native, &envelope, &args, &id, &deadline); err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if json.Unmarshal(args, &decoded) != nil || len(decoded) != 4 || strings.Contains(string(args), "private") || deadline != e.OccurredAt.Unix()+120 || decoded["deadline"] != float64(deadline) || !strings.Contains(native, "privateNative") || strings.Contains(envelope, "conflicting") {
		t.Fatal("original input/deadline or safe args lost", native, envelope, string(args))
	}
	// An existing retained identity never reconstructs a removed/terminal job.
	if _, err = f.Admin.Exec(`DELETE FROM river_job WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if added, err := restarted.IngestStream(t.Context(), e, nil); err != nil || added {
		t.Fatal(added, err)
	}
	var jobs int
	if err = a.QueryRowContext(t.Context(), `SELECT count(*) FROM river_job`).Scan(&jobs); err != nil || jobs != 0 {
		t.Fatal("duplicate reconstructed job", jobs, err)
	}
}

func TestStreamKindIsolationJobConstraintsAndRollback(t *testing.T) {
	f, a, _ := fixture(t)
	s, _ := New(a)
	e := streamEvent(t, a.TenantID(), "delivery", platform.StreamOnlineKind)
	if _, err := s.IngestStream(t.Context(), e, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	for _, worker := range []struct{ url, kind string }{{f.WorkerURL, "overlaid_chat_dispatch"}, {f.OutboundWorkerURL, "overlaid_chat_send"}, {f.ModerationWorkerURL, "overlaid_moderation"}} {
		pool, err := sql.Open("pgx", worker.url)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = platformqueue.Validate(t.Context(), pool, a, worker.kind); err != nil {
			t.Fatal("existing role invalidated", err)
		}
		tx, err := pool.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		var count int
		if _, err = tx.Exec(`SELECT set_config('app.tenant_id',$1,true)`, a.TenantID()); err != nil {
			t.Fatal(err)
		}
		if err = tx.QueryRow(`SELECT count(*) FROM river_job WHERE kind='overlaid_stream_dispatch'`).Scan(&count); err != nil || count != 0 {
			t.Fatal("existing worker exposed stream jobs", count, err)
		}
		if err = tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		if err = pool.Close(); err != nil {
			t.Fatal(err)
		}
	}
	for _, mutation := range []string{"deadline", "connection", "queue", "private_argument"} {
		t.Run(mutation, func(t *testing.T) {
			args := map[string]any{"tenant_id": e.TenantID, "connection_id": e.ConnectionID, "event_id": e.ID, "deadline": e.OccurredAt.Unix() + 120}
			queue := "stream_followup"
			switch mutation {
			case "deadline":
				args["deadline"] = e.OccurredAt.Unix() + 121
			case "connection":
				args["connection_id"] = "twitch:other"
			case "queue":
				queue = "ingest_followup"
			case "private_argument":
				args["private"] = "input"
			}
			raw, _ := json.Marshal(args)
			// Remove the original job transactionally so uniqueness cannot mask a
			// missing composite binding or argument constraint.
			tx, err := a.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback() }()
			if _, err = tx.ExecContext(t.Context(), `DELETE FROM river_job WHERE kind='overlaid_stream_dispatch'`); err != nil {
				t.Fatal(err)
			}
			_, err = tx.ExecContext(t.Context(), `INSERT INTO river_job(kind,args,queue,max_attempts,priority,tags) VALUES('overlaid_stream_dispatch',$1,$2,3,4,'{}')`, raw, queue)
			var pgerr *pgconn.PgError
			if !errors.As(err, &pgerr) || (pgerr.Code != "23503" && pgerr.Code != "23514") {
				t.Fatal("unsafe job accepted", err)
			}
		})
	}
	u, _ := url.Parse(f.RuntimeURL)
	if _, err := f.Admin.Exec(`REVOKE INSERT ON river_job FROM ` + u.User.Username()); err != nil {
		t.Fatal(err)
	}
	if added, err := s.IngestStream(t.Context(), streamEvent(t, a.TenantID(), "failed", platform.StreamOfflineKind), json.RawMessage(`{}`)); err == nil || added {
		t.Fatal("failed enqueue committed", added, err)
	}
	streamCounts(t, a, 1)
}

func TestStreamSharedLimitsAndInvalidBinding(t *testing.T) {
	f, a, _ := fixture(t)
	s, _ := New(a)
	for _, mode := range []string{"id", "version", "trace", "channel", "roles", "old", "future", "received", "raw_array", "raw_null", "raw_limit"} {
		t.Run(mode, func(t *testing.T) {
			e := streamEvent(t, a.TenantID(), mode, platform.ChannelUpdateKind)
			native := json.RawMessage(`{}`)
			switch mode {
			case "id":
				e.ID = "forged"
			case "version":
				e.Version++
			case "trace":
				e.TraceID = "forged"
			case "channel":
				e.Channel.ID = "other"
			case "roles":
				e.Channel.Roles = []string{"moderator"}
			case "old":
				e.OccurredAt = time.Now().Add(-400 * 24 * time.Hour)
			case "future":
				e.OccurredAt = time.Now().Add(time.Hour)
			case "received":
				e.ReceivedAt = e.OccurredAt.Add(-time.Hour)
			case "raw_array":
				native = json.RawMessage(`[]`)
			case "raw_null":
				native = json.RawMessage(`null`)
			case "raw_limit":
				native = json.RawMessage(`{"data":"` + strings.Repeat("x", 65536) + `"}`)
			}
			if added, err := s.IngestStream(t.Context(), e, native); !errors.Is(err, ErrInvalid) || added {
				t.Fatal("invalid stream accepted", added, err)
			}
		})
	}
	streamCounts(t, a, 0)
	if _, err := f.Admin.Exec(`UPDATE platform_connections SET revoked_at=now() WHERE tenant_id=$1`, a.TenantID()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.IngestStream(t.Context(), streamEvent(t, a.TenantID(), "revoked", platform.StreamOfflineKind), nil); !errors.Is(err, ErrInvalid) {
		t.Fatal("revoked source accepted", err)
	}
	if _, err := f.Admin.Exec(`UPDATE platform_connections SET revoked_at=NULL WHERE tenant_id=$1;`, a.TenantID()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Admin.Exec(`UPDATE plans SET limits=jsonb_set(limits,'{storedEvents}','3') WHERE id='unlimited'`); err != nil {
		t.Fatal(err)
	}
	var accepted atomic.Int32
	var group sync.WaitGroup
	for i := range 16 {
		group.Add(1)
		go func() {
			defer group.Done()
			var added bool
			var err error
			if i%2 == 0 {
				added, err = s.IngestChat(t.Context(), event(t, a.TenantID(), fmt.Sprint(i)), nil)
			} else {
				added, err = s.IngestStream(t.Context(), streamEvent(t, a.TenantID(), fmt.Sprint(i), platform.StreamOfflineKind), nil)
			}
			if err != nil && !errors.Is(err, tenantdb.ErrLimit) {
				t.Error(err)
			} else if added {
				accepted.Add(1)
			}
		}()
	}
	group.Wait()
	var count int
	if err := a.QueryRowContext(t.Context(), retainedEventCount).Scan(&count); err != nil || count != 3 || accepted.Load() != 3 {
		t.Fatal("combined admission limit exceeded", count, accepted.Load(), err)
	}
}

func TestStreamLifecycleAndPrivateExport(t *testing.T) {
	f, a, b := fixture(t)
	for _, db := range []*tenantdb.DB{a, b} {
		s, _ := New(db)
		if _, err := s.IngestStream(t.Context(), streamEvent(t, db.TenantID(), "delivery", platform.ChannelUpdateKind), json.RawMessage(`{"privateNative":"input"}`)); err != nil {
			t.Fatal(err)
		}
	}
	control, err := tenancy.Open(f.RuntimeURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = control.Close() }()
	exported, err := control.Export(t.Context(), owner, a.TenantID())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(exported)
	if !strings.Contains(string(raw), "normalized_stream_events") || !strings.Contains(string(raw), "private stream title") || strings.Contains(string(raw), "privateNative") || strings.Contains(string(raw), "platform_stream_payloads") || strings.Contains(string(raw), "river_job") {
		t.Fatal("unsafe export", string(raw))
	}
	if err = control.DeleteViewer(t.Context(), owner, a.TenantID(), "channel", "Channel"); err != nil {
		t.Fatal(err)
	}
	streamCounts(t, a, 0)
	streamCounts(t, b, 1)
	if _, err = f.Admin.Exec(`UPDATE normalized_stream_events SET received_at=now()-interval '400 days' WHERE tenant_id=$1`, b.TenantID()); err != nil {
		t.Fatal(err)
	}
	if err = control.Expire(t.Context()); err != nil {
		t.Fatal(err)
	}
	streamCounts(t, b, 0)
	s, _ := New(b)
	if _, err = s.IngestStream(t.Context(), streamEvent(t, b.TenantID(), "connection-delete", platform.StreamOfflineKind), json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if _, err = b.ExecContext(t.Context(), `DELETE FROM platform_connections WHERE /*tenant:platform_connections*/`); err != nil {
		t.Fatal(err)
	}
	streamCounts(t, b, 0)
	if _, err = f.Admin.Exec(`INSERT INTO platform_connections(tenant_id,provider,provider_user_id,login) VALUES($1,'twitch','channel','channel')`, b.TenantID()); err != nil {
		t.Fatal(err)
	}
	if _, err = s.IngestStream(t.Context(), streamEvent(t, b.TenantID(), "tenant-delete", platform.StreamOfflineKind), json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if err = control.DeleteTenant(t.Context(), owner, b.TenantID()); err != nil {
		t.Fatal(err)
	}
	streamCounts(t, b, 0)
}

func TestStreamMigrationPreservesChatAndFailsClosed(t *testing.T) {
	f, a, _ := fixture(t)
	s, _ := New(a)
	if _, err := s.IngestChat(t.Context(), event(t, a.TenantID(), "before-migration"), json.RawMessage(`{"private":"chat"}`)); err != nil {
		t.Fatal(err)
	}
	var before, after string
	if err := f.Admin.QueryRow(`SELECT jsonb_agg(to_jsonb(j) - ARRAY['overlaid_stream_tenant_id','overlaid_stream_event_id','overlaid_stream_connection_id','overlaid_stream_deadline'])::text FROM river_job j`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Admin.Exec(`ALTER TABLE river_job DROP COLUMN overlaid_stream_tenant_id CASCADE; ALTER TABLE river_job DROP COLUMN overlaid_stream_event_id CASCADE; ALTER TABLE river_job DROP COLUMN overlaid_stream_connection_id CASCADE; ALTER TABLE river_job DROP COLUMN overlaid_stream_deadline CASCADE; ALTER TABLE river_job DROP CONSTRAINT overlaid_stream_args; DROP TABLE platform_stream_payloads; DROP TABLE normalized_stream_events; DELETE FROM overlaid_migrations WHERE name='0031_stream_event_ingest.sql'`); err != nil {
		t.Fatal(err)
	}
	if db, err := tenantdb.Open(f.RuntimeURL, a.TenantID()); err == nil {
		_ = db.Close()
		t.Fatal("missing migration accepted")
	}
	u, _ := url.Parse(f.RuntimeURL)
	if _, err := f.Admin.Exec(`CREATE POLICY tenant_isolation ON river_job TO ` + u.User.Username() + ` USING(false) WITH CHECK(false)`); err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "0031_stream_event_ingest.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Admin.Exec(strings.ReplaceAll(string(migration), "overlaid_runtime", u.User.Username())); err != nil {
		t.Fatal(err)
	}
	if err = f.Admin.QueryRow(`SELECT jsonb_agg(to_jsonb(j) - ARRAY['overlaid_stream_tenant_id','overlaid_stream_event_id','overlaid_stream_connection_id','overlaid_stream_deadline'])::text FROM river_job j`).Scan(&after); err != nil || before != after {
		t.Fatal("existing job changed", before, after, err)
	}
	counts(t, a, 1)
}

func TestStreamSchemaDriftsFailClosed(t *testing.T) {
	for _, test := range []struct{ name, sql string }{
		{"RLS", `ALTER TABLE normalized_stream_events NO FORCE ROW LEVEL SECURITY`},
		{"job cascade", `ALTER TABLE river_job DROP CONSTRAINT overlaid_stream_job; ALTER TABLE river_job ADD CONSTRAINT overlaid_stream_job FOREIGN KEY(overlaid_stream_tenant_id,overlaid_stream_event_id,overlaid_stream_connection_id,overlaid_stream_deadline) REFERENCES normalized_stream_events(tenant_id,id,connection_id,dispatch_deadline)`},
		{"argument check", `ALTER TABLE river_job DROP CONSTRAINT overlaid_stream_args; ALTER TABLE river_job ADD CONSTRAINT overlaid_stream_args CHECK(true)`},
		{"unique job", `DROP INDEX overlaid_stream_dispatch`},
		{"tenant policy", `ALTER POLICY tenant_isolation ON normalized_stream_events USING(true) WITH CHECK(true)`},
		{"extra policy", `CREATE POLICY broad_streams ON platform_stream_payloads USING(true) WITH CHECK(true)`},
	} {
		t.Run(test.name, func(t *testing.T) {
			f, a, _ := fixture(t)
			if _, err := f.Admin.Exec(test.sql); err != nil {
				t.Fatal(err)
			}
			if db, err := tenantdb.Open(f.RuntimeURL, a.TenantID()); err == nil {
				_ = db.Close()
				t.Fatal("unsafe stream schema accepted")
			}
		})
	}
}

// Keep contexts bounded when checking cancellation after admission lock waits.
func TestStreamCanceledAdmissionLeavesNoRows(t *testing.T) {
	_, a, _ := fixture(t)
	s, _ := New(a)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if added, err := s.IngestStream(ctx, streamEvent(t, a.TenantID(), "canceled", platform.StreamOfflineKind), json.RawMessage(`{}`)); !errors.Is(err, context.Canceled) || added {
		t.Fatal("canceled admission committed", added, err)
	}
	streamCounts(t, a, 0)
}

func TestStreamRetentionUsesPostLockDatabaseTime(t *testing.T) {
	f, a, _ := fixture(t)
	s, _ := New(a)
	if _, err := f.Admin.Exec(`UPDATE tenants SET retention_days=1 WHERE id=$1`, a.TenantID()); err != nil {
		t.Fatal(err)
	}
	lock, err := f.Admin.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Rollback() }()
	// Lock the connection rather than the tenant: the eligibility clock must
	// follow every admission lock, including concurrent reauthorization.
	if _, err = lock.Exec(`SELECT 1 FROM platform_connections WHERE tenant_id=$1 FOR UPDATE`, a.TenantID()); err != nil {
		t.Fatal(err)
	}
	var now time.Time
	if err = lock.QueryRow(`SELECT clock_timestamp()`).Scan(&now); err != nil {
		t.Fatal(err)
	}
	now = now.UTC()
	e := streamEvent(t, a.TenantID(), "post-lock-expiry", platform.StreamOfflineKind)
	e.OccurredAt = now.Add(-24*time.Hour + 3*time.Second)
	e.ReceivedAt = e.OccurredAt
	result := make(chan error, 1)
	go func() {
		_, err := s.IngestStream(t.Context(), e, json.RawMessage(`{}`))
		result <- err
	}()
	u, _ := url.Parse(f.RuntimeURL)
	deadline := time.Now().Add(5 * time.Second)
	for {
		select {
		case early := <-result:
			t.Fatal("admission returned before the held connection lock", early)
		default:
		}
		var blocked bool
		if err = f.Admin.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE usename=$1 AND wait_event_type='Lock')`, u.User.Username()).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("admission did not reach the held connection lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Wait using the same database clock; a transaction-start clock would still
	// consider this event inside retention after the connection lock is released.
	if _, err = f.Admin.Exec(`SELECT pg_sleep(greatest(0,extract(epoch FROM $1::timestamptz+interval '3.1 seconds'-clock_timestamp())))`, now); err != nil {
		t.Fatal(err)
	}
	if err = lock.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, ErrInvalid) {
			t.Fatal("post-lock expired source accepted", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("admission did not finish after connection release")
	}
	streamCounts(t, a, 0)
}
