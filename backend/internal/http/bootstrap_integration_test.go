package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/config"
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
	"git.subcult.tv/PatrickFanella/over-laid/internal/state"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"git.subcult.tv/PatrickFanella/over-laid/internal/testpostgres"
)

func TestSelfhostImportIsEncryptedIdempotentAndPreservesFiles(t *testing.T) {
	f := testpostgres.NewTenantFixture(t, "")
	dir := t.TempDir()
	event := events.NewAlert("fixture", "legacy", "retain me", events.SeverityInfo)
	snapshots, _ := json.Marshal([]state.Snapshot{{Key: "alert", Event: event}})
	disk := twitchTokenDisk{AccessToken: "private-fixture-access", RefreshToken: "private-fixture-refresh", UserID: "fixture-id", Login: "fixture", ExpiresAt: time.Now().Add(time.Hour)}
	token, _ := json.Marshal(disk)
	for name, raw := range map[string][]byte{"state.json": snapshots, "token.json": token, "history.jsonl": append(mustJSON(t, event), '\n')} {
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Config{DeploymentMode: "selfhost", ExposureMode: "local", PublicWebURL: "http://localhost:5173", DatabaseURL: f.RuntimeURL, VaultActiveKey: "v1", VaultKeys: "v1:" + base64.StdEncoding.EncodeToString(make([]byte, 32)), OverlayToken: "fixture-overlay", WebhookSecret: "fixture-webhook", OverlayStateFile: filepath.Join(dir, "state.json"), TwitchTokenFile: filepath.Join(dir, "token.json"), EventHistoryFile: filepath.Join(dir, "history.jsonl")}
	for i := 0; i < 2; i++ {
		app, err := NewApplication(cfg)
		if err != nil {
			t.Fatal(err)
		}
		scope, _ := app.store.Product.Scoped(tenantdb.BootstrapID)
		retained, err := state.NewTenant(scope)
		if err != nil || len(retained.List()) != 1 {
			t.Fatal("state import lost", err)
		}
		raw, err := app.store.ResolveConnection(t.Context(), app.keys, tenantdb.BootstrapID, "twitch", func(raw json.RawMessage) (json.RawMessage, error) { return raw, nil })
		if err != nil || !json.Valid(raw) {
			t.Fatal("vault import missing", err)
		}
		app.Close()
	}
	var count int
	var ciphertext []byte
	if err := f.Admin.QueryRow(`SELECT count(*) FROM stream_events WHERE tenant_id=$1`, tenantdb.BootstrapID).Scan(&count); err != nil || count != 1 {
		t.Fatal("history import duplicated", count, err)
	}
	if err := f.Admin.QueryRow(`SELECT ciphertext FROM platform_connections WHERE tenant_id=$1`, tenantdb.BootstrapID).Scan(&ciphertext); err != nil || json.Valid(ciphertext) {
		t.Fatal("plaintext connection", err)
	}
	got, err := os.ReadFile(cfg.TwitchTokenFile)
	if err != nil || string(got) != string(token) {
		t.Fatal("legacy rollback file changed", err)
	}
}
func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestPublicSelfhostReadinessRejectsMissingCredentials(t *testing.T) {
	f := testpostgres.NewTenantFixture(t, "")
	cfg := config.Config{DeploymentMode: "selfhost", ExposureMode: "public", PublicWebURL: "https://overlaid.example", DatabaseURL: f.RuntimeURL}
	app, err := NewApplication(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	w := executeApplication(app, httptest.NewRequest("GET", "/readyz", nil))
	var report struct {
		Status string
		Checks []struct{ Name, Status string }
	}
	if err = json.Unmarshal(w.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, check := range report.Checks {
		if check.Name == "access_policy" {
			found = true
			if check.Status != "degraded" {
				t.Fatal("missing secrets passed readiness")
			}
		}
	}
	if !found || report.Status != "degraded" {
		t.Fatal("invalid public readiness")
	}
	if w := executeApplication(app, httptest.NewRequest("GET", "/api/events/history", nil)); w.Code != 401 {
		t.Fatal("missing operator secret authorized public read", w.Code)
	}
}
