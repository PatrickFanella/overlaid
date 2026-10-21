package twitchconduit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var ErrAssignmentUncertain = errors.New("conduit shard assignment uncertain")

type AssignmentOutcome string

const (
	AssignmentNotStarted AssignmentOutcome = "not_started"
	AssignmentRefused    AssignmentOutcome = "refused"
	AssignmentUncertain  AssignmentOutcome = "uncertain"
	AssignmentAssigned   AssignmentOutcome = "assigned"
)

// AssignmentConfig is trusted service configuration, never tenant/browser input.
// The token source and custom transports must honor cancellation and protect
// private credentials. A separate durable owner must fence each invocation.
type AssignmentConfig struct {
	ClientID   string
	Token      func(context.Context) (string, error)
	HTTPClient *http.Client
}
type Assigner struct {
	clientID string
	token    func(context.Context) (string, error)
	http     *http.Client
}

// ShardBinding is private caller input from a durable owner and live session.
// ShardCount is the original intended conduit bound, not a fresh provider read.
type ShardBinding struct {
	ConduitID  string
	ShardCount int
	ShardID    string
	SessionID  string
}

// AssignedShard is a provider acknowledgement, not ownership or readiness.
// The session ID is compared privately and never returned.
type AssignedShard struct {
	ClientID    string    `json:"clientId"`
	ConduitID   string    `json:"conduitId"`
	ShardID     string    `json:"shardId"`
	StartedAt   time.Time `json:"startedAt"`
	CompletedAt time.Time `json:"completedAt"`
}
type Assignment struct {
	Outcome AssignmentOutcome `json:"outcome"`
	Shard   *AssignedShard    `json:"shard,omitempty"`
}

func NewAssigner(config AssignmentConfig) (*Assigner, error) {
	if !atom(config.ClientID, 128) || config.Token == nil {
		return nil, ErrInvalid
	}
	return &Assigner{clientID: config.ClientID, token: config.Token, http: providerClient(config.HTTPClient)}, nil
}

// Assign makes at most one non-replayable assignment PATCH. The caller must retain
// its own original intent/claim and never turn uncertainty into another call.
// This adapter performs no local admission, acquisition, refresh or recovery.
func (c *Assigner) Assign(caller context.Context, binding ShardBinding) (result Assignment, err error) {
	result.Outcome = AssignmentNotStarted
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
			result = Assignment{Outcome: AssignmentNotStarted}
			err = ErrUnavailable
			if operation != nil && operation.Err() != nil {
				err = operation.Err()
			}
			if attempted {
				result.Outcome = AssignmentUncertain
				err = errors.Join(ErrAssignmentUncertain, err)
			}
		}
	}()
	if c == nil || c.http == nil || c.token == nil || caller == nil || !validShardBinding(binding) {
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, "https://api.twitch.tv/helix/eventsub/conduits/shards", strings.NewReader(fmt.Sprintf(`{"conduit_id":%q,"shards":[{"id":%q,"transport":{"method":"websocket","session_id":%q}}]}`, binding.ConduitID, binding.ShardID, binding.SessionID)))
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
	result.Outcome = AssignmentUncertain
	uncertain := func(cause error) (Assignment, error) {
		return Assignment{Outcome: AssignmentUncertain}, errors.Join(ErrAssignmentUncertain, cause)
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
	case http.StatusAccepted:
	case http.StatusBadRequest, http.StatusNotFound:
		return Assignment{Outcome: AssignmentRefused}, ErrInvalid
	case http.StatusUnauthorized:
		return Assignment{Outcome: AssignmentRefused}, ErrUnauthorized
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
	if !assignmentAcknowledged(root, binding) {
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
	return Assignment{Outcome: AssignmentAssigned, Shard: &AssignedShard{ClientID: c.clientID, ConduitID: binding.ConduitID, ShardID: binding.ShardID, StartedAt: started.UTC(), CompletedAt: time.Now().UTC()}}, nil
}

func validShardBinding(b ShardBinding) bool {
	n, err := strconv.Atoi(b.ShardID)
	return atom(b.ConduitID, 128) && len(b.SessionID) <= 512 && bearer(b.SessionID) && b.ShardCount >= 1 && b.ShardCount <= maxShards && err == nil && n >= 0 && n < b.ShardCount && strconv.Itoa(n) == b.ShardID
}

func assignmentAcknowledged(root map[string]json.RawMessage, b ShardBinding) bool {
	// Unlike general metadata, update errors must be present and empty. This
	// single-shard operation cannot safely accept a partial or contradictory body.
	for key := range root {
		if (strings.EqualFold(key, "data") && key != "data") || (strings.EqualFold(key, "errors") && key != "errors") {
			return false
		}
	}
	var data []map[string]json.RawMessage
	var failures []json.RawMessage
	if json.Unmarshal(root["data"], &data) != nil || len(data) != 1 || json.Unmarshal(root["errors"], &failures) != nil || failures == nil || len(failures) != 0 {
		return false
	}
	var id, status, method, session string
	var transport map[string]json.RawMessage
	row := data[0]
	if json.Unmarshal(row["id"], &id) != nil || id != b.ShardID || json.Unmarshal(row["status"], &status) != nil || status != "enabled" || json.Unmarshal(row["transport"], &transport) != nil || transport == nil {
		return false
	}
	if json.Unmarshal(transport["method"], &method) != nil || method != "websocket" || json.Unmarshal(transport["session_id"], &session) != nil || session != b.SessionID {
		return false
	}
	// A contradictory disconnected/webhook transport cannot acknowledge a live
	// websocket binding. Unknown bounded metadata is otherwise discarded.
	for key := range transport {
		switch strings.ToLower(key) {
		case "disconnected_at", "callback", "secret":
			return false
		}
	}
	return true
}
