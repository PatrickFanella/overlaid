package twitchconduit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var (
	ErrCreationUncertain = errors.New("conduit creation uncertain")
	ErrCreationLimited   = errors.New("conduit creation limited")
)

type CreationOutcome string

const (
	CreationNotStarted CreationOutcome = "not_started"
	CreationRefused    CreationOutcome = "refused"
	CreationUncertain  CreationOutcome = "uncertain"
	CreationCreated    CreationOutcome = "created"
)

// CreationConfig is trusted service configuration, never tenant/browser input.
// The token source and custom transports must honor cancellation and protect
// private credentials. A separate durable owner must fence each invocation.
type CreationConfig struct {
	ClientID   string
	Token      func(context.Context) (string, error)
	HTTPClient *http.Client
}
type Creator struct {
	clientID string
	token    func(context.Context) (string, error)
	http     *http.Client
}

// CreatedConduit is provider acknowledgement metadata, not durable ownership,
// shard assignment, tenant mapping or permission for any subsequent effect.
type CreatedConduit struct {
	ClientID    string    `json:"clientId"`
	ID          string    `json:"id"`
	ShardCount  int       `json:"shardCount"`
	StartedAt   time.Time `json:"startedAt"`
	CompletedAt time.Time `json:"completedAt"`
}
type Creation struct {
	Outcome CreationOutcome `json:"outcome"`
	Conduit *CreatedConduit `json:"conduit,omitempty"`
}

func NewCreator(config CreationConfig) (*Creator, error) {
	if !atom(config.ClientID, 128) || config.Token == nil {
		return nil, ErrInvalid
	}
	return &Creator{clientID: config.ClientID, token: config.Token, http: providerClient(config.HTTPClient)}, nil
}

// Create makes at most one non-replayable creation POST. The caller must retain
// its own original intent/claim and never turn uncertainty into another call.
// This adapter performs no local admission, acquisition, refresh or recovery.
func (c *Creator) Create(caller context.Context, shards int) (result Creation, err error) {
	result.Outcome = CreationNotStarted
	attempted := false
	operation := caller
	var release context.CancelFunc
	defer func() {
		defer func() {
			if release != nil {
				release()
			}
		}()
		if recover() != nil {
			result = Creation{Outcome: CreationNotStarted}
			err = ErrUnavailable
			if operation != nil && operation.Err() != nil {
				err = operation.Err()
			}
			if attempted {
				result.Outcome = CreationUncertain
				err = errors.Join(ErrCreationUncertain, err)
			}
		}
	}()
	if c == nil || c.http == nil || c.token == nil || caller == nil || shards < 1 || shards > maxShards {
		return result, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(caller, inspectionTimeout)
	release = cancel
	operation = ctx
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	token, err := c.token(ctx)
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if err != nil || !bearer(token) {
		return result, ErrUnauthorized
	}
	inspector := Inspector{clientID: c.clientID, http: c.http}
	budget := maxBytes
	originalUntil, err := inspector.validateUntil(ctx, token, &budget)
	if err != nil {
		return result, err
	}
	if !originalUntil.After(time.Now().Add(30 * time.Second)) {
		return result, ErrUnauthorized
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, conduitURL, strings.NewReader(fmt.Sprintf(`{"shard_count":%d}`, shards)))
	if err != nil {
		return result, ErrInvalid
	}
	req.GetBody = nil
	req.Header.Set("Client-Id", c.clientID)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	started := time.Now()
	attempted = true
	result.Outcome = CreationUncertain
	uncertain := func(cause error) (Creation, error) {
		return Creation{Outcome: CreationUncertain}, errors.Join(ErrCreationUncertain, cause)
	}
	response, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return uncertain(ctx.Err())
		}
		return uncertain(ErrUnavailable)
	}
	defer func() { _ = response.Body.Close() }()
	if ctx.Err() != nil {
		return uncertain(ctx.Err())
	}
	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusBadRequest:
		return Creation{Outcome: CreationRefused}, ErrInvalid
	case http.StatusUnauthorized:
		return Creation{Outcome: CreationRefused}, ErrUnauthorized
	case http.StatusTooManyRequests:
		return Creation{Outcome: CreationRefused}, ErrCreationLimited
	default:
		return uncertain(ErrUnavailable)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if ctx.Err() != nil {
		return uncertain(ctx.Err())
	}
	if err != nil {
		return uncertain(ErrUnavailable)
	}
	budget -= len(raw)
	if len(raw) > 65536 || budget < 0 || !uniqueJSON(raw) {
		return uncertain(ErrInvalid)
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(raw, &root) != nil {
		return uncertain(ErrInvalid)
	}
	var data []map[string]json.RawMessage
	if json.Unmarshal(root["data"], &data) != nil || len(data) != 1 {
		return uncertain(ErrInvalid)
	}
	var id string
	var count int
	if json.Unmarshal(data[0]["id"], &id) != nil || !atom(id, 128) || json.Unmarshal(data[0]["shard_count"], &count) != nil || count != shards {
		return uncertain(ErrInvalid)
	}
	finalUntil, err := inspector.validateUntil(ctx, token, &budget)
	if err != nil {
		return uncertain(err)
	}
	if !originalUntil.After(time.Now().Add(30*time.Second)) || !finalUntil.After(time.Now().Add(30*time.Second)) {
		return uncertain(ErrUnauthorized)
	}
	if ctx.Err() != nil {
		return uncertain(ctx.Err())
	}
	return Creation{Outcome: CreationCreated, Conduit: &CreatedConduit{ClientID: c.clientID, ID: id, ShardCount: count, StartedAt: started.UTC(), CompletedAt: time.Now().UTC()}}, nil
}
