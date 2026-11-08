package obsws

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gorilla/websocket"
)

// ErrFallbackEvidence exposes no provider comment, URL, credentials or names.
var ErrFallbackEvidence = errors.New("obs_fallback_evidence_unavailable")

// ErrFallbackConnection distinguishes a failed connection from unknown evidence.
var ErrFallbackConnection = errors.New("obs_fallback_connection_unavailable")

// FallbackSnapshot contains bounded read-only evidence from one connection.
// Missing means absent from a complete input list, never missing video/silence.
// Presence and intentional suppression cannot qualify the fallback condition.
type FallbackSnapshot struct {
	InputPresent, InputActive, InputMuted, SceneAvailable bool
}

// InspectFallback reads the exact current collection, input and target. It
// refuses malformed responses, collection drift and interleaved events. The
// result is evidence only, with no retained identity or effect permission.
func (c Client) InspectFallback(ctx context.Context, collection, input, scene string) (out FallbackSnapshot, failure error) {
	if ctx == nil || !fallbackName(collection) || !fallbackName(input) || !fallbackName(scene) {
		return out, ErrFallbackEvidence
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	defer func() {
		if failure != nil {
			out = FallbackSnapshot{}
			if ctx.Err() != nil {
				failure = ctx.Err()
			} else if !errors.Is(failure, ErrFallbackConnection) {
				failure = ErrFallbackEvidence
			}
		}
	}()
	conn, _, err := c.connect(ctx)
	if err != nil {
		return out, ErrFallbackConnection
	}
	// Closing the socket cannot change the already correlated read result.
	defer func() { _ = conn.Close() }()
	conn.SetReadLimit(1 << 20)
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	return inspectFallback(ctx, conn, collection, input, scene)
}

// SetFallbackScene rechecks absence, collection and selected scene through the
// same connection used for one scene request. The trusted guard runs after the
// provider reads and immediately before transport. Callers must already own a
// durable one-use claim and must never replay an error from this method.
func (c Client) SetFallbackScene(ctx context.Context, collection, input, scene string, guard func(context.Context) error) (failure error) {
	if ctx == nil || guard == nil || !fallbackName(collection) || !fallbackName(input) || !fallbackName(scene) {
		return ErrFallbackEvidence
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	defer func() {
		if failure != nil {
			if ctx.Err() != nil {
				failure = ctx.Err()
			} else {
				failure = ErrFallbackEvidence
			}
		}
	}()
	conn, _, err := c.connect(ctx)
	if err != nil {
		return err
	}
	// A close error cannot revoke an acknowledged scene attempt or allow replay.
	defer func() { _ = conn.Close() }()
	conn.SetReadLimit(1 << 20)
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	snapshot, err := inspectFallback(ctx, conn, collection, input, scene)
	if err != nil || snapshot.InputPresent || !snapshot.SceneAvailable {
		return ErrFallbackEvidence
	}
	if err = guard(ctx); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	_, _, err = fallbackRequest(ctx, conn, "SetCurrentProgramScene", map[string]any{"sceneName": scene})
	return err
}

func fallbackName(s string) bool {
	return len(s) > 0 && len(s) <= 255 && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}

func inspectFallback(ctx context.Context, conn *websocket.Conn, collection, input, scene string) (out FallbackSnapshot, err error) {
	if err = checkFallbackCollection(ctx, conn, collection); err != nil {
		return out, err
	}
	readInputs := func() (bool, error) {
		data, _, e := fallbackRequest(ctx, conn, "GetInputList", nil)
		if e != nil {
			return false, e
		}
		var body struct {
			Inputs *[]Input `json:"inputs"`
		}
		if json.Unmarshal(data, &body) != nil || body.Inputs == nil || len(*body.Inputs) > 1000 {
			return false, ErrFallbackEvidence
		}
		seen := map[string]bool{}
		for _, item := range *body.Inputs {
			if !fallbackName(item.InputName) || seen[item.InputName] {
				return false, ErrFallbackEvidence
			}
			seen[item.InputName] = true
		}
		return seen[input], nil
	}
	if out.InputPresent, err = readInputs(); err != nil {
		return FallbackSnapshot{}, err
	}
	data, _, err := fallbackRequest(ctx, conn, "GetSceneList", nil)
	if err != nil {
		return FallbackSnapshot{}, err
	}
	var scenes struct {
		Scenes *[]Scene `json:"scenes"`
	}
	if json.Unmarshal(data, &scenes) != nil || scenes.Scenes == nil || len(*scenes.Scenes) > 1000 {
		return FallbackSnapshot{}, ErrFallbackEvidence
	}
	seen := map[string]bool{}
	for _, item := range *scenes.Scenes {
		if !fallbackName(item.SceneName) || seen[item.SceneName] {
			return FallbackSnapshot{}, ErrFallbackEvidence
		}
		seen[item.SceneName] = true
	}
	out.SceneAvailable = seen[scene]
	if out.InputPresent {
		data, _, err = fallbackRequest(ctx, conn, "GetSourceActive", map[string]any{"sourceName": input})
		if err != nil {
			return FallbackSnapshot{}, err
		}
		var active struct {
			Active *bool `json:"videoActive"`
		}
		if json.Unmarshal(data, &active) != nil || active.Active == nil {
			return FallbackSnapshot{}, ErrFallbackEvidence
		}
		out.InputActive = *active.Active
		data, code, e := fallbackRequest(ctx, conn, "GetInputMute", map[string]any{"inputName": input})
		// OBS reports InvalidResourceState for inputs without audio. No such
		// response is interpreted as absence; the complete list proved present.
		if e != nil && code != 604 {
			return FallbackSnapshot{}, e
		}
		if e == nil {
			var muted struct {
				Muted *bool `json:"inputMuted"`
			}
			if json.Unmarshal(data, &muted) != nil || muted.Muted == nil {
				return FallbackSnapshot{}, ErrFallbackEvidence
			}
			out.InputMuted = *muted.Muted
		}
	}
	// Bound a snapshot by a second complete input/collection read. Changes or
	// subscribed events during the sequence cannot establish fresh absence.
	if present, e := readInputs(); e != nil || present != out.InputPresent {
		return FallbackSnapshot{}, ErrFallbackEvidence
	}
	if err = checkFallbackCollection(ctx, conn, collection); err != nil {
		return FallbackSnapshot{}, err
	}
	return out, nil
}

func checkFallbackCollection(ctx context.Context, conn *websocket.Conn, want string) error {
	data, _, err := fallbackRequest(ctx, conn, "GetSceneCollectionList", nil)
	if err != nil {
		return err
	}
	var body struct {
		Current string `json:"currentSceneCollectionName"`
	}
	if json.Unmarshal(data, &body) != nil || body.Current != want {
		return ErrFallbackEvidence
	}
	return nil
}

// fallbackRequest is strict about correlation and events. Every frame remains
// bounded by the connection read limit and the original operation deadline.
func fallbackRequest(ctx context.Context, conn *websocket.Conn, kind string, payload map[string]any) (json.RawMessage, int, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	if err := setConnDeadline(conn, deadlineFromContext(ctx)); err != nil {
		return nil, 0, err
	}
	id := newRequestID()
	if err := conn.WriteJSON(envelope{Op: opRequest, Data: mustJSON(requestData{RequestType: kind, RequestID: id, RequestData: payload})}); err != nil {
		return nil, 0, err
	}
	var msg envelope
	if err := conn.ReadJSON(&msg); err != nil {
		return nil, 0, err
	}
	if msg.Op != opRequestResponse {
		return nil, 0, ErrFallbackEvidence
	}
	var response struct {
		RequestType string `json:"requestType"`
		RequestID   string `json:"requestId"`
		Status      struct {
			Result *bool `json:"result"`
			Code   int   `json:"code"`
		} `json:"requestStatus"`
		Data json.RawMessage `json:"responseData"`
	}
	if json.Unmarshal(msg.Data, &response) != nil || response.RequestType != kind || response.RequestID != id || response.Status.Result == nil {
		return nil, 0, ErrFallbackEvidence
	}
	if !*response.Status.Result || response.Status.Code != 100 {
		return nil, response.Status.Code, ErrFallbackEvidence
	}
	return response.Data, 100, nil
}
