package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/chatpacing"
	"git.subcult.tv/PatrickFanella/over-laid/internal/testpostgres"
)

type fakeInspector struct {
	report          chatpacing.Report
	err             error
	closed          bool
	sender, channel string
	deadline        time.Time
}

func (s *fakeInspector) Inspect(ctx context.Context, sender, channel string, deadline time.Time) (chatpacing.Report, error) {
	s.sender = sender
	s.channel = channel
	s.deadline = deadline
	return s.report, s.err
}
func (s *fakeInspector) Close() error { s.closed = true; return nil }
func TestProbeOutputAndExitContract(t *testing.T) {
	for _, ready := range []bool{false, true} {
		for _, required := range []bool{false, true} {
			f := &fakeInspector{report: chatpacing.Report{Ready: ready, Warnings: []string{"low_headroom"}}}
			args := []string{"--sender", "123", "--channel", "456", "--deadline", "2026-10-15T12:00:00Z"}
			if required {
				args = append(args, "--require-ready")
			}
			var out, diagnostic bytes.Buffer
			code := run(t.Context(), args, func(string) string { return "PRIVATE-DSN" }, &out, &diagnostic, func(ctx context.Context, dsn string) (inspector, error) {
				if dsn != "PRIVATE-DSN" {
					t.Error("wrong configuration")
				}
				return f, nil
			})
			expected := 0
			if required && !ready {
				expected = 1
			}
			if code != expected || !f.closed || f.sender != "123" || f.channel != "456" || f.deadline.IsZero() || !json.Valid(out.Bytes()) || diagnostic.Len() != 0 || strings.Contains(out.String(), "PRIVATE") {
				t.Fatal("probe contract", code, out.String(), diagnostic.String())
			}
		}
	}
}
func TestProbeArgumentAndFailureContracts(t *testing.T) {
	for _, args := range [][]string{{}, {"--sender", "invalid", "--channel", "456"}, {"--sender", "123", "--channel", "456", "--timeout", "0s"}, {"--sender", "123", "--channel", "456", "--deadline", "relative"}, {"--sender", "123", "--channel", "456", "extra"}} {
		var out, diagnostic bytes.Buffer
		code := run(t.Context(), args, func(string) string { return "PRIVATE-DSN" }, &out, &diagnostic, func(context.Context, string) (inspector, error) {
			t.Fatal("invalid input opened database")
			return nil, nil
		})
		if code != 2 || out.Len() != 0 || strings.Contains(diagnostic.String(), "PRIVATE") {
			t.Fatal(code, out.String(), diagnostic.String())
		}
	}
	for _, mode := range []string{"open", "inspect", "missing-env", "cancel"} {
		var out, diagnostic bytes.Buffer
		ctx, cancel := context.WithCancel(t.Context())
		if mode == "cancel" {
			cancel()
		}
		defer cancel()
		env := func(string) string {
			if mode == "missing-env" {
				return ""
			}
			return "PRIVATE-DSN"
		}
		code := run(ctx, []string{"--sender", "123", "--channel", "456"}, env, &out, &diagnostic, func(context.Context, string) (inspector, error) {
			if mode == "inspect" {
				return &fakeInspector{err: errors.New("PRIVATE provider failure")}, nil
			}
			return nil, errors.New("PRIVATE connection error")
		})
		expected := 1
		if mode == "missing-env" {
			expected = 2
		}
		if mode == "cancel" {
			expected = 130
		}
		if code != expected || out.Len() != 0 || strings.Contains(diagnostic.String(), "PRIVATE") {
			t.Fatal("failure leaked or was ready", code, diagnostic.String())
		}
	}
}

func TestProbeReadsPostgresWithoutChangingPacing(t *testing.T) {
	f := testpostgres.NewTenantFixture(t, "")
	pacing, err := chatpacing.Open(t.Context(), f.PacerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pacing.Close() }()
	permit, err := pacing.Acquire(t.Context(), "123", "456")
	if err != nil || !permit.Granted {
		t.Fatal(err)
	}
	if err = pacing.Complete(t.Context(), permit.Permit, 42*time.Second); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		var raw string
		if err := f.Admin.QueryRow(`SELECT jsonb_build_array((SELECT jsonb_agg(to_jsonb(a)) FROM chat_pacing_accounts a),(SELECT jsonb_agg(to_jsonb(c)) FROM chat_pacing_channels c))::text`).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		return raw
	}
	before := snapshot()
	var out, diagnostic bytes.Buffer
	code := run(t.Context(), []string{"--sender", "123", "--channel", "456", "--require-ready"}, func(string) string { return f.PacerURL }, &out, &diagnostic, func(ctx context.Context, dsn string) (inspector, error) { return chatpacing.Open(ctx, dsn) })
	var report chatpacing.Report
	if err = json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if code != 1 || report.Ready || report.CooldownWaitMS < 40000 || diagnostic.Len() != 0 || snapshot() != before {
		t.Fatal("real probe failed read-only contract", code, report, diagnostic.String())
	}
}
