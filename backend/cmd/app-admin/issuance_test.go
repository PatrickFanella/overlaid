package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"git.subcult.tv/PatrickFanella/over-laid/internal/appvault"
)

const requestID = "00000000-0000-4000-8000-000000000abc"

func TestIssuanceOfflineSelectionAndInputRefusal(t *testing.T) {
	var out, diagnostic bytes.Buffer
	if code := run(t.Context(), []string{"issuance", "--help"}, noEnv, &out, &diagnostic, mustNotOpen); code != 0 || !strings.Contains(out.String(), "--request UUID") || !strings.Contains(out.String(), "unresolved") {
		t.Fatal(code, out.String())
	}
	for _, args := range [][]string{
		{"issuance"}, {"issuance", "--request", private}, {"issuance", "--request", strings.ToUpper(requestID)},
		{"issuance", "--request", requestID, "--request", requestID}, {"issuance", "--request", requestID, "--app", appID},
		{"issuance", "--request", requestID, "--revision", "1"}, {"issuance", "--request", requestID, "--limit", "1"},
		{"issuance", "--request", requestID, "--token", private}, {"issuance", "--request", requestID, "--help"},
		{"issuance", requestID}, {"issuance", "--request", requestID, private}, {"list", "--request", requestID},
		{"receipts", "--app", appID, "--request", requestID}, {"revoke", "--app", appID, "--revision", "1", "--request", requestID},
	} {
		out.Reset()
		diagnostic.Reset()
		if code := run(t.Context(), args, noEnv, &out, &diagnostic, mustNotOpen); code != 2 || out.Len() != 0 || strings.Contains(diagnostic.String(), private) {
			t.Fatal(args, code, diagnostic.String())
		}
	}
	f := new(fakeCustody)
	closed := false
	writer := callbackWriter(func(p []byte) (int, error) {
		if !closed {
			t.Fatal("issuance output before close")
		}
		return out.Write(p)
	})
	out.Reset()
	diagnostic.Reset()
	store := &closeObserved{fakeCustody: f, closed: &closed}
	code := run(t.Context(), []string{"issuance", "--request", requestID}, func(name string) string {
		if name != "OVERLAID_APP_ADMIN_DATABASE_URL" {
			t.Fatal("unexpected configuration", name)
		}
		return private
	}, writer, &diagnostic, func(ctx context.Context, dsn string) (custody, error) {
		if dsn != private {
			t.Fatal("wrong DSN")
		}
		return store, nil
	})
	var result struct {
		Issuance appvault.Issuance `json:"issuance"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil || code != 0 || f.calls != 1 || f.closes != 1 || f.revoke != 0 || f.id != requestID || !f.bounded || result.Issuance.State != "uncertain" || result.Issuance.ID != requestID || diagnostic.Len() != 0 {
		t.Fatal(code, f, out.String(), diagnostic.String(), err)
	}
}

type callbackWriter func([]byte) (int, error)

func (f callbackWriter) Write(p []byte) (int, error) { return f(p) }

type closeObserved struct {
	*fakeCustody
	closed *bool
}

func (s *closeObserved) Close() error { *s.closed = true; return s.fakeCustody.Close() }

func TestIssuanceFailureOutputAndInterruptionKeepOriginalRequest(t *testing.T) {
	for _, name := range []string{"missing", "operation", "close", "cancel", "write", "short"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			f := new(fakeCustody)
			switch name {
			case "missing":
				f.fail = appvault.ErrRevision
			case "operation":
				f.fail = errors.New(private)
			case "close":
				f.closeErr = errors.New(private)
			case "cancel":
				f.cancel = cancel
			}
			var out, diagnostic bytes.Buffer
			writer := callbackWriter(out.Write)
			if name == "write" {
				writer = func([]byte) (int, error) { return 0, errors.New(private) }
			}
			if name == "short" {
				writer = func(p []byte) (int, error) { return len(p) - 1, nil }
			}
			code := run(ctx, []string{"issuance", "--request", requestID}, func(string) string { return private }, writer, &diagnostic, func(context.Context, string) (custody, error) { return f, nil })
			want := 1
			if name == "cancel" {
				want = 130
			}
			if code != want || f.calls != 1 || f.closes != 1 || f.revoke != 0 || out.Len() != 0 || !strings.Contains(diagnostic.String(), "original") || strings.Contains(diagnostic.String(), private) {
				t.Fatal(code, f, diagnostic.String())
			}
		})
	}
}
