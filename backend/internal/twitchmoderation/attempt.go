// Package twitchmoderation performs one bounded Helix moderation attempt.
// Callers own fresh authorization, durable claims and endpoint pacing. This
// transport is not registered in application startup and never retries effects.
package twitchmoderation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
)

const responseLimit = 16 * 1024

// AttemptError exposes no provider body, target, URL, reason or credential.
// Only cancellation/deadline identity is retained through Unwrap.
type AttemptError struct{ cause error }

func (*AttemptError) Error() string   { return "twitch moderation attempt failed" }
func (e *AttemptError) Unwrap() error { return e.cause }

func contextCause(err error) error {
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return nil
}

// AttemptUser requires a freshly validated user token whose subject equals
// moderatorID. Delete requires moderator:manage:chat_messages; the other
// operations require moderator:manage:banned_users. The caller must also
// recheck channel role, consent and entitlement. This function grants none of
// those permissions and an action ID is not a provider idempotency key.
func AttemptUser(ctx context.Context, client *http.Client, clientID, accessToken, moderatorID string, action platform.ModerationAction) (platform.ModerationReceipt, error) {
	r := platform.ModerationReceipt{ActionID: action.ActionID, Status: platform.Rejected, Failure: platform.Permanent}
	fail := func(err error) (platform.ModerationReceipt, error) { return r, &AttemptError{cause: contextCause(err)} }
	if client == nil || !headerToken(clientID) || !headerToken(accessToken) || !numericID(moderatorID) || action.ValidateTwitch() != nil {
		return fail(nil)
	}
	if ctx.Err() != nil {
		return fail(ctx.Err())
	}
	query := url.Values{"broadcaster_id": {action.ChannelID}, "moderator_id": {moderatorID}}
	path, method, success := "/moderation/bans", http.MethodDelete, http.StatusNoContent
	var body []byte
	switch action.Capability {
	case "message.delete":
		path = "/moderation/chat"
		query.Set("message_id", action.MessageID)
	case "user.unban":
		query.Set("user_id", action.UserID)
	default: // ValidateTwitch already restricts this to ban/timeout.
		method, success = http.MethodPost, http.StatusOK
		data := struct {
			UserID   string `json:"user_id"`
			Duration int64  `json:"duration,omitempty"`
			Reason   string `json:"reason,omitempty"`
		}{UserID: action.UserID, Reason: action.Reason}
		if action.Capability == "user.timeout" {
			data.Duration = int64(action.Duration / time.Second)
		}
		body, _ = json.Marshal(struct {
			Data any `json:"data"`
		}{data})
	}
	attemptCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	// A non-replayable reader also covers DELETE's empty body. Never supply
	// GetBody or an idempotency header for these provider effects.
	req, err := http.NewRequestWithContext(attemptCtx, method, "https://api.twitch.tv/helix"+path+"?"+query.Encode(), io.NopCloser(bytes.NewReader(body)))
	if err != nil {
		return fail(nil)
	}
	req.ContentLength = int64(len(body))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Client-Id", clientID)
	req.Header.Set("Accept", "application/json")
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	bounded := *client
	bounded.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := bounded.Do(req)
	if err != nil {
		r.Status, r.Failure = platform.Uncertain, ""
		return fail(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != success {
		switch resp.StatusCode {
		case http.StatusBadRequest:
		case http.StatusNotFound:
			if action.Capability != "message.delete" {
				r.Status, r.Failure = platform.Uncertain, ""
			}
		case http.StatusUnauthorized, http.StatusForbidden:
			r.Failure = platform.Unauthorized
		case http.StatusConflict:
			if action.Capability == "message.delete" {
				r.Status, r.Failure = platform.Uncertain, ""
			} else {
				r.Failure = platform.Retryable
			}
		case http.StatusTooManyRequests:
			r.Failure, r.RetryAfter = platform.QuotaLimited, retryAfter(resp.Header, time.Now())
		default:
			r.Status, r.Failure = platform.Uncertain, ""
		}
		return fail(nil)
	}
	if success == http.StatusOK {
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, responseLimit+1))
		var decoded struct {
			Data []struct {
				ChannelID   string `json:"broadcaster_id"`
				ModeratorID string `json:"moderator_id"`
				UserID      string `json:"user_id"`
			} `json:"data"`
		}
		if readErr != nil || len(raw) > responseLimit || json.Unmarshal(raw, &decoded) != nil || len(decoded.Data) != 1 ||
			decoded.Data[0].ChannelID != action.ChannelID || decoded.Data[0].ModeratorID != moderatorID || decoded.Data[0].UserID != action.UserID {
			r.Status, r.Failure = platform.Uncertain, ""
			return fail(readErr)
		}
	}
	r.Status, r.Failure = platform.Delivered, ""
	return r, nil
}

func numericID(s string) bool {
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func headerToken(s string) bool {
	if len(s) == 0 || len(s) > 4096 {
		return false
	}
	for _, c := range s {
		if c < 33 || c > 126 {
			return false
		}
	}
	return true
}

// Hints pace future independent actions, never replay this claimed action.
// Clamp to the moderation receipt contract without duration arithmetic overflow.
func retryAfter(h http.Header, now time.Time) time.Duration {
	const maximum = 24 * time.Hour
	delay := time.Duration(0)
	if n, err := strconv.ParseUint(h.Get("Retry-After"), 10, 64); err == nil || errors.Is(err, strconv.ErrRange) {
		if n > 86400 {
			delay = maximum
		} else {
			delay = time.Duration(n) * time.Second
		}
	} else if date, err := http.ParseTime(h.Get("Retry-After")); err == nil {
		delay = date.Sub(now)
	}
	if n, err := strconv.ParseInt(h.Get("Ratelimit-Reset"), 10, 64); (err == nil || errors.Is(err, strconv.ErrRange)) && n > now.Unix() {
		reset := maximum
		if n-now.Unix() < 86400 {
			reset = time.Duration(n-now.Unix()) * time.Second
		}
		if reset > delay {
			delay = reset
		}
	}
	if delay > maximum {
		return maximum
	}
	if delay < time.Second {
		return 30 * time.Second
	}
	return delay
}
