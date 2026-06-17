package bot

import (
	"strings"
	"testing"
)

func TestRenderTemplateKnownVariables(t *testing.T) {
	result := RenderTemplate("hello {user} in {channel}: {args} via {command}", TemplateContext{DisplayName: "Viewer", User: "viewer_login", Channel: "patrick__eff", Args: "hydrate now", Command: "discord"})
	if result.Text != "hello Viewer in patrick__eff: hydrate now via discord" {
		t.Fatalf("unexpected render: %#v", result)
	}
	if len(result.UnknownVariables) != 0 {
		t.Fatalf("unexpected unknown variables: %#v", result.UnknownVariables)
	}
}

func TestRenderTemplateUnknownVariablesBecomeEmpty(t *testing.T) {
	result := RenderTemplate("hello {missing} {user}", TemplateContext{User: "viewer"})
	if result.Text != "hello  viewer" {
		t.Fatalf("unexpected render: %#v", result)
	}
	if len(result.UnknownVariables) != 1 || result.UnknownVariables[0] != "missing" {
		t.Fatalf("expected missing variable, got %#v", result.UnknownVariables)
	}
}

func TestRenderTemplateCapsRenderedResponse(t *testing.T) {
	result := RenderTemplate(strings.Repeat("界", 401), TemplateContext{})
	if len([]rune(result.Text)) != 400 {
		t.Fatalf("expected 400-rune cap, got %d", len([]rune(result.Text)))
	}
}

func TestTemplateVariableDiscoveryNormalizesAndDedupes(t *testing.T) {
	template := "{USER} {user} {Count:Hype} {count:hype} {API:Weather} {api:weather} {Quote:Random} {quote:random}"

	if got := TemplateVariableNames(template); len(got) != 4 || got[0] != "user" || got[1] != "count:hype" || got[2] != "api:weather" || got[3] != "quote:random" {
		t.Fatalf("unexpected template variables: %#v", got)
	}
	if got := CountVariableNames(template); len(got) != 1 || got[0] != "hype" {
		t.Fatalf("unexpected count variables: %#v", got)
	}
	if got := APIVariableNames(template); len(got) != 1 || got[0] != "weather" {
		t.Fatalf("unexpected api variables: %#v", got)
	}
	if !UsesRandomQuote(template) {
		t.Fatal("expected random quote usage to be detected")
	}
}

func TestUsesRandomQuoteRejectsSimilarNames(t *testing.T) {
	if UsesRandomQuote("{quote:randomness} {quote:Randomish}") {
		t.Fatal("similar names should not count as random quote usage")
	}
}
