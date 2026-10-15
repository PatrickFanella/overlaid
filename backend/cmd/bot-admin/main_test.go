package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/servicebotoauth"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/testpostgres"
)

func TestUsageValidationNeverPrintsSecretsOrOpensDatabase(t *testing.T) {
	original := openVault
	t.Cleanup(func() { openVault = original })
	openVault = func(context.Context, string) (vault, error) { t.Fatal("unexpected database open"); return nil, nil }
	cases := [][]string{{}, {"wrong"}, {"--help"}, {"publish", "--bot", "wrong"}, {"authorize", "--client-secret", "private-argv"}, {"receipts", "--limit", "101"}, {"list", "--secret=private-argv"}, {"list"}}
	for _, args := range cases {
		var out, errs bytes.Buffer
		code := run(t.Context(), args, func(string) string { return "" }, &out, &errs)
		if len(args) > 0 && args[0] == "--help" {
			if code != 0 {
				t.Fatal(code)
			}
		} else if code != 2 {
			t.Fatal(args, code)
		}
		if strings.Contains(out.String()+errs.String(), "private-argv") {
			t.Fatal("flag leaked")
		}
	}
}
func TestRestrictedDatabaseCLIAndPublication(t *testing.T) {
	f := testpostgres.NewTenantFixture(t, "")
	env := func(name string) string {
		switch name {
		case "OVERLAID_BOT_ADMIN_DATABASE_URL":
			return f.BotAdminURL
		case "VAULT_ACTIVE_KEY":
			return "v1"
		case "VAULT_KEYS":
			return "v1:" + base64.StdEncoding.EncodeToString(make([]byte, 32))
		case "TWITCH_CLIENT_ID":
			return "fixture"
		case "TWITCH_CLIENT_SECRET":
			return "private-client"
		}
		return ""
	}
	old := authorize
	t.Cleanup(func() { authorize = old })
	authorize = func(ctx context.Context, c servicebotoauth.Config, save servicebotoauth.Save, out io.Writer) (tenancy.ServiceBotAccount, error) {
		return save(ctx, tenancy.BotCredential{Version: 1, UserID: c.ExpectedUserID, Login: "sharedbot", AccessToken: "private-access", RefreshToken: "private-refresh", ExpiresAt: time.Now().Add(time.Hour), Scopes: []string{"user:bot", "user:read:chat", "user:write:chat"}})
	}
	call := func(args ...string) (string, int) {
		t.Helper()
		var out, errs bytes.Buffer
		code := run(t.Context(), args, env, &out, &errs)
		combined := out.String() + errs.String()
		for _, secret := range []string{"private-client", "private-access", "private-refresh", f.BotAdminURL} {
			if strings.Contains(combined, secret) {
				t.Fatal("secret output", secret)
			}
		}
		return out.String(), code
	}
	output, code := call("authorize", "--expect-user-id", "bot-one", "--revision", "0", "--redirect-url", "http://127.0.0.1:41925/oauth/callback")
	if code != 0 {
		t.Fatal(output, code)
	}
	var account tenancy.ServiceBotAccount
	if err := json.Unmarshal([]byte(output), &account); err != nil || account.Revision != 1 || account.UserID != "bot-one" {
		t.Fatal(output, err)
	}
	if output, code = call("catalog"); code != 0 || strings.TrimSpace(output) != "[]" {
		t.Fatal("OAuth published automatically", output, code)
	}
	if _, code = call("publish", "--bot", account.ID, "--revision", "1", "--catalog-revision", "0"); code != 0 {
		t.Fatal("publish", code)
	}
	if _, code = call("publish", "--bot", account.ID, "--revision", "1", "--catalog-revision", "0"); code != 1 {
		t.Fatal("stale publication", code)
	}
	if _, code = call("unpublish", "--bot", account.ID, "--revision", "1", "--catalog-revision", "1"); code != 0 {
		t.Fatal("unpublish", code)
	}
	if _, code = call("revoke", "--bot", account.ID, "--revision", "1"); code != 0 {
		t.Fatal("revoke", code)
	}
	if output, code = call("list"); code != 0 || !strings.Contains(output, `"revoked":true`) {
		t.Fatal(output, code)
	}
	if output, code = call("receipts", "--limit", "100"); code != 0 {
		t.Fatal(code)
	} else {
		var receipts []tenancy.ServiceBotReceipt
		if err := json.Unmarshal([]byte(output), &receipts); err != nil || len(receipts) != 4 {
			t.Fatal(output, err)
		}
	}
	var cleared bool
	if err := f.Admin.QueryRow(`SELECT ciphertext IS NULL AND key_version IS NULL FROM service_bot_accounts WHERE id=$1`, account.ID).Scan(&cleared); err != nil || !cleared {
		t.Fatal("credentials retained", err)
	}
	badEnv := func(name string) string {
		if name == "OVERLAID_BOT_ADMIN_DATABASE_URL" {
			return f.RuntimeURL
		}
		return env(name)
	}
	var out, errs bytes.Buffer
	called := false
	authorize = func(context.Context, servicebotoauth.Config, servicebotoauth.Save, io.Writer) (tenancy.ServiceBotAccount, error) {
		called = true
		return tenancy.ServiceBotAccount{}, errors.New("private failure")
	}
	if code = run(t.Context(), []string{"authorize", "--expect-user-id", "bot-one", "--revision", "2", "--redirect-url", "http://127.0.0.1:41925/oauth/callback"}, badEnv, &out, &errs); code != 1 || called || out.Len() != 0 {
		t.Fatal("unsafe role started authorization", code, called, out.String())
	}
}

type noticeWriter chan string

func (w noticeWriter) Write(p []byte) (int, error) { w <- string(p); return len(p), nil }
func TestLoopbackListenerStopsOnCancellation(t *testing.T) {
	reservation, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := reservation.Addr().String()
	reservation.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	notices := make(noticeWriter, 1)
	done := make(chan error, 1)
	go func() {
		_, err := authorizeLoopback(ctx, servicebotoauth.Config{ClientID: "fixture", ClientSecret: "private-client", ExpectedUserID: "bot-one", RedirectURL: "http://" + address + "/oauth/callback"}, func(context.Context, tenancy.BotCredential) (tenancy.ServiceBotAccount, error) {
			t.Error("unexpected save")
			return tenancy.ServiceBotAccount{}, nil
		}, notices)
		done <- err
	}()
	select {
	case line := <-notices:
		if strings.Contains(line, "private-client") || !strings.Contains(line, "/oauth/start") {
			t.Fatal("unsafe instructions", line)
		}
	case err := <-done:
		t.Fatal("listener failed", err)
	case <-time.After(5 * time.Second):
		t.Fatal("listener did not start")
	}
	client := &http.Client{Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Get("http://" + address + "/oauth/start")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 302 || len(response.Cookies()) != 1 {
		t.Fatal("start response")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation left listener running")
	}
	listener, err := net.Listen("tcp4", address)
	if err != nil {
		t.Fatal("port not released", err)
	}
	listener.Close()
}
func TestVersionAndCancellationAreOffline(t *testing.T) {
	old := openVault
	t.Cleanup(func() { openVault = old })
	openVault = func(context.Context, string) (vault, error) {
		t.Fatal("offline command opened database")
		return nil, nil
	}
	var out, errs bytes.Buffer
	if code := run(t.Context(), []string{"--version"}, func(string) string { return "" }, &out, &errs); code != 0 || !strings.Contains(out.String(), "overlaid-bot-admin") {
		t.Fatal(code, out.String())
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if code := run(ctx, []string{"list"}, func(string) string { return "" }, &out, &errs); code != 130 {
		t.Fatal(code)
	}
}
