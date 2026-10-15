package twitchsend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDeliveryOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		body    string
		want    platform.DeliveryStatus
		failure platform.FailureKind
	}{
		{"sent", 200, `{"data":[{"message_id":"sent-123","is_sent":true}]}`, platform.Delivered, ""},
		{"dropped", 200, `{"data":[{"is_sent":false,"drop_reason":{"message":"private provider body"}}]}`, platform.Rejected, platform.Permanent},
		{"missing flag", 200, `{"data":[{"message_id":"sent-123"}]}`, platform.Uncertain, ""},
		{"null flag", 200, `{"data":[{"is_sent":null}]}`, platform.Uncertain, ""},
		{"missing ID", 200, `{"data":[{"is_sent":true}]}`, platform.Uncertain, ""},
		{"bad ID", 200, `{"data":[{"message_id":"bad\nID","is_sent":true}]}`, platform.Uncertain, ""},
		{"empty data", 200, `{"data":[]}`, platform.Uncertain, ""},
		{"duplicate data", 200, `{"data":[{"is_sent":true},{"is_sent":false}]}`, platform.Uncertain, ""},
		{"malformed", 200, `{`, platform.Uncertain, ""},
		{"trailing data", 200, `{"data":[{"is_sent":false}]} secret`, platform.Uncertain, ""},
		{"oversize", 200, strings.Repeat("x", responseLimit+1), platform.Uncertain, ""},
		{"bad request", 400, "private provider body", platform.Rejected, platform.Permanent},
		{"unauthorized", 401, "private provider body", platform.Rejected, platform.Unauthorized},
		{"forbidden", 403, "private provider body", platform.Rejected, platform.Unauthorized},
		{"not found", 404, "private provider body", platform.Uncertain, ""},
		{"too large", 422, "private provider body", platform.Rejected, platform.Permanent},
		{"rate limited", 429, "private provider body", platform.Rejected, platform.QuotaLimited},
		{"gateway failure", 502, "private provider body", platform.Uncertain, ""},
		{"server failure", 500, "private provider body", platform.Uncertain, ""},
		{"unexpected success", 204, "", platform.Uncertain, ""},
		{"redirect GET", 302, "", platform.Uncertain, ""},
		{"redirect POST", 307, "", platform.Uncertain, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: roundTrip(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.URL.String() != endpoint || req.Method != "POST" || req.GetBody != nil {
					t.Fatal("unsafe send request")
				}
				if req.Header.Get("Authorization") != "Bearer secret-access" || req.Header.Get("Client-Id") != "client" {
					t.Fatal("missing authorization")
				}
				deadline, ok := req.Context().Deadline()
				if !ok || time.Until(deadline) > 10*time.Second {
					t.Fatal("unbounded request")
				}
				var payload map[string]any
				if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				if len(payload) != 4 || payload["broadcaster_id"] != "channel" || payload["sender_id"] != "sender" || payload["message"] != "hello" || payload["reply_parent_message_id"] != "parent" {
					t.Fatalf("payload: %#v", payload)
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Location": {"https://elsewhere.invalid/"}, "Retry-After": {"42"}}, Body: io.NopCloser(strings.NewReader(tc.body)), Request: req}, nil
			})}
			r, err := SendUserMessage(t.Context(), client, "client", "secret-access", "sender", platform.OutboundMessage{ActionID: "action", ChannelID: "channel", Text: "hello", ReplyToID: "parent"})
			if calls != 1 || r.ActionID != "action" || r.Status != tc.want || r.Failure != tc.failure {
				t.Fatalf("receipt: %#v; calls=%d", r, calls)
			}
			if tc.want == platform.Delivered {
				if err != nil || r.ProviderMessageID != "sent-123" {
					t.Fatalf("delivery: %#v %v", r, err)
				}
			} else {
				var deliveryErr *DeliveryError
				if !errors.As(err, &deliveryErr) || deliveryErr.Receipt != r || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "private") {
					t.Fatalf("unsafe/untyped error: %v", err)
				}
				if r.ProviderMessageID != "" {
					t.Fatal("unconfirmed echo identity")
				}
			}
			if tc.status == 429 && r.RetryAfter != 42*time.Second {
				t.Fatalf("retry delay: %v", r.RetryAfter)
			}
		})
	}
}

func TestTransportFailureDoesNotReplayOrExposeError(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, fmt.Errorf("secret-access private error")
	})}
	r, err := SendUserMessage(t.Context(), client, "client", "access", "sender", platform.OutboundMessage{ChannelID: "channel", Text: "hello"})
	if calls != 1 || r.Status != platform.Uncertain || r.Failure != "" || err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("receipt: %#v, error %v, calls %d", r, err, calls)
	}
}

type brokenBody struct{}

func (brokenBody) Read([]byte) (int, error) { return 0, errors.New("private read error") }
func (brokenBody) Close() error             { return nil }

func TestResponseReadFailureIsUncertain(t *testing.T) {
	client := &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: brokenBody{}}, nil
	})}
	r, err := SendUserMessage(t.Context(), client, "client", "access", "sender", platform.OutboundMessage{ChannelID: "channel", Text: "hello"})
	if r.Status != platform.Uncertain || err == nil || strings.Contains(err.Error(), "private") {
		t.Fatalf("%#v %v", r, err)
	}
}

func TestInvalidOrCanceledRequestNeverSends(t *testing.T) {
	client := &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) { t.Fatal("unexpected provider request"); return nil, nil })}
	for _, text := range []string{"", "  ", strings.Repeat("x", 501), string([]byte{0xff})} {
		r, err := SendUserMessage(t.Context(), client, "client", "access", "sender", platform.OutboundMessage{ChannelID: "channel", Text: text})
		if r.Status != platform.Rejected || err == nil {
			t.Fatalf("%#v %v", r, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r, err := SendUserMessage(ctx, client, "client", "access", "sender", platform.OutboundMessage{ChannelID: "channel", Text: "hello"})
	if r.Status != platform.Rejected || err == nil {
		t.Fatalf("%#v %v", r, err)
	}
}

func TestRetryHints(t *testing.T) {
	now := time.Unix(1700000000, 0).UTC()
	for _, tc := range []struct {
		retry, reset string
		want         time.Duration
	}{
		{"42", "", 42 * time.Second},
		{now.Add(time.Minute).Format(http.TimeFormat), "", time.Minute},
		{"10", strconvInt(now.Unix() + 90), 90 * time.Second},
		{"garbage", "", 30 * time.Second},
		{"-1", "-1", 30 * time.Second},
		{"0", strconvInt(now.Unix() - 1), 30 * time.Second},
		{"9223372036854775807", "", time.Duration(1<<63 - 1)},
		{"", "9223372036854775807", time.Duration(1<<63 - 1)},
	} {
		if got := retryAfter(http.Header{"Retry-After": {tc.retry}, "Ratelimit-Reset": {tc.reset}}, now); got != tc.want {
			t.Fatalf("%q %q got %v want %v", tc.retry, tc.reset, got, tc.want)
		}
	}
}

func strconvInt(n int64) string { return fmt.Sprintf("%d", n) }
