package conduitingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"git.subcult.tv/PatrickFanella/over-laid/internal/testpostgres"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitchconduit"
	"github.com/gorilla/websocket"
)

const tenantA = "00000000-0000-4000-8000-000000000098"
const tenantB = "00000000-0000-4000-8000-000000000099"

func fixture(t *testing.T) (testpostgres.TenantFixture, *tenantdb.DB, *tenantdb.DB) {
	t.Helper()
	f := testpostgres.NewTenantFixture(t, "")
	for i, id := range []string{tenantA, tenantB} {
		if _, e := f.Admin.Exec(`INSERT INTO tenants(id,slug,name,owner_account_id,plan_id) VALUES($1,$2,'Conduit fixture','00000000-0000-4000-8000-000000000002','unlimited');`, id, fmt.Sprint("conduit-", i)); e != nil {
			t.Fatal(e)
		}
		if _, e := f.Admin.Exec(`INSERT INTO platform_connections(tenant_id,provider,provider_user_id,login) VALUES($1,'twitch','channel','channel')`, id); e != nil {
			t.Fatal(e)
		}
	}
	a, e := tenantdb.Open(f.RuntimeURL, tenantA)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := a.Close(); e != nil {
			t.Error(e)
		}
	})
	b, e := a.Scoped(tenantB)
	if e != nil {
		t.Fatal(e)
	}
	return f, a, b
}
func binding(id, kind string) Binding {
	version := "1"
	if kind == "channel.update" {
		version = "2"
	}
	return Binding{ConduitID: "owned", SubscriptionID: id, Type: kind, Version: version, TenantID: tenantA, BroadcasterID: "channel"}
}
func packet(id string, b Binding) map[string]any {
	event := map[string]any{"broadcaster_user_id": b.BroadcasterID, "broadcaster_user_login": "channel", "broadcaster_user_name": "Channel"}
	switch b.Type {
	case "stream.online":
		event["id"] = "stream"
		event["type"] = "live"
		event["started_at"] = time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	case "channel.update":
		event["title"] = "PRIVATE TITLE"
		event["language"] = "en"
		event["category_id"] = ""
		event["category_name"] = ""
		event["content_classification_labels"] = []string{}
	}
	return map[string]any{"metadata": map[string]any{"message_id": id, "message_type": "notification", "message_timestamp": time.Now().UTC().Format(time.RFC3339Nano), "subscription_type": b.Type, "subscription_version": b.Version}, "payload": map[string]any{"subscription": map[string]any{"id": b.SubscriptionID, "status": "enabled", "type": b.Type, "version": b.Version, "condition": map[string]any{"broadcaster_user_id": b.BroadcasterID}, "transport": map[string]any{"method": "conduit", "conduit_id": b.ConduitID}}, "event": event}}
}
func object(p map[string]any, key string) map[string]any { return p[key].(map[string]any) }

// Each input crosses a real owned socket. The wrapper observes sink outcomes
// without stopping so one fixture can inspect several refused inputs.
func socket(t *testing.T, accept func(context.Context, twitchconduit.SessionNotification) error) (*twitchconduit.Session, func(map[string]any) error) {
	t.Helper()
	connected := make(chan *websocket.Conn, 1)
	observed := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, e := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if e != nil {
			return
		}
		defer func() { _ = c.Close() }()
		welcome := `{"metadata":{"message_id":"welcome","message_type":"session_welcome","message_timestamp":"2026-10-21T00:00:00Z"},"payload":{"session":{"id":"PRIVATE-SESSION","status":"connected","connected_at":"2026-10-21T00:00:00Z","keepalive_timeout_seconds":30,"reconnect_url":null}}}`
		if e := c.WriteMessage(websocket.TextMessage, []byte(welcome)); e != nil {
			return
		}
		connected <- c
		_, _, _ = c.ReadMessage()
	}))
	t.Cleanup(server.Close)
	s, e := twitchconduit.OpenSession(t.Context(), twitchconduit.SessionConfig{ConduitID: "owned", ShardCount: 1, ShardID: "0", Notify: func(ctx context.Context, n twitchconduit.SessionNotification) error {
		observed <- accept(ctx, n)
		return nil
	}, Dial: func(ctx context.Context, _ string) (*websocket.Conn, error) {
		c, r, e := websocket.DefaultDialer.DialContext(ctx, strings.Replace(server.URL, "http", "ws", 1), nil)
		if e != nil && r != nil && r.Body != nil {
			_ = r.Body.Close()
		}
		return c, e
	}})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if e := s.Close(ctx); e != nil {
			t.Error(e)
		}
	})
	c := <-connected
	return s, func(p map[string]any) error {
		t.Helper()
		if e := c.WriteJSON(p); e != nil {
			t.Fatal(e)
		}
		select {
		case e := <-observed:
			return e
		case <-time.After(3 * time.Second):
			t.Fatal("socket sink did not complete")
			return nil
		}
	}
}
func count(t *testing.T, db *tenantdb.DB, want int) {
	t.Helper()
	for _, table := range []string{"normalized_stream_events", "platform_stream_payloads"} {
		var n int
		if e := db.QueryRowContext(t.Context(), `SELECT count(*) FROM `+table).Scan(&n); e != nil || n != want {
			t.Fatal(table, n, want, e)
		}
	}
	var n int
	if e := db.QueryRowContext(t.Context(), `SELECT count(*) FROM river_job WHERE kind='overlaid_stream_dispatch'`).Scan(&n); e != nil || n != want {
		t.Fatal("stream jobs", n, want, e)
	}
}
func TestOwnedConduitStreamAtomicRoutingAndRestart(t *testing.T) {
	f, a, b := fixture(t)
	routes := []Route{{binding("online", "stream.online"), a}, {binding("offline", "stream.offline"), a}, {binding("update", "channel.update"), a}}
	var held atomic.Bool
	var releases atomic.Int32
	var expected atomic.Int32
	acquire := func(ctx context.Context, got Binding) (func(), error) {
		if ctx.Err() != nil || got.TenantID != tenantA || got.ConduitID != "owned" || !held.CompareAndSwap(false, true) {
			t.Error("invalid owner guard")
		}
		return func() {
			ctx, stop := context.WithTimeout(context.Background(), time.Second)
			defer stop()
			var committed int
			if e := a.QueryRowContext(ctx, `SELECT count(*) FROM normalized_stream_events`).Scan(&committed); e != nil || int32(committed) != expected.Load() {
				t.Error("lease released before complete admission", committed, e)
			}
			held.Store(false)
			releases.Add(1)
		}, nil
	}
	sink, e := NewStreamSink(Config{Routes: routes, Acquire: acquire})
	if e != nil {
		t.Fatal(e)
	}
	// Construction must copy all metadata rather than borrow the mutable slice.
	routes[0].Binding.TenantID = tenantB
	routes[0].DB = b
	var received time.Time
	_, send := socket(t, func(ctx context.Context, n twitchconduit.SessionNotification) error {
		received = n.ReceivedAt()
		return sink.Accept(ctx, n)
	})
	packets := make([]map[string]any, 3)
	for i, kind := range []string{"stream.online", "stream.offline", "channel.update"} {
		id := []string{"online", "offline", "update"}[i]
		packets[i] = packet(fmt.Sprint("native-", i), binding(id, kind))
		expected.Store(int32(i + 1))
		if e := send(packets[i]); e != nil {
			t.Fatal(e)
		}
		if received.IsZero() {
			t.Fatal("original receipt missing")
		}
		if e := send(packets[i]); e != nil {
			t.Fatal("duplicate", e)
		}
	}
	count(t, a, 3)
	count(t, b, 0)
	if held.Load() || releases.Load() != 6 {
		t.Fatal("lease cleanup", releases.Load())
	}
	var raw string
	var at time.Time
	if e := a.QueryRowContext(t.Context(), `SELECT p.payload::text,e.received_at FROM normalized_stream_events e JOIN platform_stream_payloads p ON p.tenant_id=e.tenant_id AND p.event_id=e.id WHERE e.kind='channel_update'`).Scan(&raw, &at); e != nil || !strings.Contains(raw, "PRIVATE TITLE") || at.After(received) {
		t.Fatal("private payload/original receipt", e)
	}
	var jobs, chat int
	if e := f.Admin.QueryRow(`SELECT (SELECT count(*) FROM river_job),(SELECT count(*) FROM normalized_chat_events)`).Scan(&jobs, &chat); e != nil || jobs != 3 || chat != 0 {
		t.Fatal("unexpected dispatcher work", jobs, chat, e)
	}
	reopened, e := tenantdb.Open(f.RuntimeURL, tenantA)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = reopened.Close() }()
	restarted, e := NewStreamSink(Config{Routes: []Route{{binding("update", "channel.update"), reopened}}, Acquire: acquire})
	if e != nil {
		t.Fatal(e)
	}
	_, again := socket(t, restarted.Accept)
	if e := again(packets[2]); e != nil {
		t.Fatal("restart duplicate", e)
	}
	object(object(packets[2], "payload"), "event")["title"] = "changed"
	if e := again(packets[2]); !errors.Is(e, ErrConflict) {
		t.Fatal("changed original", e)
	}
	count(t, a, 3)
}

func TestOwnedConduitStreamRefusalAndCleanup(t *testing.T) {
	f, a, b := fixture(t)
	bound := binding("update", "channel.update")
	var acquired, released atomic.Int32
	var mode atomic.Value
	mode.Store("ok")
	acquire := func(ctx context.Context, got Binding) (func(), error) {
		acquired.Add(1)
		release := func() {
			released.Add(1)
			if mode.Load().(string) == "release_panic" {
				panic("PRIVATE CLEANUP")
			}
		}
		switch mode.Load().(string) {
		case "refuse":
			return release, errors.New("PRIVATE REFUSAL")
		case "missing":
			return nil, nil
		case "panic":
			panic("PRIVATE GUARD")
		case "cancel":
			return release, context.Canceled
		}
		return release, nil
	}
	sink, e := NewStreamSink(Config{Routes: []Route{{bound, a}}, Acquire: acquire})
	if e != nil {
		t.Fatal(e)
	}
	_, send := socket(t, sink.Accept)
	for _, kind := range []string{"unknown_subscription", "other_channel", "condition_case", "extra_condition", "wrong_version", "too_large", "typed_field"} {
		p := packet("refused", bound)
		payload := object(p, "payload")
		sub := object(payload, "subscription")
		switch kind {
		case "unknown_subscription":
			sub["id"] = "foreign"
		case "other_channel":
			object(payload, "event")["broadcaster_user_id"] = "other"
		case "condition_case":
			sub["condition"] = map[string]string{"Broadcaster_User_ID": "channel"}
		case "extra_condition":
			object(sub, "condition")["user_id"] = "other"
		case "wrong_version":
			sub["version"] = "1"
			object(p, "metadata")["subscription_version"] = "1"
		case "too_large":
			object(payload, "event")["unknown"] = strings.Repeat("X", 65536)
		case "typed_field":
			object(payload, "event")["title"] = nil
		}
		if e := send(p); !errors.Is(e, ErrInvalid) {
			t.Fatal(kind, e)
		}
	}
	if acquired.Load() != 0 {
		t.Fatal("guard reached invalid input")
	}
	for _, kind := range []string{"refuse", "missing", "panic", "cancel"} {
		mode.Store(kind)
		e := send(packet(kind, bound))
		if e == nil || strings.Contains(fmt.Sprint(e), "PRIVATE") {
			t.Fatal("guard failure escaped", kind, e)
		}
	}
	if released.Load() != 2 {
		t.Fatal("refusal cleanup", released.Load())
	}
	count(t, a, 0)
	count(t, b, 0)
	mode.Store("release_panic")
	original := packet("committed", bound)
	if e := send(original); !errors.Is(e, ErrUnavailable) {
		t.Fatal("cleanup failure after commit", e)
	}
	count(t, a, 1)
	mode.Store("ok")
	if e := send(original); e != nil {
		t.Fatal("committed duplicate", e)
	}
	count(t, a, 1)
	if _, e := f.Admin.Exec(`UPDATE plans SET limits=jsonb_set(limits,'{storedEvents}','0') WHERE id='unlimited'`); e != nil {
		t.Fatal(e)
	}
	if e := send(packet("limited", bound)); !errors.Is(e, ErrLimited) {
		t.Fatal("plan admission", e)
	}
	count(t, a, 1)
	if _, e := f.Admin.Exec(`UPDATE platform_connections SET revoked_at=clock_timestamp() WHERE tenant_id=$1`, tenantA); e != nil {
		t.Fatal(e)
	}
	if e := send(packet("revoked", bound)); !errors.Is(e, ErrInvalid) {
		t.Fatal("revoked connection admitted", e)
	}
	count(t, a, 1)
	if _, e := f.Admin.Exec(`DELETE FROM platform_connections WHERE tenant_id=$1`, tenantA); e != nil {
		t.Fatal(e)
	}
	count(t, a, 0)
	// A public metadata-only value cannot manufacture private socket input.
	if e := sink.Accept(t.Context(), twitchconduit.SessionNotification{ID: "forged", SubscriptionID: "update", Type: "channel.update", Version: "2", Timestamp: time.Now()}); !errors.Is(e, ErrInvalid) {
		t.Fatal("forged notification", e)
	}
	var absent *StreamSink
	if e := absent.Accept(t.Context(), twitchconduit.SessionNotification{}); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
}

func TestConduitStreamConstructorBounds(t *testing.T) {
	_, a, b := fixture(t)
	good := Route{binding("update", "channel.update"), a}
	guard := func(context.Context, Binding) (func(), error) { return func() {}, nil }
	for _, kind := range []string{"empty", "no_guard", "duplicate", "unscoped", "foreign_scope", "unsupported", "invalid_atom", "too_many"} {
		c := Config{Routes: []Route{good}, Acquire: guard}
		switch kind {
		case "empty":
			c.Routes = nil
		case "no_guard":
			c.Acquire = nil
		case "duplicate":
			c.Routes = append(c.Routes, good)
		case "unscoped":
			c.Routes[0].DB = nil
		case "foreign_scope":
			c.Routes[0].DB = b
		case "unsupported":
			c.Routes[0].Binding.Type = "channel.follow"
		case "invalid_atom":
			c.Routes[0].Binding.ConduitID = "private URL/"
		case "too_many":
			c.Routes = make([]Route, 4097)
		}
		if _, e := NewStreamSink(c); !errors.Is(e, ErrInvalid) {
			t.Fatal(kind, e)
		}
	}
	if _, e := json.Marshal(&StreamSink{}); e != nil {
		t.Fatal(e)
	}
}

func TestOwnedConduitStreamCancelBeforeCommit(t *testing.T) {
	_, a, _ := fixture(t)
	var cancelAttempt context.CancelFunc
	var releases atomic.Int32
	sink, e := NewStreamSink(Config{Routes: []Route{{binding("update", "channel.update"), a}}, Acquire: func(context.Context, Binding) (func(), error) {
		cancelAttempt()
		return func() { releases.Add(1) }, nil
	}})
	if e != nil {
		t.Fatal(e)
	}
	_, send := socket(t, func(ctx context.Context, n twitchconduit.SessionNotification) error {
		bound, cancel := context.WithCancel(ctx)
		defer cancel()
		cancelAttempt = cancel
		return sink.Accept(bound, n)
	})
	if e := send(packet("cancelled", binding("update", "channel.update"))); !errors.Is(e, context.Canceled) {
		t.Fatal("cancellation identity", e)
	}
	if releases.Load() != 1 {
		t.Fatal("canceled lease lost cleanup")
	}
	count(t, a, 0)
}

func TestOwnedConduitSameNativeDeliveryAcrossTenantRoutes(t *testing.T) {
	_, a, b := fixture(t)
	first := binding("first", "stream.offline")
	second := first
	second.SubscriptionID = "second"
	second.TenantID = tenantB
	sink, e := NewStreamSink(Config{Routes: []Route{{first, a}, {second, b}}, Acquire: func(_ context.Context, got Binding) (func(), error) {
		if got != first && got != second {
			t.Error("unknown trusted route")
		}
		return func() {}, nil
	}})
	if e != nil {
		t.Fatal(e)
	}
	_, send := socket(t, sink.Accept)
	for _, route := range []Binding{first, second} {
		p := packet("same-native", route)
		// Supplied tenant/private extensions must never select the product scope.
		object(object(p, "payload"), "event")["tenant_id"] = "forged-tenant"
		if e := send(p); e != nil {
			t.Fatal(e)
		}
	}
	count(t, a, 1)
	count(t, b, 1)
}

func TestOwnedConduitOneSubscriptionFansOutAtomically(t *testing.T) {
	_, a, b := fixture(t)
	first := binding("shared", "channel.update")
	second := first
	second.TenantID = tenantB
	var held []string
	var released []string
	expectedBefore := 0
	expected := 0
	cleanupPanic := false
	acquire := func(ctx context.Context, got Binding) (func(), error) {
		if got != first && got != second {
			t.Error("foreign route")
		}
		// Every owner must be acquired before either tenant receives new work.
		if len(held) == 0 {
			count(t, a, expectedBefore)
			count(t, b, expectedBefore)
		}
		held = append(held, got.TenantID)
		return func() {
			count(t, a, expected)
			count(t, b, expected)
			released = append(released, got.TenantID)
			if cleanupPanic && got.TenantID == tenantB {
				panic("PRIVATE CLEANUP")
			}
		}, nil
	}
	sink, err := NewStreamSink(Config{Routes: []Route{{second, b}, {first, a}}, Acquire: acquire})
	if err != nil {
		t.Fatal(err)
	}
	_, send := socket(t, sink.Accept)
	original := packet("one-native-for-two", first)
	object(object(original, "payload"), "event")["tenant_id"] = "forged-scope"
	// The cleanup sees the complete commit while both leases are still owned.
	expected = 1
	if err := send(original); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(held) != fmt.Sprint([]string{tenantA, tenantB}) || fmt.Sprint(released) != fmt.Sprint([]string{tenantB, tenantA}) {
		t.Fatal("owner order", held, released)
	}
	held = nil
	released = nil
	expectedBefore = 1
	if err := send(original); err != nil {
		t.Fatal("duplicate", err)
	}
	count(t, a, 1)
	count(t, b, 1)
	cleanupPanic = true
	if err := send(original); !errors.Is(err, ErrUnavailable) {
		t.Fatal("cleanup panic escaped", err)
	}
	if len(released) != 4 {
		t.Fatal("panic skipped earlier cleanup", released)
	}
	count(t, a, 1)
	count(t, b, 1)
}

func TestOwnedConduitLaterOwnerFailureReleasesAllWithoutAdmission(t *testing.T) {
	_, a, b := fixture(t)
	first := binding("shared", "stream.offline")
	second := first
	second.TenantID = tenantB
	for _, mode := range []string{"refused", "missing", "panic", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			var released []string
			var cancel context.CancelFunc
			sink, err := NewStreamSink(Config{Routes: []Route{{second, b}, {first, a}}, Acquire: func(ctx context.Context, got Binding) (func(), error) {
				count(t, a, 0)
				count(t, b, 0)
				release := func() { released = append(released, got.TenantID) }
				if got.TenantID == tenantA {
					return release, nil
				}
				switch mode {
				case "refused":
					return release, errors.New("PRIVATE REFUSAL")
				case "missing":
					return nil, nil
				case "panic":
					panic("PRIVATE PANIC")
				case "cancel":
					cancel()
					return release, nil
				}
				return release, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			_, send := socket(t, func(ctx context.Context, n twitchconduit.SessionNotification) error {
				ctx, stop := context.WithCancel(ctx)
				defer stop()
				cancel = stop
				return sink.Accept(ctx, n)
			})
			err = send(packet(mode, first))
			if err == nil || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal("owner failure", err)
			}
			if mode == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal("context identity", err)
			}
			want := []string{tenantA}
			if mode == "refused" || mode == "cancel" {
				want = []string{tenantB, tenantA}
			}
			if fmt.Sprint(released) != fmt.Sprint(want) {
				t.Fatal("cleanup order", released, want)
			}
			count(t, a, 0)
			count(t, b, 0)
		})
	}
}

func TestOwnedConduitFanoutRejectsContradictoryMapping(t *testing.T) {
	f, a, b := fixture(t)
	separate, err := tenantdb.Open(f.RuntimeURL, tenantB)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = separate.Close() }()
	first := binding("shared", "stream.offline")
	for _, mode := range []string{"conduit", "kind", "version", "broadcaster", "pool", "bound"} {
		second := first
		second.TenantID = tenantB
		db := b
		switch mode {
		case "conduit":
			second.ConduitID = "other"
		case "kind":
			second.Type = "stream.online"
		case "version":
			second.Type = "channel.update"
			second.Version = "2"
		case "broadcaster":
			second.BroadcasterID = "other"
		case "pool":
			db = separate
		}
		routes := []Route{{first, a}, {second, db}}
		if mode == "bound" {
			routes = routes[:1]
			for i := 1; i <= 16; i++ {
				id := fmt.Sprintf("00000000-0000-4000-8000-%012d", 200+i)
				scope, err := a.Scoped(id)
				if err != nil {
					t.Fatal(err)
				}
				next := first
				next.TenantID = id
				routes = append(routes, Route{next, scope})
			}
			if _, err := NewStreamSink(Config{Routes: routes[:16], Acquire: func(context.Context, Binding) (func(), error) { return func() {}, nil }}); err != nil {
				t.Fatal("valid maximum mapping", err)
			}
		}
		if _, err := NewStreamSink(Config{Routes: routes, Acquire: func(context.Context, Binding) (func(), error) { return func() {}, nil }}); !errors.Is(err, ErrInvalid) {
			t.Fatal("contradictory mapping", mode, err)
		}
	}
}

// A private payload copied from an earlier callback is not input owned by the
// current callback, even when its native fields and route bindings are valid.
func TestConduitStreamRejectsSavedNotificationDuringLaterCallback(t *testing.T) {
	_, a, _ := fixture(t)
	var acquired atomic.Int32
	sink, err := NewStreamSink(Config{Routes: []Route{{binding("update", "channel.update"), a}}, Acquire: func(context.Context, Binding) (func(), error) { acquired.Add(1); return func() {}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	var saved twitchconduit.SessionNotification
	var original context.Context
	_, send := socket(t, func(ctx context.Context, n twitchconduit.SessionNotification) error {
		if n.ID == "saved" {
			saved = n
			original = ctx
			return nil
		}
		// Keep both contexts live: this is a later callback on the same still-open
		// owner, not a forged public value or merely an already-canceled caller.
		return sink.Accept(original, saved)
	})
	if err := send(packet("saved", binding("update", "channel.update"))); err != nil {
		t.Fatal(err)
	}
	err = send(packet("barrier", binding("update", "channel.update")))
	if !errors.Is(err, ErrUnavailable) || acquired.Load() != 0 {
		t.Fatalf("saved notification reached route admission: err=%v guards=%d", err, acquired.Load())
	}
	count(t, a, 0)
}

func TestConduitStreamOwnerCancellationSurvivesStrippedCaller(t *testing.T) {
	f, a, _ := fixture(t)
	lock, err := f.Admin.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Rollback() }()
	if _, err = lock.Exec(`SELECT 1 FROM tenants WHERE id=$1 FOR UPDATE`, a.TenantID()); err != nil {
		t.Fatal(err)
	}
	var releases atomic.Int32
	sink, err := NewStreamSink(Config{Routes: []Route{{binding("update", "channel.update"), a}}, Acquire: func(context.Context, Binding) (func(), error) { return func() { releases.Add(1) }, nil }})
	if err != nil {
		t.Fatal(err)
	}
	owner, send := socket(t, func(ctx context.Context, n twitchconduit.SessionNotification) error {
		return sink.Accept(context.WithoutCancel(ctx), n)
	})
	done := make(chan error, 1)
	go func() { done <- send(packet("owner-cancel", binding("update", "channel.update"))) }()
	u, _ := url.Parse(f.RuntimeURL)
	deadline := time.Now().Add(2 * time.Second)
	for {
		select {
		case err := <-done:
			t.Fatal("admission did not wait on tenant lock", err)
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
			t.Fatal("no admission lock wait")
		}
		time.Sleep(10 * time.Millisecond)
	}
	closing, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := owner.Close(closing); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("socket cancellation lost", err)
		}
	case <-time.After(time.Second):
		t.Fatal("stripped caller continued after owner cancellation")
	}
	if releases.Load() != 1 || owner.Snapshot().NotificationLeased {
		t.Fatal("local/transport cleanup incomplete")
	}
	if err := lock.Rollback(); err != nil {
		t.Fatal(err)
	}
	count(t, a, 0)
}
