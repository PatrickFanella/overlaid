package twitchconduit

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/url"
	"slices"
	"sort"
	"time"
)

const subscriptionsURL = "https://api.twitch.tv/helix/eventsub/subscriptions"
const maxSubscriptions = 20000

type SubscriptionMetadata struct {
	ID             string `json:"id"`
	Type           string `json:"type"`
	Version        string `json:"version"`
	Status         string `json:"status"`
	ReviewedStream bool   `json:"reviewedStream"`
	BroadcasterID  string `json:"broadcasterId,omitempty"`
}

// SubscriptionObservation is a non-atomic provider observation. ReviewedStream
// describes a codec-compatible condition, not tenant ownership or permission.
// ReportedTotal is provider metadata; it is not asserted to be filtered count.
type SubscriptionObservation struct {
	ClientID      string                 `json:"clientId"`
	ConduitID     string                 `json:"conduitId"`
	ShardCount    int                    `json:"shardCount"`
	StartedAt     time.Time              `json:"startedAt"`
	CompletedAt   time.Time              `json:"completedAt"`
	ReportedTotal int                    `json:"reportedTotal"`
	Subscriptions []SubscriptionMetadata `json:"subscriptions"`
}
type subscriptionRecord struct {
	metadata SubscriptionMetadata
	private  [32]byte
}

// InspectSubscriptions uses one token and two complete selected-conduit walks.
// It never adopts, creates, deletes or activates a subscription or route.
func (i *Inspector) InspectSubscriptions(caller context.Context) (result SubscriptionObservation, err error) {
	defer func() {
		if recover() != nil {
			result = SubscriptionObservation{}
			err = ErrUnavailable
			if caller != nil && caller.Err() != nil {
				err = caller.Err()
			}
		}
	}()
	if caller == nil || i == nil || i.http == nil || i.token == nil || !atom(i.conduitID, 128) {
		return result, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(caller, inspectionTimeout)
	defer cancel()
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	started := time.Now().UTC()
	token, e := i.token(ctx)
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if e != nil || !bearer(token) {
		return result, ErrUnauthorized
	}
	budget := maxBytes
	originalUntil, e := i.validateUntil(ctx, token, &budget)
	if e != nil {
		return result, e
	}
	if !originalUntil.After(time.Now().Add(30 * time.Second)) {
		return result, ErrUnauthorized
	}
	count, e := i.selected(ctx, token, &budget)
	if e != nil {
		return result, e
	}
	before, total, e := i.subscriptions(ctx, token, &budget)
	if e != nil {
		return result, e
	}
	after, finalTotal, e := i.subscriptions(ctx, token, &budget)
	if e != nil {
		return result, e
	}
	if total != finalTotal || !slices.Equal(before, after) {
		return result, ErrChanged
	}
	finalCount, e := i.selected(ctx, token, &budget)
	if e != nil {
		return result, e
	}
	if count != finalCount {
		return result, ErrChanged
	}
	finalUntil, e := i.validateUntil(ctx, token, &budget)
	if e != nil {
		return result, e
	}
	if !originalUntil.After(time.Now().Add(30*time.Second)) || !finalUntil.After(time.Now().Add(30*time.Second)) {
		return result, ErrUnauthorized
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	metadata := make([]SubscriptionMetadata, len(before))
	for n, r := range before {
		metadata[n] = r.metadata
	}
	return SubscriptionObservation{ClientID: i.clientID, ConduitID: i.conduitID, ShardCount: count, StartedAt: started, CompletedAt: time.Now().UTC(), ReportedTotal: total, Subscriptions: metadata}, nil
}
func (i *Inspector) subscriptions(ctx context.Context, token string, budget *int) ([]subscriptionRecord, int, error) {
	records := []subscriptionRecord{}
	seenIDs := map[string]bool{}
	seenCursors := map[string]bool{}
	cursor := ""
	total := -1
	for range maxPages {
		q := url.Values{"conduit_id": {i.conduitID}}
		if cursor != "" {
			q.Set("after", cursor)
		}
		var root map[string]json.RawMessage
		if e := i.get(ctx, subscriptionsURL+"?"+q.Encode(), token, budget, &root); e != nil {
			return nil, 0, e
		}
		var data []map[string]json.RawMessage
		var reported *int
		var pageTotal int
		var pagination map[string]json.RawMessage
		if json.Unmarshal(root["data"], &data) != nil || data == nil || len(data) > maxSubscriptions || json.Unmarshal(root["total"], &reported) != nil || reported == nil || *reported < 0 || *reported > 1000000000 || json.Unmarshal(root["pagination"], &pagination) != nil || pagination == nil {
			return nil, 0, ErrInvalid
		}
		pageTotal = *reported
		if total < 0 {
			total = pageTotal
		} else if total != pageTotal {
			return nil, 0, ErrChanged
		}
		for _, raw := range data {
			r, e := subscription(raw, i.conduitID)
			if e != nil {
				return nil, 0, e
			}
			if seenIDs[r.metadata.ID] {
				return nil, 0, ErrChanged
			}
			seenIDs[r.metadata.ID] = true
			records = append(records, r)
			if len(records) > maxSubscriptions || len(records) > total {
				return nil, 0, ErrChanged
			}
		}
		next := ""
		for k, v := range pagination {
			var value *string
			if k != "cursor" || json.Unmarshal(v, &value) != nil || value == nil || !boundedText(*value, 512) {
				return nil, 0, ErrInvalid
			}
			next = *value
		}
		if next == "" {
			sort.Slice(records, func(a, b int) bool { return records[a].metadata.ID < records[b].metadata.ID })
			return records, total, nil
		}
		if len(data) == 0 || seenCursors[next] {
			return nil, 0, ErrChanged
		}
		seenCursors[next] = true
		cursor = next
	}
	return nil, 0, ErrChanged
}
func subscription(raw map[string]json.RawMessage, conduit string) (subscriptionRecord, error) {
	var r subscriptionRecord
	m := SubscriptionMetadata{ID: text(raw, "id"), Type: text(raw, "type"), Version: text(raw, "version"), Status: text(raw, "status")}
	if !atom(m.ID, 128) || !protocolAtom(m.Type, 128) || !protocolAtom(m.Version, 32) || !subscriptionStatus(m.Status) {
		return r, ErrInvalid
	}
	var transport map[string]json.RawMessage
	var privateCondition map[string]json.RawMessage
	condition := map[string]string{}
	var cost *int
	created, e := time.Parse(time.RFC3339Nano, text(raw, "created_at"))
	if e != nil || created.IsZero() || json.Unmarshal(raw["cost"], &cost) != nil || cost == nil || *cost < 0 || *cost > 1000000000 || json.Unmarshal(raw["condition"], &privateCondition) != nil || privateCondition == nil || len(privateCondition) > 16 || json.Unmarshal(raw["transport"], &transport) != nil || len(transport) != 2 || text(transport, "method") != "conduit" || text(transport, "conduit_id") != conduit {
		return r, ErrInvalid
	}
	for k, rawValue := range privateCondition {
		var v *string
		if !protocolAtom(k, 64) || json.Unmarshal(rawValue, &v) != nil || v == nil || !boundedText(*v, 256) {
			return r, ErrInvalid
		}
		condition[k] = *v
	}
	reviewed := (m.Type == "stream.online" || m.Type == "stream.offline") && m.Version == "1" || m.Type == "channel.update" && m.Version == "2"
	if reviewed && len(condition) == 1 && atom(condition["broadcaster_user_id"], 128) {
		m.ReviewedStream = true
		m.BroadcasterID = condition["broadcaster_user_id"]
	}
	// Canonical private comparisons detect unknown-condition drift without ever
	// returning those values or their guessable digest in an observation.
	normalized, e := json.Marshal(struct {
		Condition map[string]string
		Cost      int
		Created   string
	}{condition, *cost, created.UTC().Format(time.RFC3339Nano)})
	if e != nil {
		return r, ErrInvalid
	}
	return subscriptionRecord{metadata: m, private: sha256.Sum256(normalized)}, nil
}
func subscriptionStatus(s string) bool {
	switch s {
	case "enabled", "webhook_callback_verification_pending", "webhook_callback_verification_failed", "notification_failures_exceeded", "authorization_revoked", "moderator_removed", "user_removed", "chat_user_banned", "version_removed", "beta_maintenance", "websocket_disconnected", "websocket_failed_ping_pong", "websocket_received_inbound_traffic", "websocket_connection_unused", "websocket_internal_error", "websocket_network_timeout", "websocket_network_error", "websocket_failed_to_reconnect", "conduit_deleted":
		return true
	}
	return false
}

// ValidFor checks only an allowlisted observation's shape and exact requested
// scope. It cannot prove a provider read, grant ownership or authorize a route.
func (r SubscriptionObservation) ValidFor(client, conduit string) bool {
	if !atom(client, 128) || !atom(conduit, 128) || r.ClientID != client || r.ConduitID != conduit || r.ShardCount < 1 || r.ShardCount > maxShards || r.StartedAt.IsZero() || r.CompletedAt.Before(r.StartedAt) || r.CompletedAt.Sub(r.StartedAt) > inspectionTimeout || r.ReportedTotal < 0 || r.ReportedTotal > 1000000000 || r.Subscriptions == nil || len(r.Subscriptions) > maxSubscriptions || len(r.Subscriptions) > r.ReportedTotal {
		return false
	}
	previous := ""
	for _, sub := range r.Subscriptions {
		if sub.ID <= previous || !atom(sub.ID, 128) || !protocolAtom(sub.Type, 128) || !protocolAtom(sub.Version, 32) || !subscriptionStatus(sub.Status) {
			return false
		}
		reviewed := (sub.Type == "stream.online" || sub.Type == "stream.offline") && sub.Version == "1" || sub.Type == "channel.update" && sub.Version == "2"
		if sub.ReviewedStream {
			if !reviewed || !atom(sub.BroadcasterID, 128) {
				return false
			}
		} else if sub.BroadcasterID != "" {
			return false
		}
		previous = sub.ID
	}
	return true
}
