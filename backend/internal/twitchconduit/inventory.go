package twitchconduit

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"
)

type InventoryConfig struct {
	ClientID   string
	Token      func(context.Context) (string, error)
	HTTPClient *http.Client
}
type InventoryReader struct {
	clientID string
	token    func(context.Context) (string, error)
	http     *http.Client
}
type ConduitMetadata struct {
	ID         string `json:"id"`
	ShardCount int    `json:"shardCount"`
}

// InventoryObservation is a non-atomic sequence of bounded app-wide reads. It
// cannot identify an unknown creation request or establish durable ownership.
type InventoryObservation struct {
	ClientID    string            `json:"clientId"`
	StartedAt   time.Time         `json:"startedAt"`
	CompletedAt time.Time         `json:"completedAt"`
	Conduits    []ConduitMetadata `json:"conduits"`
}

func NewInventoryReader(config InventoryConfig) (*InventoryReader, error) {
	if !atom(config.ClientID, 128) || config.Token == nil {
		return nil, ErrInvalid
	}
	return &InventoryReader{clientID: config.ClientID, token: config.Token, http: providerClient(config.HTTPClient)}, nil
}

func (r *InventoryReader) Inspect(caller context.Context) (result InventoryObservation, err error) {
	defer func() {
		if recover() != nil {
			result = InventoryObservation{}
			err = ErrUnavailable
			if caller != nil && caller.Err() != nil {
				err = caller.Err()
			}
		}
	}()
	if r == nil || r.http == nil || r.token == nil || caller == nil {
		return result, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(caller, inspectionTimeout)
	defer cancel()
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	started := time.Now().UTC()
	token, err := r.token(ctx)
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if err != nil || !bearer(token) {
		return result, ErrUnauthorized
	}
	i := Inspector{clientID: r.clientID, http: r.http}
	budget := maxBytes
	originalUntil, err := i.validateUntil(ctx, token, &budget)
	if err != nil {
		return result, err
	}
	if !originalUntil.After(time.Now().Add(30 * time.Second)) {
		return result, ErrUnauthorized
	}
	before, err := i.inventory(ctx, token, &budget)
	if err != nil {
		return result, err
	}
	after, err := i.inventory(ctx, token, &budget)
	if err != nil {
		return result, err
	}
	if !slices.Equal(before, after) {
		return result, ErrChanged
	}
	finalUntil, err := i.validateUntil(ctx, token, &budget)
	if err != nil {
		return result, err
	}
	if !originalUntil.After(time.Now().Add(30*time.Second)) || !finalUntil.After(time.Now().Add(30*time.Second)) {
		return result, ErrUnauthorized
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	return InventoryObservation{ClientID: r.clientID, StartedAt: started, CompletedAt: time.Now().UTC(), Conduits: before}, nil
}

func (i *Inspector) inventory(ctx context.Context, token string, budget *int) ([]ConduitMetadata, error) {
	var root map[string]json.RawMessage
	if err := i.get(ctx, conduitURL, token, budget, &root); err != nil {
		return nil, err
	}
	for key := range root {
		if (strings.EqualFold(key, "data") && key != "data") || (strings.EqualFold(key, "pagination") && key != "pagination") {
			return nil, ErrInvalid
		}
	}
	var data []map[string]json.RawMessage
	if json.Unmarshal(root["data"], &data) != nil || data == nil || len(data) > 100 {
		return nil, ErrInvalid
	}
	if raw, ok := root["pagination"]; ok {
		var pagination map[string]json.RawMessage
		if json.Unmarshal(raw, &pagination) != nil || pagination == nil {
			return nil, ErrInvalid
		}
		for key, value := range pagination {
			var cursor *string
			if key != "cursor" || json.Unmarshal(value, &cursor) != nil || cursor == nil {
				return nil, ErrInvalid
			}
			if *cursor != "" {
				return nil, ErrChanged
			}
		}
	}
	out := make([]ConduitMetadata, 0, len(data))
	seen := make(map[string]bool, len(data))
	for _, item := range data {
		var m ConduitMetadata
		if json.Unmarshal(item["id"], &m.ID) != nil || !atom(m.ID, 128) || seen[m.ID] || json.Unmarshal(item["shard_count"], &m.ShardCount) != nil || m.ShardCount < 1 || m.ShardCount > maxShards {
			return nil, ErrInvalid
		}
		seen[m.ID] = true
		out = append(out, m)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].ID < out[b].ID })
	return out, nil
}
