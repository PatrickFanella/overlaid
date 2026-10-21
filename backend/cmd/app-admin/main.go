// overlaid-app-admin operates only service-app custody metadata and revocation.
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
	"strconv"
	"strings"
	"syscall"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/appvault"
	"git.subcult.tv/PatrickFanella/over-laid/internal/buildinfo"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

type custody interface {
	Accounts(context.Context) ([]appvault.Metadata, error)
	Receipts(context.Context, string, int) ([]appvault.Receipt, error)
	Revoke(context.Context, string, int64) (appvault.Metadata, error)
	LookupIssuance(context.Context, string) (appvault.Issuance, error)
	Close() error
}
type opener func(context.Context, string) (custody, error)

const usage = `Usage: overlaid-app-admin COMMAND [flags]
Commands:
  list                              List app metadata, including revoked records
  receipts --app UUID [--limit N]    Read original receipts (default 20, range 1..1000)
  revoke --app UUID --revision N     Clear local credentials at an exact positive revision
  issuance --request UUID           Read one original issuance request; never retry it

Private environment: OVERLAID_APP_ADMIN_DATABASE_URL (restricted app-admin login).
Successful stdout is one JSON object. Diagnostics go to stderr.
Exit codes: 0 success, 1 unavailable/refused/unresolved, 2 invalid input, 130 interrupted.
Database operations have a five-second bound and never retry automatically.
Revocation is local; inspect list and original receipts before retrying an unresolved operation.
No acquisition, provider calls, key access, plaintext resolution or runtime activation.
Use overlaid-app-admin COMMAND --help for command examples; --version works offline.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr, func(ctx context.Context, dsn string) (custody, error) { return appvault.Open(ctx, dsn) })
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, env func(string) string, out, diagnostic io.Writer, open opener) int {
	command := ""
	invalid := func() int {
		_, _ = fmt.Fprintln(diagnostic, "Invalid arguments; use overlaid-app-admin --help.")
		return 2
	}
	report := func(err error) int {
		if command == "issuance" {
			if ctx.Err() != nil {
				_, _ = fmt.Fprintln(diagnostic, "Issuance lookup interrupted; retain the original request ID.")
				return 130
			}
			_, _ = fmt.Fprintln(diagnostic, "Issuance request missing or unavailable; retain the original ID and do not create a replacement request.")
			return 1
		}
		if ctx.Err() != nil {
			_, _ = fmt.Fprintln(diagnostic, "Custody operation interrupted; inspect list and original receipts before retrying.")
			return 130
		}
		if errors.Is(err, appvault.ErrRevision) {
			_, _ = fmt.Fprintln(diagnostic, "App revision conflict or record missing; inspect list and original receipts.")
			return 1
		}
		_, _ = fmt.Fprintln(diagnostic, "Custody operation unavailable or unresolved; inspect list and original receipts before retrying.")
		return 1
	}
	encode := func(value any) int {
		raw, err := json.Marshal(value)
		if err != nil {
			return report(err)
		}
		raw = append(raw, '\n')
		n, err := out.Write(raw)
		if err != nil || n != len(raw) {
			if command == "issuance" {
				_, _ = fmt.Fprintln(diagnostic, "Output unavailable; discard any partial result and retain the original issuance request ID.")
				return 1
			}
			_, _ = fmt.Fprintln(diagnostic, "Output unavailable; inspect list and original receipts before retrying.")
			return 1
		}
		return 0
	}
	if len(args) == 0 {
		return invalid()
	}
	if len(args) == 1 {
		switch args[0] {
		case "--help", "-h":
			_, err := io.WriteString(out, usage)
			if err != nil {
				return 1
			}
			return 0
		case "--version":
			info := buildinfo.Report()
			info.Name = "overlaid-app-admin"
			return encode(info)
		}
	}
	command = args[0]
	if command != "list" && command != "receipts" && command != "revoke" && command != "issuance" {
		return invalid()
	}
	if len(args) == 2 && (args[1] == "--help" || args[1] == "-h") {
		example := "overlaid-app-admin list"
		if command == "receipts" {
			example = "overlaid-app-admin receipts --app UUID --limit 20"
		}
		if command == "revoke" {
			example = "overlaid-app-admin revoke --app UUID --revision N"
		}
		if command == "issuance" {
			example = "overlaid-app-admin issuance --request UUID\nSuccess reads metadata only; started/uncertain remains unresolved. Never replace a missing request ID."
		}
		_, err := io.WriteString(out, usage+"\nExample: "+example+"\n")
		if err != nil {
			return 1
		}
		return 0
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	id := ""
	revision := int64(0)
	limit := 20
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
	if command == "receipts" || command == "revoke" {
		add("app", func(value string) error { id = value; return nil })
	}
	if command == "issuance" {
		add("request", func(value string) error { id = value; return nil })
	}
	if command == "receipts" {
		add("limit", func(value string) error { var err error; limit, err = strconv.Atoi(value); return err })
	}
	if command == "revoke" {
		add("revision", func(value string) error { var err error; revision, err = strconv.ParseInt(value, 10, 64); return err })
	}
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
		return invalid()
	}
	if command != "list" && !tenantdb.ValidID(id) || command == "receipts" && (limit < 1 || limit > 1000) || command == "revoke" && revision < 1 || command == "issuance" && id != strings.ToLower(id) {
		return invalid()
	}
	if ctx.Err() != nil {
		return report(ctx.Err())
	}
	dsn := env("OVERLAID_APP_ADMIN_DATABASE_URL")
	if strings.TrimSpace(dsn) == "" {
		_, _ = fmt.Fprintln(diagnostic, "Private OVERLAID_APP_ADMIN_DATABASE_URL is required.")
		return 2
	}
	bound, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	store, err := open(bound, dsn)
	if err != nil {
		if store != nil {
			_ = store.Close()
		}
		return report(err)
	}
	if store == nil {
		return report(appvault.ErrUnavailable)
	}
	var value any
	switch command {
	case "list":
		var accounts []appvault.Metadata
		accounts, err = store.Accounts(bound)
		if accounts == nil {
			accounts = []appvault.Metadata{}
		}
		value = struct {
			Accounts []appvault.Metadata `json:"accounts"`
		}{accounts}
	case "receipts":
		var receipts []appvault.Receipt
		receipts, err = store.Receipts(bound, id, limit)
		if receipts == nil {
			receipts = []appvault.Receipt{}
		}
		value = struct {
			Receipts []appvault.Receipt `json:"receipts"`
		}{receipts}
	case "revoke":
		var account appvault.Metadata
		account, err = store.Revoke(bound, id, revision)
		value = struct {
			Account appvault.Metadata `json:"account"`
		}{account}
	case "issuance":
		var request appvault.Issuance
		request, err = store.LookupIssuance(bound, id)
		value = struct {
			Issuance appvault.Issuance `json:"issuance"`
		}{request}
	}
	closeErr := store.Close()
	if err != nil {
		return report(err)
	}
	if closeErr != nil {
		return report(closeErr)
	}
	if bound.Err() != nil {
		return report(bound.Err())
	}
	return encode(value)
}
