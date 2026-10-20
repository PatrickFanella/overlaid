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
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (fn roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

type fixture struct {
	mu                             sync.Mutex
	validates, lists, pages, posts int
	respond                        func(*http.Request, int) (int, string)
	tokenCalls                     atomic.Int32
}

func inspectorFixture(t *testing.T, respond func(*http.Request, int) (int, string)) (*Inspector, *fixture) {
	t.Helper()
	f := &fixture{respond: respond}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.URL.Path != "/oauth2/token" && (r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer PRIVATE-TOKEN") {
			t.Error("unexpected method or credentials")
		}
		n := 0
		switch r.URL.Path {
		case "/oauth2/token":
			f.posts++
			n = f.posts
			if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Idempotency-Key") != "" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
				t.Error("invalid credential transport")
			}
			if r.ParseForm() != nil || len(r.PostForm) != 3 || r.PostForm.Get("client_id") != "client" || r.PostForm.Get("client_secret") != "PRIVATE+CLIENT/&=SECRET" || r.PostForm.Get("grant_type") != "client_credentials" {
				t.Error("invalid credential form")
			}
		case "/oauth2/validate":
			f.validates++
			n = f.validates
		case "/helix/eventsub/conduits":
			f.lists++
			n = f.lists
		case "/helix/eventsub/conduits/shards":
			f.pages++
			n = f.pages
			if r.URL.Query().Get("conduit_id") != "selected" {
				t.Error("wrong selected conduit")
			}
		default:
			t.Error("unexpected endpoint")
		}
		status, body := defaultResponse(r, n)
		if f.respond != nil {
			status, body = f.respond(r, n)
		}
		if status == http.StatusFound {
			w.Header().Set("Location", "https://redirect.invalid/PRIVATE-PATH")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	target, _ := url.Parse(server.URL)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "id.twitch.tv" && r.URL.Host != "api.twitch.tv" {
			t.Error("unexpected destination")
			return nil, errors.New("fixture destination refused")
		}
		if r.URL.Host == "api.twitch.tv" && r.Header.Get("Client-Id") != "client" {
			t.Error("client identity missing")
		}
		if r.URL.Path == "/oauth2/token" && r.GetBody != nil {
			t.Error("credential POST permits body replay")
		}
		cloned := r.Clone(r.Context())
		u := *r.URL
		u.Scheme = target.Scheme
		u.Host = target.Host
		cloned.URL = &u
		return transport.RoundTrip(cloned)
	})}
	inspector, err := NewInspector(Config{ClientID: "client", ConduitID: "selected", HTTPClient: client, Token: func(ctx context.Context) (string, error) {
		f.tokenCalls.Add(1)
		if _, ok := ctx.Deadline(); !ok {
			t.Error("unbounded token callback")
		}
		return "PRIVATE-TOKEN", nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	return inspector, f
}

func defaultResponse(r *http.Request, n int) (int, string) {
	switch r.URL.Path {
	case "/oauth2/token":
		return 200, `{"access_token":"PRIVATE-TOKEN","expires_in":3600,"token_type":"bearer"}`
	case "/oauth2/validate":
		return 200, `{"client_id":"client","login":null,"user_id":null,"scopes":null,"expires_in":3600}`
	case "/helix/eventsub/conduits":
		return 200, `{"data":[{"id":"other","shard_count":1},{"id":"selected","shard_count":2}]}`
	case "/helix/eventsub/conduits/shards":
		if r.URL.Query().Get("after") == "" {
			return 200, `{"data":[{"id":"1","status":"websocket_disconnected","transport":{"method":"websocket","session_id":"PRIVATE-SESSION"}}],"pagination":{"cursor":"cursor/+=="}}`
		}
		return 200, `{"data":[{"id":"0","status":"enabled","transport":{"method":"webhook","callback":"https://private.invalid/callback?secret=PRIVATE-CALLBACK","secret":"PRIVATE-SECRET"}}],"pagination":{}}`
	}
	return 500, `{"message":"PRIVATE-ERROR"}`
}

func TestInspectSelectedConduitReadOnlySortedAndPrivate(t *testing.T) {
	inspector, f := inspectorFixture(t, nil)
	report, err := inspector.Inspect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if report.ClientID != "client" || report.ConduitID != "selected" || report.ShardCount != 2 || len(report.Shards) != 2 || report.Shards[0].ID != 0 || report.Shards[1].ID != 1 || report.Shards[1].Status != "websocket_disconnected" || report.StartedAt.IsZero() || report.CompletedAt.Before(report.StartedAt) {
		t.Fatal("incomplete metadata", report)
	}
	raw, _ := json.Marshal(report)
	for _, private := range []string{"PRIVATE", "callback", "session_id", "other", "secret", "access_token"} {
		if strings.Contains(string(raw), private) {
			t.Fatal("private data escaped", private)
		}
	}
	f.mu.Lock()
	if f.validates != 2 || f.lists != 2 || f.pages != 2 || f.tokenCalls.Load() != 1 {
		t.Error("read sequence", f.validates, f.lists, f.pages)
	}
	f.mu.Unlock()
	report.Shards[0].Status = "mutated"
	again, err := inspector.Inspect(t.Context())
	if err != nil || again.Shards[0].Status != "enabled" || f.tokenCalls.Load() != 2 {
		t.Fatal("cached or shared report", err)
	}
}

func TestInspectIdentityFailureStopsBeforeInventory(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       error
	}{
		{"user", `{"client_id":"client","user_id":"owner","login":"owner","expires_in":3600}`, ErrUnauthorized},
		{"empty-user", `{"client_id":"client","user_id":"","expires_in":3600}`, ErrUnauthorized},
		{"other-client", `{"client_id":"other","expires_in":3600}`, ErrUnauthorized},
		{"expired", `{"client_id":"client","expires_in":0}`, ErrUnauthorized},
		{"short", `{"client_id":"client","expires_in":29}`, ErrUnauthorized},
		{"wrong-type", `{"client_id":"client","expires_in":"3600"}`, ErrInvalid},
		{"case-alias", `{"CLIENT_ID":"client","expires_in":3600}`, ErrInvalid},
		{"duplicate", `{"client_id":"other","client_id":"client","expires_in":3600}`, ErrInvalid},
		{"depth", strings.Repeat("[", 34) + "0" + strings.Repeat("]", 34), ErrInvalid},
		{"trailing", `{"client_id":"client","expires_in":3600} {}`, ErrInvalid},
		{"oversized", strings.Repeat("x", maxBody+1), ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i, f := inspectorFixture(t, func(r *http.Request, n int) (int, string) {
				if r.URL.Path == "/oauth2/validate" {
					return 200, tc.body
				}
				return defaultResponse(r, n)
			})
			report, err := i.Inspect(t.Context())
			if !errors.Is(err, tc.want) || report.ConduitID != "" {
				t.Fatal(report, err)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.lists != 0 || f.pages != 0 {
				t.Fatal("inventory accessed without app identity")
			}
		})
	}
}

func TestInspectRejectsMalformedIncompleteOrChangedInventory(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*http.Request, int) (int, string)
		want   error
	}{
		{"not-owned", func(r *http.Request, n int) (int, string) {
			if strings.HasSuffix(r.URL.Path, "conduits") {
				return 200, `{"data":[]}`
			}
			return defaultResponse(r, n)
		}, ErrNotFound},
		{"duplicate-conduit", func(r *http.Request, n int) (int, string) {
			if strings.HasSuffix(r.URL.Path, "conduits") {
				return 200, `{"data":[{"id":"selected","shard_count":2},{"id":"selected","shard_count":2}]}`
			}
			return defaultResponse(r, n)
		}, ErrInvalid},
		{"too-many-shards", func(r *http.Request, n int) (int, string) {
			if strings.HasSuffix(r.URL.Path, "conduits") {
				return 200, `{"data":[{"id":"selected","shard_count":20001}]}`
			}
			return defaultResponse(r, n)
		}, ErrInvalid},
		{"missing", func(r *http.Request, n int) (int, string) {
			if strings.HasSuffix(r.URL.Path, "shards") {
				return 200, `{"data":[],"pagination":{}}`
			}
			return defaultResponse(r, n)
		}, ErrChanged},
		{"duplicate-shard", func(r *http.Request, n int) (int, string) {
			if strings.HasSuffix(r.URL.Path, "shards") {
				return 200, `{"data":[{"id":"0","status":"enabled","transport":{"method":"websocket","session_id":"private"}},{"id":"0","status":"enabled","transport":{"method":"websocket","session_id":"private"}}]}`
			}
			return defaultResponse(r, n)
		}, ErrInvalid},
		{"out-of-range", func(r *http.Request, n int) (int, string) {
			if strings.HasSuffix(r.URL.Path, "shards") {
				return 200, `{"data":[{"id":"2","status":"enabled","transport":{"method":"websocket","session_id":"private"}}]}`
			}
			return defaultResponse(r, n)
		}, ErrInvalid},
		{"unknown-status", func(r *http.Request, n int) (int, string) {
			status, body := defaultResponse(r, n)
			return status, strings.ReplaceAll(body, "websocket_disconnected", "future-status")
		}, ErrInvalid},
		{"contradictory-transport", func(r *http.Request, n int) (int, string) {
			status, body := defaultResponse(r, n)
			return status, strings.ReplaceAll(body, "websocket_disconnected", "webhook_callback_verification_failed")
		}, ErrInvalid},
		{"noncanonical-id", func(r *http.Request, n int) (int, string) {
			status, body := defaultResponse(r, n)
			return status, strings.ReplaceAll(body, `"id":"1"`, `"id":"01"`)
		}, ErrInvalid},
		{"repeated-cursor", func(r *http.Request, n int) (int, string) {
			status, body := defaultResponse(r, n)
			if strings.HasSuffix(r.URL.Path, "shards") && n == 2 {
				body = strings.ReplaceAll(body, `"pagination":{}`, `"pagination":{"cursor":"cursor/+=="}`)
			}
			return status, body
		}, ErrInvalid},
		{"changed-count", func(r *http.Request, n int) (int, string) {
			status, body := defaultResponse(r, n)
			if strings.HasSuffix(r.URL.Path, "conduits") && n == 2 {
				body = strings.ReplaceAll(body, `"shard_count":2`, `"shard_count":3`)
			}
			return status, body
		}, ErrChanged},
		{"removed-after-read", func(r *http.Request, n int) (int, string) {
			if strings.HasSuffix(r.URL.Path, "conduits") && n == 2 {
				return 200, `{"data":[]}`
			}
			return defaultResponse(r, n)
		}, ErrChanged},
		{"revoked-at-end", func(r *http.Request, n int) (int, string) {
			if r.URL.Path == "/oauth2/validate" && n == 2 {
				return 401, `{"message":"PRIVATE-ERROR"}`
			}
			return defaultResponse(r, n)
		}, ErrUnauthorized},
		{"identity-changed-at-end", func(r *http.Request, n int) (int, string) {
			status, body := defaultResponse(r, n)
			if r.URL.Path == "/oauth2/validate" && n == 2 {
				body = strings.ReplaceAll(body, `"client_id":"client"`, `"client_id":"other"`)
			}
			return status, body
		}, ErrUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i, _ := inspectorFixture(t, tc.change)
			report, err := i.Inspect(t.Context())
			if !errors.Is(err, tc.want) || report.ConduitID != "" || report.Shards != nil {
				t.Fatal("partial or accepted", report, err)
			}
		})
	}
}

func TestInspectBoundedPaginationAndTotalBytes(t *testing.T) {
	for _, large := range []bool{false, true} {
		t.Run(fmt.Sprint(large), func(t *testing.T) {
			count := maxPages + 1
			if large {
				count = 10
			}
			i, f := inspectorFixture(t, func(r *http.Request, n int) (int, string) {
				if strings.HasSuffix(r.URL.Path, "conduits") {
					return 200, fmt.Sprintf(`{"data":[{"id":"selected","shard_count":%d}]}`, count)
				}
				if strings.HasSuffix(r.URL.Path, "shards") {
					cursor := fmt.Sprint(n)
					if n == count {
						cursor = ""
					}
					padding := ""
					if large {
						padding = strings.Repeat("x", 900000)
					}
					return 200, fmt.Sprintf(`{"data":[{"id":"%d","status":"enabled","transport":{"method":"websocket","session_id":"private"}}],"pagination":{"cursor":%q},"private_padding":%q}`, n-1, cursor, padding)
				}
				return defaultResponse(r, n)
			})
			report, err := i.Inspect(t.Context())
			if !errors.Is(err, ErrInvalid) || report.Shards != nil {
				t.Fatal(report, err)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.pages > maxPages {
				t.Fatal("unbounded page reads", f.pages)
			}
		})
	}
}

func TestInspectRedactsFailuresCancellationAndRedirects(t *testing.T) {
	for _, status := range []int{401, 403, 429, 500, 302} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			i, f := inspectorFixture(t, func(*http.Request, int) (int, string) { return status, `{"message":"PRIVATE-ERROR"}` })
			report, err := i.Inspect(t.Context())
			want := ErrUnavailable
			if status == 401 || status == 403 {
				want = ErrUnauthorized
			}
			if status == 429 {
				want = ErrRateLimited
			}
			if !errors.Is(err, want) || strings.Contains(err.Error(), "PRIVATE") || report.Shards != nil {
				t.Fatal(report, err)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.validates != 1 || f.lists != 0 {
				t.Fatal("retried or redirected")
			}
		})
	}
	i, f := inspectorFixture(t, nil)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := i.Inspect(ctx); !errors.Is(err, context.Canceled) || f.tokenCalls.Load() != 0 {
		t.Fatal("canceled caller acquired credentials", err)
	}
	i.token = func(ctx context.Context) (string, error) { <-ctx.Done(); return "PRIVATE-TOKEN", nil }
	ctx, stop := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer stop()
	if _, err := i.Inspect(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	i.token = func(context.Context) (string, error) { return "", errors.New("PRIVATE-CREDENTIAL") }
	if _, err := i.Inspect(t.Context()); !errors.Is(err, ErrUnauthorized) {
		t.Fatal(err)
	}
	i.token = func(context.Context) (string, error) { panic("PRIVATE-PANIC") }
	if _, err := i.Inspect(t.Context()); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
}

func TestInspectorConfigurationAndPrivateTransportFailure(t *testing.T) {
	for _, cfg := range []Config{{}, {ClientID: "client", ConduitID: "../other", Token: func(context.Context) (string, error) { return "token", nil }}} {
		if _, err := NewInspector(cfg); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
	}
	var absent *Inspector
	if _, err := absent.Inspect(t.Context()); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	i, err := NewInspector(Config{ClientID: "client", ConduitID: "selected", Token: func(context.Context) (string, error) { return "token", nil }})
	if err != nil {
		t.Fatal(err)
	}
	if i.http.Timeout != inspectionTimeout {
		t.Fatal("unbounded default client")
	}
	i.http.Transport = roundTrip(func(*http.Request) (*http.Response, error) { return nil, errors.New("PRIVATE-TRANSPORT") })
	if _, err := i.Inspect(t.Context()); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
}

func TestInspectCancelsActualHTTPAndContainsPanics(t *testing.T) {
	entered := make(chan struct{})
	i, _ := inspectorFixture(t, func(r *http.Request, n int) (int, string) {
		close(entered)
		<-r.Context().Done()
		return 500, `{"message":"PRIVATE-ERROR"}`
	})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := i.Inspect(ctx); done <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("request did not cancel")
	}
	i.token = func(context.Context) (string, error) { return "PRIVATE-TOKEN", nil }
	i.http.Transport = roundTrip(func(*http.Request) (*http.Response, error) { panic("PRIVATE-TRANSPORT") })
	if _, err := i.Inspect(t.Context()); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	ctx, cancel = context.WithCancel(t.Context())
	i.token = func(context.Context) (string, error) { cancel(); panic("PRIVATE-PANIC") }
	if _, err := i.Inspect(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("panic lost cancellation", err)
	}
}

func TestInspectConcurrentReadsUseIndependentCredentialsAndReports(t *testing.T) {
	i, f := inspectorFixture(t, nil)
	done := make(chan error, 8)
	for range 8 {
		go func() {
			report, err := i.Inspect(t.Context())
			if err == nil && (report.ConduitID != "selected" || len(report.Shards) != 2) {
				err = ErrInvalid
			}
			done <- err
		}()
	}
	for range 8 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if f.tokenCalls.Load() != 8 {
		t.Fatal("credential cache or shared invocation")
	}
}

func TestInspectRejectsMalformedTokensBeforeProviderAccess(t *testing.T) {
	for _, token := range []string{"", "bad token", "bad\nheader", strings.Repeat("x", 4097)} {
		i, f := inspectorFixture(t, nil)
		i.token = func(context.Context) (string, error) { return token, nil }
		if _, err := i.Inspect(t.Context()); !errors.Is(err, ErrUnauthorized) {
			t.Fatal(err)
		}
		f.mu.Lock()
		if f.validates != 0 || f.lists != 0 || f.pages != 0 {
			t.Error("invalid token reached provider")
		}
		f.mu.Unlock()
	}
}
