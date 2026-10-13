package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"git.subcult.tv/PatrickFanella/over-laid/internal/config"
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

func TestWebhookExposureCredentialBoundary(t *testing.T) {
	credentials := []struct {
		name, header, authorization string
		valid                       bool
	}{
		{name: "missing"},
		{name: "wrong_header", header: "webhook-secrex"},
		{name: "wrong_bearer", authorization: "Bearer webhook-secrex"},
		{name: "operator_is_not_webhook", header: "operator-secret"},
		{name: "header", header: " webhook-secret ", valid: true},
		{name: "bearer", authorization: "bearer webhook-secret", valid: true},
		{name: "blank_header_bearer", header: " ", authorization: "Bearer webhook-secret", valid: true},
		{name: "wrong_header_overrides_bearer", header: "wrong", authorization: "Bearer webhook-secret"},
	}
	for _, mode := range []string{"", "local", " LOCAL ", "lan", "public", " PUBLIC ", "unknown"} {
		for _, secret := range []string{"", " ", " webhook-secret "} {
			for _, credential := range credentials {
				t.Run(mode+"/"+secret+"/"+credential.name, func(t *testing.T) {
					bus := events.NewBus(4)
					s := NewServer(config.Config{ExposureMode: mode, WebhookSecret: secret, OperatorSecret: "operator-secret"}, bus)
					req := httptest.NewRequest(http.MethodPost, "/api/integrations/webhook/fixture", strings.NewReader(`{"title":"Fixture","message":"Synthetic","data":{"goal":{"current":1}}}`))
					req.Header.Set("X-Overlay-Webhook-Secret", credential.header)
					req.Header.Set("Authorization", credential.authorization)
					r := httptest.NewRecorder()
					s.Handler().ServeHTTP(r, req)

					local := strings.TrimSpace(mode) == "" || strings.EqualFold(strings.TrimSpace(mode), "local")
					configured := strings.TrimSpace(secret) != ""
					allowed := configured && credential.valid || !configured && local
					want := http.StatusUnauthorized
					count := 0
					if allowed {
						want = http.StatusAccepted
						count = 1
					}
					if r.Code != want {
						t.Fatalf("status=%d want=%d body=%s", r.Code, want, r.Body.String())
					}
					if got := bus.Stats().Published; got != uint64(count) {
						t.Fatalf("published=%d want=%d", got, count)
					}
					if got := len(s.history.List(10)); got != count {
						t.Fatalf("history entries=%d want=%d", got, count)
					}
					if got := len(s.state.List()); got != count {
						t.Fatalf("retained events=%d want=%d", got, count)
					}
					found := false
					for _, check := range s.buildExposureReadinessChecks() {
						if check.Name == "webhook_secret" {
							found = true
							wantStatus := "degraded"
							if configured {
								wantStatus = "ok"
							}
							if check.Status != wantStatus {
								t.Fatalf("webhook readiness=%s want=%s", check.Status, wantStatus)
							}
						}
					}
					if found == local {
						t.Fatalf("webhook credential readiness check present=%t local=%t", found, local)
					}
				})
			}
		}
	}
}

func TestWebhookAuthorizationPrecedesPayloadValidation(t *testing.T) {
	for _, mode := range []string{"lan", "public"} {
		bus := events.NewBus(4)
		s := NewServer(config.Config{ExposureMode: mode}, bus)
		r := httptest.NewRecorder()
		s.Handler().ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/api/integrations/webhook/fixture", strings.NewReader("invalid JSON")))
		if r.Code != http.StatusUnauthorized || bus.Stats().Published != 0 {
			t.Fatalf("mode=%s status=%d published=%d", mode, r.Code, bus.Stats().Published)
		}
	}
}
