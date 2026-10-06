package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/buildinfo"
)

const maxReportBytes = 64 << 10

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("over-laid-probe", flag.ContinueOnError)
	flags.SetOutput(stderr)
	endpoint := flags.String("url", "http://127.0.0.1:8080/readyz", "readiness endpoint URL (HTTP or HTTPS)")
	checks := flags.String("checks", "", "comma-separated required check names; empty requires the entire report to be ready")
	timeout := flags.Duration("timeout", 5*time.Second, "maximum request and response duration")
	version := flags.Bool("version", false, "print build version and exit")
	completion := flags.String("completion", "", "print bash, zsh, or fish shell completions")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: over-laid-probe [flags]\nRead /readyz and return an exit code for monitoring; no credentials or writes.\nExit codes: 0 ready, 1 not ready/request failure, 2 invalid arguments, 130 interrupted.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "Unexpected arguments; use --help for probe flags.")
		return 2
	}
	if *version {
		info := buildinfo.Report()
		fmt.Fprintf(stdout, "over-laid-probe %s (%s)\n", info.Version, info.Commit)
		return 0
	}
	if *completion != "" {
		script, ok := completionScript(*completion)
		if !ok {
			fmt.Fprintln(stderr, "Completion shell must be bash, zsh, or fish.")
			return 2
		}
		fmt.Fprint(stdout, script)
		return 0
	}
	parsed, err := url.Parse(*endpoint)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.Fragment != "" {
		fmt.Fprintln(stderr, "URL must be an absolute HTTP(S) endpoint without embedded credentials or a fragment.")
		return 2
	}
	if *timeout <= 0 {
		fmt.Fprintln(stderr, "Timeout must be greater than zero.")
		return 2
	}
	required, ok := requiredChecks(*checks)
	if !ok {
		fmt.Fprintln(stderr, "Check names must contain only lowercase letters, digits, or underscores.")
		return 2
	}
	requestCtx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		fmt.Fprintln(stderr, "Unable to construct readiness request.")
		return 2
	}
	req.Header.Set("Accept", "application/json")
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return requestFailure(ctx, requestCtx, stderr)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(stderr, "Readiness endpoint returned HTTP %d; check endpoint and proxy configuration.\n", resp.StatusCode)
		return 1
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxReportBytes+1))
	if err != nil {
		return requestFailure(ctx, requestCtx, stderr)
	}
	if len(body) > maxReportBytes {
		fmt.Fprintln(stderr, "Readiness report exceeds 64 KiB.")
		return 1
	}
	var report readinessReport
	if json.Unmarshal(body, &report) != nil || !report.valid() {
		fmt.Fprintln(stderr, "Readiness endpoint returned an invalid report.")
		return 1
	}
	failures := report.failures(required)
	if len(failures) != 0 {
		fmt.Fprintf(stderr, "Readiness failed: %s.\n", strings.Join(failures, ", "))
		return 1
	}
	if len(required) == 0 && report.Status != "ok" {
		fmt.Fprintln(stderr, "Overall readiness is degraded.")
		return 1
	}
	label := strings.Join(required, ", ")
	if label == "" {
		label = fmt.Sprintf("all %d checks", len(report.Checks))
	}
	fmt.Fprintf(stderr, "Readiness ok: %s.\n", label)
	return 0
}

func requestFailure(ctx, requestCtx context.Context, stderr io.Writer) int {
	switch {
	case ctx.Err() != nil:
		fmt.Fprintln(stderr, "Readiness probe interrupted.")
		return 130
	case requestCtx.Err() != nil:
		fmt.Fprintln(stderr, "Readiness request timed out.")
		return 1
	default:
		fmt.Fprintln(stderr, "Readiness request failed; check endpoint reachability and TLS.")
		return 1
	}
}

func requiredChecks(value string) ([]string, bool) {
	names := []string{}
	seen := map[string]bool{}
	if strings.TrimSpace(value) == "" {
		return names, true
	}
	for _, name := range strings.Split(value, ",") {
		name = strings.TrimSpace(name)
		if !safeCheckName(name) {
			return nil, false
		}
		if !seen[name] {
			names = append(names, name)
			seen[name] = true
		}
	}
	sort.Strings(names)
	return names, true
}

func safeCheckName(name string) bool {
	if name == "" || len(name) > 80 {
		return false
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}

type readinessReport struct {
	Status string `json:"status"`
	Checks []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	} `json:"checks"`
}

func (r readinessReport) valid() bool {
	if (r.Status != "ok" && r.Status != "degraded") || len(r.Checks) == 0 {
		return false
	}
	seen := map[string]bool{}
	for _, check := range r.Checks {
		if !safeCheckName(check.Name) || seen[check.Name] || (check.Status != "ok" && check.Status != "degraded") {
			return false
		}
		seen[check.Name] = true
	}
	return true
}

func (r readinessReport) failures(required []string) []string {
	statuses := map[string]string{}
	for _, check := range r.Checks {
		statuses[check.Name] = check.Status
	}
	failures := []string{}
	if len(required) == 0 {
		for name, status := range statuses {
			if status != "ok" {
				failures = append(failures, name)
			}
		}
	} else {
		for _, name := range required {
			status, exists := statuses[name]
			if !exists {
				failures = append(failures, name+" (missing)")
			} else if status != "ok" {
				failures = append(failures, name)
			}
		}
	}
	sort.Strings(failures)
	return failures
}

func completionScript(shell string) (string, bool) {
	switch shell {
	case "bash":
		return "complete -W '--url --checks --timeout --version --completion --help' over-laid-probe\n", true
	case "zsh":
		return "#compdef over-laid-probe\n_arguments '--url[Readiness endpoint]:URL:' '--checks[Required check names]:checks:' '--timeout[Request timeout]:duration:' '--version[Build version]' '--completion[Completion shell]:shell:(bash zsh fish)' '--help[Help]'\n", true
	case "fish":
		return "complete -c over-laid-probe -l url -r -d 'Readiness endpoint'\ncomplete -c over-laid-probe -l checks -r -d 'Required check names'\ncomplete -c over-laid-probe -l timeout -r -d 'Request timeout'\ncomplete -c over-laid-probe -l version -d 'Build version'\ncomplete -c over-laid-probe -l completion -r -a 'bash zsh fish'\ncomplete -c over-laid-probe -l help -d 'Help'\n", true
	default:
		return "", false
	}
}
