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

type creationFixture struct {
	tokens, posts, validations atomic.Int32
	respond                    func(string, *http.Request) (*http.Response, error)
}

func creationResponse(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func newCreationFixture(t *testing.T, shards int) (*Creator, *creationFixture) {
	t.Helper()
	f := &creationFixture{}
	c, err := NewCreator(CreationConfig{ClientID: "client", Token: func(ctx context.Context) (string, error) {
		f.tokens.Add(1)
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > inspectionTimeout {
			t.Error("missing bounded context")
		}
		return "PRIVATE-TOKEN", nil
	}, HTTPClient: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		stage := "before"
		if r.Header.Get("Authorization") != "Bearer PRIVATE-TOKEN" || r.URL.RawQuery != "" || r.Header.Get("Cookie") != "" {
			t.Error("unexpected authorization/query/cookie")
		}
		switch r.Method + " " + r.URL.String() {
		case "GET " + validateURL:
			if f.validations.Add(1) > 1 {
				stage = "after"
			}
		case "POST " + conduitURL:
			stage = "create"
			f.posts.Add(1)
			body, readErr := io.ReadAll(r.Body)
			if readErr != nil || string(body) != fmt.Sprintf(`{"shard_count":%d}`, shards) || r.GetBody != nil || r.Header.Get("Client-Id") != "client" || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Accept") != "application/json" || r.Header.Get("Idempotency-Key") != "" {
				t.Error("unexpected creation request")
			}
		default:
			t.Error("unexpected provider operation")
		}
		if f.respond != nil {
			if response, responseErr := f.respond(stage, r); response != nil || responseErr != nil {
				return response, responseErr
			}
		}
		if stage == "create" {
			return creationResponse(200, fmt.Sprintf(`{"data":[{"id":"created","shard_count":%d,"private":"PRIVATE-FIELD"}],"private":"PRIVATE-FIELD"}`, shards)), nil
		}
		return creationResponse(200, `{"client_id":"client","expires_in":3600,"user_id":null,"login":null,"scopes":[]}`), nil
	})}})
	if err != nil {
		t.Fatal(err)
	}
	return c, f
}

func assertCreationFailure(t *testing.T, result Creation, err error, outcome CreationOutcome, want error) {
	t.Helper()
	if result.Outcome != outcome || result.Conduit != nil || !errors.Is(err, want) {
		t.Fatalf("outcome=%s metadata=%t error=%v; want %s / %v", result.Outcome, result.Conduit != nil, err, outcome, want)
	}
	if outcome == CreationUncertain && !errors.Is(err, ErrCreationUncertain) {
		t.Fatal("missing uncertainty identity")
	}
	if strings.Contains(fmt.Sprintf("%+v %#v", result, err), "PRIVATE") {
		t.Fatal("private value escaped")
	}
}

func TestCreationExactProjection(t *testing.T) {
	for _, shards := range []int{1, maxShards} {
		t.Run(fmt.Sprint(shards), func(t *testing.T) {
			c, f := newCreationFixture(t, shards)
			start := time.Now()
			result, err := c.Create(context.Background(), shards)
			if err != nil || result.Outcome != CreationCreated || result.Conduit == nil {
				t.Fatalf("creation: %v %v", result, err)
			}
			m := result.Conduit
			if m.ClientID != "client" || m.ID != "created" || m.ShardCount != shards || m.StartedAt.Before(start) || m.CompletedAt.Before(m.StartedAt) || m.CompletedAt.After(time.Now()) || f.posts.Load() != 1 || f.tokens.Load() != 1 || f.validations.Load() != 2 {
				t.Fatal("incorrect metadata or operation counts")
			}
			raw, err := json.Marshal(result)
			if err != nil || strings.Contains(string(raw), "PRIVATE") {
				t.Fatal("projection contains private data")
			}
		})
	}
}

func TestCreationInputAndTokenRefusal(t *testing.T) {
	if _, err := NewCreator(CreationConfig{ClientID: "bad/client", Token: func(context.Context) (string, error) { return "", nil }}); !errors.Is(err, ErrInvalid) {
		t.Fatal("invalid client accepted")
	}
	if _, err := NewCreator(CreationConfig{ClientID: "client"}); !errors.Is(err, ErrInvalid) {
		t.Fatal("missing token source accepted")
	}
	for _, shards := range []int{-1, 0, maxShards + 1} {
		c, f := newCreationFixture(t, shards)
		r, err := c.Create(context.Background(), shards)
		assertCreationFailure(t, r, err, CreationNotStarted, ErrInvalid)
		if f.tokens.Load() != 0 || f.posts.Load() != 0 || f.validations.Load() != 0 {
			t.Fatal("invalid input accessed provider")
		}
	}
	c, f := newCreationFixture(t, 1)
	r, err := c.Create(nil, 1) //nolint:staticcheck // Exercise explicit nil-context refusal.
	assertCreationFailure(t, r, err, CreationNotStarted, ErrInvalid)
	var missing *Creator
	r, err = missing.Create(context.Background(), 1)
	assertCreationFailure(t, r, err, CreationNotStarted, ErrInvalid)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, err = c.Create(ctx, 1)
	assertCreationFailure(t, r, err, CreationNotStarted, context.Canceled)
	if f.tokens.Load() != 0 {
		t.Fatal("canceled input read token")
	}
	for _, token := range []string{"", "PRIVATE\nTOKEN", strings.Repeat("x", 4097)} {
		c.token = func(context.Context) (string, error) { return token, nil }
		r, err = c.Create(context.Background(), 1)
		assertCreationFailure(t, r, err, CreationNotStarted, ErrUnauthorized)
	}
	c.token = func(context.Context) (string, error) { return "", errors.New("PRIVATE-source") }
	r, err = c.Create(context.Background(), 1)
	assertCreationFailure(t, r, err, CreationNotStarted, ErrUnauthorized)
	c.token = func(context.Context) (string, error) { panic("PRIVATE-panic") }
	r, err = c.Create(context.Background(), 1)
	assertCreationFailure(t, r, err, CreationNotStarted, ErrUnavailable)
	if errors.Is(err, context.Canceled) || f.posts.Load() != 0 || f.validations.Load() != 0 {
		t.Fatal("panic misclassified or provider accessed")
	}
}

func TestCreationIdentityBeforeAndAfter(t *testing.T) {
	bodies := []string{
		`{"client_id":"other","expires_in":3600}`,
		`{"client_id":"client","expires_in":3600,"user_id":""}`,
		`{"client_id":"client","expires_in":3600,"login":"PRIVATE-user"}`,
		`{"client_id":"client","expires_in":29}`,
		`{"client_id":"client","expires_in":30}`,
		`{"client_id":"client","expires_in":3600,"Client_ID":"client"}`,
		`{"client_id":"client","expires_in":3600,"private":"PRIVATE"}`,
		`null`, `{"client_id":`,
	}
	for _, stage := range []string{"before", "after"} {
		for index, body := range bodies {
			t.Run(fmt.Sprintf("%s-%d", stage, index), func(t *testing.T) {
				c, f := newCreationFixture(t, 1)
				f.respond = func(current string, _ *http.Request) (*http.Response, error) {
					if current == stage {
						return creationResponse(200, body), nil
					}
					return nil, nil
				}
				r, err := c.Create(context.Background(), 1)
				want := CreationNotStarted
				if stage == "after" {
					want = CreationUncertain
				}
				if err == nil || r.Outcome != want || r.Conduit != nil || (stage == "before" && f.posts.Load() != 0) || (stage == "after" && (f.posts.Load() != 1 || !errors.Is(err, ErrCreationUncertain))) {
					t.Fatalf("identity refusal: %v %v", r, err)
				}
			})
		}
	}
}

func TestCreationResponseRefusal(t *testing.T) {
	bodies := []string{
		`null`, `[]`, `{}`, `{"data":null}`, `{"data":[]}`,
		`{"data":[{},{}]}`, `{"data":[null]}`,
		`{"Data":[{"id":"created","shard_count":1}]}`,
		`{"data":[{"ID":"created","shard_count":1}]}`,
		`{"data":[{"id":"created","ID":"created","shard_count":1}]}`,
		`{"data":[{"id":null,"shard_count":1}]}`,
		`{"data":[{"id":42,"shard_count":1}]}`,
		`{"data":[{"id":"https://PRIVATE","shard_count":1}]}`,
		`{"data":[{"id":" created","shard_count":1}]}`,
		`{"data":[{"id":"` + strings.Repeat("a", 129) + `","shard_count":1}]}`,
		`{"data":[{"id":"created"}]}`,
		`{"data":[{"id":"created","shard_count":null}]}`,
		`{"data":[{"id":"created","shard_count":"1"}]}`,
		`{"data":[{"id":"created","shard_count":1.0}]}`,
		`{"data":[{"id":"created","shard_count":2}]}`,
		`{"data":[{"id":"created","shard_count":999999999999999999999}]}`,
		`{"data":[{"id":"created","shard_count":1}]} {}`,
		`{"data":[{"id":"created","shard_count":1}],"private":` + strings.Repeat("[", 33) + `0` + strings.Repeat("]", 33) + `}`,
		strings.Repeat(" ", 65537),
	}
	for index, body := range bodies {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			c, f := newCreationFixture(t, 1)
			f.respond = func(stage string, _ *http.Request) (*http.Response, error) {
				if stage == "create" {
					return creationResponse(200, body), nil
				}
				return nil, nil
			}
			r, err := c.Create(context.Background(), 1)
			assertCreationFailure(t, r, err, CreationUncertain, ErrInvalid)
			if f.posts.Load() != 1 || f.validations.Load() != 1 {
				t.Fatal("invalid response retried or validated")
			}
		})
	}
}

func TestCreationStatusClassification(t *testing.T) {
	for _, status := range []int{201, 302, 400, 401, 403, 404, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			c, f := newCreationFixture(t, 1)
			f.respond = func(stage string, _ *http.Request) (*http.Response, error) {
				if stage == "create" {
					response := creationResponse(status, "PRIVATE-provider-error")
					response.Header.Set("Retry-After", "1")
					return response, nil
				}
				return nil, nil
			}
			outcome, want := CreationUncertain, ErrUnavailable
			switch status {
			case 400:
				outcome, want = CreationRefused, ErrInvalid
			case 401:
				outcome, want = CreationRefused, ErrUnauthorized
			case 429:
				outcome, want = CreationRefused, ErrCreationLimited
			}
			r, err := c.Create(context.Background(), 1)
			assertCreationFailure(t, r, err, outcome, want)
			if f.posts.Load() != 1 || f.validations.Load() != 1 {
				t.Fatal("failure retried")
			}
		})
	}
}

type creationBrokenBody struct{ closePanic bool }

func (b creationBrokenBody) Read([]byte) (int, error) { return 0, errors.New("PRIVATE-read") }
func (b creationBrokenBody) Close() error {
	if b.closePanic {
		panic("PRIVATE-close")
	}
	return nil
}

type creationClosePanic struct{ io.Reader }

func (creationClosePanic) Close() error { panic("PRIVATE-close") }

func TestCreationPrivateFailuresAndPanic(t *testing.T) {
	for _, failure := range []string{"transport", "panic", "read", "close", "after"} {
		t.Run(failure, func(t *testing.T) {
			c, f := newCreationFixture(t, 1)
			f.respond = func(stage string, _ *http.Request) (*http.Response, error) {
				if stage == "after" && failure == "after" {
					return nil, errors.New("PRIVATE-validation")
				}
				if stage != "create" {
					return nil, nil
				}
				switch failure {
				case "transport":
					return nil, errors.New("PRIVATE-transport")
				case "panic":
					panic("PRIVATE-transport")
				case "read":
					return &http.Response{StatusCode: 200, Body: creationBrokenBody{}}, nil
				case "close":
					return &http.Response{StatusCode: 200, Body: creationClosePanic{strings.NewReader(`{"data":[{"id":"created","shard_count":1}]}`)}}, nil
				}
				return nil, nil
			}
			r, err := c.Create(context.Background(), 1)
			assertCreationFailure(t, r, err, CreationUncertain, ErrUnavailable)
			if errors.Is(err, context.Canceled) || f.posts.Load() != 1 {
				t.Fatal("panic cleanup became cancellation or retried")
			}
		})
	}
}

func TestCreationDoesNotExtendOriginalLifetime(t *testing.T) {
	c, f := newCreationFixture(t, 1)
	f.respond = func(stage string, r *http.Request) (*http.Response, error) {
		if stage == "before" {
			return creationResponse(200, `{"client_id":"client","expires_in":31}`), nil
		}
		if stage == "create" {
			select {
			case <-time.After(1100 * time.Millisecond):
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
		}
		return nil, nil
	}
	r, err := c.Create(context.Background(), 1)
	assertCreationFailure(t, r, err, CreationUncertain, ErrUnauthorized)
	if f.validations.Load() != 2 || f.posts.Load() != 1 {
		t.Fatal("original lifetime fixture did not reach final validation")
	}
}

func TestCreationActualCancellationAndRedirect(t *testing.T) {
	for _, redirect := range []bool{false, true} {
		t.Run(fmt.Sprint(redirect), func(t *testing.T) {
			var posts, redirected atomic.Int32
			entered := make(chan struct{})
			observedCancel := make(chan struct{})
			target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected.Add(1) }))
			defer target.Close()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/oauth2/validate" {
					_, _ = io.WriteString(w, `{"client_id":"client","expires_in":3600}`)
					return
				}
				posts.Add(1)
				_, _ = io.Copy(io.Discard, r.Body)
				close(entered)
				if redirect {
					http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
					return
				}
				<-r.Context().Done()
				close(observedCancel)
			}))
			defer server.Close()
			local, err := url.Parse(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			transport := http.DefaultTransport.(*http.Transport).Clone()
			defer transport.CloseIdleConnections()
			c, err := NewCreator(CreationConfig{ClientID: "client", Token: func(context.Context) (string, error) { return "PRIVATE-TOKEN", nil }, HTTPClient: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return nil }, Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				copyRequest := r.Clone(r.Context())
				copyRequest.URL.Scheme, copyRequest.URL.Host = local.Scheme, local.Host
				return transport.RoundTrip(copyRequest)
			})}})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			type answer struct {
				result Creation
				err    error
			}
			done := make(chan answer, 1)
			go func() { r, callErr := c.Create(ctx, 1); done <- answer{r, callErr} }()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("creation request not observed")
			}
			want := ErrUnavailable
			if !redirect {
				cancel()
				want = context.Canceled
			}
			select {
			case a := <-done:
				assertCreationFailure(t, a.result, a.err, CreationUncertain, want)
			case <-time.After(4 * time.Second):
				t.Fatal("creation did not settle")
			}
			if !redirect {
				select {
				case <-observedCancel:
				case <-time.After(time.Second):
					t.Fatal("HTTP request remained active")
				}
			}
			if posts.Load() != 1 || redirected.Load() != 0 {
				t.Fatal("creation replayed or redirected")
			}
		})
	}
}
