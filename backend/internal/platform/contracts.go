// Package platform defines the internal provider boundary. These types are not
// overlay DTOs; public projections must explicitly select their fields.
package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const EventVersion = 1

type Identity struct {
	ID          string   `json:"id"`
	Login       string   `json:"login,omitempty"`
	DisplayName string   `json:"display_name,omitempty"`
	Roles       []string `json:"roles,omitempty"`
}

type Badge struct {
	ID      string `json:"id"`
	Version string `json:"version,omitempty"`
}

type ChatMessage struct {
	MessageID string   `json:"message_id"`
	ReplyToID string   `json:"reply_to_id,omitempty"`
	Text      string   `json:"text"`
	Color     string   `json:"color,omitempty"`
	Badges    []Badge  `json:"badges,omitempty"`
	Actor     Identity `json:"actor"`
	Channel   Identity `json:"channel"`
}

// ChatEvent is the first typed envelope. Further kinds require their own typed
// data contracts rather than putting arbitrary provider maps in engine inputs.
type ChatEvent struct {
	Version        int         `json:"version"`
	ID             string      `json:"id"`
	TenantID       string      `json:"tenant_id"`
	ConnectionID   string      `json:"connection_id"`
	Platform       string      `json:"platform"`
	Kind           string      `json:"kind"`
	NativeEventID  string      `json:"native_event_id"`
	IdempotencyKey string      `json:"idempotency_key"`
	OccurredAt     time.Time   `json:"occurred_at"`
	ReceivedAt     time.Time   `json:"received_at"`
	TraceID        string      `json:"trace_id"`
	Chat           ChatMessage `json:"chat"`
}

// NewChatEvent requires a native delivery identity. Adapters without one must
// supply a documented stable fingerprint; a random ID or receipt time is unsafe.
// Stable identity is not durable deduplication: the ingest transaction must
// enforce uniqueness and enqueue work atomically before enabling a consumer.
func NewChatEvent(connection Connection, nativeID string, occurred, received time.Time, chat ChatMessage) (ChatEvent, error) {
	for _, value := range []string{connection.TenantID, connection.ID, connection.Platform, nativeID, chat.MessageID, chat.Actor.ID, chat.Channel.ID, chat.Text} {
		if strings.TrimSpace(value) == "" {
			return ChatEvent{}, errors.New("chat event requires scope, native identities and text")
		}
	}
	if occurred.IsZero() || received.IsZero() {
		return ChatEvent{}, errors.New("chat event requires occurrence and receipt timestamps")
	}
	// JSON array encoding avoids ambiguous separators in provider IDs.
	key, _ := json.Marshal([]string{connection.TenantID, connection.ID, connection.Platform, "chat_message", nativeID})
	sum := sha256.Sum256(key)
	id := hex.EncodeToString(sum[:])
	chat.Actor.Roles = append([]string(nil), chat.Actor.Roles...)
	chat.Channel.Roles = append([]string(nil), chat.Channel.Roles...)
	chat.Badges = append([]Badge(nil), chat.Badges...)
	return ChatEvent{Version: EventVersion, ID: id, TenantID: connection.TenantID, ConnectionID: connection.ID,
		Platform: connection.Platform, Kind: "chat_message", NativeEventID: nativeID, IdempotencyKey: id,
		OccurredAt: occurred.UTC(), ReceivedAt: received.UTC(), TraceID: id, Chat: chat}, nil
}

// Connection contains a vault reference, never a resolved provider token.
type Connection struct {
	TenantID, ID, Platform, CredentialRef string
}

type DeliveryStatus string

const (
	Delivered DeliveryStatus = "delivered"
	Rejected  DeliveryStatus = "rejected"
	Uncertain DeliveryStatus = "uncertain"
)

type FailureKind string

const (
	Retryable    FailureKind = "retryable"
	Unauthorized FailureKind = "unauthorized"
	Unsupported  FailureKind = "unsupported"
	QuotaLimited FailureKind = "quota_limited"
	Permanent    FailureKind = "permanent"
)

type Receipt struct {
	ActionID, ProviderMessageID string
	Status                      DeliveryStatus
	Failure                     FailureKind
	RetryAfter                  time.Duration
}

type OutboundMessage struct {
	ActionID, ChannelID, Text, ReplyToID string
}

type ModerationAction struct {
	ActionID, ChannelID, UserID, MessageID, Reason string
	Capability                                     Capability
	Duration                                       time.Duration
}

// EventSink acknowledges only after durable ingest. Webhook adapters call it
// from their verified receiver; they need not manufacture a polling loop.
type EventSink interface {
	AcceptChat(context.Context, ChatEvent) error
}

type Adapter interface {
	Ingest(context.Context, Connection, EventSink) error
	Send(context.Context, Connection, OutboundMessage) (Receipt, error)
	Moderate(context.Context, Connection, ModerationAction) (Receipt, error)
	Capabilities(context.Context, Connection) CapabilityReport
}
