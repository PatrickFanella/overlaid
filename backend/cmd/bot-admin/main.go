// overlaid-bot-admin operates only the restricted shared service bot vault.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/buildinfo"
	"git.subcult.tv/PatrickFanella/over-laid/internal/servicebotoauth"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

type vault interface {
	Close() error
	Accounts(context.Context) ([]tenancy.ServiceBotAccount, error)
	Catalog(context.Context) ([]tenancy.SharedBotMetadata, error)
	Receipts(context.Context, int) ([]tenancy.ServiceBotReceipt, error)
	SaveValidated(context.Context, tenancy.Keyring, tenancy.BotCredential, int64) (tenancy.ServiceBotAccount, error)
	PublishMetadata(context.Context, string, int64, int64, bool) (tenancy.SharedBotMetadata, error)
	Revoke(context.Context, string, int64) (int64, error)
}

var openVault = func(ctx context.Context, dsn string) (vault, error) { return tenancy.OpenBotAdminStore(ctx, dsn) }
var authorize = authorizeLoopback

const usage = `Usage: overlaid-bot-admin COMMAND [flags]
Commands:
  list, catalog                  Read account or publication metadata
  receipts --limit 20             Read administrative receipts (1..100)
  publish|unpublish --bot UUID --revision N --catalog-revision N
  revoke --bot UUID --revision N
  authorize --expect-user-id ID --revision N --redirect-url http://127.0.0.1:PORT/oauth/callback

Revision 0 means creation only for authorize and initial publication only for catalog revision.
Secrets are read only from OVERLAID_BOT_ADMIN_DATABASE_URL, TWITCH_CLIENT_ID,
TWITCH_CLIENT_SECRET, VAULT_ACTIVE_KEY and VAULT_KEYS. OAuth does not publish a bot.
`

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	os.Exit(run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}
func run(ctx context.Context, args []string, env func(string) string, out, errs io.Writer) int {
	if ctx.Err() != nil {
		fmt.Fprintln(errs, "Operation canceled; inspect receipts before retrying.")
		return 130
	}
	if len(args) == 0 {
		fmt.Fprint(errs, usage)
		return 2
	}
	if args[0] == "--version" && len(args) == 1 {
		info := buildinfo.Report()
		info.Name = "overlaid-bot-admin"
		if err := json.NewEncoder(out).Encode(info); err != nil {
			return 1
		}
		return 0
	}
	if args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(out, usage)
		return 0
	}
	command := args[0]
	switch command {
	case "list", "catalog", "receipts", "publish", "unpublish", "revoke", "authorize":
	default:
		fmt.Fprint(errs, usage)
		return 2
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var id, subject, redirect string
	var revision, catalogRevision int64 = -1, -1
	limit := 20
	timeout := 10 * time.Minute
	switch command {
	case "receipts":
		flags.IntVar(&limit, "limit", 20, "receipt count")
	case "publish", "unpublish":
		flags.StringVar(&id, "bot", "", "bot UUID")
		flags.Int64Var(&revision, "revision", -1, "service grant revision")
		flags.Int64Var(&catalogRevision, "catalog-revision", -1, "catalog revision")
	case "revoke":
		flags.StringVar(&id, "bot", "", "bot UUID")
		flags.Int64Var(&revision, "revision", -1, "service grant revision")
	case "authorize":
		flags.StringVar(&subject, "expect-user-id", "", "intended immutable Twitch subject")
		flags.Int64Var(&revision, "revision", -1, "expected grant revision; 0 for creation")
		flags.StringVar(&redirect, "redirect-url", "", "registered callback")
		flags.DurationVar(&timeout, "timeout", 10*time.Minute, "authorization deadline (at most 10m)")
	}
	if err := flags.Parse(args[1:]); err != nil {
		if err == flag.ErrHelp {
			fmt.Fprint(out, usage)
			return 0
		}
		fmt.Fprintln(errs, "Invalid flags; use --help. No operation started.")
		return 2
	}
	if flags.NArg() != 0 || limit < 1 || limit > 100 || ((command == "publish" || command == "unpublish" || command == "revoke") && (!tenantdb.ValidID(id) || revision < 1)) || ((command == "publish" || command == "unpublish") && catalogRevision < 0) {
		fmt.Fprintln(errs, "Invalid arguments; use --help. No operation started.")
		return 2
	}
	var keys tenancy.Keyring
	var oauth servicebotoauth.Config
	if command == "authorize" {
		if revision < 0 || timeout <= 0 || timeout > 10*time.Minute {
			fmt.Fprintln(errs, "Explicit grant revision and a timeout between 0 and 10m are required.")
			return 2
		}
		var err error
		keys, err = tenancy.ParseKeyring(env("VAULT_ACTIVE_KEY"), env("VAULT_KEYS"))
		if err != nil {
			fmt.Fprintln(errs, "A valid private vault keyring is required.")
			return 2
		}
		oauth = servicebotoauth.Config{ClientID: env("TWITCH_CLIENT_ID"), ClientSecret: env("TWITCH_CLIENT_SECRET"), RedirectURL: redirect, ExpectedUserID: subject}
		checkCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		if _, err = servicebotoauth.New(checkCtx, oauth, func(context.Context, tenancy.BotCredential) (tenancy.ServiceBotAccount, error) {
			return tenancy.ServiceBotAccount{}, nil
		}); err != nil {
			fmt.Fprintln(errs, "Invalid OAuth configuration; use a registered IPv4 loopback callback, client credentials and expected subject.")
			return 2
		}
	}
	dsn := env("OVERLAID_BOT_ADMIN_DATABASE_URL")
	if dsn == "" {
		fmt.Fprintln(errs, "OVERLAID_BOT_ADMIN_DATABASE_URL is required; no fallback DSN is used.")
		return 2
	}
	store, err := openVault(ctx, dsn)
	if err != nil {
		if ctx.Err() != nil {
			fmt.Fprintln(errs, "Operation canceled; inspect receipts before retrying.")
			return 130
		}
		fmt.Fprintln(errs, "Restricted bot vault unavailable; verify its role, schema and connection privately.")
		return 1
	}
	defer store.Close()
	var result any
	opCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	switch command {
	case "list":
		result, err = store.Accounts(opCtx)
	case "catalog":
		result, err = store.Catalog(opCtx)
	case "receipts":
		result, err = store.Receipts(opCtx, limit)
	case "publish", "unpublish":
		result, err = store.PublishMetadata(opCtx, id, revision, catalogRevision, command == "publish")
	case "revoke":
		var next int64
		next, err = store.Revoke(opCtx, id, revision)
		result = map[string]any{"botId": id, "revision": next, "revoked": true}
	case "authorize":
		authCtx, stop := context.WithTimeout(ctx, timeout)
		defer stop()
		result, err = authorize(authCtx, oauth, func(ctx context.Context, c tenancy.BotCredential) (tenancy.ServiceBotAccount, error) {
			return store.SaveValidated(ctx, keys, c, revision)
		}, errs)
	}
	if err != nil {
		if ctx.Err() != nil {
			fmt.Fprintln(errs, "Operation canceled; inspect receipts before retrying.")
			return 130
		}
		fmt.Fprintln(errs, "Operation failed; reload metadata/revisions and inspect configuration privately. No successful receipt is reported.")
		return 1
	}
	if err = json.NewEncoder(out).Encode(result); err != nil {
		fmt.Fprintln(errs, "Operation completed but metadata output failed; inspect the audit before retrying.")
		return 1
	}
	return 0
}
func authorizeLoopback(ctx context.Context, c servicebotoauth.Config, save servicebotoauth.Save, out io.Writer) (tenancy.ServiceBotAccount, error) {
	flow, err := servicebotoauth.New(ctx, c, save)
	if err != nil {
		return tenancy.ServiceBotAccount{}, err
	}
	listener, err := net.Listen("tcp4", flow.Address())
	if err != nil {
		return tenancy.ServiceBotAccount{}, err
	}
	server := &http.Server{Handler: flow, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 40 * time.Second, IdleTimeout: 10 * time.Second, MaxHeaderBytes: 16384, BaseContext: func(net.Listener) context.Context { return ctx }}
	defer server.Close()
	finished := make(chan error, 1)
	go func() { finished <- server.Serve(listener) }()
	fmt.Fprintf(out, "Open %s in the browser that will authorize the intended bot account. The listener expires with this command.\n", flow.StartURL())
	select {
	case result := <-flow.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		return result.Account, result.Err
	case <-ctx.Done():
		return tenancy.ServiceBotAccount{}, ctx.Err()
	case err := <-finished:
		return tenancy.ServiceBotAccount{}, err
	}
}
