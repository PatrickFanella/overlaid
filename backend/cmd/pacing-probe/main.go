package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/chatpacing"
)

type inspector interface {
	Inspect(context.Context, string, string, time.Time) (chatpacing.Report, error)
	Close() error
}
type openInspector func(context.Context, string) (inspector, error)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr, func(ctx context.Context, dsn string) (inspector, error) { return chatpacing.Open(ctx, dsn) })
	stop()
	os.Exit(code)
}
func run(ctx context.Context, args []string, env func(string) string, out, diagnostic io.Writer, open openInspector) int {
	flags := flag.NewFlagSet("overlaid-pacing-probe", flag.ContinueOnError)
	flags.SetOutput(diagnostic)
	sender := flags.String("sender", "", "numeric Twitch sender ID")
	channel := flags.String("channel", "", "numeric Twitch broadcaster ID")
	deadline := flags.String("deadline", "", "original action deadline in RFC3339 (optional)")
	requireReady := flags.Bool("require-ready", false, "exit 1 when the snapshot cannot admit an attempt now")
	timeout := flags.Duration("timeout", 5*time.Second, "maximum duration, greater than zero and at most 5s")
	flags.Usage = func() {
		_, _ = fmt.Fprintln(diagnostic, "Usage: overlaid-pacing-probe --sender ID --channel ID [--deadline RFC3339] [--require-ready]\nRead-only pacing metadata; never reserves a permit or sends.\nRequires private OVERLAID_PACING_DATABASE_URL with the restricted pacing role.\nExit codes: 0 valid report, 1 unavailable/not ready when required, 2 invalid arguments, 130 interrupted.\nExample: overlaid-pacing-probe --sender 123 --channel 456 --require-ready")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 || !numeric(*sender) || !numeric(*channel) || *timeout <= 0 || *timeout > 5*time.Second {
		_, _ = fmt.Fprintln(diagnostic, "Valid --sender, --channel and --timeout are required; use --help.")
		return 2
	}
	var end time.Time
	var err error
	if *deadline != "" {
		end, err = time.Parse(time.RFC3339Nano, *deadline)
		if err != nil {
			_, _ = fmt.Fprintln(diagnostic, "Deadline must be an absolute RFC3339 timestamp.")
			return 2
		}
	}
	dsn := env("OVERLAID_PACING_DATABASE_URL")
	if dsn == "" {
		_, _ = fmt.Fprintln(diagnostic, "Private OVERLAID_PACING_DATABASE_URL is required.")
		return 2
	}
	request, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	failure := func() int {
		if ctx.Err() != nil {
			_, _ = fmt.Fprintln(diagnostic, "Pacing inspection interrupted.")
			return 130
		}
		_, _ = fmt.Fprintln(diagnostic, "Pacing inspection unavailable; no valid readiness report produced.")
		return 1
	}
	s, err := open(request, dsn)
	if err != nil {
		return failure()
	}
	defer func() { _ = s.Close() }()
	report, err := s.Inspect(request, *sender, *channel, end)
	if err != nil {
		return failure()
	}
	if err = json.NewEncoder(out).Encode(report); err != nil {
		return failure()
	}
	if *requireReady && !report.Ready {
		return 1
	}
	return 0
}
func numeric(id string) bool {
	if len(id) < 1 || len(id) > 128 {
		return false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
