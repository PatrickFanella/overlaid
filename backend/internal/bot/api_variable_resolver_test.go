package bot

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type apiVariableRepositoryFixture struct {
	lookup func(context.Context, string) (APIVariable, error)
	status func(context.Context, APIVariable, string, string) error
}

func (f apiVariableRepositoryFixture) lookupAPIVariable(ctx context.Context, name string) (APIVariable, error) {
	return f.lookup(ctx, name)
}

func (f apiVariableRepositoryFixture) updateAPIVariableStatus(ctx context.Context, variable APIVariable, status, detail string) error {
	if f.status != nil {
		return f.status(ctx, variable, status, detail)
	}
	return nil
}

func TestAPIVariableResolverFetchCacheExpiryAndConfigurationChanges(t *testing.T) {
	now := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	variable := APIVariable{ID: "first", Name: "weather", Enabled: true, Fallback: "offline", CacheTTLSeconds: 60, UpdatedAt: now}
	lookups, fetches, writes := 0, 0, 0
	missing := false
	repository := apiVariableRepositoryFixture{
		lookup: func(context.Context, string) (APIVariable, error) {
			lookups++
			if missing {
				return APIVariable{}, errAPIVariableMissing
			}
			return variable, nil
		},
		status: func(_ context.Context, config APIVariable, status, detail string) error {
			writes++
			if config.ID != variable.ID || !config.UpdatedAt.Equal(variable.UpdatedAt) || status != "fetched" || detail != "" {
				t.Fatalf("diagnostics lost revision: %#v %s %s", config, status, detail)
			}
			return nil
		},
	}
	resolver := newAPIVariableResolver(repository, func(context.Context, APIVariable) (string, string, int, error) {
		fetches++
		return "72F", "fetched", 200, nil
	}, func() time.Time { return now })
	check := func(status, value string, expectedFetches int) {
		t.Helper()
		result := resolver.ResolveAPIVariables([]string{"weather"})["weather"]
		if result.Status != status || result.Value != value || fetches != expectedFetches {
			t.Fatalf("result=%#v, fetches=%d, want %s/%s/%d", result, fetches, status, value, expectedFetches)
		}
	}
	check("fetched", "72F", 1)
	check("cache_hit", "72F", 1)
	now = now.Add(61 * time.Second)
	check("fetched", "72F", 2)
	variable.UpdatedAt = now
	check("fetched", "72F", 3)
	variable.Enabled = false
	check("disabled", "offline", 3)
	missing = true
	check("missing", "", 3)
	missing = false
	variable.Enabled = true
	variable.ID = "replacement"
	check("fetched", "72F", 4)
	if lookups != 7 || writes != 4 {
		t.Fatalf("lookups=%d writes=%d", lookups, writes)
	}
}

func TestAPIVariableResolverLookupFailuresDoNotFetchOrExposeStorageErrors(t *testing.T) {
	for _, test := range []struct {
		err    error
		status string
	}{
		{errAPIVariableMissing, "missing"},
		{errors.New("PRIVATE_DATABASE_CONNECTION"), "unavailable"},
	} {
		resolver := newAPIVariableResolver(apiVariableRepositoryFixture{
			lookup: func(context.Context, string) (APIVariable, error) { return APIVariable{}, test.err },
			status: func(context.Context, APIVariable, string, string) error {
				t.Fatal("unexpected diagnostics")
				return nil
			},
		}, func(context.Context, APIVariable) (string, string, int, error) {
			t.Fatal("unexpected fetch")
			return "", "", 0, nil
		}, time.Now)
		result := resolver.ResolveAPIVariables([]string{"weather"})["weather"]
		if result.Status != test.status || result.Value != "" || result.Error != "" {
			t.Fatalf("lookup failure=%#v", result)
		}
	}
}

func TestAPIVariableResolverFallbackRetryAndDiagnosticFailure(t *testing.T) {
	variable := APIVariable{ID: "first", Name: "weather", Enabled: true, Fallback: "offline", CacheTTLSeconds: 60, UpdatedAt: time.Now()}
	fetches, writes := 0, 0
	details := []string{}
	resolver := newAPIVariableResolver(apiVariableRepositoryFixture{
		lookup: func(context.Context, string) (APIVariable, error) { return variable, nil },
		status: func(_ context.Context, config APIVariable, status, detail string) error {
			writes++
			details = append(details, detail)
			if config.ID != variable.ID || !config.UpdatedAt.Equal(variable.UpdatedAt) {
				t.Fatal("diagnostic write lost configuration revision")
			}
			return errors.New("diagnostic storage unavailable")
		},
	}, func(context.Context, APIVariable) (string, string, int, error) {
		fetches++
		if fetches == 1 {
			return "", "http_error", 503, errors.New(strings.Repeat("é", 200))
		}
		return "73F", "fetched", 200, nil
	}, time.Now)
	failed := resolver.ResolveAPIVariables([]string{"weather"})["weather"]
	if failed.Value != "offline" || failed.Status != "http_error" || failed.HTTPStatus != 503 || len([]rune(failed.Error)) != 180 {
		t.Fatalf("fallback=%#v", failed)
	}
	success := resolver.ResolveAPIVariables([]string{"weather"})["weather"]
	cached := resolver.ResolveAPIVariables([]string{"weather"})["weather"]
	if success.Value != "73F" || success.Status != "fetched" || cached.Value != "73F" || cached.Status != "cache_hit" || fetches != 2 || writes != 2 || details[0] != failed.Error || details[1] != "" {
		t.Fatalf("success=%#v cached=%#v fetches=%d writes=%d details=%v", success, cached, fetches, writes, details)
	}
}

func TestAPIVariableResolverLimitsUniqueNamesAndSharesRenderDeadline(t *testing.T) {
	loaded := []string{}
	var deadline time.Time
	resolver := newAPIVariableResolver(apiVariableRepositoryFixture{lookup: func(ctx context.Context, name string) (APIVariable, error) {
		loaded = append(loaded, name)
		current, ok := ctx.Deadline()
		if !ok || time.Until(current) > 3*time.Second {
			t.Fatal("render has no bounded deadline")
		}
		if deadline.IsZero() {
			deadline = current
		} else if !deadline.Equal(current) {
			t.Fatal("variable lookup reset the render deadline")
		}
		return APIVariable{ID: name, Name: name, Enabled: true}, nil
	}}, func(ctx context.Context, _ APIVariable) (string, string, int, error) {
		current, _ := ctx.Deadline()
		if !current.Equal(deadline) {
			t.Fatal("fetch reset the render deadline")
		}
		return "value", "fetched", 200, nil
	}, time.Now)
	results := resolver.ResolveAPIVariables([]string{"", " Weather ", "weather", "quote", "one", "two"})
	if len(results) != 3 || strings.Join(loaded, ",") != "weather,quote,one" {
		t.Fatalf("results=%#v loaded=%v", results, loaded)
	}
}

func TestAPIVariableResolverConcurrentRendersShareSafeCache(t *testing.T) {
	variable := APIVariable{ID: "first", Name: "weather", Enabled: true, CacheTTLSeconds: 60, UpdatedAt: time.Now()}
	var fetches atomic.Int32
	resolver := newAPIVariableResolver(apiVariableRepositoryFixture{lookup: func(context.Context, string) (APIVariable, error) { return variable, nil }}, func(context.Context, APIVariable) (string, string, int, error) {
		fetches.Add(1)
		return "72F", "fetched", 200, nil
	}, time.Now)
	results := make(chan APIVariableResult, 20)
	for range 20 {
		go func() { results <- resolver.ResolveAPIVariables([]string{"weather"})["weather"] }()
	}
	for range 20 {
		if result := <-results; result.Value != "72F" || (result.Status != "fetched" && result.Status != "cache_hit") {
			t.Fatalf("concurrent result=%#v", result)
		}
	}
	before := fetches.Load()
	if result := resolver.ResolveAPIVariables([]string{"weather"})["weather"]; result.Status != "cache_hit" || fetches.Load() != before {
		t.Fatalf("shared cache=%#v fetches=%d/%d", result, before, fetches.Load())
	}
}
