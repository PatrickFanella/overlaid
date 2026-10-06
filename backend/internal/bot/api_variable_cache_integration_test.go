package bot

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestAPIVariableCacheConfigurationPostgres(t *testing.T) {
	databaseURL := os.Getenv("OVERLAID_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set OVERLAID_TEST_DATABASE_URL to a disposable Postgres database")
	}
	store, err := NewPostgres(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	resolver := NewAPIVariableResolver(store)
	ctx := context.Background()
	name := "cache_test_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	variable, err := store.SaveAPIVariable(ctx, APIVariable{Name: name, Enabled: true, URL: "https://api.example.test/one", Fallback: "offline", CacheTTLSeconds: 60, TimeoutMs: 1500})
	if err != nil {
		t.Fatal(err)
	}
	fixtureIDs := []string{variable.ID}
	t.Cleanup(func() {
		for _, id := range fixtureIDs {
			if _, err := store.db.ExecContext(ctx, `DELETE FROM bot_api_variables WHERE id=$1`, id); err != nil {
				t.Error(err)
			}
		}
	})
	resolver.cacheAPIVariable(variable, "original", time.Hour)
	if result := resolver.resolveAPIVariable(ctx, name); result.Status != "cache_hit" || result.Value != "original" {
		t.Fatalf("cache miss: %#v", result)
	}
	if err := store.updateAPIVariableStatus(ctx, variable, "ok", ""); err != nil {
		t.Fatal(err)
	}
	if result := resolver.resolveAPIVariable(ctx, name); result.Status != "cache_hit" {
		t.Fatalf("diagnostic update invalidated cache: %#v", result)
	}
	original := variable
	variable.Enabled = false
	variable, err = store.SaveAPIVariable(ctx, variable)
	if err != nil {
		t.Fatal(err)
	}
	if result := resolver.resolveAPIVariable(ctx, name); result.Status != "disabled" || result.Value != "offline" {
		t.Fatalf("disabled variable served cache: %#v", result)
	}
	if err := store.updateAPIVariableStatus(ctx, original, "stale_response", "old fetch"); err != nil {
		t.Fatal(err)
	}
	rows, err := store.ListAPIVariables(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.ID == variable.ID && (row.LastStatus != "" || row.LastError != "" || row.LastCheckedAt != nil) {
			t.Fatalf("old fetch overwrote new diagnostics: %#v", row)
		}
	}
	variable.Enabled = true
	variable.URL = "https://api.example.test/two"
	variable, err = store.SaveAPIVariable(ctx, variable)
	if err != nil {
		t.Fatal(err)
	}
	if _, hit := resolver.cachedAPIVariable(variable); hit {
		t.Fatal("changed configuration reused old cache")
	}
	// A fetch finishing after an edit may populate the old version; it must not
	// become a cache hit for the current version.
	resolver.cacheAPIVariable(original, "late original", time.Hour)
	if _, hit := resolver.cachedAPIVariable(variable); hit {
		t.Fatal("late old fetch revived stale cache")
	}
	resolver.cacheAPIVariable(variable, "new value", time.Hour)
	if result := resolver.resolveAPIVariable(ctx, name); result.Value != "new value" || result.Status != "cache_hit" {
		t.Fatalf("new cache unavailable: %#v", result)
	}
	if err := store.DeleteAPIVariable(ctx, variable.ID); err != nil {
		t.Fatal(err)
	}
	if result := resolver.resolveAPIVariable(ctx, name); result.Status != "missing" || result.Value != "" {
		t.Fatalf("deleted variable served cache: %#v", result)
	}
	variable, err = store.SaveAPIVariable(ctx, APIVariable{Name: name, Enabled: true, URL: "https://api.example.test/two"})
	if err != nil {
		t.Fatal(err)
	}
	fixtureIDs = append(fixtureIDs, variable.ID)
	if _, hit := resolver.cachedAPIVariable(variable); hit {
		t.Fatal("recreated name reused deleted record cache")
	}
}
