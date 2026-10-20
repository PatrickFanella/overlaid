package platformingest

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
)

var ErrObservationDenied = errors.New("retained stream observations unavailable")

// StreamObservations describes retained input, never current provider truth or
// handler completion. It is private to an authenticated workspace read boundary.
type StreamObservations struct {
	State      string                `json:"state"`
	ObservedAt time.Time             `json:"observedAt"`
	ChannelID  string                `json:"channelId,omitempty"`
	Lifecycle  *LifecycleObservation `json:"lifecycle"`
	Metadata   *MetadataObservation  `json:"metadata"`
}
type LifecycleObservation struct {
	EventID    string                   `json:"eventId"`
	OccurredAt time.Time                `json:"occurredAt"`
	ReceivedAt time.Time                `json:"receivedAt"`
	Kind       platform.StreamEventKind `json:"kind"`
	StreamID   string                   `json:"streamId,omitempty"`
	StartedAt  *time.Time               `json:"startedAt,omitempty"`
}
type MetadataObservation struct {
	EventID    string                 `json:"eventId"`
	OccurredAt time.Time              `json:"occurredAt"`
	ReceivedAt time.Time              `json:"receivedAt"`
	Data       platform.ChannelUpdate `json:"data"`
}

// LatestStreamObservations reads one statement snapshot with one DB clock. It
// creates/prunes no row and charges no allowance. Dispatch expiry is unrelated
// to historical observation eligibility; retention and current binding apply.
func (s *Store) LatestStreamObservations(ctx context.Context) (StreamObservations, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result := StreamObservations{State: "empty"}
	var limitsRaw, lifecycleRaw, metadataRaw []byte
	err := s.db.QueryRowContext(ctx, `WITH observed AS MATERIALIZED(SELECT clock_timestamp() AS at)
 SELECT observed.at,coalesce(platform_connections.provider_user_id,''),plans.limits,coalesce(to_jsonb(lifecycle),'null'::jsonb),coalesce(to_jsonb(metadata),'null'::jsonb)
 FROM observed,tenants JOIN plans ON plans.id=tenants.plan_id
 LEFT JOIN platform_connections ON platform_connections.tenant_id=tenants.id AND /*tenant:platform_connections*/ AND platform_connections.provider='twitch' AND platform_connections.revoked_at IS NULL
 LEFT JOIN LATERAL(SELECT normalized_stream_events.* FROM normalized_stream_events WHERE /*tenant:normalized_stream_events*/ AND connection_id='twitch:'||platform_connections.provider_user_id AND kind IN('stream_online','stream_offline') AND occurred_at>=observed.at-make_interval(days=>tenants.retention_days) ORDER BY occurred_at DESC,id DESC LIMIT 1) lifecycle ON true
 LEFT JOIN LATERAL(SELECT normalized_stream_events.* FROM normalized_stream_events WHERE /*tenant:normalized_stream_events*/ AND connection_id='twitch:'||platform_connections.provider_user_id AND kind='channel_update' AND occurred_at>=observed.at-make_interval(days=>tenants.retention_days) ORDER BY occurred_at DESC,id DESC LIMIT 1) metadata ON true
 WHERE tenants.id=$1`, s.db.TenantID()).Scan(&result.ObservedAt, &result.ChannelID, &limitsRaw, &lifecycleRaw, &metadataRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return StreamObservations{}, ErrObservationDenied
	}
	if err != nil {
		return StreamObservations{}, err
	}
	var limits map[string]int64
	if json.Unmarshal(limitsRaw, &limits) != nil {
		return StreamObservations{}, ErrInvalid
	}
	limit, ok := limits["storedEvents"]
	if !ok || limit < -1 {
		return StreamObservations{}, ErrInvalid
	}
	if limit == 0 {
		return StreamObservations{}, ErrObservationDenied
	}
	if result.ChannelID == "" {
		result.State = "disconnected"
		return result, nil
	}
	for _, source := range []struct {
		raw      []byte
		metadata bool
	}{{lifecycleRaw, false}, {metadataRaw, true}} {
		if string(source.raw) == "null" {
			continue
		}
		event, err := observationSource(source.raw, s.db.TenantID(), result.ChannelID)
		if err != nil {
			return StreamObservations{}, ErrInvalid
		}
		if source.metadata {
			if event.Kind != platform.ChannelUpdateKind || event.Update == nil {
				return StreamObservations{}, ErrInvalid
			}
			result.Metadata = &MetadataObservation{EventID: event.ID, OccurredAt: event.OccurredAt, ReceivedAt: event.ReceivedAt, Data: *event.Update}
		} else {
			value := &LifecycleObservation{EventID: event.ID, OccurredAt: event.OccurredAt, ReceivedAt: event.ReceivedAt, Kind: event.Kind}
			switch event.Kind {
			case platform.StreamOnlineKind:
				value.StreamID = event.Online.StreamID
				started := event.Online.StartedAt
				value.StartedAt = &started
			case platform.StreamOfflineKind:
				value.StreamID = event.Offline.StreamID
			default:
				return StreamObservations{}, ErrInvalid
			}
			result.Lifecycle = value
		}
		result.State = "observed"
	}
	return result, nil
}

func observationSource(raw []byte, tenant, channel string) (platform.StreamEvent, error) {
	var row struct {
		ID             string                   `json:"id"`
		TenantID       string                   `json:"tenant_id"`
		ConnectionID   string                   `json:"connection_id"`
		IdempotencyKey string                   `json:"idempotency_key"`
		Platform       string                   `json:"platform"`
		Kind           platform.StreamEventKind `json:"kind"`
		ChannelID      string                   `json:"channel_id"`
		ChannelLogin   string                   `json:"channel_login"`
		OccurredAt     time.Time                `json:"occurred_at"`
		ReceivedAt     time.Time                `json:"received_at"`
		Deadline       int64                    `json:"dispatch_deadline"`
		Envelope       json.RawMessage          `json:"envelope"`
	}
	var event platform.StreamEvent
	if json.Unmarshal(raw, &row) != nil {
		return event, ErrInvalid
	}
	if json.Unmarshal(row.Envelope, &event) != nil {
		return event, ErrInvalid
	}
	canonical, err := platform.NewStreamEvent(platform.Connection{TenantID: event.TenantID, ID: event.ConnectionID, Platform: event.Platform}, event.NativeEventID, event.Kind, event.OccurredAt, event.ReceivedAt, event.Channel, event.Online, event.Offline, event.Update)
	rebuilt, _ := json.Marshal(canonical)
	var originalJSON, canonicalJSON any
	if json.Unmarshal(row.Envelope, &originalJSON) != nil || json.Unmarshal(rebuilt, &canonicalJSON) != nil {
		return platform.StreamEvent{}, ErrInvalid
	}
	if err != nil || !reflect.DeepEqual(originalJSON, canonicalJSON) || event.TenantID != tenant || event.Platform != "twitch" || event.Channel.ID != channel || event.ConnectionID != "twitch:"+channel || len(event.NativeEventID) > 256 || row.ID != event.ID || row.TenantID != tenant || row.ConnectionID != event.ConnectionID || row.IdempotencyKey != event.ID || row.Platform != event.Platform || row.Kind != event.Kind || row.ChannelID != channel || row.ChannelLogin != strings.ToLower(event.Channel.Login) || !row.OccurredAt.Equal(event.OccurredAt.Truncate(time.Microsecond)) || !row.ReceivedAt.Equal(event.ReceivedAt.Truncate(time.Microsecond)) || row.Deadline != event.OccurredAt.Unix()+120 {
		return platform.StreamEvent{}, ErrInvalid
	}
	return canonical, nil
}
