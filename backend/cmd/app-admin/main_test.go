package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/appvault"
)

const appID = "00000000-0000-4000-8000-000000000001"
const private = "PRIVATE-DSN-OR-ERROR"

type fakeCustody struct {
	calls, closes, revoke int
	id                    string
	revision              int64
	limit                 int
	fail, closeErr        error
	cancel                context.CancelFunc
	bounded               bool
}

func (f *fakeCustody) check(ctx context.Context) {
	f.calls++
	end, ok := ctx.Deadline()
	f.bounded = ok && time.Until(end) > 0 && time.Until(end) <= 5*time.Second
	if f.cancel != nil {
		f.cancel()
	}
}
func (f *fakeCustody) Accounts(ctx context.Context) ([]appvault.Metadata, error) {
	f.check(ctx)
	return nil, f.fail
}
func (f *fakeCustody) Receipts(ctx context.Context, id string, limit int) ([]appvault.Receipt, error) {
	f.check(ctx)
	f.id = id
	f.limit = limit
	return nil, f.fail
}
func (f *fakeCustody) Revoke(ctx context.Context, id string, revision int64) (appvault.Metadata, error) {
	f.check(ctx)
	f.revoke++
	f.id = id
	f.revision = revision
	return appvault.Metadata{ID: id, ClientID: "client", Revision: revision + 1, Revoked: true}, f.fail
}
func (f *fakeCustody) LookupIssuance(ctx context.Context, id string) (appvault.Issuance, error) {
	f.check(ctx)
	f.id = id
	return appvault.Issuance{ID: id, ClientID: "client", State: "uncertain", Failure: "uncertain"}, f.fail
}
func (f *fakeCustody) Close() error                        { f.closes++; return f.closeErr }
func mustNotOpen(context.Context, string) (custody, error) { panic("unexpected pool") }
func noEnv(string) string                                  { panic("unexpected environment read") }

func TestOfflineHelpVersionAndInvalidInputs(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"list", "--help"}, {"receipts", "--help"}, {"revoke", "-h"}, {"--version"}} {
		var out, diagnostic bytes.Buffer
		if code := run(t.Context(), args, noEnv, &out, &diagnostic, mustNotOpen); code != 0 || out.Len() == 0 || diagnostic.Len() != 0 {
			t.Fatal(args, code, diagnostic.String())
		}
		if args[0] == "--version" {
			var info map[string]any
			if err := json.Unmarshal(out.Bytes(), &info); err != nil || info["name"] != "overlaid-app-admin" {
				t.Fatal(out.String(), err)
			}
		}
	}
	for _, args := range [][]string{nil, {private}, {"--version", private}, {"list", private}, {"list", "--app", appID}, {"receipts"}, {"receipts", "--app", private}, {"receipts", "--app", appID, "--limit", "0"}, {"receipts", "--app", appID, "--limit", "1001"}, {"receipts", "--app", appID, "--limit", private}, {"receipts", "--app", appID, "--revision", "1"}, {"revoke", "--app", appID}, {"revoke", "--app", appID, "--revision", "0"}, {"revoke", "--app", appID, "--revision", "-1"}, {"revoke", "--app", appID, "--revision", "9223372036854775808"}, {"revoke", "--app", appID, "--revision", "1", "--revision", "2"}, {"revoke", "--app", appID, "--app", appID, "--revision", "1"}, {"revoke", "--app", appID, "--revision", "1", "--limit", "2"}, {"list", "--help", private}, {"list", "--dsn", private}} {
		var out, diagnostic bytes.Buffer
		if code := run(t.Context(), args, noEnv, &out, &diagnostic, mustNotOpen); code != 2 || out.Len() != 0 || strings.Contains(diagnostic.String(), private) {
			t.Fatal(args, code, diagnostic.String())
		}
	}
}
func TestCommandSelectionJSONAndPrivateEnvironment(t *testing.T) {
	for _, args := range [][]string{{"list"}, {"receipts", "--app", appID}, {"receipts", "--app", appID, "--limit", "1000"}, {"revoke", "--app", appID, "--revision", "7"}} {
		f := new(fakeCustody)
		opened := 0
		var out, diagnostic bytes.Buffer
		env := func(name string) string {
			if name != "OVERLAID_APP_ADMIN_DATABASE_URL" {
				t.Fatal("unrelated environment", name)
			}
			return private
		}
		code := run(t.Context(), args, env, &out, &diagnostic, func(ctx context.Context, dsn string) (custody, error) {
			opened++
			if dsn != private {
				t.Fatal("DSN changed")
			}
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("unbounded open")
			}
			return f, nil
		})
		if code != 0 || opened != 1 || f.closes != 1 || f.calls != 1 || !f.bounded || diagnostic.Len() != 0 || strings.Contains(out.String(), private) {
			t.Fatal(args, code, f, diagnostic.String())
		}
		var decoded map[string]json.RawMessage
		if err := json.Unmarshal(out.Bytes(), &decoded); err != nil || len(decoded) != 1 {
			t.Fatal(out.String(), err)
		}
		switch args[0] {
		case "list":
			if string(decoded["accounts"]) != "[]" || f.revoke != 0 {
				t.Fatal(out.String(), f)
			}
		case "receipts":
			if string(decoded["receipts"]) != "[]" || f.id != appID || f.limit < 20 || f.revoke != 0 {
				t.Fatal(out.String(), f)
			}
		case "revoke":
			var account appvault.Metadata
			if err := json.Unmarshal(decoded["account"], &account); err != nil || !account.Revoked || account.Revision != 8 || f.id != appID || f.revision != 7 || f.revoke != 1 {
				t.Fatal(out.String(), f, err)
			}
		}
	}
	var out, diagnostic bytes.Buffer
	if code := run(t.Context(), []string{"list"}, func(string) string { return " " }, &out, &diagnostic, mustNotOpen); code != 2 || out.Len() != 0 {
		t.Fatal(code)
	}
}

type failingWriter struct{ short bool }

func (w failingWriter) Write(p []byte) (int, error) {
	if w.short {
		return len(p) - 1, nil
	}
	return 0, errors.New(private)
}
func TestFailureCancellationAndOutputNeverRepeat(t *testing.T) {
	for _, tc := range []struct {
		name           string
		fail, closeErr error
		cancel         bool
		writer         io.Writer
	}{
		{name: "operation", fail: errors.New(private)}, {name: "revision", fail: appvault.ErrRevision}, {name: "close", closeErr: errors.New(private)}, {name: "cancel", cancel: true}, {name: "write", writer: failingWriter{}}, {name: "short-write", writer: failingWriter{short: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			f := &fakeCustody{fail: tc.fail, closeErr: tc.closeErr}
			if tc.cancel {
				f.cancel = cancel
			}
			var out, diagnostic bytes.Buffer
			writer := tc.writer
			if writer == nil {
				writer = &out
			}
			opened := 0
			code := run(ctx, []string{"revoke", "--app", appID, "--revision", "1"}, func(string) string { return private }, writer, &diagnostic, func(context.Context, string) (custody, error) { opened++; return f, nil })
			expected := 1
			if tc.cancel {
				expected = 130
			}
			if code != expected || f.revoke != 1 || f.closes != 1 || opened != 1 || out.Len() != 0 || strings.Contains(diagnostic.String(), private) {
				t.Fatal(code, f, diagnostic.String())
			}
		})
	}
	for _, partial := range []bool{false, true} {
		var out, diagnostic bytes.Buffer
		f := new(fakeCustody)
		code := run(t.Context(), []string{"list"}, func(string) string { return private }, &out, &diagnostic, func(context.Context, string) (custody, error) {
			if partial {
				return f, errors.New(private)
			}
			return nil, errors.New(private)
		})
		expectedClose := 0
		if partial {
			expectedClose = 1
		}
		if code != 1 || f.closes != expectedClose || strings.Contains(diagnostic.String(), private) {
			t.Fatal(code, f, diagnostic.String())
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var out, diagnostic bytes.Buffer
	if code := run(ctx, []string{"list"}, noEnv, &out, &diagnostic, mustNotOpen); code != 130 {
		t.Fatal(code)
	}
}
