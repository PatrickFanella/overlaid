package twitchconduit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const assignURL = "https://api.twitch.tv/helix/eventsub/conduits/shards"
const assignmentOK = `{"data":[{"id":"0","status":"enabled","transport":{"method":"websocket","session_id":"PRIVATE-SESSION","connected_at":"2026-10-20T00:00:00Z","unknown":"PRIVATE-EXTRA"}}],"errors":[]}`

func assignmentBinding() ShardBinding {
	return ShardBinding{ConduitID: "conduit", ShardCount: 1, ShardID: "0", SessionID: "PRIVATE-SESSION"}
}

type assignmentFixture struct {
	tokens, patches, validations atomic.Int32
	respond                      func(string, *http.Request) (*http.Response, error)
}

func newAssignmentFixture(t *testing.T) (*Assigner, *assignmentFixture) {
	t.Helper()
	f := &assignmentFixture{}
	c, err := NewAssigner(AssignmentConfig{ClientID: "client", Token: func(ctx context.Context) (string, error) {
		f.tokens.Add(1)
		if d, ok := ctx.Deadline(); !ok || time.Until(d) > inspectionTimeout {
			t.Error("unbounded context")
		}
		return "PRIVATE-TOKEN", nil
	}, HTTPClient: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		stage := "before"
		if r.Header.Get("Authorization") != "Bearer PRIVATE-TOKEN" || r.URL.RawQuery != "" || r.Header.Get("Cookie") != "" {
			t.Error("credential boundary")
		}
		switch r.Method + " " + r.URL.String() {
		case "GET " + validateURL:
			if f.validations.Add(1) > 1 {
				stage = "after"
			}
		case "PATCH " + assignURL:
			stage = "assign"
			f.patches.Add(1)
			raw, err := io.ReadAll(r.Body)
			if err != nil || string(raw) != `{"conduit_id":"conduit","shards":[{"id":"0","transport":{"method":"websocket","session_id":"PRIVATE-SESSION"}}]}` || r.GetBody != nil || r.Header.Get("Client-Id") != "client" || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Accept") != "application/json" || r.Header.Get("Idempotency-Key") != "" {
				t.Error("unexpected mutation")
			}
			// The fixture inspected this stream; restore it for real HTTP forwarding.
			r.Body = io.NopCloser(strings.NewReader(string(raw)))
		default:
			t.Error("unexpected provider operation")
		}
		if f.respond != nil {
			if response, err := f.respond(stage, r); response != nil || err != nil {
				return response, err
			}
		}
		if stage == "assign" {
			return creationResponse(202, assignmentOK), nil
		}
		return creationResponse(200, `{"client_id":"client","expires_in":3600,"scopes":[]}`), nil
	})}})
	if err != nil {
		t.Fatal(err)
	}
	return c, f
}
func assertAssignmentFailure(t *testing.T, r Assignment, err error, outcome AssignmentOutcome, want error) {
	t.Helper()
	if r.Outcome != outcome || r.Shard != nil || !errors.Is(err, want) {
		t.Fatalf("outcome=%s metadata=%t error=%v", r.Outcome, r.Shard != nil, err)
	}
	if outcome == AssignmentUncertain && !errors.Is(err, ErrAssignmentUncertain) {
		t.Fatal("missing uncertainty")
	}
	if strings.Contains(fmt.Sprintf("%+v %#v", r, err), "PRIVATE") {
		t.Fatal("private detail escaped")
	}
}
func TestAssignmentExactAcknowledgement(t *testing.T) {
	c, f := newAssignmentFixture(t)
	start := time.Now()
	r, err := c.Assign(context.Background(), assignmentBinding())
	if err != nil || r.Outcome != AssignmentAssigned || r.Shard == nil {
		t.Fatalf("assignment: %+v %v", r, err)
	}
	m := r.Shard
	if m.ClientID != "client" || m.ConduitID != "conduit" || m.ShardID != "0" || m.StartedAt.Before(start) || m.CompletedAt.Before(m.StartedAt) || m.CompletedAt.After(time.Now()) || f.tokens.Load() != 1 || f.patches.Load() != 1 || f.validations.Load() != 2 {
		t.Fatal("metadata/operation mismatch")
	}
	raw, _ := json.Marshal(r)
	if strings.Contains(string(raw), "PRIVATE") {
		t.Fatal("private session/response escaped")
	}
}
func TestAssignmentBindingAndSourceRefusal(t *testing.T) {
	if _, err := NewAssigner(AssignmentConfig{ClientID: "bad/client", Token: func(context.Context) (string, error) { return "", nil }}); !errors.Is(err, ErrInvalid) {
		t.Fatal("client accepted")
	}
	if _, err := NewAssigner(AssignmentConfig{ClientID: "client"}); !errors.Is(err, ErrInvalid) {
		t.Fatal("missing callback")
	}
	for _, binding := range []ShardBinding{
		{}, {ConduitID: "bad/url", ShardCount: 1, ShardID: "0", SessionID: "session"},
		{ConduitID: "c", ShardCount: 0, ShardID: "0", SessionID: "s"}, {ConduitID: "c", ShardCount: 20001, ShardID: "0", SessionID: "s"},
		{ConduitID: "c", ShardCount: 1, ShardID: "1", SessionID: "s"}, {ConduitID: "c", ShardCount: 1, ShardID: "00", SessionID: "s"}, {ConduitID: "c", ShardCount: 1, ShardID: "-1", SessionID: "s"},
		{ConduitID: "c", ShardCount: 1, ShardID: "+0", SessionID: "s"}, {ConduitID: "c", ShardCount: 1, ShardID: "0", SessionID: ""}, {ConduitID: "c", ShardCount: 1, ShardID: "0", SessionID: strings.Repeat("s", 513)},
		{ConduitID: "c", ShardCount: 1, ShardID: "0", SessionID: "line\nbreak"},
	} {
		c, f := newAssignmentFixture(t)
		r, err := c.Assign(context.Background(), binding)
		assertAssignmentFailure(t, r, err, AssignmentNotStarted, ErrInvalid)
		if f.tokens.Load() != 0 || f.patches.Load() != 0 {
			t.Fatal("invalid binding accessed provider")
		}
	}
	if !validShardBinding(ShardBinding{ConduitID: "c", ShardCount: 20000, ShardID: "19999", SessionID: `opaque+/=\"`}) {
		t.Fatal("opaque session or boundary refused")
	}
	c, f := newAssignmentFixture(t)
	r, err := c.Assign(nil, assignmentBinding()) //nolint:staticcheck // Explicit nil-context refusal.
	assertAssignmentFailure(t, r, err, AssignmentNotStarted, ErrInvalid)
	var missing *Assigner
	r, err = missing.Assign(context.Background(), assignmentBinding())
	assertAssignmentFailure(t, r, err, AssignmentNotStarted, ErrInvalid)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, err = c.Assign(ctx, assignmentBinding())
	assertAssignmentFailure(t, r, err, AssignmentNotStarted, context.Canceled)
	if f.tokens.Load() != 0 {
		t.Fatal("canceled caller accessed token")
	}
	for _, kind := range []string{"error", "panic", "invalid", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			c, f := newAssignmentFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c.token = func(context.Context) (string, error) {
				switch kind {
				case "error":
					return "", errors.New("PRIVATE-SOURCE")
				case "panic":
					panic("PRIVATE-SOURCE")
				case "cancel":
					cancel()
					return "token", nil
				default:
					return "bad\ntoken", nil
				}
			}
			r, err := c.Assign(ctx, assignmentBinding())
			want := ErrUnauthorized
			if kind == "panic" {
				want = ErrUnavailable
			}
			if kind == "cancel" {
				want = context.Canceled
			}
			assertAssignmentFailure(t, r, err, AssignmentNotStarted, want)
			if f.patches.Load() != 0 || f.validations.Load() != 0 {
				t.Fatal("invalid token reached provider")
			}
		})
	}
}
func TestAssignmentMalformedAcknowledgements(t *testing.T) {
	cases := []string{`null`, `{}`, `{"data":null,"errors":[]}`, `{"data":[],"errors":[]}`, `{"data":[null],"errors":[]}`, `{"data":[],"Data":[],"errors":[]}`, strings.Replace(assignmentOK, `"errors":[]`, `"Errors":[]`, 1), strings.Replace(assignmentOK, `,"errors":[]`, "", 1), strings.Replace(assignmentOK, `"errors":[]`, `"errors":null`, 1), strings.Replace(assignmentOK, `"errors":[]`, `"errors":[{"id":"0","message":"PRIVATE-PROVIDER"}]`, 1), strings.Replace(assignmentOK, `"id":"0"`, `"id":"1"`, 1), strings.Replace(assignmentOK, `"id":"0"`, `"id":0`, 1), strings.Replace(assignmentOK, `"status":"enabled"`, `"status":"websocket_disconnected"`, 1), strings.Replace(assignmentOK, `"method":"websocket"`, `"method":"webhook"`, 1), strings.Replace(assignmentOK, `"session_id":"PRIVATE-SESSION"`, `"session_id":"wrong"`, 1), strings.Replace(assignmentOK, `"session_id":"PRIVATE-SESSION"`, `"session_id":null`, 1), strings.Replace(assignmentOK, `"connected_at":`, `"callback":"PRIVATE-CALLBACK","connected_at":`, 1), strings.Replace(assignmentOK, `"connected_at":`, `"disconnected_at":null,"connected_at":`, 1), strings.Replace(assignmentOK, `"id":"0"`, `"id":"0","ID":"0"`, 1), assignmentOK + `{}`, strings.Repeat(" ", 65537) + assignmentOK, `{"extra":` + strings.Repeat("[", 40) + `0` + strings.Repeat("]", 40) + `,"data":[],"errors":[]}`}
	for index, body := range cases {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			c, f := newAssignmentFixture(t)
			f.respond = func(stage string, _ *http.Request) (*http.Response, error) {
				if stage == "assign" {
					return creationResponse(202, body), nil
				}
				return nil, nil
			}
			r, err := c.Assign(context.Background(), assignmentBinding())
			assertAssignmentFailure(t, r, err, AssignmentUncertain, ErrInvalid)
			if f.patches.Load() != 1 || f.validations.Load() != 1 {
				t.Fatal("malformed acknowledgement retried/validated")
			}
		})
	}
}
func TestAssignmentStatusAndPrivateFailures(t *testing.T) {
	for _, code := range []int{200, 204, 400, 401, 403, 404, 429, 500, 302} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			c, f := newAssignmentFixture(t)
			f.respond = func(stage string, _ *http.Request) (*http.Response, error) {
				if stage == "assign" {
					return creationResponse(code, "PRIVATE-PROVIDER"), nil
				}
				return nil, nil
			}
			r, err := c.Assign(context.Background(), assignmentBinding())
			outcome, want := AssignmentUncertain, ErrUnavailable
			switch code {
			case 400, 404:
				outcome, want = AssignmentRefused, ErrInvalid
			case 401:
				outcome, want = AssignmentRefused, ErrUnauthorized
			}
			assertAssignmentFailure(t, r, err, outcome, want)
			if f.patches.Load() != 1 {
				t.Fatal("mutation retried")
			}
		})
	}
	for _, stage := range []string{"before", "assign", "after"} {
		for _, kind := range []string{"transport", "panic", "read", "close"} {
			t.Run(stage+"/"+kind, func(t *testing.T) {
				c, f := newAssignmentFixture(t)
				f.respond = func(at string, _ *http.Request) (*http.Response, error) {
					if at != stage {
						return nil, nil
					}
					switch kind {
					case "transport":
						return nil, errors.New("PRIVATE-TRANSPORT")
					case "panic":
						panic("PRIVATE-PANIC")
					case "read":
						return &http.Response{StatusCode: map[bool]int{true: 202, false: 200}[stage == "assign"], Header: make(http.Header), Body: &assignmentBadBody{}}, nil
					default:
						body := `{"client_id":"client","expires_in":3600}`
						status := 200
						if stage == "assign" {
							body = assignmentOK
							status = 202
						}
						return &http.Response{StatusCode: status, Header: make(http.Header), Body: &assignmentBadBody{Reader: strings.NewReader(body)}}, nil
					}
				}
				r, err := c.Assign(context.Background(), assignmentBinding())
				outcome := AssignmentNotStarted
				if stage != "before" {
					outcome = AssignmentUncertain
				}
				assertAssignmentFailure(t, r, err, outcome, ErrUnavailable)
				if f.patches.Load() > 1 {
					t.Fatal("mutation retried")
				}
			})
		}
	}
}

type assignmentBadBody struct{ io.Reader }

func (b *assignmentBadBody) Read(p []byte) (int, error) {
	if b.Reader == nil {
		return 0, errors.New("PRIVATE-READ")
	}
	return b.Reader.Read(p)
}
func (b *assignmentBadBody) Close() error { panic("PRIVATE-CLOSE") }
func TestAssignmentIdentityAndOriginalLifetime(t *testing.T) {
	for _, stage := range []string{"before", "after"} {
		for _, body := range []string{`{"client_id":"other","expires_in":3600}`, `{"client_id":"client","expires_in":30}`, `{"client_id":"client","expires_in":3600,"user_id":"user"}`} {
			t.Run(stage+body, func(t *testing.T) {
				c, f := newAssignmentFixture(t)
				f.respond = func(at string, _ *http.Request) (*http.Response, error) {
					if at == stage {
						return creationResponse(200, body), nil
					}
					return nil, nil
				}
				r, err := c.Assign(context.Background(), assignmentBinding())
				outcome := AssignmentNotStarted
				if stage == "after" {
					outcome = AssignmentUncertain
				}
				assertAssignmentFailure(t, r, err, outcome, ErrUnauthorized)
			})
		}
	}
	c, f := newAssignmentFixture(t)
	f.respond = func(stage string, r *http.Request) (*http.Response, error) {
		if stage == "before" {
			return creationResponse(200, `{"client_id":"client","expires_in":31}`), nil
		}
		if stage == "assign" {
			select {
			case <-time.After(1100 * time.Millisecond):
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
		}
		return nil, nil
	}
	r, err := c.Assign(context.Background(), assignmentBinding())
	assertAssignmentFailure(t, r, err, AssignmentUncertain, ErrUnauthorized)
}
func TestAssignmentActualCancellationAndRedirect(t *testing.T) {
	for _, redirect := range []bool{false, true} {
		t.Run(fmt.Sprint(redirect), func(t *testing.T) {
			var targetCalls atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { targetCalls.Add(1); w.WriteHeader(202) }))
			defer target.Close()
			entered := make(chan struct{})
			stopped := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if redirect {
					w.Header().Set("Location", target.URL)
					w.WriteHeader(307)
					return
				}
				close(entered)
				_, _ = io.ReadAll(r.Body)
				<-r.Context().Done()
				close(stopped)
			}))
			defer server.Close()
			destination, _ := url.Parse(server.URL)
			c, f := newAssignmentFixture(t)
			f.respond = func(stage string, r *http.Request) (*http.Response, error) {
				if stage != "assign" {
					return nil, nil
				}
				clone := r.Clone(r.Context())
				u := *r.URL
				clone.URL = &u
				clone.URL.Scheme = destination.Scheme
				clone.URL.Host = destination.Host
				return http.DefaultTransport.RoundTrip(clone)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if redirect {
				r, err := c.Assign(ctx, assignmentBinding())
				assertAssignmentFailure(t, r, err, AssignmentUncertain, ErrUnavailable)
			} else {
				done := make(chan struct{})
				go func() {
					defer close(done)
					r, err := c.Assign(ctx, assignmentBinding())
					assertAssignmentFailure(t, r, err, AssignmentUncertain, context.Canceled)
				}()
				select {
				case <-entered:
					cancel()
				case <-time.After(3 * time.Second):
					t.Fatal("no request")
				}
				select {
				case <-done:
				case <-time.After(3 * time.Second):
					t.Fatal("cancellation hung")
				}
				select {
				case <-stopped:
				case <-time.After(time.Second):
					t.Fatal("server not canceled")
				}
			}
			if targetCalls.Load() != 0 || f.patches.Load() != 1 {
				t.Fatal("redirect/replay")
			}
		})
	}
}
