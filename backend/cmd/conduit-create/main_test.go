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
	"git.subcult.tv/PatrickFanella/over-laid/internal/conduitownership"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
)

const appID = "11111111-1111-4111-8111-111111111111"
const requestID = "22222222-2222-4222-8222-222222222222"
const private = "PRIVATE-CONFIGURATION"

func arguments(command string) []string {
	if command == "status" {
		return []string{command, "--request", requestID}
	}
	args := []string{command, "--app", appID, "--client", "Client-A", "--revision", "1", "--shards", "2"}
	if command == "execute" {
		args = append(args, "--request", requestID)
	}
	return args
}
func environment(name string) string {
	switch name {
	case "OVERLAID_CONDUIT_DATABASE_URL":
		return private + "-ledger"
	case "OVERLAID_CONDUIT_EXECUTION_DATABASE_URL":
		return private + "-custody"
	case "OVERLAID_CONDUIT_VAULT_ACTIVE_KEY":
		return "v1"
	case "OVERLAID_CONDUIT_VAULT_KEYS":
		return "v1:" + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("1", 32)))
	}
	return ""
}
func intent(state string) conduitownership.Intent {
	now := time.Date(2026, 10, 20, 1, 0, 0, 0, time.UTC)
	r := conduitownership.Intent{ID: requestID, Binding: conduitownership.Binding{AppID: appID, ClientID: "Client-A", Revision: 1}, ShardCount: 2, State: state, Principal: "fixture", CreatedAt: now, DeadlineAt: now.Add(5 * time.Minute)}
	switch state {
	case "created":
		r.ConduitID = "owned"
	case "uncertain":
		r.Failure = "uncertain"
	case "refused":
		r.Failure = "provider_refused"
	}
	return r
}

type fakeOperator struct {
	t                        *testing.T
	result                   conduitownership.Intent
	err, closeErr            error
	closed                   bool
	prepare, lookup, execute int
	callback                 func(context.Context)
}

func (f *fakeOperator) step(ctx context.Context) {
	f.t.Helper()
	if f.closed {
		f.t.Fatal("closed operator invoked")
	}
	if _, ok := ctx.Deadline(); !ok {
		f.t.Fatal("unbounded operation")
	}
	if f.callback != nil {
		f.callback(ctx)
	}
}
func (f *fakeOperator) Prepare(ctx context.Context, b conduitownership.Binding, count int) (conduitownership.Intent, error) {
	f.step(ctx)
	f.prepare++
	if b != intent("prepared").Binding || count != 2 {
		f.t.Error("wrong preparation binding")
	}
	return f.result, f.err
}
func (f *fakeOperator) Lookup(ctx context.Context, id string) (conduitownership.Intent, error) {
	f.step(ctx)
	f.lookup++
	if id != requestID {
		f.t.Error("wrong lookup ID")
	}
	return f.result, f.err
}
func (f *fakeOperator) Execute(ctx context.Context, k tenancy.Keyring, b appvault.Binding, id string, count int, _ *http.Client) (conduitownership.Intent, error) {
	f.step(ctx)
	f.execute++
	if k.Active != "v1" || len(k.Keys["v1"]) != 32 || b.AppID != appID || b.ClientID != "Client-A" || b.Revision != 1 || id != requestID || count != 2 {
		f.t.Error("wrong execution selection")
	}
	return f.result, f.err
}
func (f *fakeOperator) Close() error {
	if f.closed {
		f.t.Error("double close")
	}
	f.closed = true
	return f.closeErr
}

func TestOfflineAndInvalidArgumentsNeverReadConfiguration(t *testing.T) {
	never := func(string) string { t.Fatal("unexpected configuration access"); return "" }
	open := func(context.Context, string, string) (operator, error) {
		t.Fatal("unexpected pool access")
		return nil, nil
	}
	for _, args := range [][]string{{"--help"}, {"--version"}, {"prepare", "--help"}, {"status", "-h"}, {"execute", "--help"}} {
		var out, diag bytes.Buffer
		if code := run(t.Context(), args, never, &out, &diag, open, nil); code != 0 || out.Len() == 0 || diag.Len() != 0 {
			t.Fatal(args, code, diag.String())
		}
	}
	bad := [][]string{nil, {"unknown"}, {"prepare"}, {"status"}, {"execute"}, {"status", "--request", strings.ToUpper("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")}, {"prepare", "--help", private}, {"--version", private}}
	for _, command := range []string{"prepare", "status", "execute"} {
		for _, tail := range [][]string{{"positional"}, {"--token", private}, {"--provider-url", private}, {"--request", requestID}, {"--client", "Client-A"}, {"--", private}} {
			bad = append(bad, append(arguments(command), tail...))
		}
	}
	for _, replacement := range []struct {
		index int
		value string
	}{{2, "bad"}, {4, "bad/client"}, {6, "0"}, {6, "9223372036854775808"}, {8, "0"}, {8, "20001"}} {
		a := arguments("prepare")
		a[replacement.index] = replacement.value
		bad = append(bad, a)
	}
	bad = append(bad, []string{"status", "--request", requestID, "--shards", "2"}, []string{"prepare", "--app", appID, "--client", "Client-A", "--revision", "1"})
	for _, args := range bad {
		var out, diag bytes.Buffer
		code := run(t.Context(), args, never, &out, &diag, open, nil)
		if code != 2 || out.Len() != 0 || strings.Contains(diag.String(), private) {
			t.Fatal(args, code, diag.String())
		}
	}
}

func TestModeSpecificConfigurationAndDispatch(t *testing.T) {
	for _, command := range []string{"prepare", "status", "execute"} {
		t.Run(command, func(t *testing.T) {
			state := "created"
			if command == "prepare" {
				state = "prepared"
			}
			f := &fakeOperator{t: t, result: intent(state)}
			env := func(name string) string {
				if command != "execute" && name != "OVERLAID_CONDUIT_DATABASE_URL" {
					t.Fatal("provider/key/custody config on metadata command", name)
				}
				return environment(name)
			}
			open := func(ctx context.Context, ledger, custody string) (operator, error) {
				if ledger != environment("OVERLAID_CONDUIT_DATABASE_URL") {
					t.Error("wrong ledger config")
				}
				if command != "execute" && custody != "" || command == "execute" && custody != environment("OVERLAID_CONDUIT_EXECUTION_DATABASE_URL") {
					t.Error("wrong custody config")
				}
				deadline, ok := ctx.Deadline()
				limit := 5 * time.Second
				if command == "execute" {
					limit = 20 * time.Second
				}
				if !ok || time.Until(deadline) > limit {
					t.Error("wrong operation bound")
				}
				return f, nil
			}
			var out, diag bytes.Buffer
			code := run(t.Context(), arguments(command), env, &out, &diag, open, nil)
			if code != 0 || !f.closed || diag.Len() != 0 || !json.Valid(out.Bytes()) {
				t.Fatal(code, diag.String())
			}
			if command == "prepare" && f.prepare != 1 || command == "status" && f.lookup != 1 || command == "execute" && f.execute != 1 || f.prepare+f.lookup+f.execute != 1 {
				t.Fatal("wrong command dispatch")
			}
		})
	}
}

func TestConfigurationRefusalPrecedesPools(t *testing.T) {
	for _, name := range []string{"OVERLAID_CONDUIT_DATABASE_URL", "OVERLAID_CONDUIT_EXECUTION_DATABASE_URL", "OVERLAID_CONDUIT_VAULT_ACTIVE_KEY", "OVERLAID_CONDUIT_VAULT_KEYS"} {
		var out, diag bytes.Buffer
		env := func(key string) string {
			if key == name {
				return ""
			}
			return environment(key)
		}
		code := run(t.Context(), arguments("execute"), env, &out, &diag, func(context.Context, string, string) (operator, error) {
			t.Fatal("invalid configuration opened pools")
			return nil, nil
		}, nil)
		if code != 2 || out.Len() != 0 || strings.Contains(diag.String(), private) {
			t.Fatal(code, diag.String())
		}
	}
}

func TestStatusUnresolvedAndFailureMetadata(t *testing.T) {
	for _, state := range []string{"prepared", "started", "uncertain", "refused", "created"} {
		f := &fakeOperator{t: t, result: intent(state)}
		var out, diag bytes.Buffer
		if code := run(t.Context(), arguments("status"), environment, &out, &diag, func(context.Context, string, string) (operator, error) { return f, nil }, nil); code != 0 || f.execute != 0 || f.prepare != 0 || out.Len() == 0 || diag.Len() != 0 {
			t.Fatal(state, code, diag.String())
		}
	}
	f := &fakeOperator{t: t, result: intent("uncertain"), err: errors.New(private)}
	var out, diag bytes.Buffer
	if code := run(t.Context(), arguments("execute"), environment, &out, &diag, func(context.Context, string, string) (operator, error) { return f, nil }, nil); code != 1 || f.execute != 1 || out.Len() == 0 || strings.Contains(diag.String(), private) {
		t.Fatal(code, diag.String())
	}
	// Changed local custody must retain the original unresolved client owner.
	f = &fakeOperator{t: t, result: intent("uncertain"), err: conduitownership.ErrUncertain}
	f.result.Binding.Revision = 8
	out.Reset()
	diag.Reset()
	if code := run(t.Context(), arguments("prepare"), environment, &out, &diag, func(context.Context, string, string) (operator, error) { return f, nil }, nil); code != 1 || out.Len() == 0 {
		t.Fatal(code, diag.String())
	}
}

type checkedWriter struct {
	check func()
	short bool
	err   error
}

func (w checkedWriter) Write(p []byte) (int, error) {
	if w.check != nil {
		w.check()
	}
	if w.short {
		return len(p) - 1, nil
	}
	if w.err != nil {
		return 0, w.err
	}
	return len(p), nil
}

func TestCloseBeforeOutputAndFailureRedaction(t *testing.T) {
	for _, kind := range []string{"good", "close", "write", "short", "open"} {
		t.Run(kind, func(t *testing.T) {
			f := &fakeOperator{t: t, result: intent("created")}
			var diag bytes.Buffer
			writer := checkedWriter{check: func() {
				if !f.closed {
					t.Fatal("output before close")
				}
			}}
			if kind == "close" {
				f.closeErr = errors.New(private)
				writer.check = func() { t.Fatal("close failure output") }
			}
			if kind == "write" {
				writer.err = errors.New(private)
			}
			if kind == "short" {
				writer.short = true
			}
			open := func(context.Context, string, string) (operator, error) {
				if kind == "open" {
					return f, errors.New(private)
				}
				return f, nil
			}
			code := run(t.Context(), arguments("execute"), environment, writer, &diag, open, nil)
			if kind == "good" && code != 0 || kind != "good" && code != 1 || !f.closed || strings.Contains(diag.String(), private) {
				t.Fatal(kind, code, diag.String())
			}
		})
	}
}

func TestMalformedOrMismatchedMetadataIsNotOutput(t *testing.T) {
	for _, mutate := range []func(*conduitownership.Intent){
		func(r *conduitownership.Intent) { r.ID = appID }, func(r *conduitownership.Intent) { r.Binding.ClientID = "other" }, func(r *conduitownership.Intent) { r.ShardCount = 3 },
		func(r *conduitownership.Intent) { r.State = "PRIVATE-state" }, func(r *conduitownership.Intent) { r.Failure = private }, func(r *conduitownership.Intent) { r.ConduitID = "https://PRIVATE" },
		func(r *conduitownership.Intent) { r.DeadlineAt = r.CreatedAt }, func(r *conduitownership.Intent) { r.Principal = "" }, func(r *conduitownership.Intent) { r.State = "uncertain"; r.ConduitID = ""; r.Failure = "uncertain" },
	} {
		f := &fakeOperator{t: t, result: intent("created")}
		mutate(&f.result)
		var out, diag bytes.Buffer
		if code := run(t.Context(), arguments("execute"), environment, &out, &diag, func(context.Context, string, string) (operator, error) { return f, nil }, nil); code != 1 || out.Len() != 0 || strings.Contains(diag.String(), private) {
			t.Fatal(code, out.String(), diag.String())
		}
	}
}

func TestCancellationAndOfflineOutputFailures(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var out, diag bytes.Buffer
	if code := run(ctx, arguments("execute"), func(string) string { t.Fatal("canceled command accessed config"); return "" }, &out, &diag, func(context.Context, string, string) (operator, error) {
		t.Fatal("opened on cancellation")
		return nil, nil
	}, nil); code != 130 || out.Len() != 0 {
		t.Fatal(code)
	}
	ctx, cancel = context.WithCancel(t.Context())
	defer cancel()
	f := &fakeOperator{t: t, result: intent("uncertain"), err: context.Canceled, callback: func(context.Context) { cancel() }}
	out.Reset()
	diag.Reset()
	if code := run(ctx, arguments("execute"), environment, &out, &diag, func(context.Context, string, string) (operator, error) { return f, nil }, nil); code != 130 || out.Len() == 0 || !f.closed {
		t.Fatal(code)
	}
	for _, args := range [][]string{{"--help"}, {"--version"}} {
		if code := run(t.Context(), args, environment, checkedWriter{short: true}, io.Discard, nil, nil); code != 1 {
			t.Fatal(code)
		}
	}
}
