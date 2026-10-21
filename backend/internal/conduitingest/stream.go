// Package conduitingest explicitly bridges owned conduit input to retained
// stream admission. It registers no subscriptions, consumers or startup path.
package conduitingest

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformingest"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitch"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitchconduit"
)

var (
	ErrInvalid     = errors.New("conduit stream binding or input invalid")
	ErrUnavailable = errors.New("conduit stream admission unavailable")
	ErrConflict    = errors.New("conduit stream input conflicts with retained source")
	ErrLimited     = errors.New("conduit stream retention allowance exhausted")
)

// Binding is trusted metadata from a separately owned subscription mapping.
// It is never inferred from incoming condition/channel/tenant fields.
type Binding struct {
	ConduitID, SubscriptionID, Type, Version, TenantID, BroadcasterID string
}
type Route struct {
	Binding Binding
	DB      *tenantdb.DB
}
type Config struct {
	Routes []Route
	// Acquire is a trusted local lifecycle/permission fence. It must honor context,
	// freshly validate this exact binding and retain controller/runtime/mapping
	// ownership through release. It may not call providers or reenter admission.
	// Return cleanup even on refusal when needed. Release is invoked once.
	Acquire func(context.Context, Binding) (func(), error)
}
type route struct {
	binding Binding
	store   *platformingest.Store
}
type StreamSink struct {
	routes  map[string]route
	acquire func(context.Context, Binding) (func(), error)
}

// NewStreamSink copies a bounded route table; changing it requires another owner.
// Product databases are borrowed immutable scopes, not opened or closed here.
func NewStreamSink(c Config) (*StreamSink, error) {
	if c.Acquire == nil || len(c.Routes) == 0 || len(c.Routes) > 4096 {
		return nil, ErrInvalid
	}
	s := &StreamSink{routes: make(map[string]route, len(c.Routes)), acquire: c.Acquire}
	for _, r := range c.Routes {
		b := r.Binding
		if !atom(b.ConduitID) || !atom(b.SubscriptionID) || !atom(b.BroadcasterID) || !tenantdb.ValidID(b.TenantID) || r.DB == nil || !r.DB.IsScoped() || b.TenantID != r.DB.TenantID() || !supported(b.Type, b.Version) {
			return nil, ErrInvalid
		}
		if _, exists := s.routes[b.SubscriptionID]; exists {
			return nil, ErrInvalid
		}
		store, e := platformingest.New(r.DB)
		if e != nil {
			return nil, ErrInvalid
		}
		s.routes[b.SubscriptionID] = route{b, store}
	}
	return s, nil
}
func (s *StreamSink) String() string               { return "conduit stream sink (private input excluded)" }
func (s *StreamSink) GoString() string             { return s.String() }
func (s *StreamSink) MarshalJSON() ([]byte, error) { return []byte(`{}`), nil }
func atom(v string) bool {
	if len(v) == 0 || len(v) > 128 {
		return false
	}
	for _, c := range []byte(v) {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}
func supported(kind, version string) bool {
	return (kind == "stream.online" || kind == "stream.offline") && version == "1" || kind == "channel.update" && version == "2"
}

// Accept is suitable for SessionConfig.Notify. One committed event/native/job
// transaction is the only output. A commit followed by canceled/failed cleanup
// can return an error; retained native identity prevents duplicate admission.
func (s *StreamSink) Accept(caller context.Context, n twitchconduit.SessionNotification) (err error) {
	if caller == nil || s == nil || s.acquire == nil {
		return ErrInvalid
	}
	ctx, cancel := context.WithTimeout(caller, 5*time.Second)
	defer cancel()
	var release func()
	defer func() {
		panicked := recover() != nil
		cleanupFailed := false
		if release != nil {
			func() {
				defer func() {
					if recover() != nil {
						cleanupFailed = true
					}
				}()
				release()
			}()
		}
		if panicked || cleanupFailed {
			err = ErrUnavailable
		}
		if ctx.Err() != nil {
			err = ctx.Err()
		}
	}()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	r, ok := s.routes[n.SubscriptionID]
	if !ok {
		return ErrInvalid
	}
	raw := n.NativePayload()
	if len(raw) == 0 || len(raw) > 65536 || n.ReceivedAt().IsZero() {
		return ErrInvalid
	}
	var message twitch.EventSubMessage
	var envelope struct {
		Metadata struct {
			Type    string `json:"subscription_type"`
			Version string `json:"subscription_version"`
		} `json:"metadata"`
		Payload struct {
			Subscription struct {
				ID        string                     `json:"id"`
				Condition map[string]json.RawMessage `json:"condition"`
				Transport map[string]json.RawMessage `json:"transport"`
			} `json:"subscription"`
		} `json:"payload"`
	}
	if json.Unmarshal(raw, &message) != nil || json.Unmarshal(raw, &envelope) != nil {
		return ErrInvalid
	}
	b := r.binding
	sub := message.Payload.Subscription
	if message.Metadata.MessageType != "notification" || message.Metadata.MessageID != n.ID || !message.Metadata.MessageTimestamp.Equal(n.Timestamp) || envelope.Metadata.Type != b.Type || envelope.Metadata.Version != b.Version || n.Type != b.Type || n.Version != b.Version || envelope.Payload.Subscription.ID != b.SubscriptionID || sub.Type != b.Type || sub.Version != b.Version || sub.Status != "enabled" || len(envelope.Payload.Subscription.Condition) != 1 || sub.Condition.BroadcasterUserID != b.BroadcasterID {
		return ErrInvalid
	}
	var conditionChannel string
	if json.Unmarshal(envelope.Payload.Subscription.Condition["broadcaster_user_id"], &conditionChannel) != nil || conditionChannel != b.BroadcasterID {
		return ErrInvalid
	}
	transport := envelope.Payload.Subscription.Transport
	var method, conduit string
	if len(transport) != 2 || json.Unmarshal(transport["method"], &method) != nil || json.Unmarshal(transport["conduit_id"], &conduit) != nil || method != "conduit" || conduit != b.ConduitID {
		return ErrInvalid
	}
	event, e := twitch.NormalizeStreamNotification(platform.Connection{TenantID: b.TenantID, ID: "twitch:" + b.BroadcasterID, Platform: "twitch"}, b.BroadcasterID, message, n.ReceivedAt())
	if e != nil {
		return ErrInvalid
	}
	release, e = s.acquire(ctx, b)
	if e != nil || release == nil {
		return fixed(ctx, e)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	_, e = r.store.IngestStream(ctx, event, raw)
	if errors.Is(e, platformingest.ErrInvalid) {
		return ErrInvalid
	}
	if errors.Is(e, platformingest.ErrConflict) {
		return ErrConflict
	}
	if errors.Is(e, tenantdb.ErrLimit) {
		return ErrLimited
	}
	if e != nil {
		return fixed(ctx, e)
	}
	return nil
}
func fixed(ctx context.Context, e error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(e, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(e, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return ErrUnavailable
}
