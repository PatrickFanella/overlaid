// overlaid-conduit-create explicitly prepares, reads or executes original intents.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/appvault"
	"git.subcult.tv/PatrickFanella/over-laid/internal/buildinfo"
	"git.subcult.tv/PatrickFanella/over-laid/internal/conduitownership"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitchconduit"
)

const usage = `Usage: overlaid-conduit-create COMMAND [flags]
Commands:
  prepare --app UUID --client ID --revision N --shards N
  status --request UUID
  execute --request UUID --app UUID --client ID --revision N --shards N

Examples (replace the UUIDs with exact observed app/request identities):
  overlaid-conduit-create prepare --app 11111111-1111-4111-8111-111111111111 --client app-client --revision 1 --shards 1
  overlaid-conduit-create status --request 22222222-2222-4222-8222-222222222222
  overlaid-conduit-create execute --request 22222222-2222-4222-8222-222222222222 --app 11111111-1111-4111-8111-111111111111 --client app-client --revision 1 --shards 1

Prepare/status make no provider request. Save the original request ID before
execution. Never repeat or replace unresolved work. Status lookup never creates
missing work. Revision is the exact positive observed custody revision; shards
must be 1..20000. Execution makes at most one retained creation attempt.

Private environment (credentials are never accepted as flags):
  OVERLAID_CONDUIT_DATABASE_URL          Dedicated metadata-only ledger login
Execute additionally requires:
  OVERLAID_CONDUIT_EXECUTION_DATABASE_URL Separate restricted token-reader login
  OVERLAID_CONDUIT_VAULT_ACTIVE_KEY      Retained key version
  OVERLAID_CONDUIT_VAULT_KEYS            Comma-separated version:base64 AES-256 keys

stdout contains one intent metadata object when a validated original result is
known, including on nonzero operation status. Diagnostics are fixed on stderr.
Exit: 0 prepared/existing-created, successful status lookup (even unresolved), or
created execution; 1 refused/unavailable/unresolved/output; 2 invalid arguments
or private configuration; 130 interrupted. Nonzero output status invalidates
partial bytes. Metadata on failure grants no retry or effect permission.
All pools close before JSON. A blocked output sink is outside operation bounds.
Prepare/status have a five-second cooperative combined open/operation bound;
execute has twenty seconds plus at most five seconds detached failure bookkeeping.
No prompts, automatic retries, credential refresh/acquisition, ownership clearing,
API-image packaging or application startup activation. --help, COMMAND --help
and --version work offline.
`

type operator interface {
	Prepare(context.Context, conduitownership.Binding, int) (conduitownership.Intent, error)
	Lookup(context.Context, string) (conduitownership.Intent, error)
	Execute(context.Context, tenancy.Keyring, appvault.Binding, string, int, *http.Client) (conduitownership.Intent, error)
	Close() error
}
type opener func(context.Context, string, string) (operator, error)
type nativeOperator struct {
	ledger    *conduitownership.Store
	execution *appvault.ExecutionStore
}

func openOperator(ctx context.Context, ledgerDSN, custodyDSN string) (operator, error) {
	l, err := conduitownership.Open(ctx, ledgerDSN)
	if err != nil {
		return nil, err
	}
	n := &nativeOperator{ledger: l}
	if custodyDSN != "" {
		n.execution, err = appvault.OpenExecution(ctx, custodyDSN)
		if err != nil {
			_ = n.Close()
			return nil, err
		}
	}
	return n, nil
}
func (n *nativeOperator) Prepare(ctx context.Context, b conduitownership.Binding, count int) (conduitownership.Intent, error) {
	return n.ledger.Prepare(ctx, b, count)
}
func (n *nativeOperator) Lookup(ctx context.Context, id string) (conduitownership.Intent, error) {
	return n.ledger.Lookup(ctx, id)
}
func (n *nativeOperator) Execute(ctx context.Context, k tenancy.Keyring, b appvault.Binding, id string, count int, c *http.Client) (conduitownership.Intent, error) {
	if n.execution == nil {
		return conduitownership.Intent{}, appvault.ErrBoundary
	}
	return n.execution.CreateConduit(ctx, k, b, id, count, n.ledger, c)
}
func (n *nativeOperator) Close() error {
	if n == nil {
		return nil
	}
	var first, second error
	if n.execution != nil {
		first = n.execution.Close()
	}
	if n.ledger != nil {
		second = n.ledger.Close()
	}
	return errors.Join(first, second)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr, openOperator, nil)
	stop()
	os.Exit(code)
}
func validID(s string) bool { return tenantdb.ValidID(s) && s == strings.ToLower(s) }
func atom(s string) bool {
	if len(s) < 1 || len(s) > 128 {
		return false
	}
	for _, c := range []byte(s) {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' {
			continue
		}
		return false
	}
	return true
}
func validIntent(r conduitownership.Intent) bool {
	if !validID(r.ID) || !validID(r.Binding.AppID) || !atom(r.Binding.ClientID) || r.Binding.Revision < 1 || r.ShardCount < 1 || r.ShardCount > 20000 || r.CreatedAt.IsZero() || r.DeadlineAt.Sub(r.CreatedAt) != 5*time.Minute || len(r.Principal) < 1 || len(r.Principal) > 63 {
		return false
	}
	for _, c := range r.Principal {
		if c < 32 || c == 127 {
			return false
		}
	}
	switch r.State {
	case "prepared", "started":
		return r.Failure == "" && r.ConduitID == ""
	case "created":
		return r.Failure == "" && atom(r.ConduitID)
	case "uncertain":
		return r.Failure == "uncertain" && r.ConduitID == ""
	case "refused":
		return r.ConduitID == "" && (r.Failure == "expired" || r.Failure == "not_started" || r.Failure == "provider_refused")
	}
	return false
}
func run(ctx context.Context, args []string, env func(string) string, out, diagnostic io.Writer, open opener, client *http.Client) int {
	invalid := func() int {
		_, _ = fmt.Fprintln(diagnostic, "Invalid arguments; use overlaid-conduit-create --help.")
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
	commandKnown := func(s string) bool { return s == "prepare" || s == "status" || s == "execute" }
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") || len(args) == 2 && commandKnown(args[0]) && (args[1] == "--help" || args[1] == "-h") {
		n, err := io.WriteString(out, usage)
		if err != nil || n != len(usage) {
			return 1
		}
		return 0
	}
	if len(args) == 1 && args[0] == "--version" {
		info := buildinfo.Report()
		info.Name = "overlaid-conduit-create"
		if !write(info) {
			return 1
		}
		return 0
	}
	if len(args) == 0 || !commandKnown(args[0]) {
		return invalid()
	}
	command := args[0]
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	id := ""
	b := appvault.Binding{}
	shards := 0
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
	if command != "prepare" {
		add("request", func(s string) error { id = s; return nil })
	}
	if command != "status" {
		add("app", func(s string) error { b.AppID = s; return nil })
		add("client", func(s string) error { b.ClientID = s; return nil })
		add("revision", func(s string) error { var err error; b.Revision, err = strconv.ParseInt(s, 10, 64); return err })
		add("shards", func(s string) error { n, err := strconv.ParseInt(s, 10, 32); shards = int(n); return err })
	}
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
		return invalid()
	}
	if command != "prepare" && (!seen["request"] || !validID(id)) {
		return invalid()
	}
	if command != "status" {
		if !seen["app"] || !seen["client"] || !seen["revision"] || !seen["shards"] || !validID(b.AppID) || b.Revision < 1 || shards < 1 || shards > 20000 {
			return invalid()
		}
		if _, err := twitchconduit.NewCreator(twitchconduit.CreationConfig{ClientID: b.ClientID, Token: func(context.Context) (string, error) { return "", nil }}); err != nil {
			return invalid()
		}
	}
	report := func() int {
		if ctx.Err() != nil {
			_, _ = fmt.Fprintln(diagnostic, "Conduit operation interrupted; retain the original request ID and inspect status. Never repeat or replace unresolved work.")
			return 130
		}
		_, _ = fmt.Fprintln(diagnostic, "Conduit operation refused, unavailable or unresolved; retain any original request ID and inspect status. Never repeat or replace unresolved work.")
		return 1
	}
	if ctx.Err() != nil {
		return report()
	}
	ledgerDSN := env("OVERLAID_CONDUIT_DATABASE_URL")
	custodyDSN := ""
	keys := tenancy.Keyring{}
	var err error
	if command == "execute" {
		custodyDSN = env("OVERLAID_CONDUIT_EXECUTION_DATABASE_URL")
		keys, err = tenancy.ParseKeyring(env("OVERLAID_CONDUIT_VAULT_ACTIVE_KEY"), env("OVERLAID_CONDUIT_VAULT_KEYS"))
	}
	if strings.TrimSpace(ledgerDSN) == "" || err != nil || command == "execute" && strings.TrimSpace(custodyDSN) == "" {
		_, _ = fmt.Fprintln(diagnostic, "Valid dedicated private conduit configuration is required; use --help.")
		return 2
	}
	duration := 5 * time.Second
	if command == "execute" {
		duration = 20 * time.Second
	}
	bound, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	service, err := open(bound, ledgerDSN, custodyDSN)
	if err != nil || service == nil {
		if service != nil {
			_ = service.Close()
		}
		return report()
	}
	var result conduitownership.Intent
	switch command {
	case "prepare":
		result, err = service.Prepare(bound, conduitownership.Binding{AppID: b.AppID, ClientID: b.ClientID, Revision: b.Revision}, shards)
	case "status":
		result, err = service.Lookup(bound, id)
	case "execute":
		result, err = service.Execute(bound, keys, b, id, shards, client)
	}
	closeErr := service.Close()
	known := validIntent(result)
	if known && command != "prepare" {
		known = result.ID == id
	}
	if known && command != "status" {
		wanted := conduitownership.Binding{AppID: b.AppID, ClientID: b.ClientID, Revision: b.Revision}
		known = result.Binding == wanted && result.ShardCount == shards
		// Prepare may disclose the original unresolved owner for this exact
		// client after a changed local credential binding. It grants no retry.
		if command == "prepare" && err != nil && (result.State == "started" || result.State == "uncertain") && result.Binding.ClientID == b.ClientID {
			known = true
		}
	}
	if !known {
		result = conduitownership.Intent{}
		if err == nil {
			err = conduitownership.ErrUnavailable
		}
	}
	if err == nil && (command == "execute" && result.State != "created" || command == "prepare" && result.State != "prepared" && result.State != "created") {
		err = conduitownership.ErrUnavailable
		result = conduitownership.Intent{}
	}
	code := 0
	if err != nil || closeErr != nil || bound.Err() != nil {
		code = report()
	}
	if closeErr == nil && result.ID != "" {
		if !write(struct {
			Intent conduitownership.Intent `json:"intent"`
		}{result}) {
			_, _ = fmt.Fprintln(diagnostic, "Output unavailable; discard partial bytes and retain the original request ID. Never repeat unresolved work.")
			return 1
		}
	}
	return code
}
