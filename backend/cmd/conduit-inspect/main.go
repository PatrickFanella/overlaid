// overlaid-conduit-inspect performs one explicit custodied conduit observation.
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
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitchconduit"
)

type inspection interface {
	InspectConduit(context.Context, tenancy.Keyring, appvault.Binding, string, *http.Client) (twitchconduit.Observation, error)
	Close() error
}
type opener func(context.Context, string) (inspection, error)
type inventoryInspection interface {
	InventoryConduits(context.Context, tenancy.Keyring, appvault.Binding, *http.Client) (twitchconduit.InventoryObservation, error)
}

const usage = `Usage: overlaid-conduit-inspect --app UUID --client ID --revision N --conduit ID
       overlaid-conduit-inspect --app UUID --client ID --revision N --inventory

Read one selected conduit through separately restricted service-app custody.
Use the exact lowercase app UUID, client ID and positive stored revision from
overlaid-app-admin list, and an explicit conduit ID. There is no automatic selection.
Use --inventory instead of --conduit for a complete app-wide ID/count observation.
Empty inventory is valid. Inventory never identifies an unknown creation request,
clears uncertainty, selects/adopts ownership or grants retry/effect permission.

Private environment (required; never pass credentials as flags):
  OVERLAID_APP_EXECUTION_DATABASE_URL      Dedicated restricted execution login
  OVERLAID_APP_EXECUTION_VAULT_ACTIVE_KEY  Active key version
  OVERLAID_APP_EXECUTION_VAULT_KEYS        version:base64-key entries, comma separated
Retain the stored credential's AES-256 key version in the keyring.

Successful stdout is one JSON object containing binding and observation.
Inventory mode returns binding and inventory instead, without shard/session reads.
Diagnostics go to stderr. Exit: 0 complete observation, 1 refused/unavailable,
2 invalid input/configuration, 130 interrupted. No automatic retries.
One ten-second cooperative bound includes opening and inspecting; a blocked output
sink is outside that bound. Pool closure precedes output. Failure discards the report.
This makes validation/inventory GETs only. No issuance, refresh, provider mutation,
socket, subscription, startup activation, readiness claim or effect permission.
--help and --version work offline.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr, func(ctx context.Context, dsn string) (inspection, error) {
		return appvault.OpenExecution(ctx, dsn)
	}, nil)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, env func(string) string, out, diagnostic io.Writer, open opener, client *http.Client) int {
	invalid := func() int {
		_, _ = fmt.Fprintln(diagnostic, "Invalid arguments; use overlaid-conduit-inspect --help.")
		return 2
	}
	report := func() int {
		if ctx.Err() != nil {
			_, _ = fmt.Fprintln(diagnostic, "Conduit inspection interrupted; no report returned.")
			return 130
		}
		_, _ = fmt.Fprintln(diagnostic, "Conduit inspection refused or unavailable; no report returned.")
		return 1
	}
	encode := func(value any) int {
		raw, err := json.Marshal(value)
		if err != nil {
			return report()
		}
		raw = append(raw, '\n')
		n, err := out.Write(raw)
		if err != nil || n != len(raw) {
			_, _ = fmt.Fprintln(diagnostic, "Output unavailable; discard any partial report.")
			return 1
		}
		return 0
	}
	if len(args) == 1 {
		switch args[0] {
		case "--help", "-h":
			n, err := io.WriteString(out, usage)
			if err != nil || n != len(usage) {
				return 1
			}
			return 0
		case "--version":
			info := buildinfo.Report()
			info.Name = "overlaid-conduit-inspect"
			return encode(info)
		}
	}
	flags := flag.NewFlagSet("conduit-inspect", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	binding := appvault.Binding{}
	conduit := ""
	inventory := false
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
	add("app", func(value string) error { binding.AppID = value; return nil })
	add("client", func(value string) error { binding.ClientID = value; return nil })
	add("revision", func(value string) error {
		var err error
		binding.Revision, err = strconv.ParseInt(value, 10, 64)
		return err
	})
	add("conduit", func(value string) error { conduit = value; return nil })
	flags.BoolFunc("inventory", "", func(value string) error {
		if seen["inventory"] || value != "true" {
			return errors.New("invalid inventory flag")
		}
		seen["inventory"] = true
		inventory = true
		return nil
	})
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || len(seen) != 4 || !tenantdb.ValidID(binding.AppID) || binding.AppID != strings.ToLower(binding.AppID) || binding.Revision < 1 {
		return invalid()
	}
	// Reuse the exact client/ID grammar before private configuration access.
	if inventory {
		if seen["conduit"] {
			return invalid()
		}
		if _, err := twitchconduit.NewInventoryReader(twitchconduit.InventoryConfig{ClientID: binding.ClientID, Token: func(context.Context) (string, error) { return "", nil }}); err != nil {
			return invalid()
		}
	} else {
		if _, err := twitchconduit.NewInspector(twitchconduit.Config{ClientID: binding.ClientID, ConduitID: conduit, Token: func(context.Context) (string, error) { return "", nil }}); err != nil {
			return invalid()
		}
	}
	if ctx.Err() != nil {
		return report()
	}
	dsn := env("OVERLAID_APP_EXECUTION_DATABASE_URL")
	keys, err := tenancy.ParseKeyring(env("OVERLAID_APP_EXECUTION_VAULT_ACTIVE_KEY"), env("OVERLAID_APP_EXECUTION_VAULT_KEYS"))
	if strings.TrimSpace(dsn) == "" || err != nil {
		_, _ = fmt.Fprintln(diagnostic, "Private execution database URL and valid versioned AES-256 keyring are required; use --help.")
		return 2
	}
	bound, cancel := context.WithTimeout(ctx, 10*time.Second)
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
	if inventory {
		reader, ok := store.(inventoryInspection)
		if !ok {
			_ = store.Close()
			return report()
		}
		observed, err := reader.InventoryConduits(bound, keys, binding, client)
		closeErr := store.Close()
		if err != nil || closeErr != nil || bound.Err() != nil || !validInventory(binding, observed) {
			return report()
		}
		return encode(struct {
			Binding   appvault.Binding                   `json:"binding"`
			Inventory twitchconduit.InventoryObservation `json:"inventory"`
		}{binding, observed})
	}
	observation, err := store.InspectConduit(bound, keys, binding, conduit, client)
	closeErr := store.Close()
	if err != nil || closeErr != nil || bound.Err() != nil {
		return report()
	}
	return encode(struct {
		Binding     appvault.Binding          `json:"binding"`
		Observation twitchconduit.Observation `json:"observation"`
	}{binding, observation})
}

func validInventory(b appvault.Binding, r twitchconduit.InventoryObservation) bool {
	if r.ClientID != b.ClientID || r.StartedAt.IsZero() || r.CompletedAt.Before(r.StartedAt) || r.CompletedAt.Sub(r.StartedAt) > 10*time.Second || r.Conduits == nil || len(r.Conduits) > 100 {
		return false
	}
	previous := ""
	for _, c := range r.Conduits {
		if c.ID <= previous || c.ShardCount < 1 || c.ShardCount > 20000 {
			return false
		}
		if _, err := twitchconduit.NewInspector(twitchconduit.Config{ClientID: b.ClientID, ConduitID: c.ID, Token: func(context.Context) (string, error) { return "", nil }}); err != nil {
			return false
		}
		previous = c.ID
	}
	return true
}
