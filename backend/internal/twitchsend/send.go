// Package twitchsend implements a single bounded Helix send attempt. It does
// not resolve credentials, authorize tenant effects, schedule or retry them.
package twitchsend

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
)

const endpoint = "https://api.twitch.tv/helix/chat/messages"
const responseLimit = 16 * 1024

// DeliveryError carries the receipt through compatibility callers that still
// return (messageID, error). Its text contains no credentials or provider body.
type DeliveryError struct {
	Receipt platform.Receipt
	reason  string
	cause   error
}

func (e *DeliveryError) Error() string {
	return fmt.Sprintf("twitch chat delivery %s: %s", e.Receipt.Status, e.reason)
}

func (e *DeliveryError) Unwrap() error { return e.cause }

func contextCause(err error) error {
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return nil
}

// SendUserMessage requires a freshly resolved user token with user:write:chat,
// whose subject is senderID. The caller owns those checks and channel consent.
// An action ID is carried unchanged; this function does not deduplicate it.
func SendUserMessage(ctx context.Context, client *http.Client, clientID, accessToken, senderID string, message platform.OutboundMessage) (platform.Receipt, error) {
	r := platform.Receipt{ActionID: message.ActionID, Status: platform.Rejected, Failure: platform.Permanent}
	var cause error
	fail := func(reason string) (platform.Receipt, error) {
		return r, &DeliveryError{Receipt: r, reason: reason, cause: cause}
	}
	if client == nil || strings.TrimSpace(clientID) == "" || strings.TrimSpace(accessToken) == "" || strings.TrimSpace(senderID) == "" || strings.TrimSpace(message.ChannelID) == "" || strings.TrimSpace(message.Text) == "" || !utf8.ValidString(message.Text) || utf8.RuneCountInString(message.Text) > 500 {
		return fail("invalid send request")
	}
	if ctx.Err() != nil {
		cause = ctx.Err()
		return fail("canceled before send")
	}
	body, err := json.Marshal(struct {
		ChannelID string `json:"broadcaster_id"`
		SenderID  string `json:"sender_id"`
		Text      string `json:"message"`
		ReplyID   string `json:"reply_parent_message_id,omitempty"`
	}{message.ChannelID, senderID, message.Text, message.ReplyToID})
	if err != nil {
		return fail("invalid send request")
	}
	attemptCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	// Do not give net/http a replayable body: this POST has no provider
	// idempotency key. The local action identity is never sent as one.
	req, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, endpoint, io.NopCloser(bytes.NewReader(body)))
	if err != nil {
		return fail("invalid send request")
	}
	req.ContentLength = int64(len(body))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Client-Id", clientID)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	boundedClient := *client
	boundedClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := boundedClient.Do(req)
	if err != nil {
		// A timeout/cancellation can follow provider acceptance. Never wrap
		// transport errors: they can contain a URL, headers or response text.
		r.Status, r.Failure = platform.Uncertain, ""
		cause = contextCause(err)
		return fail("transport outcome unknown")
	}
	// Closing the response cannot revise an already observed delivery result.
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		switch resp.StatusCode {
		case http.StatusBadRequest, http.StatusUnprocessableEntity:
		case http.StatusUnauthorized, http.StatusForbidden:
			r.Failure = platform.Unauthorized
		case http.StatusTooManyRequests:
			r.Failure, r.RetryAfter = platform.QuotaLimited, retryAfter(resp.Header, time.Now())
		default:
			// A proxy or upstream failure, including an unexpected redirect,
			// is not proof the provider rejected this non-idempotent action.
			r.Status, r.Failure = platform.Uncertain, ""
		}
		return fail(fmt.Sprintf("HTTP %d", resp.StatusCode))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, responseLimit+1))
	if err != nil || len(data) > responseLimit {
		cause = contextCause(err)
		r.Status, r.Failure = platform.Uncertain, ""
		return fail("response outcome unknown")
	}
	var decoded struct {
		Data []struct {
			MessageID string `json:"message_id"`
			IsSent    *bool  `json:"is_sent"`
		} `json:"data"`
	}
	if json.Unmarshal(data, &decoded) != nil || len(decoded.Data) != 1 || decoded.Data[0].IsSent == nil {
		r.Status, r.Failure = platform.Uncertain, ""
		return fail("response outcome unknown")
	}
	item := decoded.Data[0]
	if !*item.IsSent {
		return fail("message dropped")
	}
	if !validMessageID(item.MessageID) {
		r.Status, r.Failure = platform.Uncertain, ""
		return fail("sent message identity unknown")
	}
	r.ProviderMessageID, r.Status, r.Failure = item.MessageID, platform.Delivered, ""
	return r, nil
}

func validMessageID(id string) bool {
	if len(id) == 0 || len(id) > 256 {
		return false
	}
	for _, c := range id {
		if c < 33 || c > 126 {
			return false
		}
	}
	return true
}

// These are advisory delays, never permission to replay an uncertain attempt.
// Saturation avoids overflow without shortening an unusually long valid hint.
func retryAfter(h http.Header, now time.Time) time.Duration {
	const maxDuration = time.Duration(1<<63 - 1)
	delay := time.Duration(0)
	if seconds, err := strconv.ParseInt(h.Get("Retry-After"), 10, 64); err == nil && seconds > 0 {
		if seconds > int64(maxDuration/time.Second) {
			delay = maxDuration
		} else {
			delay = time.Duration(seconds) * time.Second
		}
	} else if date, err := http.ParseTime(h.Get("Retry-After")); err == nil {
		delay = date.Sub(now)
	}
	if seconds, err := strconv.ParseInt(h.Get("Ratelimit-Reset"), 10, 64); err == nil && seconds > now.Unix() {
		reset := maxDuration
		if seconds-now.Unix() <= int64(maxDuration/time.Second) {
			reset = time.Unix(seconds, 0).Sub(now)
		}
		if reset > delay {
			delay = reset
		}
	}
	if delay < time.Second {
		return 30 * time.Second
	}
	return delay
}
