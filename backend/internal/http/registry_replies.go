package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformoutbound"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

var errRegistryReply = errors.New("registry reply source or snapshot unavailable")

// Explicit construction only: no startup path selects this admission boundary.
// Installation must happen before a runtime starts accepting work. Selection
// blocks every unbound central chat send, including manual callbacks.
type registryReplyAdmission struct {
	db    *tenantdb.DB
	store *platformoutbound.Store
}

func newRegistryReplyAdmission(db *tenantdb.DB) (*registryReplyAdmission, error) {
	store, err := platformoutbound.New(db)
	if err != nil {
		return nil, err
	}
	return &registryReplyAdmission{db: db, store: store}, nil
}

type registryReplyCollection struct {
	mu        sync.Mutex
	server    *Server
	source    platform.ChatEvent
	requests  []platformoutbound.RegistryReply
	snapshots map[string]string
	current   *registryReplyOutput
	closed    bool
	failure   error
}

type registryReplyContextKey struct{}
type registryReplyOutput struct {
	collection                            *registryReplyCollection
	eventID, commandID, fingerprint, kind string
}

func (s *Server) beginRegistryReplies(ctx context.Context, source platform.ChatEvent) (*registryReplyCollection, error) {
	admission := s.retainedReplies
	if admission == nil || s.tenantID != admission.db.TenantID() || source.TenantID != s.tenantID || source.Platform != "twitch" {
		return nil, errRegistryReply
	}
	canonical, err := platform.NewChatEvent(platform.Connection{TenantID: source.TenantID, ID: source.ConnectionID, Platform: source.Platform}, source.NativeEventID, source.OccurredAt, source.ReceivedAt, source.Chat)
	if err != nil || source.Version != canonical.Version || source.Kind != canonical.Kind || source.ID != canonical.ID || source.IdempotencyKey != canonical.IdempotencyKey || source.TraceID != canonical.TraceID {
		return nil, errRegistryReply
	}
	raw, err := json.Marshal(canonical)
	if err != nil {
		return nil, errRegistryReply
	}
	// Require the actual retained envelope, not merely a syntactically valid hash.
	var retained bool
	err = admission.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM normalized_chat_events WHERE /*tenant:normalized_chat_events*/ AND id=$1 AND connection_id=$2 AND envelope=$3::jsonb)`, source.ID, source.ConnectionID, raw).Scan(&retained)
	if err != nil {
		return nil, err
	}
	if !retained {
		return nil, errRegistryReply
	}
	return &registryReplyCollection{server: s, source: canonical, snapshots: map[string]string{}}, nil
}

func (c *registryReplyCollection) outputContext(ctx context.Context, event events.Event) context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	output := &registryReplyOutput{collection: c, eventID: event.ID}
	if event.Type == events.EventTypeCommand {
		if _, ok := outboundChatResponseFromEvent(event); ok {
			output.kind = "chat-response-v1"
		} else if boolData(event.Data, "newsroomRequest") && stringData(event.Data, "action") == "newsroom_current" {
			output.kind = "newsroom-current-v1"
		}
		output.commandID = stringData(event.Data, "registryCommandId")
		output.fingerprint = stringData(event.Data, "registryCommandFingerprint")
	}
	c.current = output
	return context.WithValue(ctx, registryReplyContextKey{}, output)
}

func (s *Server) collectRegistryReply(ctx context.Context, actionID, message string) error {
	output, ok := ctx.Value(registryReplyContextKey{}).(*registryReplyOutput)
	if !ok || output == nil || output.collection.server != s {
		return errRegistryReply
	}
	c := output.collection
	c.mu.Lock()
	defer c.mu.Unlock()
	fail := func(err error) error { c.failure = err; return err }
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	if c.closed || c.failure != nil || c.current != output || output.kind == "" || output.commandID == "" || len(output.fingerprint) != 64 {
		return fail(errRegistryReply)
	}
	if output.kind == "chat-response-v1" && (actionID == "" || actionID != output.eventID) || output.kind == "newsroom-current-v1" && actionID != "" {
		return fail(errRegistryReply)
	}
	message = sanitizeOutboundChatMessage(message)
	if message == "" || len(c.requests) >= 16 {
		return fail(errRegistryReply)
	}
	// Position zero is the single chat effect of these versioned output kinds.
	// Display IDs and rendered text are deliberately absent from the action key.
	key := platformoutbound.RegistryReplyKey(output.commandID, output.fingerprint, output.kind)
	if key == "" {
		return fail(errRegistryReply)
	}
	for _, prior := range c.requests {
		if prior.Key == key {
			return fail(errRegistryReply)
		}
	}
	if previous, ok := c.snapshots[output.commandID]; ok && previous != output.fingerprint {
		return fail(errRegistryReply)
	}
	c.snapshots[output.commandID] = output.fingerprint
	c.requests = append(c.requests, platformoutbound.RegistryReply{Request: platformoutbound.Request{EventID: c.source.ID, ConnectionID: c.source.ConnectionID, Key: key, Text: message}, CommandID: output.commandID, SnapshotFingerprint: output.fingerprint, EffectKind: output.kind})
	return nil
}

func (c *registryReplyCollection) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
}

func (c *registryReplyCollection) admit(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errRegistryReply
	}
	c.closed = true
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.failure != nil {
		return c.failure
	}
	if len(c.requests) == 0 {
		return nil
	}
	if err := c.server.checkChatFence(ctx); err != nil {
		return err
	}
	_, err := c.server.retainedReplies.store.AdmitRegistryBatch(ctx, c.requests)
	return err
}
