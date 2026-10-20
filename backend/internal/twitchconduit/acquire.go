package twitchconduit

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const tokenURL = "https://id.twitch.tv/oauth2/token"

var ErrAcquisitionUncertain = errors.New("app credential acquisition uncertain")

type AcquisitionOutcome string

const (
	AcquisitionNotStarted AcquisitionOutcome = "not_started"
	AcquisitionRefused    AcquisitionOutcome = "refused"
	AcquisitionUncertain  AcquisitionOutcome = "uncertain"
	AcquisitionIssued     AcquisitionOutcome = "issued"
)

// AppAuthorizationConfig is trusted service configuration. The callback owns
// secret custody and must honor context cancellation. No secret is cached here.
type AppAuthorizationConfig struct {
	ClientID     string
	ClientSecret func(context.Context) (string, error)
	HTTPClient   *http.Client
}

type AppAuthorizer struct {
	clientID string
	secret   func(context.Context) (string, error)
	http     *http.Client
}

// AppCredential holds ephemeral plaintext for explicit trusted use. Its JSON and
// formatting expose metadata only. It is not a vault, grant revision or permit.
type AppCredential struct {
	clientID               string
	readToken              func() string
	validatedAt, expiresAt time.Time
}

type AppCredentialMetadata struct {
	ClientID    string    `json:"clientId"`
	ValidatedAt time.Time `json:"validatedAt"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

func (c AppCredential) Metadata() AppCredentialMetadata {
	return AppCredentialMetadata{ClientID: c.clientID, ValidatedAt: c.validatedAt.UTC(), ExpiresAt: c.expiresAt.UTC()}
}
func (c AppCredential) String() string               { return "twitch app credential (redacted)" }
func (c AppCredential) GoString() string             { return c.String() }
func (c AppCredential) MarshalJSON() ([]byte, error) { return json.Marshal(c.Metadata()) }
func (c *AppCredential) UnmarshalJSON([]byte) error {
	if c != nil {
		*c = AppCredential{}
	}
	return ErrUnauthorized
}

// Token is an explicit plaintext accessor for trusted custody/inspection. Fresh
// provider authorization and local ownership checks remain the consumer's job.
func (c AppCredential) Token(ctx context.Context) (string, error) {
	if ctx == nil {
		return "", ErrInvalid
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if !atom(c.clientID, 128) || c.readToken == nil || time.Until(c.expiresAt) < 30*time.Second {
		return "", ErrUnauthorized
	}
	token := c.readToken()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if !bearer(token) {
		return "", ErrUnauthorized
	}
	return token, nil
}

type Acquisition struct {
	Outcome    AcquisitionOutcome `json:"outcome"`
	Credential *AppCredential     `json:"credential,omitempty"`
}

func providerClient(source *http.Client) *http.Client {
	client := &http.Client{Timeout: inspectionTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if source != nil {
		client.Transport = source.Transport
		if source.Timeout > 0 && source.Timeout < client.Timeout {
			client.Timeout = source.Timeout
		}
	}
	return client
}

func NewAppAuthorizer(config AppAuthorizationConfig) (*AppAuthorizer, error) {
	if !atom(config.ClientID, 128) || config.ClientSecret == nil {
		return nil, ErrInvalid
	}
	return &AppAuthorizer{clientID: config.ClientID, secret: config.ClientSecret, http: providerClient(config.HTTPClient)}, nil
}

func validLifetime(seconds int64) bool {
	return seconds >= 30 && seconds <= math.MaxInt64/int64(time.Second)
}

// Acquire makes at most one non-replayable token POST and one validation GET.
// Once issuance may have started, ambiguity is uncertain even with cancellation.
// The caller must not turn uncertainty into an automatic acquisition retry.
func (a *AppAuthorizer) Acquire(caller context.Context) (result Acquisition, err error) {
	result.Outcome = AcquisitionNotStarted
	attempted := false
	defer func() {
		if recover() != nil {
			result = Acquisition{Outcome: AcquisitionNotStarted}
			err = ErrUnavailable
			if caller != nil && caller.Err() != nil {
				err = caller.Err()
			}
			if attempted {
				result.Outcome = AcquisitionUncertain
				err = errors.Join(ErrAcquisitionUncertain, err)
			}
		}
	}()
	if a == nil || a.http == nil || a.secret == nil || caller == nil {
		return result, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(caller, inspectionTimeout)
	defer cancel()
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	secret, err := a.secret(ctx)
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if err != nil || !bearer(secret) {
		return result, ErrUnauthorized
	}
	form := url.Values{"client_id": {a.clientID}, "client_secret": {secret}, "grant_type": {"client_credentials"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return result, ErrInvalid
	}
	// No body replay, redirects, idempotency key or application retry loop.
	req.GetBody = nil
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	started := time.Now()
	attempted = true
	result.Outcome = AcquisitionUncertain
	uncertain := func(cause error) (Acquisition, error) {
		return Acquisition{Outcome: AcquisitionUncertain}, errors.Join(ErrAcquisitionUncertain, cause)
	}
	response, err := a.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return uncertain(ctx.Err())
		}
		return uncertain(ErrUnavailable)
	}
	defer func() { _ = response.Body.Close() }()
	if ctx.Err() != nil {
		return uncertain(ctx.Err())
	}
	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden:
		return Acquisition{Outcome: AcquisitionRefused}, ErrUnauthorized
	case http.StatusTooManyRequests:
		return Acquisition{Outcome: AcquisitionRefused}, ErrRateLimited
	default:
		return uncertain(ErrUnavailable)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if ctx.Err() != nil {
		return uncertain(ctx.Err())
	}
	if err != nil {
		return uncertain(ErrUnavailable)
	}
	if len(raw) > 65536 || !uniqueJSON(raw) {
		return uncertain(ErrInvalid)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return uncertain(ErrInvalid)
	}
	for key := range fields {
		switch key {
		case "access_token", "expires_in", "token_type":
		default:
			return uncertain(ErrInvalid)
		}
	}
	var issued struct {
		Token   string `json:"access_token"`
		Expires int64  `json:"expires_in"`
		Type    string `json:"token_type"`
	}
	if json.Unmarshal(raw, &issued) != nil || !bearer(issued.Token) || !strings.EqualFold(issued.Type, "bearer") || !validLifetime(issued.Expires) {
		return uncertain(ErrInvalid)
	}
	inspector := Inspector{clientID: a.clientID, http: a.http}
	budget := maxBytes - len(raw)
	until, err := inspector.validateUntil(ctx, issued.Token, &budget)
	if err != nil {
		return uncertain(err)
	}
	issuanceUntil := started.Add(time.Duration(issued.Expires) * time.Second)
	if issuanceUntil.Before(until) {
		until = issuanceUntil
	}
	if ctx.Err() != nil {
		return uncertain(ctx.Err())
	}
	if time.Until(until) < 30*time.Second {
		return uncertain(ErrUnauthorized)
	}
	token := issued.Token
	credential := AppCredential{clientID: a.clientID, readToken: func() string { return token }, validatedAt: time.Now(), expiresAt: until}
	return Acquisition{Outcome: AcquisitionIssued, Credential: &credential}, nil
}
