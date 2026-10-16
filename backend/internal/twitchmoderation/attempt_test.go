package twitchmoderation

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func action(capability platform.Capability) platform.ModerationAction {
	a := platform.ModerationAction{ActionID: strings.Repeat("a", 64), ChannelID: "123", Capability: capability, UserID: "456"}
	if capability == "message.delete" {
		a.UserID, a.MessageID = "", "message&safe=1"
	}
	if capability == "user.timeout" {
		a.Duration = 5 * time.Minute
	}
	return a
}

const accepted = `{"data":[{"broadcaster_id":"123","moderator_id":"789","user_id":"456"}]}`

func TestRequestShapesAndAcceptance(t *testing.T) {
	for _, cap := range []platform.Capability{"message.delete", "user.timeout", "user.ban", "user.unban"} {
		t.Run(string(cap), func(t *testing.T) {
			a := action(cap)
			if cap == "user.ban" || cap == "user.timeout" {
				a.Reason = "PRIVATE reason"
			}
			calls := 0
			client := &http.Client{Transport: transport(func(req *http.Request) (*http.Response, error) {
				calls++
				deadline, ok := req.Context().Deadline()
				if !ok || time.Until(deadline) > 10*time.Second || req.GetBody != nil || req.Body == nil || req.Header.Get("Idempotency-Key") != "" {
					t.Fatal("unbounded or replayable request")
				}
				if req.URL.Scheme != "https" || req.URL.Host != "api.twitch.tv" || req.Header.Get("Client-Id") != "client" || req.Header.Get("Authorization") != "Bearer PRIVATE_TOKEN" {
					t.Fatal("incorrect provider boundary")
				}
				q := req.URL.Query()
				if q.Get("broadcaster_id") != "123" || q.Get("moderator_id") != "789" {
					t.Fatal("incorrect scope")
				}
				body, err := io.ReadAll(req.Body)
				if err != nil {
					t.Fatal(err)
				}
				status := http.StatusNoContent
				switch cap {
				case "message.delete":
					if req.Method != "DELETE" || req.URL.Path != "/helix/moderation/chat" || q.Get("message_id") != a.MessageID || len(q) != 3 || len(body) != 0 {
						t.Fatal("incorrect message delete")
					}
				case "user.unban":
					if req.Method != "DELETE" || req.URL.Path != "/helix/moderation/bans" || q.Get("user_id") != a.UserID || len(q) != 3 || len(body) != 0 {
						t.Fatal("incorrect unban")
					}
				default:
					status = http.StatusOK
					var decoded struct {
						Data map[string]any `json:"data"`
					}
					if req.Method != "POST" || req.URL.Path != "/helix/moderation/bans" || len(q) != 2 || json.Unmarshal(body, &decoded) != nil || decoded.Data["user_id"] != "456" || decoded.Data["reason"] != a.Reason {
						t.Fatal("incorrect ban body")
					}
					_, hasDuration := decoded.Data["duration"]
					if cap == "user.timeout" && decoded.Data["duration"] != float64(300) || cap == "user.ban" && hasDuration {
						t.Fatal("ban/timeout confusion")
					}
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(accepted))}, nil
			})}
			r, err := AttemptUser(t.Context(), client, "client", "PRIVATE_TOKEN", "789", a)
			if err != nil || r.Status != platform.Delivered || r.Validate(a.ActionID) != nil || calls != 1 {
				t.Fatalf("acceptance: %+v %v calls=%d", r, err, calls)
			}
			raw, _ := json.Marshal(r)
			if strings.Contains(string(raw), "PRIVATE") || strings.Contains(string(raw), "456") {
				t.Fatal("receipt leaked effect input")
			}
		})
	}
}

func TestStatusClassificationAndNoRedirect(t *testing.T) {
	for _, tc := range []struct {
		code    int
		status  platform.DeliveryStatus
		failure platform.FailureKind
	}{
		{400, platform.Rejected, platform.Permanent}, {401, platform.Rejected, platform.Unauthorized}, {403, platform.Rejected, platform.Unauthorized},
		{404, platform.Uncertain, ""}, {409, platform.Rejected, platform.Retryable}, {429, platform.Rejected, platform.QuotaLimited},
		{500, platform.Uncertain, ""}, {503, platform.Uncertain, ""}, {302, platform.Uncertain, ""}, {307, platform.Uncertain, ""}, {204, platform.Uncertain, ""},
	} {
		t.Run(http.StatusText(tc.code), func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: tc.code, Header: http.Header{"Location": {"https://PRIVATE.invalid/target"}, "Retry-After": {"99999999999999999999999999999"}}, Body: io.NopCloser(strings.NewReader("PRIVATE response"))}, nil
			})}
			a := action("user.ban")
			r, err := AttemptUser(t.Context(), client, "client", "PRIVATE_TOKEN", "789", a)
			if err == nil || r.Status != tc.status || r.Failure != tc.failure || r.Validate(a.ActionID) != nil || calls != 1 || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatalf("classification: %+v %v calls=%d", r, err, calls)
			}
			if tc.code == 429 && r.RetryAfter != 24*time.Hour {
				t.Fatal("unbounded quota hint")
			}
		})
	}
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }
func (failingReader) Close() error               { return nil }

func TestAmbiguousResponsesAndCancellation(t *testing.T) {
	for _, body := range []string{`{`, `{}`, `{"data":[]}`, `{"data":[{},{}]}`, strings.Replace(accepted, `"123"`, `"999"`, 1), strings.Replace(accepted, `"789"`, `"999"`, 1), strings.Replace(accepted, `"456"`, `"999"`, 1), strings.Repeat("x", responseLimit+1)} {
		client := &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
		})}
		a := action("user.ban")
		r, err := AttemptUser(t.Context(), client, "client", "token", "789", a)
		if err == nil || r.Status != platform.Uncertain || r.Validate(a.ActionID) != nil {
			t.Fatalf("ambiguous success: %+v %v", r, err)
		}
	}
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded, errors.New("PRIVATE transport error")} {
		for _, readFailure := range []bool{false, true} {
			calls := 0
			client := &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
				calls++
				if readFailure {
					return &http.Response{StatusCode: 200, Body: failingReader{cause}}, nil
				}
				return nil, cause
			})}
			a := action("user.ban")
			r, err := AttemptUser(t.Context(), client, "client", "token", "789", a)
			if err == nil || r.Status != platform.Uncertain || r.Validate(a.ActionID) != nil || calls != 1 || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal("unsafe transport failure")
			}
			if cause == context.Canceled || cause == context.DeadlineExceeded {
				if !errors.Is(err, cause) {
					t.Fatal("lost context identity")
				}
			} else if errors.Unwrap(err) != nil {
				t.Fatal("retained private error")
			}
		}
	}
}

func TestInvalidAndCanceledInputsNeverReachTransport(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("must not send") })}
	for _, mutate := range []func(*platform.ModerationAction){func(a *platform.ModerationAction) { a.MessageID = "" }, func(a *platform.ModerationAction) { a.ActionID = "invalid" }, func(a *platform.ModerationAction) { a.ChannelID = "invalid" }, func(a *platform.ModerationAction) { a.Capability = "chat.send" }} {
		a := action("message.delete")
		mutate(&a)
		if _, err := AttemptUser(t.Context(), client, "client", "token", "789", a); err == nil {
			t.Fatal("accepted invalid action")
		}
	}
	a := action("user.ban")
	for _, input := range [][3]string{{"", "token", "789"}, {"client", "bad\r\ntoken", "789"}, {"client", "token", "not-id"}} {
		if _, err := AttemptUser(t.Context(), client, input[0], input[1], input[2], a); err == nil {
			t.Fatal("accepted invalid credential boundary")
		}
	}
	if _, err := AttemptUser(t.Context(), nil, "client", "token", "789", a); err == nil {
		t.Fatal("accepted nil client")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r, err := AttemptUser(ctx, client, "client", "token", "789", a)
	if !errors.Is(err, context.Canceled) || r.Status != platform.Rejected || calls != 0 {
		t.Fatal("canceled/invalid action reached transport")
	}
}

func TestQuotaHints(t *testing.T) {
	now := time.Unix(1000000000, 0)
	for _, tc := range []struct {
		header http.Header
		want   time.Duration
	}{
		{nil, 30 * time.Second}, {http.Header{"Retry-After": {"-1"}}, 30 * time.Second}, {http.Header{"Retry-After": {"60"}}, time.Minute},
		{http.Header{"Retry-After": {now.Add(time.Minute).UTC().Format(http.TimeFormat)}}, time.Minute},
		{http.Header{"Ratelimit-Reset": {"1000000120"}}, 2 * time.Minute},
		{http.Header{"Ratelimit-Reset": {"922337203685477580799"}}, 24 * time.Hour},
	} {
		if got := retryAfter(tc.header, now); got != tc.want {
			t.Fatalf("hint %v want %v", got, tc.want)
		}
	}
}

func TestEarlierDeadlineAndDeleteRejection(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	want, _ := ctx.Deadline()
	client := &http.Client{Transport: transport(func(req *http.Request) (*http.Response, error) {
		got, _ := req.Context().Deadline()
		if got != want {
			t.Fatal("extended initiating deadline")
		}
		<-req.Context().Done()
		return nil, req.Context().Err()
	})}
	a := action("user.ban")
	r, err := AttemptUser(ctx, client, "client", "token", "789", a)
	if r.Status != platform.Uncertain || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("in-flight deadline lost")
	}
	for _, code := range []int{404, 409} {
		client.Transport = transport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader("PRIVATE"))}, nil
		})
		r, err = AttemptUser(t.Context(), client, "client", "token", "789", action("message.delete"))
		if err == nil || code == 404 && (r.Status != platform.Rejected || r.Failure != platform.Permanent) || code == 409 && r.Status != platform.Uncertain {
			t.Fatal("delete classification")
		}
	}
}
