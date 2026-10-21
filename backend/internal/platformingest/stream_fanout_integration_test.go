package platformingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

func fanoutTargets(t *testing.T, scopes []*tenantdb.DB, original platform.StreamEvent) []StreamTarget {
	t.Helper()
	targets := make([]StreamTarget, len(scopes))
	for i, scope := range scopes {
		event, err := platform.NewStreamEvent(platform.Connection{TenantID: scope.TenantID(), ID: original.ConnectionID, Platform: original.Platform}, original.NativeEventID, original.Kind, original.OccurredAt, original.ReceivedAt, original.Channel, original.Online, original.Offline, original.Update)
		if err != nil {
			t.Fatal(err)
		}
		store, err := New(scope)
		if err != nil {
			t.Fatal(err)
		}
		targets[i] = StreamTarget{Store: store, Event: event, Native: json.RawMessage(`{"private":"original"}`)}
	}
	return targets
}

func TestStreamFanoutAtomicDuplicatesOrderAndLifecycle(t *testing.T) {
	f, a, b := fixture(t)
	original := streamEvent(t, a.TenantID(), "fanout", platform.ChannelUpdateKind)
	targets := fanoutTargets(t, []*tenantdb.DB{b, a}, original)
	// Input order differs from lock order. Concurrent reverse callers cannot
	// deadlock or renew native content, original receipt, jobs or deadlines.
	var wg sync.WaitGroup
	for i := range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			batch := append([]StreamTarget(nil), targets...)
			if i%2 == 0 {
				batch[0], batch[1] = batch[1], batch[0]
			}
			result, err := IngestStreamFanout(t.Context(), batch)
			if err != nil || len(result) != 2 || result[0] != result[1] {
				t.Error("partial admission", result, err)
			}
		}()
	}
	wg.Wait()
	for _, scope := range []*tenantdb.DB{a, b} {
		streamCounts(t, scope, 1)
	}
	snapshot := func() string {
		t.Helper()
		var value string
		if err := f.Admin.QueryRow(`SELECT jsonb_build_object('events',(SELECT jsonb_agg(to_jsonb(e) ORDER BY tenant_id) FROM normalized_stream_events e),'payloads',(SELECT jsonb_agg(to_jsonb(p) ORDER BY tenant_id) FROM platform_stream_payloads p),'jobs',(SELECT jsonb_agg(to_jsonb(j) ORDER BY id) FROM river_job j))::text`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := snapshot()
	reopened, err := tenantdb.Open(f.RuntimeURL, a.TenantID())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	other, err := reopened.Scoped(b.TenantID())
	if err != nil {
		t.Fatal(err)
	}
	original.ReceivedAt = original.ReceivedAt.Add(time.Second)
	retry := fanoutTargets(t, []*tenantdb.DB{reopened, other}, original)
	for i := range retry {
		retry[i].Native = json.RawMessage(`{"private":"replacement refused by noop"}`)
	}
	result, err := IngestStreamFanout(t.Context(), retry)
	if err != nil || len(result) != 2 || result[0] || result[1] || snapshot() != before {
		t.Fatal("restart renewed duplicate", result, err)
	}
	// Distinguish duplicate/new results even when input order is reversed.
	if _, err = f.Admin.Exec(`DELETE FROM normalized_stream_events WHERE tenant_id=$1`, a.TenantID()); err != nil {
		t.Fatal(err)
	}
	result, err = IngestStreamFanout(t.Context(), targets)
	if err != nil || len(result) != 2 || result[0] || !result[1] {
		t.Fatal("input order lost", result, err)
	}
	// The transaction must leave no pooled tenant context for later immutable views.
	for _, scope := range []*tenantdb.DB{a, b} {
		streamCounts(t, scope, 1)
	}
	if _, err = f.Admin.Exec(`DELETE FROM platform_connections WHERE tenant_id=$1`, a.TenantID()); err != nil {
		t.Fatal(err)
	}
	streamCounts(t, a, 0)
	streamCounts(t, b, 1)
	if _, err = f.Admin.Exec(`DELETE FROM tenants WHERE id=$1`, b.TenantID()); err != nil {
		t.Fatal(err)
	}
	streamCounts(t, b, 0)
}

func TestStreamFanoutLaterRefusalRollsBackAllNewWork(t *testing.T) {
	for _, mode := range []string{"limit", "revoked", "conflict", "job_failure", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			f, a, b := fixture(t)
			original := streamEvent(t, a.TenantID(), "fanout-refused", platform.ChannelUpdateKind)
			targets := fanoutTargets(t, []*tenantdb.DB{b, a}, original)
			want := ErrInvalid
			beforeB := 0
			ctx := t.Context()
			switch mode {
			case "limit":
				// Only B exhausts the combined chat+stream plan; A's new insert must roll back.
				store, _ := New(b)
				if _, err := store.IngestChat(ctx, event(t, b.TenantID(), "charge"), nil); err != nil {
					t.Fatal(err)
				}
				if _, err := f.Admin.Exec(`UPDATE plans SET limits=jsonb_set(limits,'{storedEvents}','1')`); err != nil {
					t.Fatal(err)
				}
				want = tenantdb.ErrLimit
			case "revoked":
				if _, err := f.Admin.Exec(`UPDATE platform_connections SET revoked_at=clock_timestamp() WHERE tenant_id=$1`, b.TenantID()); err != nil {
					t.Fatal(err)
				}
			case "conflict":
				existing := targets[0].Event
				existing.Update = &platform.ChannelUpdate{Title: "conflicting private source", Language: "en", ContentClassificationLabels: []string{"MatureGame"}}
				if _, err := targets[0].Store.IngestStream(ctx, existing, json.RawMessage(`{}`)); err != nil {
					t.Fatal(err)
				}
				beforeB = 1
				want = ErrConflict
			case "job_failure":
				// Fail only B's job after A's event/payload/job have been inserted.
				if _, err := f.Admin.Exec(`ALTER TABLE river_job ADD CONSTRAINT fixture_fanout_failure CHECK(overlaid_stream_tenant_id IS DISTINCT FROM '` + b.TenantID() + `'::uuid)`); err != nil {
					t.Fatal(err)
				}
				want = nil
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				want = context.Canceled
			}
			result, err := IngestStreamFanout(ctx, targets)
			if err == nil || result != nil || want != nil && !errors.Is(err, want) {
				t.Fatal("partial or wrong failure", result, err)
			}
			streamCounts(t, a, 0)
			streamCounts(t, b, beforeB)
		})
	}
}

func TestStreamFanoutBoundsAndSameDelivery(t *testing.T) {
	f, a, b := fixture(t)
	original := streamEvent(t, a.TenantID(), "bounds", platform.StreamOfflineKind)
	separate, err := tenantdb.Open(f.RuntimeURL, b.TenantID())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = separate.Close() }()
	for _, mode := range []string{"empty", "too_many", "nil_store", "duplicate_tenant", "different_pool", "different_native", "different_delivery", "different_time", "different_private"} {
		t.Run(mode, func(t *testing.T) {
			targets := fanoutTargets(t, []*tenantdb.DB{a, b}, original)
			switch mode {
			case "empty":
				targets = nil
			case "too_many":
				targets = make([]StreamTarget, 17)
			case "nil_store":
				targets[1].Store = nil
			case "duplicate_tenant":
				targets[1] = targets[0]
			case "different_pool":
				targets = fanoutTargets(t, []*tenantdb.DB{a, separate}, original)
			case "different_native":
				targets[1].Native = json.RawMessage(`{"private":"other"}`)
			case "different_delivery":
				targets[1].Event = streamEvent(t, b.TenantID(), "other", platform.StreamOfflineKind)
			case "different_time":
				targets[1].Event.ReceivedAt = targets[1].Event.ReceivedAt.Add(time.Second)
			case "different_private":
				targets[1].Event.TraceID = "forged"
			}
			result, err := IngestStreamFanout(t.Context(), targets)
			if !errors.Is(err, ErrInvalid) || result != nil {
				t.Fatal(mode, result, err)
			}
		})
	}
	streamCounts(t, a, 0)
	streamCounts(t, b, 0)
	scopes := []*tenantdb.DB{a, b}
	for i := 2; i < 16; i++ {
		id := fmt.Sprintf("00000000-0000-4000-8000-%012d", 100+i)
		if _, err = f.Admin.Exec(`INSERT INTO tenants(id,slug,name,owner_account_id,plan_id) VALUES($1,$2,'Fanout fixture',$3,'unlimited')`, id, fmt.Sprint("fanout-", i), owner); err != nil {
			t.Fatal(err)
		}
		if _, err = f.Admin.Exec(`INSERT INTO platform_connections(tenant_id,provider,provider_user_id,login) VALUES($1,'twitch','channel','channel')`, id); err != nil {
			t.Fatal(err)
		}
		scope, err := a.Scoped(id)
		if err != nil {
			t.Fatal(err)
		}
		scopes = append(scopes, scope)
	}
	result, err := IngestStreamFanout(t.Context(), fanoutTargets(t, scopes, original))
	if err != nil || len(result) != 16 {
		t.Fatal("maximum valid fanout", result, err)
	}
	for i, scope := range scopes {
		if !result[i] {
			t.Fatal("missing target", i)
		}
		streamCounts(t, scope, 1)
	}
}

func TestStreamFanoutRetentionClockFollowsLastConnectionLock(t *testing.T) {
	f, a, b := fixture(t)
	if _, err := f.Admin.Exec(`UPDATE tenants SET retention_days=1 WHERE id IN($1,$2)`, a.TenantID(), b.TenantID()); err != nil {
		t.Fatal(err)
	}
	lock, err := f.Admin.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Rollback() }()
	// B sorts last. A's clock must not be evaluated before waiting for B's lock.
	if _, err = lock.Exec(`SELECT 1 FROM platform_connections WHERE tenant_id=$1 FOR UPDATE`, b.TenantID()); err != nil {
		t.Fatal(err)
	}
	var now time.Time
	if err = lock.QueryRow(`SELECT clock_timestamp()`).Scan(&now); err != nil {
		t.Fatal(err)
	}
	original := streamEvent(t, a.TenantID(), "late-connection-retention", platform.StreamOfflineKind)
	original.OccurredAt = now.UTC().Add(-24*time.Hour + 3*time.Second)
	original.ReceivedAt = original.OccurredAt
	targets := fanoutTargets(t, []*tenantdb.DB{b, a}, original)
	done := make(chan error, 1)
	go func() {
		result, err := IngestStreamFanout(t.Context(), targets)
		if result != nil {
			t.Error("partial expired result", result)
		}
		done <- err
	}()
	u, _ := url.Parse(f.RuntimeURL)
	deadline := time.Now().Add(5 * time.Second)
	for {
		select {
		case err := <-done:
			t.Fatal("did not wait for last connection", err)
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
			t.Fatal("no last connection lock wait")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err = f.Admin.Exec(`SELECT pg_sleep(greatest(0,extract(epoch FROM $1::timestamptz+interval '3.1 seconds'-clock_timestamp())))`, now); err != nil {
		t.Fatal(err)
	}
	if err = lock.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, ErrInvalid) {
			t.Fatal("stale eligibility clock", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("admission did not finish")
	}
	streamCounts(t, a, 0)
	streamCounts(t, b, 0)
}
