package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestReadinessProbeReportContracts(t *testing.T) {
	for _, tc := range []struct {
		name, body, checks string
		code               int
		message            string
	}{
		{"healthy", `{"status":"ok","checks":[{"name":"process","status":"ok"}]}`, "", 0, "all 1 checks"},
		{"degraded_json_200", `{"status":"degraded","checks":[{"name":"process","status":"ok"},{"name":"twitch_auth","status":"degraded","hint":"private content"}]}`, "", 1, "twitch_auth"},
		{"selected_local_checks", `{"status":"degraded","checks":[{"name":"process","status":"ok"},{"name":"postgres","status":"ok"},{"name":"twitch_auth","status":"degraded"}]}`, "postgres, process,postgres", 0, "postgres, process"},
		{"selected_failed", `{"status":"degraded","checks":[{"name":"postgres","status":"degraded"}]}`, "postgres", 1, "postgres"},
		{"missing_check", `{"status":"ok","checks":[{"name":"process","status":"ok"}]}`, "postgres", 1, "postgres (missing)"},
		{"contradictory_ok", `{"status":"ok","checks":[{"name":"postgres","status":"degraded"}]}`, "", 1, "postgres"},
		{"overall_degraded", `{"status":"degraded","checks":[{"name":"process","status":"ok"}]}`, "", 1, "Overall readiness is degraded"},
		{"login_html", `<html>private content</html>`, "", 1, "invalid report"},
		{"empty", `{}`, "", 1, "invalid report"},
		{"duplicate", `{"status":"ok","checks":[{"name":"process","status":"ok"},{"name":"process","status":"ok"}]}`, "process", 1, "invalid report"},
		{"invalid_status", `{"status":"ok","checks":[{"name":"process","status":"unknown"}]}`, "", 1, "invalid report"},
		{"unsafe_name", `{"status":"ok","checks":[{"name":"private content","status":"degraded"}]}`, "", 1, "invalid report"},
		{"trailing_json", `{"status":"ok","checks":[{"name":"process","status":"ok"}]} {}`, "", 1, "invalid report"},
		{"oversized", strings.Repeat(" ", maxReportBytes+1), "", 1, "exceeds 64 KiB"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/readyz" || r.Header.Get("Authorization") != "" {
					t.Fatalf("unexpected probe request: %s %s", r.Method, r.URL)
				}
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			var stdout, stderr bytes.Buffer
			code := run(t.Context(), []string{"--url", server.URL + "/readyz", "--checks", tc.checks}, &stdout, &stderr)
			if code != tc.code || !strings.Contains(stderr.String(), tc.message) || stdout.Len() != 0 {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if strings.Contains(stderr.String(), "private content") {
				t.Fatal("untrusted readiness content printed")
			}
		})
	}
}

func TestProbeRejectsHTTPFailuresAndRedirects(t *testing.T) {
	for _, status := range []int{http.StatusServiceUnavailable, http.StatusFound} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Location", "/login")
				w.WriteHeader(status)
				fmt.Fprint(w, "private error content")
			}))
			defer server.Close()
			var out, errout bytes.Buffer
			code := run(t.Context(), []string{"--url", server.URL + "/readyz"}, &out, &errout)
			if code != 1 || calls != 1 || !strings.Contains(errout.String(), fmt.Sprint(status)) || strings.Contains(errout.String(), "private error content") {
				t.Fatalf("code=%d calls=%d stderr=%q", code, calls, errout.String())
			}
		})
	}
}

func TestProbeCancellationAndResponseTimeout(t *testing.T) {
	for _, flushHeaders := range []bool{false, true} {
		t.Run(fmt.Sprint(flushHeaders), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if flushHeaders {
					w.WriteHeader(http.StatusOK)
					w.(http.Flusher).Flush()
				}
				<-r.Context().Done()
			}))
			defer server.Close()
			var out, errout bytes.Buffer
			started := time.Now()
			code := run(t.Context(), []string{"--url", server.URL, "--timeout", "20ms"}, &out, &errout)
			if code != 1 || !strings.Contains(errout.String(), "timed out") || time.Since(started) > time.Second {
				t.Fatalf("unbounded timeout: code=%d stderr=%q", code, errout.String())
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			errout.Reset()
			code = run(ctx, []string{"--url", server.URL}, &out, &errout)
			if code != 130 || !strings.Contains(errout.String(), "interrupted") {
				t.Fatalf("cancel: code=%d stderr=%q", code, errout.String())
			}
		})
	}
}

func TestProbeArgumentAndInformationalModes(t *testing.T) {
	for _, args := range [][]string{
		{"--url", "file:///private"}, {"--url", "http://user:private@localhost/readyz"}, {"--url", "http://localhost/readyz#fragment"},
		{"--timeout", "0s"}, {"--checks", "process,,postgres"}, {"--checks", "PROCESS"}, {"--completion", "unknown"}, {"unexpected"},
	} {
		var out, errout bytes.Buffer
		if code := run(t.Context(), args, &out, &errout); code != 2 || out.Len() != 0 || errout.Len() == 0 {
			t.Fatalf("invalid flags=%v code=%d stderr=%q", args, code, errout.String())
		}
	}
	for _, args := range [][]string{{"--help"}, {"--version"}, {"--completion", "bash"}, {"--completion", "zsh"}, {"--completion", "fish"}} {
		var out, errout bytes.Buffer
		if code := run(t.Context(), args, &out, &errout); code != 0 || !strings.Contains(out.String()+errout.String(), "over-laid-probe") {
			t.Fatalf("info flags=%v code=%d stdout=%q stderr=%q", args, code, out.String(), errout.String())
		}
	}
}
