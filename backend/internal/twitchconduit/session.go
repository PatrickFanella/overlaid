package twitchconduit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const sessionURL = "wss://eventsub.wss.twitch.tv/ws?keepalive_timeout_seconds=30"
const sessionQueue = 8

var (
	ErrSessionClosed    = errors.New("conduit session closed")
	ErrSessionReconnect = errors.New("conduit session requires reconnect handoff")
	ErrSessionRevoked   = errors.New("conduit session revoked")
	ErrSessionCapacity  = errors.New("conduit session input capacity reached")
)

// SessionConfig is trusted service configuration. Dial and Notify must honor
// context and protect private data. There is no default publisher or dispatcher.
// The optional dial function receives only the fixed endpoint, never headers.
type SessionConfig struct {
	ConduitID  string
	ShardCount int
	ShardID    string
	Dial       func(context.Context, string) (*websocket.Conn, error)
	Notify     func(context.Context, SessionNotification) error
}

// SessionNotification holds private native input for a trusted ingestion sink.
// Formatting and JSON expose only correlated metadata. NativePayload is explicit
// and copies data; it grants no tenant/subscription authority or effect.
type SessionNotification struct {
	ID             string    `json:"id"`
	Type           string    `json:"type"`
	Version        string    `json:"version"`
	SubscriptionID string    `json:"subscriptionId"`
	Timestamp      time.Time `json:"timestamp"`
	raw            []byte
	receivedAt     time.Time
}

// ReceivedAt is the original local frame receipt, before parsing/queue wait.
func (n SessionNotification) ReceivedAt() time.Time { return n.receivedAt.UTC() }

func (n SessionNotification) NativePayload() []byte { return append([]byte(nil), n.raw...) }
func (n SessionNotification) String() string {
	return "conduit notification (private payload redacted)"
}
func (n SessionNotification) GoString() string { return n.String() }

type SessionSnapshot struct {
	Open               bool      `json:"open"`
	Leased             bool      `json:"leased"`
	ConduitID          string    `json:"conduitId"`
	ShardID            string    `json:"shardId"`
	ShardCount         int       `json:"shardCount"`
	WelcomeAt          time.Time `json:"welcomeAt"`
	AssignmentDeadline time.Time `json:"assignmentDeadline"`
	LastMessageAt      time.Time `json:"lastMessageAt"`
	Pending            int       `json:"pending"`
}

// Session owns exactly one initial socket, reader, ordered sink worker and lease.
// Open does not establish assignment, subscription or product readiness.
type Session struct {
	mu                                                                sync.Mutex
	conn                                                              *websocket.Conn
	parent, ctx                                                       context.Context
	cancel                                                            context.CancelFunc
	parentStop                                                        func() bool
	stopOnce                                                          sync.Once
	binding                                                           ShardBinding
	welcome, deadline, last                                           time.Time
	keepalive                                                         time.Duration
	notify                                                            func(context.Context, SessionNotification) error
	queue                                                             chan SessionNotification
	done                                                              chan struct{}
	closed, leased, readerDone, workerDone, doneClosed, closeComplete bool
	err                                                               error
}
type sessionContextKey struct{}

// OpenSession makes one credential-free connection. The original welcome clock
// is local receipt time; provider timestamps and later traffic cannot renew it.
func OpenSession(parent context.Context, config SessionConfig) (owner *Session, err error) {
	var conn *websocket.Conn
	operation := parent
	var cancelOpening context.CancelFunc
	defer func() {
		if cancelOpening != nil {
			defer cancelOpening()
		}
		if recover() != nil {
			if conn != nil {
				closeSessionSocket(conn)
			}
			owner = nil
			err = sessionFailure(operation, ErrUnavailable)
		}
	}()
	binding := ShardBinding{ConduitID: config.ConduitID, ShardCount: config.ShardCount, ShardID: config.ShardID, SessionID: "pending"}
	if parent == nil || config.Notify == nil || !validShardBinding(binding) {
		return nil, ErrInvalid
	}
	opening, cancel := context.WithTimeout(parent, 10*time.Second)
	operation = opening
	cancelOpening = cancel
	if err = sessionFailure(opening, nil); err != nil {
		return nil, err
	}
	dial := config.Dial
	if dial == nil {
		dial = dialSession
	}
	conn, err = dial(opening, sessionURL)
	if err != nil || conn == nil {
		if conn != nil {
			closeSessionSocket(conn)
		}
		if opening.Err() != nil {
			return nil, opening.Err()
		}
		return nil, sessionFailure(opening, ErrUnavailable)
	}
	keep := false
	defer func() {
		if !keep {
			closeSessionSocket(conn)
		}
	}()
	stopOpening := context.AfterFunc(opening, func() { closeSessionSocket(conn) })
	defer stopOpening()
	conn.SetReadLimit(maxBody)
	deadline, _ := opening.Deadline()
	if err = conn.SetReadDeadline(deadline); err != nil {
		return nil, sessionFailure(opening, ErrUnavailable)
	}
	_, meta, payload, received, err := readSessionFrame(conn)
	if err != nil {
		if opening.Err() != nil {
			return nil, opening.Err()
		}
		return nil, sessionFailure(opening, err)
	}
	if text(meta, "message_type") != "session_welcome" {
		return nil, sessionFailure(opening, ErrInvalid)
	}
	var rawSession map[string]json.RawMessage
	if json.Unmarshal(payload["session"], &rawSession) != nil || rawSession == nil {
		return nil, sessionFailure(opening, ErrInvalid)
	}
	binding.SessionID = text(rawSession, "id")
	var seconds int
	connected, e := time.Parse(time.RFC3339Nano, text(rawSession, "connected_at"))
	if !validShardBinding(binding) || text(rawSession, "status") != "connected" || e != nil || connected.IsZero() || json.Unmarshal(rawSession["keepalive_timeout_seconds"], &seconds) != nil || seconds < 10 || seconds > 600 || string(rawSession["reconnect_url"]) != "null" {
		return nil, sessionFailure(opening, ErrInvalid)
	}
	stopOpening()
	if err = sessionFailure(opening, nil); err != nil {
		return nil, err
	}
	lifetime, stop := context.WithCancel(parent)
	s := &Session{conn: conn, parent: parent, cancel: stop, binding: binding, welcome: received, deadline: received.Add(10 * time.Second), last: received, keepalive: time.Duration(seconds) * time.Second, notify: config.Notify, queue: make(chan SessionNotification, sessionQueue), done: make(chan struct{})}
	s.ctx = context.WithValue(lifetime, sessionContextKey{}, s)
	s.parentStop = context.AfterFunc(parent, func() { s.stop(parent.Err()) })
	keep = true
	go s.read()
	go s.deliver()
	return s, nil
}
func dialSession(ctx context.Context, endpoint string) (*websocket.Conn, error) {
	d := websocket.Dialer{HandshakeTimeout: 10 * time.Second, Proxy: http.ProxyFromEnvironment}
	c, response, err := d.DialContext(ctx, endpoint, nil)
	if err != nil && response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	return c, err
}
func closeSessionSocket(c *websocket.Conn) { defer func() { _ = recover() }(); _ = c.Close() }
func (s *Session) Context() context.Context {
	if s == nil || s.ctx == nil {
		return nil
	}
	return s.ctx
}
func (s *Session) Done() <-chan struct{} {
	if s == nil || s.ctx == nil {
		done := make(chan struct{})
		close(done)
		return done
	}
	return s.done
}
func (s *Session) String() string               { return "conduit session (private binding redacted)" }
func (s *Session) GoString() string             { return s.String() }
func (s *Session) MarshalJSON() ([]byte, error) { return json.Marshal(s.Snapshot()) }
func (s *Session) Snapshot() SessionSnapshot {
	if s == nil || s.ctx == nil {
		return SessionSnapshot{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return SessionSnapshot{Open: !s.closed && s.ctx.Err() == nil, Leased: s.leased, ConduitID: s.binding.ConduitID, ShardID: s.binding.ShardID, ShardCount: s.binding.ShardCount, WelcomeAt: s.welcome.UTC(), AssignmentDeadline: s.deadline.UTC(), LastMessageAt: s.last.UTC(), Pending: len(s.queue)}
}
func (s *Session) Err() error {
	if s == nil || s.ctx == nil {
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Assignment exposes private input only for the original still-live welcome.
func (s *Session) Assignment() (ShardBinding, time.Time, error) {
	if s == nil || s.ctx == nil {
		return ShardBinding{}, time.Time{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.ctx.Err() != nil || !time.Now().Before(s.deadline) {
		return ShardBinding{}, time.Time{}, ErrUnavailable
	}
	return s.binding, s.deadline, nil
}

// Acquire is the local guard consumed by appvault.AssignmentSession. Context must
// descend from this owner and already carry the original (or shorter) deadline.
// A lease grants no claim/effect. Only the ledger decides if an attempt may start.
func (s *Session) Acquire(ctx context.Context, p ShardBinding) (func(), error) {
	if s == nil || s.ctx == nil || ctx == nil {
		return nil, ErrInvalid
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	d, ok := ctx.Deadline()
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Value(sessionContextKey{}) != s || !ok || d.After(s.deadline) || s.closed || s.leased || s.ctx.Err() != nil || !time.Now().Before(s.deadline) || p != s.binding {
		return nil, ErrUnavailable
	}
	s.leased = true
	var once sync.Once
	return func() { once.Do(func() { s.mu.Lock(); defer s.mu.Unlock(); s.leased = false; s.finishLocked() }) }, nil
}

// Close invalidates immediately, then waits for reader/worker and lease cleanup.
// A timed-out wait retains this owner; callers may retry Close after cleanup.
func (s *Session) Close(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalid
	}
	if s == nil {
		return nil
	}
	if s.ctx == nil {
		return ErrInvalid
	}
	s.stop(ErrSessionClosed)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *Session) stop(cause error) {
	s.stopOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		cause = sessionFailure(s.parent, cause)
		s.err = cause
		s.mu.Unlock()
		s.cancel()
		closeSessionSocket(s.conn)
		s.mu.Lock()
		s.closeComplete = true
		s.finishLocked()
		s.mu.Unlock()
		// parentStop is immutable after construction and both loops start afterward.
		// Parent cancellation can win construction, so avoid reading it here; the
		// callback below is stopped by finish once both loops have left.
	})
}
func (s *Session) finishLocked() {
	if s.closed && s.closeComplete && s.readerDone && s.workerDone && !s.leased && !s.doneClosed {
		s.doneClosed = true
		close(s.done)
	}
}
func (s *Session) finish(reader bool) {
	s.mu.Lock()
	if reader {
		s.readerDone = true
	} else {
		s.workerDone = true
	}
	s.finishLocked()
	both := s.readerDone && s.workerDone
	s.mu.Unlock()
	if both {
		s.parentStop()
	}
}
func (s *Session) read() {
	defer func() { _ = recover(); s.stop(ErrUnavailable); s.finish(true) }()
	for {
		if s.ctx.Err() != nil {
			return
		}
		s.mu.Lock()
		readDeadline := s.last.Add(s.keepalive)
		s.mu.Unlock()
		if err := s.conn.SetReadDeadline(readDeadline); err != nil {
			return
		}
		raw, meta, payload, received, err := readSessionFrame(s.conn)
		if err != nil {
			s.stop(err)
			return
		}
		switch text(meta, "message_type") {
		case "session_keepalive":
			if len(payload) != 0 {
				s.stop(ErrInvalid)
				return
			}
		case "notification":
			n, ok := sessionNotification(raw, meta, payload, s.binding.ConduitID)
			if !ok {
				s.stop(ErrInvalid)
				return
			}
			n.receivedAt = received
			s.mu.Lock()
			if s.closed || s.ctx.Err() != nil {
				s.mu.Unlock()
				return
			}
			select {
			case s.queue <- n:
				s.mu.Unlock()
			default:
				s.mu.Unlock()
				s.stop(ErrSessionCapacity)
				return
			}
		case "session_reconnect":
			s.stop(ErrSessionReconnect)
			return
		case "revocation":
			s.stop(ErrSessionRevoked)
			return
		default:
			s.stop(ErrInvalid)
			return
		}
		s.mu.Lock()
		s.last = received
		s.mu.Unlock()
	}
}
func (s *Session) deliver() {
	defer func() {
		_ = recover()
		s.stop(ErrUnavailable)
		for {
			select {
			case <-s.queue:
			default:
				s.finish(false)
				return
			}
		}
	}()
	for {
		select {
		case <-s.ctx.Done():
			return
		case n := <-s.queue:
			if s.ctx.Err() != nil {
				return
			}
			if err := s.notify(s.ctx, n); err != nil {
				return
			}
		}
	}
}
func text(m map[string]json.RawMessage, key string) string {
	var v string
	if json.Unmarshal(m[key], &v) != nil {
		return ""
	}
	return v
}
func readSessionFrame(c *websocket.Conn) ([]byte, map[string]json.RawMessage, map[string]json.RawMessage, time.Time, error) {
	kind, raw, err := c.ReadMessage()
	received := time.Now()
	if err != nil {
		return nil, nil, nil, time.Time{}, ErrUnavailable
	}
	if kind != websocket.TextMessage || len(raw) > maxBody || !uniqueJSON(raw) {
		return nil, nil, nil, time.Time{}, ErrInvalid
	}
	var root map[string]json.RawMessage
	var meta, payload map[string]json.RawMessage
	if json.Unmarshal(raw, &root) != nil || json.Unmarshal(root["metadata"], &meta) != nil || meta == nil || json.Unmarshal(root["payload"], &payload) != nil || payload == nil {
		return nil, nil, nil, time.Time{}, ErrInvalid
	}
	stamp, e := time.Parse(time.RFC3339Nano, text(meta, "message_timestamp"))
	if !atom(text(meta, "message_id"), 128) || text(meta, "message_type") == "" || e != nil || stamp.IsZero() {
		return nil, nil, nil, time.Time{}, ErrInvalid
	}
	return raw, meta, payload, received, nil
}
func sessionNotification(raw []byte, meta, payload map[string]json.RawMessage, conduit string) (SessionNotification, bool) {
	var sub, transport, event map[string]json.RawMessage
	if json.Unmarshal(payload["subscription"], &sub) != nil || sub == nil || json.Unmarshal(payload["event"], &event) != nil || event == nil || json.Unmarshal(sub["transport"], &transport) != nil || transport == nil {
		return SessionNotification{}, false
	}
	typ, version, id := text(sub, "type"), text(sub, "version"), text(sub, "id")
	if !protocolAtom(typ, 128) || !protocolAtom(version, 32) || !atom(id, 128) || text(sub, "status") != "enabled" || text(meta, "subscription_type") != typ || text(meta, "subscription_version") != version || text(transport, "method") != "conduit" || text(transport, "conduit_id") != conduit {
		return SessionNotification{}, false
	}
	for key := range transport {
		switch strings.ToLower(key) {
		case "session_id", "callback", "secret":
			return SessionNotification{}, false
		}
	}
	stamp, _ := time.Parse(time.RFC3339Nano, text(meta, "message_timestamp"))
	return SessionNotification{ID: text(meta, "message_id"), Type: typ, Version: version, SubscriptionID: id, Timestamp: stamp.UTC(), raw: append([]byte(nil), raw...)}, true
}
func protocolAtom(v string, limit int) bool {
	if len(v) < 1 || len(v) > limit {
		return false
	}
	for _, c := range []byte(v) {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-' {
			continue
		}
		return false
	}
	return true
}

// Network read deadlines can expire before the context timer goroutine runs.
// Preserve the original operation deadline without inventing cancellation from
// unrelated socket timeouts or exposing private transport errors.
func sessionFailure(ctx context.Context, fixed error) error {
	if ctx != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
			return context.DeadlineExceeded
		}
	}
	return fixed
}
