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

type inventoryFixture struct {
	tokens, lists, validates atomic.Int32
	respond                  func(string, *http.Request) (*http.Response, error)
}

func inventoryFixtureReader(t *testing.T) (*InventoryReader, *inventoryFixture) {
	t.Helper()
	f := &inventoryFixture{}
	r, err := NewInventoryReader(InventoryConfig{ClientID: "client", Token: func(ctx context.Context) (string, error) {
		f.tokens.Add(1)
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 10*time.Second {
			t.Error("unbounded token source")
		}
		return "PRIVATE-TOKEN", nil
	}, HTTPClient: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "Bearer PRIVATE-TOKEN" {
			t.Error("unexpected private request")
		}
		stage := ""
		switch r.URL.String() {
		case validateURL:
			stage = "before"
			if f.validates.Add(1)%2 == 0 {
				stage = "after"
			}
		case conduitURL:
			stage = "list1"
			if f.lists.Add(1)%2 == 0 {
				stage = "list2"
			}
			if r.Header.Get("Client-Id") != "client" {
				t.Error("wrong client header")
			}
		default:
			t.Error("unexpected endpoint")
		}
		if f.respond != nil {
			if result, err := f.respond(stage, r); result != nil || err != nil {
				return result, err
			}
		}
		if strings.HasPrefix(stage, "list") {
			return creationResponse(200, `{"data":[{"id":"z","shard_count":20000,"private":"PRIVATE-FIELD"},{"id":"a","shard_count":1}],"private":"PRIVATE-FIELD"}`), nil
		}
		return creationResponse(200, `{"client_id":"client","expires_in":3600,"user_id":null}`), nil
	})}})
	if err != nil {
		t.Fatal(err)
	}
	return r, f
}
func assertNoInventory(t *testing.T, r InventoryObservation, err error, want error) {
	t.Helper()
	if !errors.Is(err, want) || r.ClientID != "" || r.Conduits != nil || !r.StartedAt.IsZero() || !r.CompletedAt.IsZero() {
		t.Fatalf("partial/incorrect inventory: %v %v", r, err)
	}
	if strings.Contains(fmt.Sprintf("%+v %#v", r, err), "PRIVATE") {
		t.Fatal("private error exposed")
	}
}
func TestInventoryCompleteProjectionEmptyAndBounds(t *testing.T) {
	r, _ := inventoryFixtureReader(t)
	observed, err := r.Inspect(t.Context())
	if err != nil || observed.Conduits[1].ShardCount != 20000 {
		t.Fatal(observed, err)
	}
	raw, _ := json.Marshal(observed)
	if strings.Contains(string(raw), "PRIVATE") {
		t.Fatal("private extras escaped")
	}
	for _, count := range []int{0, 2, 100} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			r, f := inventoryFixtureReader(t)
			f.respond = func(stage string, _ *http.Request) (*http.Response, error) {
				if !strings.HasPrefix(stage, "list") {
					return nil, nil
				}
				items := make([]string, 0, count)
				for i := range count {
					items = append(items, fmt.Sprintf(`{"id":"id-%03d","shard_count":%d,"private":"PRIVATE"}`, i, i+1))
				}
				if stage == "list1" {
					for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
						items[i], items[j] = items[j], items[i]
					}
				}
				return creationResponse(200, `{"data":[`+strings.Join(items, ",")+`],"pagination":{"cursor":""}}`), nil
			}
			start := time.Now()
			result, err := r.Inspect(t.Context())
			if err != nil || result.ClientID != "client" || result.Conduits == nil || len(result.Conduits) != count || result.StartedAt.Before(start) || result.CompletedAt.Before(result.StartedAt) || result.CompletedAt.After(time.Now()) {
				t.Fatal(result, err)
			}
			for i, item := range result.Conduits {
				if item.ID != fmt.Sprintf("id-%03d", i) || item.ShardCount != i+1 {
					t.Fatal("unsorted/incorrect projection")
				}
			}
			if f.tokens.Load() != 1 || f.lists.Load() != 2 || f.validates.Load() != 2 {
				t.Fatal("wrong read count")
			}
			raw, _ := json.Marshal(result)
			if strings.Contains(string(raw), "PRIVATE") {
				t.Fatal("private metadata escaped")
			}
		})
	}
}

func TestInventoryInputsBeforeCredentials(t *testing.T) {
	if _, err := NewInventoryReader(InventoryConfig{ClientID: "bad/client", Token: func(context.Context) (string, error) { return "", nil }}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := NewInventoryReader(InventoryConfig{ClientID: "client"}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	r, f := inventoryFixtureReader(t)
	result, err := r.Inspect(nil) //nolint:staticcheck // Explicit nil-context refusal contract.
	assertNoInventory(t, result, err, ErrInvalid)
	var absent *InventoryReader
	result, err = absent.Inspect(t.Context())
	assertNoInventory(t, result, err, ErrInvalid)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result, err = r.Inspect(ctx)
	assertNoInventory(t, result, err, context.Canceled)
	if f.tokens.Load() != 0 || f.validates.Load() != 0 || f.lists.Load() != 0 {
		t.Fatal("invalid input accessed credentials/provider")
	}
	r.token = func(context.Context) (string, error) { return "PRIVATE\nTOKEN", nil }
	result, err = r.Inspect(t.Context())
	assertNoInventory(t, result, err, ErrUnauthorized)
}

func TestInventoryMalformedPartialAndDriftRefusal(t *testing.T) {
	bodies := []string{`null`, `[]`, `{}`, `{"data":null}`, `{"data":{}}`, `{"Data":[]}`, `{"data":[],"Pagination":{"cursor":"next"}}`,
		`{"data":[{}]}`, `{"data":[null]}`, `{"data":[{"ID":"a","shard_count":1}]}`, `{"data":[{"id":"a","ID":"a","shard_count":1}]}`,
		`{"data":[{"id":"a"}]}`, `{"data":[{"id":"a","shard_count":null}]}`, `{"data":[{"id":"a","shard_count":0}]}`, `{"data":[{"id":"a","shard_count":20001}]}`, `{"data":[{"id":"a","shard_count":1.0}]}`, `{"data":[{"id":"a","shard_count":"1"}]}`,
		`{"data":[{"id":"PRIVATE/url","shard_count":1}]}`, `{"data":[{"id":"` + strings.Repeat("a", 129) + `","shard_count":1}]}`, `{"data":[{"id":"a","shard_count":1},{"id":"a","shard_count":1}]}`,
		`{"data":[],"pagination":null}`, `{"data":[],"pagination":[]}`, `{"data":[],"pagination":{"cursor":null}}`, `{"data":[],"pagination":{"Cursor":""}}`, `{"data":[],"pagination":{"unknown":""}}`,
		`{"data":[],"data":[]}`, `{"data":[]} {}`, `{"data":[],"private":` + strings.Repeat("[", 33) + `0` + strings.Repeat("]", 33) + `}`, strings.Repeat(" ", maxBody+1),
	}
	tooMany := make([]string, 101)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf(`{"id":"id-%d","shard_count":1}`, i)
	}
	bodies = append(bodies, `{"data":[`+strings.Join(tooMany, ",")+`]}`)
	for i, body := range bodies {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			r, f := inventoryFixtureReader(t)
			f.respond = func(stage string, _ *http.Request) (*http.Response, error) {
				if stage == "list1" {
					return creationResponse(200, body), nil
				}
				return nil, nil
			}
			result, err := r.Inspect(t.Context())
			assertNoInventory(t, result, err, ErrInvalid)
			if f.lists.Load() != 1 {
				t.Fatal("invalid read retried")
			}
		})
	}
	for _, body := range []string{`{"data":[]}`, `{"data":[{"id":"a","shard_count":2}]}`, `{"data":[{"id":"other","shard_count":1}]}`, `{"data":[],"pagination":{"cursor":"PRIVATE-next"}}`} {
		r, f := inventoryFixtureReader(t)
		f.respond = func(stage string, _ *http.Request) (*http.Response, error) {
			if stage == "list2" {
				return creationResponse(200, body), nil
			}
			return nil, nil
		}
		result, err := r.Inspect(t.Context())
		assertNoInventory(t, result, err, ErrChanged)
	}
}

func TestInventoryIdentityLifetimeAndPrivateFailures(t *testing.T) {
	for _, stage := range []string{"before", "after"} {
		for _, body := range []string{`{"client_id":"other","expires_in":3600}`, `{"client_id":"client","expires_in":3600,"user_id":""}`, `{"client_id":"client","expires_in":30}`} {
			r, f := inventoryFixtureReader(t)
			f.respond = func(current string, _ *http.Request) (*http.Response, error) {
				if stage == current {
					return creationResponse(200, body), nil
				}
				return nil, nil
			}
			result, err := r.Inspect(t.Context())
			assertNoInventory(t, result, err, ErrUnauthorized)
			if stage == "before" && f.lists.Load() != 0 {
				t.Fatal("unauthorized inventory read")
			}
		}
	}
	for _, kind := range []string{"source", "transport", "panic", "read", "close", "429", "302"} {
		t.Run(kind, func(t *testing.T) {
			r, f := inventoryFixtureReader(t)
			if kind == "source" {
				r.token = func(context.Context) (string, error) { return "", errors.New("PRIVATE-source") }
			}
			f.respond = func(stage string, _ *http.Request) (*http.Response, error) {
				if stage != "list1" {
					return nil, nil
				}
				switch kind {
				case "transport":
					return nil, errors.New("PRIVATE-transport")
				case "panic":
					panic("PRIVATE-panic")
				case "read":
					return &http.Response{StatusCode: 200, Body: creationBrokenBody{}}, nil
				case "close":
					return &http.Response{StatusCode: 200, Body: creationClosePanic{strings.NewReader(`{"data":[]}`)}}, nil
				case "429":
					return creationResponse(429, "PRIVATE-body"), nil
				case "302":
					return creationResponse(302, "PRIVATE-body"), nil
				}
				return nil, nil
			}
			result, err := r.Inspect(t.Context())
			want := ErrUnavailable
			if kind == "source" {
				want = ErrUnauthorized
			}
			if kind == "429" {
				want = ErrRateLimited
			}
			assertNoInventory(t, result, err, want)
			if errors.Is(err, context.Canceled) {
				t.Fatal("cleanup invented cancellation")
			}
		})
	}
	r, f := inventoryFixtureReader(t)
	f.respond = func(stage string, request *http.Request) (*http.Response, error) {
		if stage == "before" {
			return creationResponse(200, `{"client_id":"client","expires_in":31}`), nil
		}
		if stage == "list1" {
			select {
			case <-time.After(1100 * time.Millisecond):
			case <-request.Context().Done():
				return nil, request.Context().Err()
			}
		}
		return nil, nil
	}
	result, err := r.Inspect(t.Context())
	assertNoInventory(t, result, err, ErrUnauthorized)
}

func TestInventoryActualCancellationAndRedirectRefusal(t *testing.T) {
	for _, redirect := range []bool{false, true} {
		t.Run(fmt.Sprint(redirect), func(t *testing.T) {
			entered, stopped := make(chan struct{}), make(chan struct{})
			var lists, redirected atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected.Add(1) }))
			defer target.Close()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/oauth2/validate" {
					_, _ = io.WriteString(w, `{"client_id":"client","expires_in":3600}`)
					return
				}
				lists.Add(1)
				close(entered)
				if redirect {
					http.Redirect(w, r, target.URL, http.StatusFound)
					return
				}
				<-r.Context().Done()
				close(stopped)
			}))
			defer server.Close()
			local, _ := url.Parse(server.URL)
			tr := http.DefaultTransport.(*http.Transport).Clone()
			defer tr.CloseIdleConnections()
			r, err := NewInventoryReader(InventoryConfig{ClientID: "client", Token: func(context.Context) (string, error) { return "PRIVATE-TOKEN", nil }, HTTPClient: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return nil }, Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				copyRequest := r.Clone(r.Context())
				copyRequest.URL.Scheme, copyRequest.URL.Host = local.Scheme, local.Host
				return tr.RoundTrip(copyRequest)
			})}})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			type answer struct {
				r   InventoryObservation
				err error
			}
			done := make(chan answer, 1)
			go func() { r, e := r.Inspect(ctx); done <- answer{r, e} }()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("inventory request missing")
			}
			want := ErrUnavailable
			if !redirect {
				cancel()
				want = context.Canceled
			}
			select {
			case a := <-done:
				assertNoInventory(t, a.r, a.err, want)
			case <-time.After(4 * time.Second):
				t.Fatal("inventory did not stop")
			}
			if !redirect {
				select {
				case <-stopped:
				case <-time.After(time.Second):
					t.Fatal("HTTP request still active")
				}
			}
			if lists.Load() != 1 || redirected.Load() != 0 {
				t.Fatal("inventory retried or redirected")
			}
		})
	}
}
