package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/appvault"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitchconduit"
)

const appID = "00000000-0000-4000-8000-000000000abc"
const private = "PRIVATE-DSN-KEY-OR-ERROR"

func arguments() []string {
	return []string{"--app", appID, "--client", "Client-A", "--revision", "7", "--conduit", "conduit-A"}
}
func environment(name string) string {
	switch name {
	case "OVERLAID_APP_EXECUTION_DATABASE_URL":
		return private
	case "OVERLAID_APP_EXECUTION_VAULT_ACTIVE_KEY":
		return "v1"
	case "OVERLAID_APP_EXECUTION_VAULT_KEYS":
		return "v1:" + base64.StdEncoding.EncodeToString(make([]byte, 32))
	default:
		panic("unrelated environment read")
	}
}
func mustNotOpen(context.Context, string) (inspection, error) { panic("unexpected pool") }
func noEnv(string) string                                     { panic("unexpected private configuration read") }

type fakeInspection struct {
	calls, closes  int
	bounded        bool
	binding        appvault.Binding
	conduit        string
	keys           tenancy.Keyring
	client         *http.Client
	fail, closeErr error
	cancel         context.CancelFunc
	closed         *bool
}

func (f *fakeInspection) InspectConduit(ctx context.Context, k tenancy.Keyring, b appvault.Binding, conduit string, client *http.Client) (twitchconduit.Observation, error) {
	f.calls++
	deadline, ok := ctx.Deadline()
	f.bounded = ok && time.Until(deadline) > 0 && time.Until(deadline) <= 10*time.Second
	f.keys, f.binding, f.conduit, f.client = k, b, conduit, client
	if f.cancel != nil {
		f.cancel()
	}
	return twitchconduit.Observation{ClientID: b.ClientID, ConduitID: conduit, ShardCount: 1, Shards: []twitchconduit.Shard{{ID: 0, Status: "enabled", Transport: "websocket"}}}, f.fail
}
func (f *fakeInspection) Close() error {
	f.closes++
	if f.closed != nil {
		*f.closed = true
	}
	return f.closeErr
}

func TestOfflineInputs(t *testing.T) {
	for _, arg := range []string{"--help", "-h", "--version"} {
		var out, diagnostic bytes.Buffer
		if code := run(t.Context(), []string{arg}, noEnv, &out, &diagnostic, mustNotOpen, nil); code != 0 || out.Len() == 0 || diagnostic.Len() != 0 {
			t.Fatal(code, diagnostic.String())
		}
		if arg == "--version" {
			var info map[string]any
			if err := json.Unmarshal(out.Bytes(), &info); err != nil || info["name"] != "overlaid-conduit-inspect" {
				t.Fatal(out.String(), err)
			}
		}
	}
	cases := [][]string{nil, {private}, {"--help", private}, {"--version", private}}
	for _, pair := range [][]string{{"--app", private}, {"--app", strings.ToUpper(appID)}, {"--client", "invalid/url"}, {"--client", ""}, {"--conduit", strings.Repeat("x", 129)}, {"--conduit", "invalid?key"}, {"--revision", "0"}, {"--revision", "-1"}, {"--revision", "9223372036854775808"}, {"--revision", private}} {
		a := arguments()
		for i := 0; i < len(a); i += 2 {
			if a[i] == pair[0] {
				a[i+1] = pair[1]
			}
		}
		cases = append(cases, a)
	}
	for i := 0; i < 8; i += 2 {
		a := arguments()
		cases = append(cases, append(a[:i:i], a[i+2:]...))
	}
	for _, suffix := range [][]string{{"--app", appID}, {"--client", "Client-A"}, {"--revision", "7"}, {"--conduit", "conduit-A"}, {"--dsn", private}, {"--token", private}, {"--key", private}, {private}, {"--help"}, {"--version"}} {
		cases = append(cases, append(arguments(), suffix...))
	}
	for _, args := range cases {
		var out, diagnostic bytes.Buffer
		if code := run(t.Context(), args, noEnv, &out, &diagnostic, mustNotOpen, nil); code != 2 || out.Len() != 0 || strings.Contains(diagnostic.String(), private) {
			t.Fatal(args, code, diagnostic.String())
		}
	}
}

func TestExactSelectionAndCloseBeforeOutput(t *testing.T) {
	closed := false
	f := &fakeInspection{closed: &closed}
	var out, diagnostic bytes.Buffer
	client := &http.Client{}
	opens := 0
	writer := writerFunc(func(p []byte) (int, error) {
		if !closed {
			t.Fatal("output before pool close")
		}
		return out.Write(p)
	})
	code := run(t.Context(), arguments(), environment, writer, &diagnostic, func(ctx context.Context, dsn string) (inspection, error) {
		opens++
		if dsn != private {
			t.Fatal("wrong DSN")
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("unbounded open")
		}
		return f, nil
	}, client)
	if code != 0 || opens != 1 || f.calls != 1 || f.closes != 1 || !f.bounded || f.binding != (appvault.Binding{AppID: appID, ClientID: "Client-A", Revision: 7}) || f.conduit != "conduit-A" || f.keys.Active != "v1" || len(f.keys.Keys["v1"]) != 32 || f.client != client || diagnostic.Len() != 0 {
		t.Fatal(code, f, diagnostic.String())
	}
	var result struct {
		Binding     appvault.Binding          `json:"binding"`
		Observation twitchconduit.Observation `json:"observation"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil || result.Binding != f.binding || result.Observation.ConduitID != f.conduit || len(result.Observation.Shards) != 1 || strings.Contains(out.String(), private) {
		t.Fatal(out.String(), err)
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func TestPrivateConfigurationRefusal(t *testing.T) {
	for _, tc := range []struct{ name, value string }{
		{"OVERLAID_APP_EXECUTION_DATABASE_URL", " "},
		{"OVERLAID_APP_EXECUTION_VAULT_ACTIVE_KEY", ""},
		{"OVERLAID_APP_EXECUTION_VAULT_ACTIVE_KEY", "missing"},
		{"OVERLAID_APP_EXECUTION_VAULT_KEYS", private},
		{"OVERLAID_APP_EXECUTION_VAULT_KEYS", "v1:" + base64.StdEncoding.EncodeToString(make([]byte, 16))},
		{"OVERLAID_APP_EXECUTION_VAULT_KEYS", environment("OVERLAID_APP_EXECUTION_VAULT_KEYS") + "," + environment("OVERLAID_APP_EXECUTION_VAULT_KEYS")},
	} {
		var out, diagnostic bytes.Buffer
		env := func(name string) string {
			if name == tc.name {
				return tc.value
			}
			return environment(name)
		}
		if code := run(t.Context(), arguments(), env, &out, &diagnostic, mustNotOpen, nil); code != 2 || out.Len() != 0 || strings.Contains(diagnostic.String(), private) {
			t.Fatal(tc, code, diagnostic.String())
		}
	}
}

func TestShorterCallerDeadlineAndOfflineOutputFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	var out, diagnostic bytes.Buffer
	f := new(fakeInspection)
	code := run(ctx, arguments(), environment, &out, &diagnostic, func(bound context.Context, _ string) (inspection, error) {
		got, ok := bound.Deadline()
		if !ok || !got.Equal(deadline) {
			t.Fatal("caller deadline extended")
		}
		return f, nil
	}, nil)
	if code != 0 || f.calls != 1 || f.closes != 1 {
		t.Fatal(code, f)
	}
	for _, arg := range []string{"--help", "--version"} {
		writer := writerFunc(func(p []byte) (int, error) { return len(p) - 1, nil })
		if code := run(t.Context(), []string{arg}, noEnv, writer, &diagnostic, mustNotOpen, nil); code != 1 {
			t.Fatal(arg, code)
		}
	}
}

func TestFailuresNeverReturnObservationOrRepeat(t *testing.T) {
	for _, name := range []string{"operation", "close", "cancel", "output", "short-output", "open", "partial-open", "nil-open"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			f := new(fakeInspection)
			if name == "operation" {
				f.fail = errors.New(private)
			}
			if name == "close" {
				f.closeErr = errors.New(private)
			}
			if name == "cancel" {
				f.cancel = cancel
			}
			var out, diagnostic bytes.Buffer
			var writer io.Writer = &out
			if name == "output" {
				writer = writerFunc(func([]byte) (int, error) { return 0, errors.New(private) })
			}
			if name == "short-output" {
				writer = writerFunc(func(p []byte) (int, error) { return len(p) - 1, nil })
			}
			opens := 0
			code := run(ctx, arguments(), environment, writer, &diagnostic, func(context.Context, string) (inspection, error) {
				opens++
				switch name {
				case "open":
					return nil, errors.New(private)
				case "partial-open":
					return f, errors.New(private)
				case "nil-open":
					return nil, nil
				}
				return f, nil
			}, nil)
			wantCalls, wantClose, wantCode := 1, 1, 1
			if name == "open" || name == "nil-open" {
				wantCalls, wantClose = 0, 0
			}
			if name == "partial-open" {
				wantCalls = 0
			}
			if name == "cancel" {
				wantCode = 130
			}
			if code != wantCode || opens != 1 || f.calls != wantCalls || f.closes != wantClose || out.Len() != 0 || diagnostic.Len() == 0 || strings.Contains(diagnostic.String(), private) {
				t.Fatal(code, f, diagnostic.String())
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var out, diagnostic bytes.Buffer
	if code := run(ctx, arguments(), noEnv, &out, &diagnostic, mustNotOpen, nil); code != 130 || out.Len() != 0 {
		t.Fatal(code)
	}
}
