package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

type StreamEventKind string

const (
	StreamOnlineKind  StreamEventKind = "stream_online"
	StreamOfflineKind StreamEventKind = "stream_offline"
	ChannelUpdateKind StreamEventKind = "channel_update"
)

// StreamEvent is an internal allowlisted input, not an overlay projection or a
// durable admission receipt. Exactly one payload must match Kind. Delivery IDs
// identify redeliveries; they do not promise ordering or cross-subscription dedup.
type StreamEvent struct {
	Version        int             `json:"version"`
	ID             string          `json:"id"`
	TenantID       string          `json:"tenant_id"`
	ConnectionID   string          `json:"connection_id"`
	Platform       string          `json:"platform"`
	Kind           StreamEventKind `json:"kind"`
	NativeEventID  string          `json:"native_event_id"`
	IdempotencyKey string          `json:"idempotency_key"`
	OccurredAt     time.Time       `json:"occurred_at"`
	ReceivedAt     time.Time       `json:"received_at"`
	TraceID        string          `json:"trace_id"`
	Channel        Identity        `json:"channel"`
	Online         *StreamOnline   `json:"online,omitempty"`
	Offline        *StreamOffline  `json:"offline,omitempty"`
	Update         *ChannelUpdate  `json:"update,omitempty"`
}

type StreamOnline struct {
	StreamID  string    `json:"stream_id"`
	Type      string    `json:"type"`
	StartedAt time.Time `json:"started_at"`
}

type StreamOffline struct {
	// Optional compatibility field. Never infer it from cached live state.
	StreamID string `json:"stream_id,omitempty"`
}

type ChannelUpdate struct {
	Title                       string   `json:"title"`
	Language                    string   `json:"language"`
	CategoryID                  string   `json:"category_id"`
	CategoryName                string   `json:"category_name"`
	ContentClassificationLabels []string `json:"content_classification_labels"`
}

// StreamReadCapability maps only reviewed kinds. The mapping grants no access;
// callers must separately evaluate current connection, role and plan gates.
func StreamReadCapability(kind StreamEventKind) (Capability, bool) {
	switch kind {
	case StreamOnlineKind, StreamOfflineKind, ChannelUpdateKind:
		return "stream.read", true
	default:
		return "", false
	}
}

// NewStreamEvent snapshots bounded typed input. The caller supplies the trusted
// connection scope; an adapter must also bind the provider channel to that scope.
// Durable uniqueness/conflict checks and lifecycle deletion precede activation.
func NewStreamEvent(connection Connection, nativeID string, kind StreamEventKind, occurred, received time.Time, channel Identity, online *StreamOnline, offline *StreamOffline, update *ChannelUpdate) (StreamEvent, error) {
	invalid := errors.New("stream event requires bounded scope, identity, timestamps and matching typed payload")
	for _, value := range []string{connection.TenantID, connection.ID, connection.Platform, nativeID, channel.ID} {
		if strings.TrimSpace(value) == "" || !streamText(value, 1024) {
			return StreamEvent{}, invalid
		}
	}
	if !streamText(channel.Login, 1024) || !streamText(channel.DisplayName, 1024) || occurred.IsZero() || received.IsZero() {
		return StreamEvent{}, invalid
	}
	switch kind {
	case StreamOnlineKind:
		if online == nil || offline != nil || update != nil || strings.TrimSpace(online.StreamID) == "" || !streamText(online.StreamID, 1024) || online.StartedAt.IsZero() {
			return StreamEvent{}, invalid
		}
		switch online.Type {
		case "live", "playlist", "watch_party", "premiere", "rerun":
		default:
			return StreamEvent{}, invalid
		}
		copy := *online
		copy.StartedAt = copy.StartedAt.UTC()
		online = &copy
	case StreamOfflineKind:
		if offline == nil || online != nil || update != nil || !streamText(offline.StreamID, 1024) || (offline.StreamID != "" && strings.TrimSpace(offline.StreamID) == "") {
			return StreamEvent{}, invalid
		}
		copy := *offline
		offline = &copy
	case ChannelUpdateKind:
		if update == nil || online != nil || offline != nil || !streamText(update.Title, 4096) || !streamText(update.Language, 128) || !streamText(update.CategoryID, 1024) || !streamText(update.CategoryName, 1024) || len(update.ContentClassificationLabels) > 32 {
			return StreamEvent{}, invalid
		}
		for _, label := range update.ContentClassificationLabels {
			if strings.TrimSpace(label) == "" || !streamText(label, 128) {
				return StreamEvent{}, invalid
			}
		}
		copy := *update
		copy.ContentClassificationLabels = append([]string{}, update.ContentClassificationLabels...)
		update = &copy
	default:
		return StreamEvent{}, invalid
	}
	key, _ := json.Marshal([]string{connection.TenantID, connection.ID, connection.Platform, string(kind), nativeID})
	sum := sha256.Sum256(key)
	id := hex.EncodeToString(sum[:])
	return StreamEvent{Version: EventVersion, ID: id, TenantID: connection.TenantID, ConnectionID: connection.ID,
		Platform: connection.Platform, Kind: kind, NativeEventID: nativeID, IdempotencyKey: id, TraceID: id,
		OccurredAt: occurred.UTC(), ReceivedAt: received.UTC(),
		Channel: Identity{ID: channel.ID, Login: channel.Login, DisplayName: channel.DisplayName},
		Online:  online, Offline: offline, Update: update}, nil
}

func streamText(value string, limit int) bool {
	return len(value) <= limit && utf8.ValidString(value)
}
