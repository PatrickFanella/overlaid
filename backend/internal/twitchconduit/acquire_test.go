package twitchconduit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func authorizerFixture(t *testing.T, respond func(*http.Request, int) (int, string)) (*AppAuthorizer, *fixture, *atomic.Int32) {
	t.Helper()
	inspector, f := inspectorFixture(t, respond)
	calls := new(atomic.Int32)
	authorizer, err := NewAppAuthorizer(AppAuthorizationConfig{ClientID: "client", HTTPClient: inspector.http, ClientSecret: func(ctx context.Context) (string, error) {
		calls.Add(1)
		if _, ok := ctx.Deadline(); !ok {
			t.Error("unbounded secret callback")
		}
		return "PRIVATE+CLIENT/&=SECRET", nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	return authorizer, f, calls
}

func TestAcquireAppCredentialValidatedPrivateAndInspectorComposition(t *testing.T) {
	a, f, calls := authorizerFixture(t, func(r *http.Request, n int) (int, string) {
		status, body := defaultResponse(r, n)
		if r.URL.Path == "/oauth2/validate" {
			body = strings.ReplaceAll(body, `"expires_in":3600`, `"expires_in":120`)
		}
		return status, body
	})
	started := time.Now()
	result, err := a.Acquire(t.Context())
	if err != nil || result.Outcome != AcquisitionIssued || result.Credential == nil {
		t.Fatal(result, err)
	}
	if !result.Credential.BindsClientSecret("PRIVATE+CLIENT/&=SECRET") || result.Credential.BindsClientSecret("other-secret") {
		t.Fatal("acquisition secret binding lost")
	}
	a.clientID = "changed-after-acquisition"
	if !result.Credential.BindsClientSecret("PRIVATE+CLIENT/&=SECRET") {
		t.Fatal("credential retained mutable authorizer identity")
	}
	a.clientID = "client"
	metadata := result.Credential.Metadata()
	if metadata.ClientID != "client" || metadata.ValidatedAt.Before(started) || metadata.ExpiresAt.After(time.Now().Add(120*time.Second)) || metadata.ExpiresAt.Before(started.Add(110*time.Second)) {
		t.Fatal("nonconservative lifetime", metadata)
	}
	token, err := result.Credential.Token(t.Context())
	if err != nil || token != "PRIVATE-TOKEN" {
		t.Fatal("trusted token accessor failed", err)
	}
	for _, value := range []any{result.Credential, *result.Credential, result, struct{ credential AppCredential }{*result.Credential}} {
		raw, err := json.Marshal(value)
		if err != nil || strings.Contains(string(raw), "PRIVATE") {
			t.Fatal("credential JSON exposed private material")
		}
		for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
			if strings.Contains(fmt.Sprintf(format, value), "PRIVATE") {
				t.Fatal("credential formatting exposed private material", format)
			}
		}
	}
	raw, _ := json.Marshal(result.Credential)
	var reconstructed AppCredential
	if err := json.Unmarshal(raw, &reconstructed); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("credential JSON decoding accepted", err)
	}
	if _, err := reconstructed.Token(t.Context()); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("metadata rehydrated a credential")
	}
	inspector, err := NewInspector(Config{ClientID: "client", ConduitID: "selected", Token: result.Credential.Token, HTTPClient: a.http})
	if err != nil {
		t.Fatal(err)
	}
	if report, err := inspector.Inspect(t.Context()); err != nil || len(report.Shards) != 2 {
		t.Fatal(report, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.posts != 1 || f.validates != 3 || f.lists != 2 || f.pages != 2 || calls.Load() != 1 {
		t.Fatal("unexpected acquisition/inspection sequence", f.posts, f.validates, f.lists, f.pages, calls.Load())
	}
}

func TestAcquireRefusalAndUncertaintyNeverRepeatPost(t *testing.T) {
	for _, tc := range []struct {
		status  int
		outcome AcquisitionOutcome
		cause   error
	}{
		{400, AcquisitionRefused, ErrUnauthorized}, {401, AcquisitionRefused, ErrUnauthorized}, {403, AcquisitionRefused, ErrUnauthorized}, {429, AcquisitionRefused, ErrRateLimited}, {500, AcquisitionUncertain, ErrUnavailable}, {302, AcquisitionUncertain, ErrUnavailable},
	} {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			a, f, _ := authorizerFixture(t, func(r *http.Request, n int) (int, string) {
				if r.URL.Path == "/oauth2/token" {
					return tc.status, `{"message":"PRIVATE-PROVIDER-ERROR"}`
				}
				return defaultResponse(r, n)
			})
			result, err := a.Acquire(t.Context())
			if result.Outcome != tc.outcome || result.Credential != nil || !errors.Is(err, tc.cause) || errors.Is(err, ErrAcquisitionUncertain) != (tc.outcome == AcquisitionUncertain) || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal(result, err)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.posts != 1 || f.validates != 0 {
				t.Fatal("POST repeated or unusable response validated", f.posts, f.validates)
			}
		})
	}
}

func TestAcquireMalformedOrUnvalidatedIssuanceIsUncertain(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		validation bool
	}{
		{"missing", `{}`, false}, {"refresh-token", `{"access_token":"PRIVATE-TOKEN","refresh_token":"PRIVATE-REFRESH","expires_in":3600,"token_type":"bearer"}`, false},
		{"token-type", `{"access_token":"PRIVATE-TOKEN","expires_in":3600,"token_type":"other"}`, false},
		{"short", `{"access_token":"PRIVATE-TOKEN","expires_in":30,"token_type":"bearer"}`, false},
		{"overflow", `{"access_token":"PRIVATE-TOKEN","expires_in":9223372036854775807,"token_type":"bearer"}`, false},
		{"duplicate", `{"access_token":"other","access_token":"PRIVATE-TOKEN","expires_in":3600,"token_type":"bearer"}`, false},
		{"oversized", strings.Repeat("x", 65537), false},
		{"wrong-client", `{"client_id":"other","user_id":null,"login":null,"expires_in":3600}`, true},
		{"user-token", `{"client_id":"client","user_id":"owner","login":"owner","expires_in":3600}`, true},
		{"validation-expired", `{"client_id":"client","expires_in":0}`, true},
		{"validation-overflow", `{"client_id":"client","expires_in":9223372036854775807}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, f, _ := authorizerFixture(t, func(r *http.Request, n int) (int, string) {
				if r.URL.Path == "/oauth2/token" && !tc.validation || r.URL.Path == "/oauth2/validate" && tc.validation {
					return 200, tc.body
				}
				return defaultResponse(r, n)
			})
			result, err := a.Acquire(t.Context())
			if result.Outcome != AcquisitionUncertain || result.Credential != nil || !errors.Is(err, ErrAcquisitionUncertain) {
				t.Fatal(result, err)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.posts != 1 {
				t.Fatal("POST repeated", f.posts)
			}
		})
	}
	a, _, _ := authorizerFixture(t, func(r *http.Request, n int) (int, string) {
		if r.URL.Path == "/oauth2/validate" {
			return 401, `{"message":"PRIVATE-ERROR"}`
		}
		return defaultResponse(r, n)
	})
	if result, err := a.Acquire(t.Context()); result.Outcome != AcquisitionUncertain || !errors.Is(err, ErrAcquisitionUncertain) || !errors.Is(err, ErrUnauthorized) {
		t.Fatal(result, err)
	}
}

func TestAcquireCancellationBeforeAndAfterIssuance(t *testing.T) {
	a, f, calls := authorizerFixture(t, nil)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if result, err := a.Acquire(ctx); result.Outcome != AcquisitionNotStarted || !errors.Is(err, context.Canceled) || calls.Load() != 0 {
		t.Fatal(result, err)
	}
	f.mu.Lock()
	if f.posts != 0 {
		t.Error("canceled acquisition posted")
	}
	f.mu.Unlock()
	for _, phase := range []string{"/oauth2/token", "/oauth2/validate"} {
		t.Run(phase, func(t *testing.T) {
			entered := make(chan struct{})
			a, f, _ := authorizerFixture(t, func(r *http.Request, n int) (int, string) {
				if r.URL.Path == phase {
					close(entered)
					<-r.Context().Done()
					return 500, `{"message":"PRIVATE-ERROR"}`
				}
				return defaultResponse(r, n)
			})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			type completed struct {
				result Acquisition
				err    error
			}
			done := make(chan completed, 1)
			go func() { result, err := a.Acquire(ctx); done <- completed{result, err} }()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("request did not start")
			}
			cancel()
			select {
			case got := <-done:
				if got.result.Outcome != AcquisitionUncertain || got.result.Credential != nil || !errors.Is(got.err, context.Canceled) || !errors.Is(got.err, ErrAcquisitionUncertain) {
					t.Fatal(got.result, got.err)
				}
			case <-time.After(time.Second):
				t.Fatal("request did not cancel")
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.posts != 1 {
				t.Fatal("canceled issuance repeated")
			}
		})
	}
}

func TestAcquireCallbackTransportAndCredentialGuards(t *testing.T) {
	for _, cfg := range []AppAuthorizationConfig{{}, {ClientID: "../client", ClientSecret: func(context.Context) (string, error) { return "secret", nil }}} {
		if _, err := NewAppAuthorizer(cfg); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
	}
	var missing *AppAuthorizer
	if result, err := missing.Acquire(t.Context()); result.Outcome != AcquisitionNotStarted || !errors.Is(err, ErrInvalid) {
		t.Fatal(result, err)
	}
	a, f, _ := authorizerFixture(t, nil)
	for _, callback := range []func(context.Context) (string, error){func(context.Context) (string, error) { return "", errors.New("PRIVATE-CREDENTIAL") }, func(context.Context) (string, error) { panic("PRIVATE-PANIC") }, func(context.Context) (string, error) { return "bad secret", nil }} {
		a.secret = callback
		if result, err := a.Acquire(t.Context()); result.Outcome != AcquisitionNotStarted || err == nil || errors.Is(err, ErrAcquisitionUncertain) || strings.Contains(err.Error(), "PRIVATE") {
			t.Fatal(result, err)
		}
	}
	f.mu.Lock()
	if f.posts != 0 {
		t.Error("invalid secret posted")
	}
	f.mu.Unlock()
	a.secret = func(context.Context) (string, error) { return "PRIVATE+CLIENT/&=SECRET", nil }
	a.http.Transport = roundTrip(func(*http.Request) (*http.Response, error) { return nil, errors.New("PRIVATE-TRANSPORT") })
	if result, err := a.Acquire(t.Context()); result.Outcome != AcquisitionUncertain || !errors.Is(err, ErrAcquisitionUncertain) || !errors.Is(err, ErrUnavailable) {
		t.Fatal(result, err)
	}
	a.http.Transport = roundTrip(func(*http.Request) (*http.Response, error) { panic("PRIVATE-TRANSPORT") })
	if result, err := a.Acquire(t.Context()); result.Outcome != AcquisitionUncertain || !errors.Is(err, ErrAcquisitionUncertain) {
		t.Fatal(result, err)
	}
	credential := AppCredential{clientID: "client", readToken: func() string { return "PRIVATE-TOKEN" }, expiresAt: time.Now().Add(29 * time.Second)}
	if _, err := credential.Token(t.Context()); !errors.Is(err, ErrUnauthorized) {
		t.Fatal(err)
	}
	credential.expiresAt = time.Now().Add(time.Hour)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := credential.Token(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestAcquireCanceledResponseAndReadFailureRemainUncertain(t *testing.T) {
	a, _, _ := authorizerFixture(t, nil)
	ctx, cancel := context.WithCancel(t.Context())
	a.http.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
		cancel()
		return &http.Response{StatusCode: 403, Body: http.NoBody}, nil
	})
	if result, err := a.Acquire(ctx); result.Outcome != AcquisitionUncertain || !errors.Is(err, context.Canceled) || !errors.Is(err, ErrAcquisitionUncertain) {
		t.Fatal(result, err)
	}
	a.http.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: brokenCredentialBody{}}, nil
	})
	if result, err := a.Acquire(t.Context()); result.Outcome != AcquisitionUncertain || !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "PRIVATE") {
		t.Fatal(result, err)
	}
}

type brokenCredentialBody struct{}

func (brokenCredentialBody) Read([]byte) (int, error) { return 0, errors.New("PRIVATE-BODY-ERROR") }
func (brokenCredentialBody) Close() error             { return nil }
