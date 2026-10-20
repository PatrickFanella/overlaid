// Package twitchconduit provides explicit app-authorized conduit observations.
// It registers no runtime, owns no credentials and grants no effect permission.
package twitchconduit

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalid      = errors.New("conduit inspection data invalid")
	ErrUnauthorized = errors.New("conduit inspection authorization unavailable")
	ErrUnavailable  = errors.New("conduit inspection unavailable")
	ErrNotFound     = errors.New("selected conduit unavailable")
	ErrChanged      = errors.New("conduit inspection changed or incomplete")
	ErrRateLimited  = errors.New("conduit inspection rate limited")
)

const (
	inspectionTimeout = 10 * time.Second
	maxBody           = 1 << 20
	maxBytes          = 8 << 20
	maxPages          = 201
	maxShards         = 20000
	conduitURL        = "https://api.twitch.tv/helix/eventsub/conduits"
	validateURL       = "https://id.twitch.tv/oauth2/validate"
)

// Config is trusted application configuration, never tenant or browser input.
// Token must respect cancellation; it owns acquisition/custody and may not log
// its value. Only the HTTP client's transport and a shorter timeout are borrowed.
type Config struct {
	ClientID, ConduitID string
	Token               func(context.Context) (string, error)
	HTTPClient          *http.Client
}

type Inspector struct {
	clientID, conduitID string
	token               func(context.Context) (string, error)
	http                *http.Client
}

type Shard struct {
	ID        int    `json:"id"`
	Status    string `json:"status"`
	Transport string `json:"transport"`
}

// Observation is a non-atomic sequence of provider reads, not a lease, tenant
// assignment, socket binding or permission to create/repair a subscription.
type Observation struct {
	ClientID    string    `json:"clientId"`
	ConduitID   string    `json:"conduitId"`
	StartedAt   time.Time `json:"startedAt"`
	CompletedAt time.Time `json:"completedAt"`
	ShardCount  int       `json:"shardCount"`
	Shards      []Shard   `json:"shards"`
}

func NewInspector(config Config) (*Inspector, error) {
	if !atom(config.ClientID, 128) || !atom(config.ConduitID, 128) || config.Token == nil {
		return nil, ErrInvalid
	}
	client := &http.Client{Timeout: inspectionTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if config.HTTPClient != nil {
		client.Transport = config.HTTPClient.Transport
		if config.HTTPClient.Timeout > 0 && config.HTTPClient.Timeout < client.Timeout {
			client.Timeout = config.HTTPClient.Timeout
		}
	}
	return &Inspector{clientID: config.ClientID, conduitID: config.ConduitID, token: config.Token, http: client}, nil
}

func atom(value string, limit int) bool {
	if value == "" || len(value) > limit {
		return false
	}
	for _, c := range value {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

func bearer(value string) bool {
	if value == "" || len(value) > 4096 {
		return false
	}
	for _, b := range []byte(value) {
		if b < 33 || b > 126 {
			return false
		}
	}
	return true
}

// Reject repeated/case-aliased JSON keys and excessive nesting before decoding
// the allowlisted projection. Unknown provider fields are otherwise discarded.
func uniqueJSON(raw []byte) bool {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	var value func(int) bool
	value = func(depth int) bool {
		if depth > 32 {
			return false
		}
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		delimiter, container := token.(json.Delim)
		if !container {
			return true
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return false
				}
				name, ok := key.(string)
				if !ok {
					return false
				}
				name = strings.ToLower(name)
				if seen[name] {
					return false
				}
				seen[name] = true
				if !value(depth + 1) {
					return false
				}
			}
		case '[':
			for decoder.More() {
				if !value(depth + 1) {
					return false
				}
			}
		default:
			return false
		}
		end, err := decoder.Token()
		return err == nil && (delimiter == '{' && end == json.Delim('}') || delimiter == '[' && end == json.Delim(']'))
	}
	if !value(0) {
		return false
	}
	_, err := decoder.Token()
	return errors.Is(err, io.EOF)
}

func boundedText(value string, limit int) bool {
	if len(value) > limit || strings.TrimSpace(value) != value {
		return false
	}
	for _, c := range value {
		if c < 32 || c == 127 {
			return false
		}
	}
	return true
}

// Inspect performs GETs only, with one token for the complete read. It returns
// no partial report and makes no application retry. Trusted callbacks/transports
// must cooperate with context cancellation; no helper goroutine is started.
func (i *Inspector) Inspect(caller context.Context) (result Observation, err error) {
	defer func() {
		if recover() != nil {
			result = Observation{}
			err = ErrUnavailable
			if caller != nil && caller.Err() != nil {
				err = caller.Err()
			}
		}
	}()
	if i == nil || i.http == nil || i.token == nil {
		return Observation{}, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(caller, inspectionTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return Observation{}, err
	}
	started := time.Now().UTC()
	token, err := i.token(ctx)
	if ctx.Err() != nil {
		return Observation{}, ctx.Err()
	}
	if err != nil || !bearer(token) {
		return Observation{}, ErrUnauthorized
	}
	budget := maxBytes
	if err := i.validate(ctx, token, &budget); err != nil {
		return Observation{}, err
	}
	count, err := i.selected(ctx, token, &budget)
	if err != nil {
		return Observation{}, err
	}
	shards, err := i.shards(ctx, token, count, &budget)
	if err != nil {
		return Observation{}, err
	}
	after, err := i.selected(ctx, token, &budget)
	if errors.Is(err, ErrNotFound) || err == nil && after != count {
		return Observation{}, ErrChanged
	}
	if err != nil {
		return Observation{}, err
	}
	if err := i.validate(ctx, token, &budget); err != nil {
		return Observation{}, err
	}
	if ctx.Err() != nil {
		return Observation{}, ctx.Err()
	}
	return Observation{ClientID: i.clientID, ConduitID: i.conduitID, StartedAt: started, CompletedAt: time.Now().UTC(), ShardCount: count, Shards: shards}, nil
}

func (i *Inspector) get(ctx context.Context, endpoint, token string, budget *int, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ErrInvalid
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if endpoint != validateURL {
		req.Header.Set("Client-Id", i.clientID)
	}
	response, err := i.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrUnavailable
	}
	defer func() { _ = response.Body.Close() }()
	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrUnauthorized
	case http.StatusTooManyRequests:
		return ErrRateLimited
	default:
		return ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxBody+1))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return ErrUnavailable
	}
	*budget -= len(raw)
	if len(raw) > maxBody || *budget < 0 || !uniqueJSON(raw) || json.Unmarshal(raw, out) != nil {
		return ErrInvalid
	}
	return nil
}

func (i *Inspector) validate(ctx context.Context, token string, budget *int) error {
	var response struct {
		ClientID string  `json:"client_id"`
		Login    *string `json:"login"`
		UserID   *string `json:"user_id"`
		Expires  int64   `json:"expires_in"`
	}
	var fields map[string]json.RawMessage
	if err := i.get(ctx, validateURL, token, budget, &fields); err != nil {
		return err
	}
	for key := range fields {
		switch key {
		case "client_id", "login", "user_id", "expires_in", "scopes":
		default:
			return ErrInvalid
		}
	}
	raw, _ := json.Marshal(fields)
	if json.Unmarshal(raw, &response) != nil {
		return ErrInvalid
	}
	// Twitch documents null user identity for app tokens; omitted fields are
	// also accepted. Even an explicitly empty string is a user-shaped response.
	if response.ClientID != i.clientID || response.Login != nil || response.UserID != nil || response.Expires < 30 {
		return ErrUnauthorized
	}
	return nil
}

func (i *Inspector) selected(ctx context.Context, token string, budget *int) (int, error) {
	var response struct {
		Data []struct {
			ID    string `json:"id"`
			Count int    `json:"shard_count"`
		} `json:"data"`
		Pagination struct {
			Cursor string `json:"cursor"`
		} `json:"pagination"`
	}
	if err := i.get(ctx, conduitURL, token, budget, &response); err != nil {
		return 0, err
	}
	if response.Data == nil || len(response.Data) > 100 || response.Pagination.Cursor != "" {
		return 0, ErrInvalid
	}
	seen := map[string]bool{}
	count := 0
	for _, conduit := range response.Data {
		if !atom(conduit.ID, 128) || seen[conduit.ID] || conduit.Count < 1 || conduit.Count > maxShards {
			return 0, ErrInvalid
		}
		seen[conduit.ID] = true
		if conduit.ID == i.conduitID {
			count = conduit.Count
		}
	}
	if count == 0 {
		return 0, ErrNotFound
	}
	return count, nil
}

type nativeShard struct {
	ID, Status string
	Transport  struct {
		Method    string
		SessionID string `json:"session_id"`
		Callback  string
	}
}

func knownStatus(status string) bool {
	switch status {
	case "enabled", "webhook_callback_verification_pending", "webhook_callback_verification_failed", "notification_failures_exceeded", "websocket_disconnected", "websocket_failed_ping_pong", "websocket_received_inbound_traffic", "websocket_internal_error", "websocket_network_timeout", "websocket_network_error", "websocket_failed_to_reconnect":
		return true
	}
	return false
}

func (i *Inspector) shards(ctx context.Context, token string, count int, budget *int) ([]Shard, error) {
	out := make([]Shard, 0, count)
	seen := map[int]bool{}
	cursors := map[string]bool{}
	cursor := ""
	for page := 0; page < maxPages; page++ {
		query := url.Values{"conduit_id": {i.conduitID}}
		if cursor != "" {
			query.Set("after", cursor)
		}
		var response struct {
			Data       []nativeShard
			Pagination struct{ Cursor string }
		}
		if err := i.get(ctx, conduitURL+"/shards?"+query.Encode(), token, budget, &response); err != nil {
			return nil, err
		}
		if response.Data == nil || len(response.Data) > maxShards-len(out) {
			return nil, ErrInvalid
		}
		for _, shard := range response.Data {
			id, err := strconv.Atoi(shard.ID)
			if err != nil || strconv.Itoa(id) != shard.ID || id < 0 || id >= count || seen[id] || !knownStatus(shard.Status) {
				return nil, ErrInvalid
			}
			transport := shard.Transport
			if !boundedText(transport.SessionID, 256) || !boundedText(transport.Callback, 4096) {
				return nil, ErrInvalid
			}
			switch transport.Method {
			case "websocket":
				if strings.HasPrefix(shard.Status, "webhook_") || transport.Callback != "" || shard.Status == "enabled" && transport.SessionID == "" {
					return nil, ErrInvalid
				}
			case "webhook":
				if strings.HasPrefix(shard.Status, "websocket_") || transport.SessionID != "" || transport.Callback == "" {
					return nil, ErrInvalid
				}
			default:
				return nil, ErrInvalid
			}
			seen[id] = true
			out = append(out, Shard{ID: id, Status: shard.Status, Transport: transport.Method})
		}
		cursor = response.Pagination.Cursor
		if cursor == "" {
			if len(out) != count {
				return nil, ErrChanged
			}
			sort.Slice(out, func(a, b int) bool { return out[a].ID < out[b].ID })
			return out, nil
		}
		if !boundedText(cursor, 1024) || cursors[cursor] || len(response.Data) == 0 {
			return nil, ErrInvalid
		}
		cursors[cursor] = true
	}
	return nil, ErrInvalid
}
