package bot

import (
	"testing"
	"time"
)

func TestNormalizeAPIVariable(t *testing.T) {
	variable, err := NormalizeAPIVariable(APIVariable{Name: " Weather ", Enabled: true, URL: "https://api.example.test/current", ResponseSelector: " data.temp ", Fallback: " unavailable ", CacheTTLSeconds: 60, TimeoutMs: 1500})
	if err != nil {
		t.Fatalf("expected valid api variable: %v", err)
	}
	if variable.Name != "weather" || variable.ResponseSelector != "data.temp" || variable.Fallback != "unavailable" {
		t.Fatalf("unexpected normalized api variable: %#v", variable)
	}
}

func TestNormalizeAPIVariableRejectsUnsafeValues(t *testing.T) {
	tests := []APIVariable{
		{Name: "bad name", URL: "https://api.example.test"},
		{Name: "local", URL: "https://localhost/value"},
		{Name: "plain", URL: "http://api.example.test/value"},
		{Name: "secret", URL: "https://user:pass@example.test/value"},
		{Name: "ttl", URL: "https://api.example.test/value", CacheTTLSeconds: 1},
		{Name: "timeout", URL: "https://api.example.test/value", TimeoutMs: 10},
	}
	for _, test := range tests {
		if _, err := NormalizeAPIVariable(test); err == nil {
			t.Fatalf("expected validation error for %#v", test)
		}
	}
}

func TestRenderTemplateAPIVariables(t *testing.T) {
	result := RenderTemplate("Weather: {api:weather} {api:missing}", TemplateContext{APIVariables: map[string]string{"weather": "72F"}})
	if result.Text != "Weather: 72F" {
		t.Fatalf("rendered=%q", result.Text)
	}
}

func TestAPIVariableResolverNormalizesAndLimitsNames(t *testing.T) {
	resolver := NewAPIVariableResolver(nil)
	results := resolver.ResolveAPIVariables([]string{" Weather ", "weather", "Quote", "One", "Two", "Three"})
	if len(results) != 3 {
		t.Fatalf("result count=%d, want 3", len(results))
	}
	for _, name := range []string{"weather", "quote", "one"} {
		result, ok := results[name]
		if !ok {
			t.Fatalf("missing result for %q: %#v", name, results)
		}
		if result.Name != name || result.Status != "unavailable" {
			t.Fatalf("unexpected result for %q: %#v", name, result)
		}
	}
}

func TestAPIVariableCacheRejectsChangedConfigurationAndExpiredValues(t *testing.T) {
	resolver := NewAPIVariableResolver(nil)
	original := APIVariable{ID: "first-record", Name: "weather", UpdatedAt: time.Now()}
	resolver.cacheAPIVariable(original, "72F", time.Hour)
	if value, hit := resolver.cachedAPIVariable(original); !hit || value != "72F" {
		t.Fatalf("healthy cache: %q, %v", value, hit)
	}
	changed := original
	changed.UpdatedAt = original.UpdatedAt.Add(time.Second)
	if _, hit := resolver.cachedAPIVariable(changed); hit {
		t.Fatal("edited configuration reused cache")
	}
	recreated := original
	recreated.ID = "replacement-record"
	if _, hit := resolver.cachedAPIVariable(recreated); hit {
		t.Fatal("recreated record reused cache")
	}
	resolver.cacheAPIVariable(changed, "73F", time.Hour)
	resolver.cacheAPIVariable(original, "late old response", time.Hour)
	if _, hit := resolver.cachedAPIVariable(changed); hit {
		t.Fatal("late old response became a current cache hit")
	}
	resolver.cacheAPIVariable(original, "expired", -time.Second)
	if _, hit := resolver.cachedAPIVariable(original); hit {
		t.Fatal("expired cache served a value")
	}
}
