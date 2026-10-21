// overlaid-app-issue explicitly prepares or executes a retained app issuance.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/appvault"
	"git.subcult.tv/PatrickFanella/over-laid/internal/buildinfo"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitchconduit"
)

type issuer interface {
	PrepareIssuance(context.Context, string, string, int64) (appvault.Issuance, error)
	Issue(context.Context, tenancy.Keyring, string, string, string, int64, *http.Client) (appvault.Issuance, error)
	Close() error
}
type opener func(context.Context, string) (issuer, error)

const usage = `Usage: overlaid-app-issue COMMAND [flags]
Commands:
  prepare --client ID --revision N
  execute --request UUID --client ID --revision N

Revision is 0 for a new client or the exact observed custody revision.
Prepare makes no provider request; save its original request ID before execution.
Execute makes at most one retained acquisition attempt. Never replace/repeat an
unresolved request. Read status with overlaid-app-admin issuance --request UUID.

Private environment (never pass credentials as flags):
  OVERLAID_APP_ISSUANCE_DATABASE_URL      Dedicated restricted app-admin login
  OVERLAID_APP_ISSUANCE_CLIENT_SECRET     Exact app client secret
Execute additionally requires:
  OVERLAID_APP_ISSUANCE_VAULT_ACTIVE_KEY  Active version
  OVERLAID_APP_ISSUANCE_VAULT_KEYS        Comma-separated version:base64 AES-256 keys

stdout contains one issuance metadata object when the original result is known,
including on nonzero operation status. Diagnostics are fixed text on stderr.
Exit: 0 prepared/issued result, 1 refused/unavailable/unresolved, 2 invalid input
or private configuration, 130 interrupted. Nonzero output status invalidates any
partial bytes. Metadata on failure grants no retry or effect permission.
The pool closes before output; a blocked output sink is outside operation bounds.
Prepare has a five-second cooperative open/operation bound. Execute has twenty
seconds, plus at most five seconds of detached failure bookkeeping on cancellation.
No automatic retry, refresh, unresolved-state clearing or startup activation.
--help, COMMAND --help and --version work offline.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr, func(ctx context.Context, dsn string) (issuer, error) { return appvault.Open(ctx, dsn) }, nil)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, env func(string) string, out, diagnostic io.Writer, open opener, client *http.Client) int {
	invalid := func() int {
		_, _ = fmt.Fprintln(diagnostic, "Invalid arguments; use overlaid-app-issue --help.")
		return 2
	}
	write := func(value any) bool {
		raw, err := json.Marshal(value)
		if err != nil {
			return false
		}
		raw = append(raw, '\n')
		n, err := out.Write(raw)
		return err == nil && n == len(raw)
	}
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") || len(args) == 2 && (args[0] == "prepare" || args[0] == "execute") && (args[1] == "--help" || args[1] == "-h") {
		n, err := io.WriteString(out, usage)
		if err != nil || n != len(usage) {
			return 1
		}
		return 0
	}
	if len(args) == 1 && args[0] == "--version" {
		info := buildinfo.Report()
		info.Name = "overlaid-app-issue"
		if !write(info) {
			return 1
		}
		return 0
	}
	if len(args) == 0 || args[0] != "prepare" && args[0] != "execute" {
		return invalid()
	}
	command := args[0]
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	id, clientID := "", ""
	revision := int64(-1)
	seen := map[string]bool{}
	add := func(name string, set func(string) error) {
		flags.Func(name, "", func(value string) error {
			if seen[name] {
				return errors.New("duplicate flag")
			}
			seen[name] = true
			return set(value)
		})
	}
	add("client", func(value string) error { clientID = value; return nil })
	add("revision", func(value string) error { var err error; revision, err = strconv.ParseInt(value, 10, 64); return err })
	if command == "execute" {
		add("request", func(value string) error { id = value; return nil })
	}
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || !seen["client"] || !seen["revision"] || revision < 0 || revision == math.MaxInt64 || command == "execute" && (!seen["request"] || !tenantdb.ValidID(id) || id != strings.ToLower(id)) {
		return invalid()
	}
	if _, err := twitchconduit.NewAppAuthorizer(twitchconduit.AppAuthorizationConfig{ClientID: clientID, ClientSecret: func(context.Context) (string, error) { return "", nil }}); err != nil {
		return invalid()
	}
	report := func() int {
		if ctx.Err() != nil {
			_, _ = fmt.Fprintln(diagnostic, "Issuance operation interrupted; retain the original request ID and inspect its status. Never repeat or replace unresolved work.")
			return 130
		}
		_, _ = fmt.Fprintln(diagnostic, "Issuance refused, unavailable or unresolved; retain any original request ID and inspect its status. Never repeat or replace unresolved work.")
		return 1
	}
	if ctx.Err() != nil {
		return report()
	}
	dsn, secret := env("OVERLAID_APP_ISSUANCE_DATABASE_URL"), env("OVERLAID_APP_ISSUANCE_CLIENT_SECRET")
	validSecret := len(secret) > 0 && len(secret) <= 4096
	for _, b := range []byte(secret) {
		if b < 33 || b > 126 {
			validSecret = false
		}
	}
	keys := tenancy.Keyring{}
	var err error
	if command == "execute" {
		keys, err = tenancy.ParseKeyring(env("OVERLAID_APP_ISSUANCE_VAULT_ACTIVE_KEY"), env("OVERLAID_APP_ISSUANCE_VAULT_KEYS"))
	}
	if strings.TrimSpace(dsn) == "" || !validSecret || err != nil {
		_, _ = fmt.Fprintln(diagnostic, "Valid dedicated private issuance configuration is required; use --help.")
		return 2
	}
	duration := 5 * time.Second
	if command == "execute" {
		duration = 20 * time.Second
	}
	bound, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	store, err := open(bound, dsn)
	if err != nil {
		if store != nil {
			_ = store.Close()
		}
		return report()
	}
	if store == nil {
		return report()
	}
	var result appvault.Issuance
	if command == "prepare" {
		result, err = store.PrepareIssuance(bound, clientID, secret, revision)
	} else {
		result, err = store.Issue(bound, keys, id, clientID, secret, revision, client)
	}
	closeErr := store.Close()
	if err == nil && (result.ID == "" || result.ClientID != clientID || result.ExpectedRevision != revision || command == "prepare" && result.State != "prepared" || command == "execute" && (result.ID != id || result.State != "issued")) {
		err = appvault.ErrUnavailable
		result = appvault.Issuance{}
	}
	code := 0
	if err != nil || closeErr != nil || bound.Err() != nil {
		code = report()
	}
	// Original metadata is useful for reconciliation even with a nonzero status.
	// A pool-close failure suppresses output because completion is unresolved.
	if closeErr == nil && result.ID != "" {
		if !write(struct {
			Issuance appvault.Issuance `json:"issuance"`
		}{result}) {
			_, _ = fmt.Fprintln(diagnostic, "Output unavailable; discard partial bytes and retain the original request ID. Never repeat unresolved work.")
			return 1
		}
	}
	return code
}
