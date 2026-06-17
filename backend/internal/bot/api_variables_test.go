package bot

import "testing"

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
