package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/conduitingest"
	"git.subcult.tv/PatrickFanella/over-laid/internal/newsroom"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformingest"
	"git.subcult.tv/PatrickFanella/over-laid/internal/testpostgres"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitch"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitchconduit"
	"github.com/gorilla/websocket"
)

// These are fixture-owned route leases, not a production mapping or startup
// composition. They exercise the real controller, runtimes and ready consumer.
func conduitNewsroomFixture(t *testing.T) (*Application, []accountFixture, testpostgres.TenantFixture, func() error) {
	t.Helper()
	old, alpha, beta, f, _ := ownedStreamFixture(t)
	if err := old.store.SaveConnection(t.Context(), old.keys, beta.tenant, beta.id, "twitch", "alpha", "alpha", nil, json.RawMessage(`{"fixture":true}`)); err != nil {
		t.Fatal(err)
	}
	for _, account := range []accountFixture{alpha, beta} {
		server, err := old.runtime(t.Context(), account.tenant)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = server.newsroom.Internal(t.Context(), newsroom.Command{Action: "automation-settings", Owner: true, Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	cfg := old.config
	cfg.TwitchStreamDispatchMode = "retained"
	cfg.StreamWorkerDatabaseURL = f.StreamWorkerURL
	if cfg.TwitchEventSub {
		t.Fatal("fixture must not open provider sockets")
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	app, err := NewApplication(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if app.stream == nil || !app.stream.isReady() || streamOwnerCheck(t, app) != "ok" {
		t.Fatal("configured retained consumer unavailable")
	}
	// Provider access is forbidden even if future code adds it to preparation.
	var calls atomic.Int32
	previous := twitchHTTPClient
	twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("fixture refuses provider transport")
	})}
	t.Cleanup(func() {
		twitchHTTPClient = previous
		if calls.Load() != 0 {
			t.Error("unexpected provider transport", calls.Load())
		}
	})
	closeApp := sync.OnceValue(app.Close)
	t.Cleanup(func() {
		if err := closeApp(); err != nil {
			t.Error(err)
		}
	})
	return app, []accountFixture{alpha, beta}, f, closeApp
}

func conduitNewsroomSink(t *testing.T, app *Application, accounts []accountFixture) *conduitingest.StreamSink {
	t.Helper()
	var routes []conduitingest.Route
	servers := map[string]*Server{}
	for _, account := range accounts {
		server, err := app.runtime(t.Context(), account.tenant)
		if err != nil {
			t.Fatal(err)
		}
		servers[account.tenant] = server
		db, err := app.store.Product.Scoped(account.tenant)
		if err != nil {
			t.Fatal(err)
		}
		for _, kind := range []string{"stream.online", "stream.offline", "channel.update"} {
			version := "1"
			if kind == "channel.update" {
				version = "2"
			}
			routes = append(routes, conduitingest.Route{Binding: conduitingest.Binding{ConduitID: "fixture-conduit", SubscriptionID: strings.ReplaceAll(kind, ".", "-"), Type: kind, Version: version, TenantID: account.tenant, BroadcasterID: "alpha"}, DB: db})
		}
	}
	sink, err := conduitingest.NewStreamSink(conduitingest.Config{Routes: routes, Acquire: func(ctx context.Context, b conduitingest.Binding) (func(), error) {
		server := servers[b.TenantID]
		if server == nil || app.stream.guard(ctx) != nil || !app.ownsStreamRuntime(b.TenantID, server) {
			return nil, errRetainedStream
		}
		// The fixed, copied fixture mapping has no concurrent mutation path. Normal
		// application mapping/owner invalidation composition remains unregistered.
		return func() {}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	return sink
}

func conduitNewsroomPacket(id, kind string, at time.Time) map[string]any {
	version := "1"
	if kind == "channel.update" {
		version = "2"
	}
	event := map[string]any{"broadcaster_user_id": "alpha", "broadcaster_user_login": "alpha", "broadcaster_user_name": "PRIVATE DISPLAY", "private_native_field": "PRIVATE-NATIVE-PAYLOAD", "tenant_id": "untrusted-scope"}
	switch kind {
	case "stream.online":
		event["id"] = "stream-" + id
		event["type"] = "live"
		event["started_at"] = at.Add(-time.Minute).Format(time.RFC3339Nano)
	case "channel.update":
		event["title"] = "retained fixture title"
		event["language"] = "en"
		event["category_id"] = ""
		event["category_name"] = ""
		event["content_classification_labels"] = []string{}
	}
	return map[string]any{"metadata": map[string]any{"message_id": id, "message_type": "notification", "message_timestamp": at.Format(time.RFC3339Nano), "subscription_type": kind, "subscription_version": version}, "payload": map[string]any{"subscription": map[string]any{"id": strings.ReplaceAll(kind, ".", "-"), "status": "enabled", "type": kind, "version": version, "condition": map[string]any{"broadcaster_user_id": "alpha"}, "transport": map[string]any{"method": "conduit", "conduit_id": "fixture-conduit"}}, "event": event}}
}

type conduitNewsroomResult struct {
	notification twitchconduit.SessionNotification
	err          error
}

func conduitNewsroomSocket(t *testing.T, sink *conduitingest.StreamSink) (*twitchconduit.Session, func(map[string]any) conduitNewsroomResult) {
	t.Helper()
	connected := make(chan *websocket.Conn, 1)
	results := make(chan conduitNewsroomResult, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		welcome := `{"metadata":{"message_id":"fixture-welcome","message_type":"session_welcome","message_timestamp":"2026-10-21T00:00:00Z"},"payload":{"session":{"id":"PRIVATE-SESSION","status":"connected","connected_at":"2026-10-21T00:00:00Z","keepalive_timeout_seconds":30,"reconnect_url":null}}}`
		if err := conn.WriteMessage(websocket.TextMessage, []byte(welcome)); err != nil {
			return
		}
		connected <- conn
		_, _, _ = conn.ReadMessage()
	}))
	t.Cleanup(server.Close)
	owner, err := twitchconduit.OpenSession(t.Context(), twitchconduit.SessionConfig{ConduitID: "fixture-conduit", ShardCount: 1, ShardID: "0", Notify: func(ctx context.Context, n twitchconduit.SessionNotification) error {
		err := sink.Accept(ctx, n)
		results <- conduitNewsroomResult{n, err}
		return err
	}, Dial: func(ctx context.Context, _ string) (*websocket.Conn, error) {
		conn, response, err := websocket.DefaultDialer.DialContext(ctx, strings.Replace(server.URL, "http", "ws", 1), nil)
		if err != nil && response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return conn, err
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := owner.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	conn := <-connected
	return owner, func(packet map[string]any) conduitNewsroomResult {
		t.Helper()
		if err := conn.WriteJSON(packet); err != nil {
			t.Fatal(err)
		}
		select {
		case result := <-results:
			return result
		case <-time.After(8 * time.Second):
			t.Fatal("fixture socket admission did not finish")
			return conduitNewsroomResult{}
		}
	}
}

func conduitNewsroomEvent(t *testing.T, account accountFixture, n twitchconduit.SessionNotification) platform.StreamEvent {
	t.Helper()
	var message twitch.EventSubMessage
	if err := json.Unmarshal(n.NativePayload(), &message); err != nil {
		t.Fatal(err)
	}
	event, err := twitch.NormalizeStreamNotification(platform.Connection{TenantID: account.tenant, ID: "twitch:alpha", Platform: "twitch"}, "alpha", message, n.ReceivedAt())
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func conduitNewsroomSettled(t *testing.T, f testpostgres.TenantFixture, want int) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for {
		var count int
		var terminal bool
		if err := f.Admin.QueryRow(`SELECT count(*),coalesce(bool_and(state IN('completed','cancelled')),false) FROM river_job WHERE kind='overlaid_stream_dispatch'`).Scan(&count, &terminal); err != nil {
			t.Fatal(err)
		}
		if count == want && terminal {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("native stream jobs did not settle", count, terminal)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func conduitNewsroomSnapshot(t *testing.T, f testpostgres.TenantFixture) string {
	t.Helper()
	var value string
	err := f.Admin.QueryRow(`SELECT jsonb_build_array((SELECT jsonb_agg(to_jsonb(e) ORDER BY tenant_id,id) FROM normalized_stream_events e),(SELECT jsonb_agg(to_jsonb(p) ORDER BY tenant_id,event_id) FROM platform_stream_payloads p),(SELECT jsonb_agg(to_jsonb(j) ORDER BY id) FROM river_job j WHERE kind='overlaid_stream_dispatch'),(SELECT jsonb_agg(to_jsonb(r) ORDER BY tenant_id,event_id) FROM platform_stream_dispatch_receipts r),(SELECT jsonb_agg(to_jsonb(d) ORDER BY tenant_id,id) FROM newsroom_documents d))::text`).Scan(&value)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestConduitNewsroomFanoutLifecycleDuplicatesAndRestart(t *testing.T) {
	app, accounts, f, closeApp := conduitNewsroomFixture(t)
	sink := conduitNewsroomSink(t, app, accounts)
	owner, send := conduitNewsroomSocket(t, sink)
	base := time.Now().UTC().Add(-5 * time.Second)
	packets := []map[string]any{conduitNewsroomPacket("online", "stream.online", base), conduitNewsroomPacket("offline", "stream.offline", base.Add(time.Second)), conduitNewsroomPacket("metadata", "channel.update", base.Add(2*time.Second))}
	var online, offline []platform.StreamEvent
	for i, packet := range packets {
		result := send(packet)
		if result.err != nil {
			t.Fatal(result.err)
		}
		for _, account := range accounts {
			event := conduitNewsroomEvent(t, account, result.notification)
			state := "completed"
			if i == 2 {
				state = "skipped"
			}
			waitOwnedStream(t, app, event, state)
			if i == 0 {
				online = append(online, event)
			}
			if i == 1 {
				offline = append(offline, event)
			}
		}
	}
	if online[0].ID == online[1].ID || offline[0].ID == offline[1].ID {
		t.Fatal("native input lost tenant identity")
	}
	for i, account := range accounts {
		server, err := app.runtime(t.Context(), account.tenant)
		if err != nil {
			t.Fatal(err)
		}
		doc, err := server.newsroom.Get(t.Context())
		if err != nil || len(doc.Broadcasts) != 1 || len(doc.Automation.Seen) != 2 || doc.Broadcasts[0].TwitchStreamID != online[i].Online.StreamID || !doc.Broadcasts[0].StartedAt.Equal(online[i].Online.StartedAt) || doc.Broadcasts[0].EndedAt == nil || !doc.Broadcasts[0].EndedAt.Equal(offline[i].OccurredAt) {
			t.Fatal("original independent lifecycle lost", err)
		}
		report := observationGet(t, app, account)
		if report.Lifecycle == nil || report.Lifecycle.EventID != offline[i].ID || report.Metadata == nil || report.Metadata.Data.Title != "retained fixture title" {
			t.Fatal("independent retained observation lost")
		}
		db, _ := app.store.Product.Scoped(account.tenant)
		var sources, payloads, jobs, receipts int
		if err := db.QueryRowContext(t.Context(), `SELECT (SELECT count(*) FROM normalized_stream_events),(SELECT count(*) FROM platform_stream_payloads),(SELECT count(*) FROM river_job WHERE kind='overlaid_stream_dispatch'),(SELECT count(*) FROM platform_stream_dispatch_receipts)`).Scan(&sources, &payloads, &jobs, &receipts); err != nil || sources != 3 || payloads != 3 || jobs != 3 || receipts != 3 {
			t.Fatal("scope isolation", sources, payloads, jobs, receipts, err)
		}
	}
	conduitNewsroomSettled(t, f, 6)
	before := conduitNewsroomSnapshot(t, f)
	for _, packet := range packets {
		if result := send(packet); result.err != nil {
			t.Fatal(result.err)
		}
	}
	if after := conduitNewsroomSnapshot(t, f); after != before {
		t.Fatal("duplicate changed source/job/receipt/editorial state")
	}
	stop, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := owner.Close(stop); err != nil {
		t.Fatal(err)
	}
	cfg := app.config
	if err := closeApp(); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewApplication(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := restarted.Close(); err != nil {
			t.Error(err)
		}
	})
	_, again := conduitNewsroomSocket(t, conduitNewsroomSink(t, restarted, accounts))
	for _, packet := range packets {
		if result := again(packet); result.err != nil {
			t.Fatal("restart duplicate", result.err)
		}
	}
	if after := conduitNewsroomSnapshot(t, f); after != before {
		t.Fatal("restart replay changed durable/editorial state")
	}
	// Removing one connection cascades only its retained input and receipts. The
	// other workspace's already completed lifecycle and observation remain intact.
	if _, err := f.Admin.Exec(`DELETE FROM platform_connections WHERE tenant_id=$1`, accounts[1].tenant); err != nil {
		t.Fatal(err)
	}
	db, _ := restarted.store.Product.Scoped(accounts[1].tenant)
	var remaining int
	if err := db.QueryRowContext(t.Context(), `SELECT (SELECT count(*) FROM normalized_stream_events)+(SELECT count(*) FROM platform_stream_payloads)+(SELECT count(*) FROM platform_stream_dispatch_receipts)+(SELECT count(*) FROM river_job WHERE kind='overlaid_stream_dispatch')`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatal("connection cascade", remaining, err)
	}
	report := observationGet(t, restarted, accounts[0])
	if report.Lifecycle == nil || report.Lifecycle.EventID != offline[0].ID {
		t.Fatal("other workspace lost retained observation")
	}
}

func TestConduitNewsroomFanoutIndependentOptOutAndNoReplay(t *testing.T) {
	app, accounts, f, _ := conduitNewsroomFixture(t)
	other, err := app.runtime(t.Context(), accounts[1].tenant)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = other.newsroom.Internal(t.Context(), newsroom.Command{Action: "automation-settings", Owner: true, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	_, send := conduitNewsroomSocket(t, conduitNewsroomSink(t, app, accounts))
	packet := conduitNewsroomPacket("opt-out", "stream.online", time.Now().UTC().Add(-time.Second))
	result := send(packet)
	if result.err != nil {
		t.Fatal(result.err)
	}
	for i, account := range accounts {
		event := conduitNewsroomEvent(t, account, result.notification)
		state := "completed"
		if i == 1 {
			state = "skipped"
		}
		waitOwnedStream(t, app, event, state)
		server, _ := app.runtime(t.Context(), account.tenant)
		doc, err := server.newsroom.Get(t.Context())
		want := 1 - i
		if err != nil || len(doc.Broadcasts) != want || len(doc.Automation.Seen) != want {
			t.Fatal("workspace opt-out crossed scope", i, err)
		}
	}
	conduitNewsroomSettled(t, f, 2)
	if _, err = other.newsroom.Internal(t.Context(), newsroom.Command{Action: "automation-settings", Owner: true, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	before := conduitNewsroomSnapshot(t, f)
	if result := send(packet); result.err != nil {
		t.Fatal(result.err)
	}
	if after := conduitNewsroomSnapshot(t, f); after != before {
		t.Fatal("re-enabled tenant replayed skipped source")
	}
	// A distinct fresh delivery remains eligible after re-enabling.
	fresh := conduitNewsroomPacket("fresh-after-opt-in", "stream.online", time.Now().UTC())
	payload := fresh["payload"].(map[string]any)
	event := payload["event"].(map[string]any)
	event["started_at"] = time.Now().UTC().Add(-500 * time.Millisecond).Format(time.RFC3339Nano)
	result = send(fresh)
	if result.err != nil {
		t.Fatal(result.err)
	}
	for i, account := range accounts {
		waitOwnedStream(t, app, conduitNewsroomEvent(t, account, result.notification), "completed")
		server, _ := app.runtime(t.Context(), account.tenant)
		doc, err := server.newsroom.Get(t.Context())
		if err != nil || len(doc.Broadcasts) != 2-i {
			t.Fatal("fresh source refused after opt-in", i, err)
		}
	}
}

func TestConduitNewsroomFanoutAdmissionRefusalHasNoEditorialEffect(t *testing.T) {
	for _, mode := range []string{"later-plan", "later-revoked"} {
		t.Run(mode, func(t *testing.T) {
			app, accounts, f, _ := conduitNewsroomFixture(t)
			// Ensure the refusing target sorts last, independent of fixture UUID order.
			first, second := accounts[0], accounts[1]
			if first.tenant > second.tenant {
				first, second = second, first
			}
			if mode == "later-plan" {
				db, _ := app.store.Product.Scoped(second.tenant)
				store, _ := platformingest.New(db)
				now := time.Now().UTC()
				chat, err := platform.NewChatEvent(platform.Connection{TenantID: second.tenant, ID: "twitch:alpha", Platform: "twitch"}, "plan-charge", now, now, platform.ChatMessage{MessageID: "plan-charge", Text: "ordinary fixture chat", Actor: platform.Identity{ID: "viewer", Login: "viewer"}, Channel: platform.Identity{ID: "alpha", Login: "alpha"}})
				if err != nil {
					t.Fatal(err)
				}
				if added, err := store.IngestChat(t.Context(), chat, nil); err != nil || !added {
					t.Fatal("chat allowance charge", added, err)
				}
				if _, err = f.Admin.Exec(`UPDATE plans SET limits=jsonb_set(limits,'{storedEvents}','1')`); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := f.Admin.Exec(`UPDATE platform_connections SET revoked_at=clock_timestamp() WHERE tenant_id=$1`, second.tenant); err != nil {
					t.Fatal(err)
				}
			}
			before := conduitNewsroomSnapshot(t, f)
			owner, send := conduitNewsroomSocket(t, conduitNewsroomSink(t, app, accounts))
			result := send(conduitNewsroomPacket(mode, "stream.online", time.Now().UTC()))
			want := conduitingest.ErrLimited
			if mode == "later-revoked" {
				want = conduitingest.ErrInvalid
			}
			if !errors.Is(result.err, want) {
				t.Fatal("wrong bounded refusal", result.err)
			}
			// The actual owner must invalidate on a callback error, rather than fixture
			// middleware suppressing it and continuing on the same socket.
			select {
			case <-owner.Done():
			case <-time.After(5 * time.Second):
				t.Fatal("refused session did not drain")
			}
			if after := conduitNewsroomSnapshot(t, f); after != before {
				t.Fatal("failed fanout left durable or editorial work")
			}
			for _, account := range accounts {
				server, _ := app.runtime(t.Context(), account.tenant)
				doc, err := server.newsroom.Get(t.Context())
				if err != nil || len(doc.Broadcasts) != 0 || len(doc.Automation.Seen) != 0 {
					t.Fatal("refused admission changed Newsroom", err)
				}
			}
		})
	}
}
