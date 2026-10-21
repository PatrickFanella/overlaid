package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/appvault"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
)

const requestID = "00000000-0000-4000-8000-000000000abc"
const private = "PRIVATE-DSN-SECRET-ERROR"

func arguments(command string) []string {
	a := []string{command, "--client", "Client-A", "--revision", "0"}
	if command == "execute" {
		a = append(a, "--request", requestID)
	}
	return a
}
func environment(name string) string {
	switch name {
	case "OVERLAID_APP_ISSUANCE_DATABASE_URL", "OVERLAID_APP_ISSUANCE_CLIENT_SECRET":
		return private
	case "OVERLAID_APP_ISSUANCE_VAULT_ACTIVE_KEY":
		return "v1"
	case "OVERLAID_APP_ISSUANCE_VAULT_KEYS":
		return "v1:" + base64.StdEncoding.EncodeToString(make([]byte, 32))
	default:
		panic("unrelated configuration read")
	}
}
func noEnv(string) string                                 { panic("unexpected private configuration read") }
func mustNotOpen(context.Context, string) (issuer, error) { panic("unexpected pool") }

type fakeIssuer struct {
	prepared, executed, closed int
	id, client, secret         string
	revision                   int64
	bounded                    bool
	keys                       tenancy.Keyring
	http                       *http.Client
	fail, closeErr             error
	cancel                     context.CancelFunc
	state                      string
}

func (f *fakeIssuer) result(ctx context.Context, client, secret string, revision int64, duration time.Duration, state string) (appvault.Issuance, error) {
	f.client, f.secret, f.revision = client, secret, revision
	deadline, ok := ctx.Deadline()
	f.bounded = ok && time.Until(deadline) > 0 && time.Until(deadline) <= duration
	if f.cancel != nil {
		f.cancel()
	}
	if f.state != "" {
		state = f.state
	}
	return appvault.Issuance{ID: requestID, ClientID: client, ExpectedRevision: revision, State: state}, f.fail
}
func (f *fakeIssuer) PrepareIssuance(ctx context.Context, client, secret string, revision int64) (appvault.Issuance, error) {
	f.prepared++
	return f.result(ctx, client, secret, revision, 5*time.Second, "prepared")
}
func (f *fakeIssuer) Issue(ctx context.Context, k tenancy.Keyring, id, client, secret string, revision int64, http *http.Client) (appvault.Issuance, error) {
	f.executed++
	f.keys, f.id, f.http = k, id, http
	return f.result(ctx, client, secret, revision, 20*time.Second, "issued")
}
func (f *fakeIssuer) Close() error { f.closed++; return f.closeErr }

func TestOfflineArgumentsAndPrivateConfiguration(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"--version"}, {"prepare", "--help"}, {"execute", "-h"}} {
		var out, diagnostic bytes.Buffer
		if code := run(t.Context(), args, noEnv, &out, &diagnostic, mustNotOpen, nil); code != 0 || out.Len() == 0 || diagnostic.Len() != 0 {
			t.Fatal(args, code, diagnostic.String())
		}
	}
	cases := [][]string{nil, {private}, {"--version", private}, {"prepare"}, {"execute"}, {"prepare", "--help", private}}
	for _, command := range []string{"prepare", "execute"} {
		base := arguments(command)
		for i := 1; i < len(base); i += 2 {
			a := append([]string{}, base[:i]...)
			a = append(a, base[i+2:]...)
			cases = append(cases, a)
		}
		for _, suffix := range [][]string{{"--client", "Other"}, {"--revision", "1"}, {"--token", private}, {"--secret", private}, {"--dsn", private}, {"--url", private}, {private}, {"--help"}} {
			cases = append(cases, append(arguments(command), suffix...))
		}
		for _, revision := range []string{"-1", "9223372036854775807", "9223372036854775808", private} {
			a := arguments(command)
			a[4] = revision
			cases = append(cases, a)
		}
		a := arguments(command)
		a[2] = "invalid/client"
		cases = append(cases, a)
	}
	cases = append(cases, append(arguments("prepare"), "--request", requestID))
	a := arguments("execute")
	a[6] = strings.ToUpper(requestID)
	cases = append(cases, a)
	for _, args := range cases {
		var out, diagnostic bytes.Buffer
		if code := run(t.Context(), args, noEnv, &out, &diagnostic, mustNotOpen, nil); code != 2 || out.Len() != 0 || strings.Contains(diagnostic.String(), private) {
			t.Fatal(args, code, diagnostic.String())
		}
	}
	for _, command := range []string{"prepare", "execute"} {
		for _, bad := range []string{"dsn", "secret", "keys"} {
			if command == "prepare" && bad == "keys" {
				continue
			}
			var out, diagnostic bytes.Buffer
			env := func(name string) string {
				if bad == "dsn" && strings.HasSuffix(name, "DATABASE_URL") {
					return " "
				}
				if bad == "secret" && strings.HasSuffix(name, "CLIENT_SECRET") {
					return "invalid\nsecret"
				}
				if bad == "keys" && strings.HasSuffix(name, "VAULT_KEYS") {
					return private
				}
				return environment(name)
			}
			if code := run(t.Context(), arguments(command), env, &out, &diagnostic, mustNotOpen, nil); code != 2 || out.Len() != 0 || strings.Contains(diagnostic.String(), private) {
				t.Fatal(command, bad, code, diagnostic.String())
			}
		}
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }
func TestExactOperationsBoundsAndCloseBeforeMetadata(t *testing.T) {
	for _, command := range []string{"prepare", "execute"} {
		f := new(fakeIssuer)
		var out, diagnostic bytes.Buffer
		env := func(name string) string {
			if command == "prepare" && strings.Contains(name, "VAULT") {
				t.Fatal("prepare read keys")
			}
			return environment(name)
		}
		client := &http.Client{}
		writer := writerFunc(func(p []byte) (int, error) {
			if f.closed != 1 {
				t.Fatal("output preceded close")
			}
			return out.Write(p)
		})
		code := run(t.Context(), arguments(command), env, writer, &diagnostic, func(ctx context.Context, dsn string) (issuer, error) {
			if dsn != private {
				t.Fatal("wrong DSN")
			}
			return f, nil
		}, client)
		var result struct {
			Issuance appvault.Issuance `json:"issuance"`
		}
		if err := json.Unmarshal(out.Bytes(), &result); err != nil || code != 0 || f.closed != 1 || !f.bounded || f.client != "Client-A" || f.secret != private || f.revision != 0 || diagnostic.Len() != 0 || strings.Contains(out.String(), private) {
			t.Fatal(code, f, out.String(), diagnostic.String(), err)
		}
		if command == "prepare" && (f.prepared != 1 || f.executed != 0 || result.Issuance.State != "prepared") {
			t.Fatal(f)
		}
		if command == "execute" && (f.executed != 1 || f.prepared != 0 || f.id != requestID || f.http != client || len(f.keys.Keys["v1"]) != 32 || result.Issuance.State != "issued") {
			t.Fatal(f)
		}
	}
}

func TestFailureMetadataIsReconciliationOnly(t *testing.T) {
	for _, name := range []string{"uncertain", "cancel", "close", "write", "short", "malformed-success", "open", "partial-open"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			f := new(fakeIssuer)
			switch name {
			case "uncertain":
				f.fail = errors.New(private)
				f.state = "uncertain"
			case "cancel":
				f.cancel = cancel
				f.state = "uncertain"
				f.fail = context.Canceled
			case "close":
				f.closeErr = errors.New(private)
			case "malformed-success":
				f.state = "started"
			}
			var out, diagnostic bytes.Buffer
			writer := writerFunc(out.Write)
			if name == "write" {
				writer = func([]byte) (int, error) { return 0, errors.New(private) }
			}
			if name == "short" {
				writer = func(p []byte) (int, error) { return len(p) - 1, nil }
			}
			code := run(ctx, arguments("execute"), environment, writer, &diagnostic, func(context.Context, string) (issuer, error) {
				if name == "open" {
					return nil, errors.New(private)
				}
				if name == "partial-open" {
					return f, errors.New(private)
				}
				return f, nil
			}, nil)
			want := 1
			if name == "cancel" {
				want = 130
			}
			if code != want || strings.Contains(out.String()+diagnostic.String(), private) || diagnostic.Len() == 0 {
				t.Fatal(code, diagnostic.String())
			}
			if name == "uncertain" || name == "cancel" {
				if !strings.Contains(out.String(), requestID) || !strings.Contains(out.String(), "uncertain") {
					t.Fatal("known original metadata lost", out.String())
				}
			} else if out.Len() != 0 {
				t.Fatal("unreliable output", out.String())
			}
			calls, closes := 1, 1
			if name == "open" {
				calls, closes = 0, 0
			}
			if name == "partial-open" {
				calls = 0
			}
			if f.executed != calls || f.closed != closes || f.prepared != 0 {
				t.Fatal(f)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var out, diagnostic bytes.Buffer
	if code := run(ctx, arguments("prepare"), noEnv, &out, &diagnostic, mustNotOpen, nil); code != 130 {
		t.Fatal(code)
	}
}
