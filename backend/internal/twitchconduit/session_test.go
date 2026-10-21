package twitchconduit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const welcomeFrame = `{"metadata":{"message_id":"welcome","message_type":"session_welcome","message_timestamp":"2026-10-21T00:00:00Z"},"payload":{"session":{"id":"PRIVATE-SESSION","status":"connected","connected_at":"2026-10-21T00:00:00Z","keepalive_timeout_seconds":10,"reconnect_url":null}}}`
const notifyFrame = `{"metadata":{"message_id":"event-1","message_type":"notification","message_timestamp":"2026-10-21T00:00:00Z","subscription_type":"channel.follow","subscription_version":"1"},"payload":{"subscription":{"id":"subscription","status":"enabled","type":"channel.follow","version":"1","transport":{"method":"conduit","conduit_id":"owned"}},"event":{"user_id":"PRIVATE-VIEWER"}}}`
const keepaliveFrame = `{"metadata":{"message_id":"keepalive","message_type":"session_keepalive","message_timestamp":"2026-10-21T00:00:00Z"},"payload":{}}`

func localSession(t *testing.T, welcome string, notify func(context.Context, SessionNotification) error, after func(*websocket.Conn)) (*Session, *atomic.Int32) {
	t.Helper()
	calls := new(atomic.Int32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Client-Id") != "" {
			t.Error("credential inherited")
		}
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()
		if welcome != "" {
			if err = c.WriteMessage(websocket.TextMessage, []byte(welcome)); err != nil {
				return
			}
		}
		if after != nil {
			after(c)
			return
		}
		for {
			if _, _, err = c.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)
	cfg := SessionConfig{ConduitID: "owned", ShardCount: 2, ShardID: "0", Notify: notify, Dial: func(ctx context.Context, endpoint string) (*websocket.Conn, error) {
		calls.Add(1)
		if endpoint != sessionURL {
			t.Error("endpoint drift")
		}
		d := websocket.Dialer{}
		c, response, err := d.DialContext(ctx, strings.Replace(server.URL, "http", "ws", 1), nil)
		if err != nil && response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return c, err
	}}
	s, err := OpenSession(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := s.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return s, calls
}
func waitSession(t *testing.T, s *Session) {
	t.Helper()
	select {
	case <-s.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("session did not stop")
	}
}
func idleNotify(context.Context, SessionNotification) error { return nil }
func TestOwnedSessionWelcomePrivateLeaseAndCloseRetry(t *testing.T) {
	s, calls := localSession(t, welcomeFrame, idleNotify, nil)
	p, deadline, err := s.Assignment()
	if err != nil || p.SessionID != "PRIVATE-SESSION" || p.ConduitID != "owned" || p.ShardCount != 2 || p.ShardID != "0" {
		t.Fatal("private welcome binding", err)
	}
	snapshot := s.Snapshot()
	if !snapshot.Open || snapshot.AssignmentDeadline.Sub(snapshot.WelcomeAt) != 10*time.Second || deadline.Before(snapshot.AssignmentDeadline) {
		t.Fatal("original welcome clock")
	}
	raw, _ := json.Marshal(s)
	if strings.Contains(string(raw), "PRIVATE") || strings.Contains(fmt.Sprintf("%+v %#v", s, s), "PRIVATE") {
		t.Fatal("private owner escaped")
	}
	for _, ctx := range []context.Context{context.Background(), s.Context()} {
		if release, err := s.Acquire(ctx, p); err == nil || release != nil {
			t.Fatal("unscoped/unbounded lease")
		}
	}
	ctx, cancel := context.WithDeadline(s.Context(), deadline)
	defer cancel()
	bad := p
	bad.SessionID = "other"
	if release, err := s.Acquire(ctx, bad); err == nil || release != nil {
		t.Fatal("private scope mismatch")
	}
	release, err := s.Acquire(ctx, p)
	if err != nil || release == nil {
		t.Fatal(err)
	}
	if again, err := s.Acquire(ctx, p); err == nil || again != nil {
		t.Fatal("concurrent lease")
	}
	release()
	release()
	release, err = s.Acquire(ctx, p)
	if err != nil {
		t.Fatal("original-window preclaim reuse", err)
	}
	closing, stop := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer stop()
	if err = s.Close(closing); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("lease wait not bounded", err)
	}
	if s.Snapshot().Open || !s.Snapshot().Leased || s.Context().Err() == nil {
		t.Fatal("invalidation did not cancel lease")
	}
	release()
	waitSession(t, s)
	if err = s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("redial")
	}
}
func TestOwnedSessionDeadlineCannotRenew(t *testing.T) {
	next := make(chan struct{})
	s, _ := localSession(t, welcomeFrame, idleNotify, func(c *websocket.Conn) {
		<-next
		_ = c.WriteMessage(websocket.TextMessage, []byte(keepaliveFrame))
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	})
	before := s.Snapshot()
	close(next)
	until := time.Now().Add(time.Second)
	for !s.Snapshot().LastMessageAt.After(before.LastMessageAt) && time.Now().Before(until) {
		time.Sleep(time.Millisecond)
	}
	if !s.Snapshot().AssignmentDeadline.Equal(before.AssignmentDeadline) || !s.Snapshot().LastMessageAt.After(before.LastMessageAt) {
		t.Fatal("keepalive renewed or not read")
	}
	s.mu.Lock()
	s.deadline = time.Now().Add(-time.Millisecond)
	s.mu.Unlock()
	if _, _, err := s.Assignment(); err == nil {
		t.Fatal("expired assignment exposed")
	}
}
func TestOwnedSessionOrderedPrivateNotifications(t *testing.T) {
	start := make(chan struct{})
	seen := make(chan SessionNotification, 2)
	s, _ := localSession(t, welcomeFrame, func(_ context.Context, n SessionNotification) error {
		raw := n.NativePayload()
		if !strings.Contains(string(raw), "PRIVATE-VIEWER") {
			t.Error("native payload missing")
		}
		raw[0] = 0
		if n.NativePayload()[0] != '{' {
			t.Error("native payload not copied")
		}
		projected, _ := json.Marshal(n)
		if strings.Contains(string(projected), "PRIVATE") || strings.Contains(fmt.Sprintf("%+v %#v", n, n), "PRIVATE") {
			t.Error("payload projection escaped")
		}
		seen <- n
		return nil
	}, func(c *websocket.Conn) {
		<-start
		for _, id := range []string{"event-1", "event-2"} {
			if err := c.WriteMessage(websocket.TextMessage, []byte(strings.Replace(notifyFrame, "event-1", id, 1))); err != nil {
				return
			}
		}
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	})
	close(start)
	for _, id := range []string{"event-1", "event-2"} {
		select {
		case n := <-seen:
			if n.ID != id || n.Type != "channel.follow" || n.Version != "1" || n.SubscriptionID != "subscription" {
				t.Fatal("notification correlation/order")
			}
		case <-time.After(time.Second):
			t.Fatal("no notification")
		}
	}
	if !s.Snapshot().Open {
		t.Fatal("valid notification closed session")
	}
}
func TestOwnedSessionProtocolFailures(t *testing.T) {
	cases := map[string]string{
		"duplicate_welcome": welcomeFrame, "binary": "binary", "invalid_json": `{`, "null_payload": strings.Replace(keepaliveFrame, `"payload":{}`, `"payload":null`, 1), "unknown": strings.Replace(keepaliveFrame, "session_keepalive", "other", 1), "keepalive_payload": strings.Replace(keepaliveFrame, `"payload":{}`, `"payload":{"x":1}`, 1), "metadata_case": strings.Replace(notifyFrame, `"metadata":`, `"Metadata":`, 1), "metadata_duplicate": strings.Replace(notifyFrame, `"message_id":"event-1"`, `"message_id":"event-1","Message_ID":"other"`, 1), "wrong_type": strings.Replace(notifyFrame, `"subscription_type":"channel.follow"`, `"subscription_type":"channel.raid"`, 1), "wrong_version": strings.Replace(notifyFrame, `"subscription_version":"1"`, `"subscription_version":"2"`, 1), "wrong_conduit": strings.Replace(notifyFrame, `"conduit_id":"owned"`, `"conduit_id":"other"`, 1), "websocket_transport": strings.Replace(notifyFrame, `"method":"conduit"`, `"method":"websocket"`, 1), "disabled": strings.Replace(notifyFrame, `"status":"enabled"`, `"status":"disabled"`, 1), "null_event": strings.Replace(notifyFrame, `"event":{"user_id":"PRIVATE-VIEWER"}`, `"event":null`, 1), "transport_extra": strings.Replace(notifyFrame, `"method":"conduit"`, `"method":"conduit","Session_ID":"PRIVATE"`, 1), "oversize": strings.Repeat(" ", maxBody+1), "deep": `{"metadata":` + strings.Repeat("[", 40) + `0` + strings.Repeat("]", 40) + `}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			start := make(chan struct{})
			var delivered atomic.Int32
			s, calls := localSession(t, welcomeFrame, func(context.Context, SessionNotification) error { delivered.Add(1); return nil }, func(c *websocket.Conn) {
				<-start
				kind := websocket.TextMessage
				if name == "binary" {
					kind = websocket.BinaryMessage
				}
				_ = c.WriteMessage(kind, []byte(body))
				for {
					if _, _, err := c.ReadMessage(); err != nil {
						return
					}
				}
			})
			close(start)
			waitSession(t, s)
			if s.Snapshot().Open || s.Context().Err() == nil || delivered.Load() != 0 || calls.Load() != 1 || strings.Contains(s.Err().Error(), "PRIVATE") {
				t.Fatal("protocol failure leaked/delivered/retried")
			}
		})
	}
}
func TestOwnedSessionControlAndCallbackStop(t *testing.T) {
	for _, kind := range []string{"reconnect", "revocation", "error", "panic", "backpressure"} {
		t.Run(kind, func(t *testing.T) {
			start, entered := make(chan struct{}), make(chan struct{})
			var notifications atomic.Int32
			s, calls := localSession(t, welcomeFrame, func(ctx context.Context, _ SessionNotification) error {
				notifications.Add(1)
				switch kind {
				case "error":
					return errors.New("PRIVATE-SINK")
				case "panic":
					panic("PRIVATE-SINK")
				default:
					close(entered)
					<-ctx.Done()
					return ctx.Err()
				}
			}, func(c *websocket.Conn) {
				<-start
				body := notifyFrame
				if kind == "reconnect" {
					body = strings.Replace(keepaliveFrame, "session_keepalive", "session_reconnect", 1)
					body = strings.Replace(body, `"payload":{}`, `"payload":{"session":{"reconnect_url":"wss://PRIVATE/?secret=PRIVATE"}}`, 1)
				}
				if kind == "revocation" {
					body = strings.Replace(keepaliveFrame, "session_keepalive", "revocation", 1)
				}
				_ = c.WriteMessage(websocket.TextMessage, []byte(body))
				if kind == "backpressure" {
					<-entered
					for range sessionQueue + 1 {
						if err := c.WriteMessage(websocket.TextMessage, []byte(notifyFrame)); err != nil {
							return
						}
					}
				}
				for {
					if _, _, err := c.ReadMessage(); err != nil {
						return
					}
				}
			})
			close(start)
			waitSession(t, s)
			want := ErrUnavailable
			switch kind {
			case "reconnect":
				want = ErrSessionReconnect
			case "revocation":
				want = ErrSessionRevoked
			case "backpressure":
				want = ErrSessionCapacity
			}
			if !errors.Is(s.Err(), want) || calls.Load() != 1 || s.Snapshot().Pending != 0 {
				t.Fatal("stop/drain/retry", s.Err())
			}
			if kind == "backpressure" && notifications.Load() != 1 {
				t.Fatal("callbacks after cancellation")
			}
		})
	}
}
func TestOwnedSessionCloseWaitsForUncooperativeSink(t *testing.T) {
	start, entered, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	defer close(release)
	s, _ := localSession(t, welcomeFrame, func(context.Context, SessionNotification) error { close(entered); <-release; return nil }, func(c *websocket.Conn) {
		<-start
		_ = c.WriteMessage(websocket.TextMessage, []byte(notifyFrame))
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	})
	close(start)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("sink not entered")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	if err := s.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("close did not retain callback", err)
	}
	select {
	case <-s.Done():
		t.Fatal("unreleased sink reported stopped")
	default:
	}
}
func TestOwnedSessionPingDoesNotExtendSilence(t *testing.T) {
	var pongs atomic.Int32
	s, _ := localSession(t, welcomeFrame, idleNotify, func(c *websocket.Conn) {
		c.SetPongHandler(func(string) error { pongs.Add(1); return nil })
		stop := make(chan struct{})
		defer close(stop)
		go func() {
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-stop:
					return
				case <-ticker.C:
					if err := c.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(time.Second)); err != nil {
						return
					}
				}
			}
		}()
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	})
	select {
	case <-s.Done():
	case <-time.After(12 * time.Second):
		t.Fatal("pings extended keepalive deadline")
	}
	if pongs.Load() == 0 || s.Snapshot().Open {
		t.Fatal("pong/liveness contract")
	}
}
func TestOwnedSessionOpeningRefusals(t *testing.T) {
	for _, kind := range []string{"input", "sink", "canceled", "transport", "panic", "nil_connection"} {
		t.Run(kind, func(t *testing.T) {
			var calls int
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			cfg := SessionConfig{ConduitID: "owned", ShardCount: 1, ShardID: "0", Notify: idleNotify, Dial: func(context.Context, string) (*websocket.Conn, error) {
				calls++
				switch kind {
				case "panic":
					panic("PRIVATE-DIAL")
				case "nil_connection":
					return nil, nil
				default:
					return nil, errors.New("PRIVATE-DIAL")
				}
			}}
			if kind == "input" {
				cfg.ShardID = "00"
			}
			if kind == "sink" {
				cfg.Notify = nil
			}
			if kind == "canceled" {
				cancel()
			}
			s, err := OpenSession(ctx, cfg)
			if s != nil || err == nil || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal("opening refusal")
			}
			if (kind == "input" || kind == "sink" || kind == "canceled") && calls != 0 {
				t.Fatal("invalid opening dialed")
			}
			if kind != "canceled" && errors.Is(err, context.Canceled) {
				t.Fatal("cleanup invented cancellation")
			}
			if kind == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatal("opening context identity")
			}
		})
	}
	for _, body := range []string{`{}`, strings.Replace(welcomeFrame, `"id":"PRIVATE-SESSION"`, `"id":null`, 1), strings.Replace(welcomeFrame, `"status":"connected"`, `"status":"other"`, 1), strings.Replace(welcomeFrame, `"keepalive_timeout_seconds":10`, `"keepalive_timeout_seconds":9`, 1), strings.Replace(welcomeFrame, `"reconnect_url":null`, `"reconnect_url":"PRIVATE"`, 1), strings.Replace(welcomeFrame, `"message_type":"session_welcome"`, `"message_type":"notification"`, 1)} {
		t.Run(body[:2]+fmt.Sprint(len(body)), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c, e := (&websocket.Upgrader{}).Upgrade(w, r, nil)
				if e != nil {
					return
				}
				defer func() { _ = c.Close() }()
				_ = c.WriteMessage(websocket.TextMessage, []byte(body))
				_, _, _ = c.ReadMessage()
			}))
			defer server.Close()
			cfg := SessionConfig{ConduitID: "owned", ShardCount: 1, ShardID: "0", Notify: idleNotify, Dial: func(ctx context.Context, _ string) (*websocket.Conn, error) {
				c, _, e := websocket.DefaultDialer.DialContext(ctx, strings.Replace(server.URL, "http", "ws", 1), nil)
				return c, e
			}}
			if s, err := OpenSession(t.Context(), cfg); s != nil || !errors.Is(err, ErrInvalid) {
				t.Fatal("invalid welcome accepted", err)
			}
		})
	}
}

func TestOwnedSessionParentCancellationWhileLeased(t *testing.T) {
	parent, cancelParent := context.WithCancel(t.Context())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, e := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if e != nil {
			return
		}
		defer func() { _ = c.Close() }()
		_ = c.WriteMessage(websocket.TextMessage, []byte(welcomeFrame))
		_, _, _ = c.ReadMessage()
	}))
	defer server.Close()
	s, err := OpenSession(parent, SessionConfig{ConduitID: "owned", ShardCount: 2, ShardID: "0", Notify: idleNotify, Dial: func(ctx context.Context, _ string) (*websocket.Conn, error) {
		c, _, e := websocket.DefaultDialer.DialContext(ctx, strings.Replace(server.URL, "http", "ws", 1), nil)
		return c, e
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = s.Close(ctx)
	}()
	p, deadline, err := s.Assignment()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithDeadline(s.Context(), deadline)
	defer cancel()
	release, err := s.Acquire(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	cancelParent()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("owner invalidation not propagated")
	}
	if again, err := s.Acquire(ctx, p); again != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("canceled lease", err)
	}
	release()
	waitSession(t, s)
	if !errors.Is(s.Err(), context.Canceled) {
		t.Fatal("parent cancellation identity", s.Err())
	}
}
func TestOwnedSessionWelcomeCancellationAndRedirect(t *testing.T) {
	for _, redirect := range []bool{false, true} {
		t.Run(fmt.Sprint(redirect), func(t *testing.T) {
			var targetCalls atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { targetCalls.Add(1) }))
			defer target.Close()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if redirect {
					http.Redirect(w, r, target.URL, http.StatusFound)
					return
				}
				c, e := (&websocket.Upgrader{}).Upgrade(w, r, nil)
				if e != nil {
					return
				}
				defer func() { _ = c.Close() }()
				_, _, _ = c.ReadMessage()
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()
			s, err := OpenSession(ctx, SessionConfig{ConduitID: "owned", ShardCount: 1, ShardID: "0", Notify: idleNotify, Dial: func(c context.Context, _ string) (*websocket.Conn, error) {
				conn, response, e := websocket.DefaultDialer.DialContext(c, strings.Replace(server.URL, "http", "ws", 1), nil)
				if e != nil && response != nil && response.Body != nil {
					_ = response.Body.Close()
				}
				return conn, e
			}})
			if s != nil || err == nil || targetCalls.Load() != 0 {
				t.Fatal("opening cancellation/redirect")
			}
			if !redirect && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("opening deadline identity", err)
			}
		})
	}
}

func TestOwnedSessionDefaultDialTransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Client-Id") != "" || r.Header.Get("Sec-Websocket-Protocol") != "" {
			t.Error("private dial configuration inherited")
		}
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()
		_, _, _ = c.ReadMessage()
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	c, err := dialSession(ctx, strings.Replace(server.URL, "http", "ws", 1))
	if err != nil {
		t.Fatal(err)
	}
	closeSessionSocket(c)
	var zero Session
	if zero.Context() != nil || zero.Snapshot().Open || !errors.Is(zero.Err(), ErrInvalid) {
		t.Fatal("zero owner")
	}
	if _, _, err = zero.Assignment(); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if release, err := zero.Acquire(ctx, ShardBinding{}); release != nil || !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if err = zero.Close(ctx); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}
