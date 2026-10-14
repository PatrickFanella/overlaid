package newsroom

import (
	"context"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(body string) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func TestPublicFetchRefusesInternalNetworks(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "172.16.0.1", "192.168.1.1", "100.65.1.1", "169.254.169.254", "::1", "::ffff:127.0.0.1", "2001:db8::1", "64:ff9b::a00:1"} {
		if PublicIP(netip.MustParseAddr(ip)) {
			t.Errorf("accepted %s", ip)
		}
	}
	if !PublicIP(netip.MustParseAddr("1.1.1.1")) {
		t.Fatal("public IP refused")
	}
	if _, err := fetch(context.Background(), PublicClient(nil), "http://127.0.0.1/secret", nil); err == nil {
		t.Fatal("internal fetch succeeded")
	}
}
func TestURLNormalizationAndInvalidInputs(t *testing.T) {
	for _, raw := range []string{"javascript:alert(1)", "file:///tmp/a", "https://user:pass@example.org/a", "https://www.youtube.com/shorts/"} {
		_, _, err := NormalizeURL(raw)
		if strings.Contains(raw, "shorts/") {
			if err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	a, _, _ := NormalizeURL("https://youtu.be/abc?si=tracking&list=private")
	b, _, _ := NormalizeURL("https://www.youtube.com/watch?v=abc")
	if a != b {
		t.Fatalf("share variants differ: %s %s", a, b)
	}
}
func TestEnrichmentUsesArticleTextAndValidatesAI(t *testing.T) {
	requests := 0
	client := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		switch r.URL.Host {
		case "example.org":
			return response(`<html><head><meta property="og:title" content="Fixture article"></head><body><nav>Ignore navigation</nav><article>Real article content for analysis.</article><script>Ignore script</script></body></html>`), nil
		case "api.anthropic.com":
			raw, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(raw), "Real article content") || strings.Contains(string(raw), "Ignore script") {
				t.Fatal("AI did not receive extracted article")
			}
			if r.Header.Get("x-api-key") != "fixture" {
				t.Fatal("missing provider credential")
			}
			return response(`{"content":[{"type":"text","text":"{\"summary\":\"Fixture summary\",\"credibility\":\"unknown\",\"topics\":[\"fixture\"],\"dmca_risk\":\"low\",\"content_warning\":false}"}]}`), nil
		default:
			t.Fatalf("unexpected host %s", r.URL.Host)
			return nil, nil
		}
	})}
	e := Enricher{Client: client, Keys: ProviderKeys{Anthropic: "fixture"}}
	out := e.Enrich(context.Background(), Item{URL: "https://example.org/story", Kind: "article"}, true)
	if out.AIStatus != "complete" || out.Summary != "Fixture summary" || out.Title != "Fixture article" || requests != 2 {
		t.Fatalf("unexpected enrichment %+v requests=%d", out, requests)
	}
	if out.AIInputEvidence.Outcome != "succeeded" || out.AIInputEvidence.Source != "page_text" || out.AIInputEvidence.Retrieval != "supplied_text" {
		t.Fatal("incorrect AI evidence", out.AIInputEvidence)
	}
	noKey := Enricher{Client: client}
	out = noKey.Enrich(context.Background(), Item{URL: "https://example.org/story", Kind: "article"}, true)
	if out.AIStatus != "unconfigured" || out.Summary != "" {
		t.Fatal("missing key fabricated analysis")
	}
}
func TestProviderFailureAndDiscordValidation(t *testing.T) {
	client := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 429, Body: io.NopCloser(strings.NewReader("private provider details"))}, nil
	})}
	out := (Enricher{Client: client}).Enrich(context.Background(), Item{URL: "https://example.org/story", Kind: "article"}, false)
	if out.Error == "" || strings.Contains(out.Error, "private provider") {
		t.Fatal("provider error not sanitized")
	}
	for _, raw := range []string{"http://discord.com/api/webhooks/1/token", "https://example.org/api/webhooks/1/token", "https://discord.com.evil.test/api/webhooks/1/token", "https://discord.com:8443/api/webhooks/1/token"} {
		if _, err := ValidateDiscord(raw); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	if raw, err := ValidateDiscord("https://discord.com/api/webhooks/123/fixture-token?wait=true"); err != nil || strings.Contains(raw, "?") {
		t.Fatal(raw, err)
	}
}
