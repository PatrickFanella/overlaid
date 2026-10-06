package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"git.subcult.tv/PatrickFanella/over-laid/internal/config"
	"git.subcult.tv/PatrickFanella/over-laid/internal/dashboardconfig"
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

type displayConfigStub struct{ raw json.RawMessage }

func (d displayConfigStub) Get(context.Context) (dashboardconfig.Document, bool, error) {
	return dashboardconfig.Document{Config: d.raw}, true, nil
}
func (d displayConfigStub) Replace(context.Context, json.RawMessage) (dashboardconfig.Document, error) {
	panic("read only")
}

func TestOverlayAccessBoundary(t *testing.T) {
	for _, mode := range []string{"local", "lan", "public"} {
		t.Run(mode, func(t *testing.T) {
			s := NewServer(config.Config{ExposureMode: mode, OperatorSecret: "operator", OverlayToken: "overlay"}, events.NewBus(4))
			for _, path := range []string{"/api/events/stream", "/api/overlay/display"} {
				for _, key := range []string{"", "wrong", "operator"} {
					r := httptest.NewRecorder()
					s.Handler().ServeHTTP(r, httptest.NewRequest("GET", path+"?key="+key, nil))
					if r.Code != http.StatusUnauthorized {
						t.Fatalf("%s key=%q status=%d", path, key, r.Code)
					}
				}
			}
			for _, secret := range []string{"", "overlay", "operator"} {
				req := httptest.NewRequest("GET", "/api/events/history?key=overlay", nil)
				req.Header.Set("X-Overlay-Operator-Secret", secret)
				r := httptest.NewRecorder()
				s.Handler().ServeHTTP(r, req)
				want := http.StatusUnauthorized
				if secret == "operator" {
					want = http.StatusOK
				}
				if r.Code != want {
					t.Fatalf("history secret=%q status=%d", secret, r.Code)
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			req := httptest.NewRequest("GET", "/api/events/stream?key=overlay", nil).WithContext(ctx)
			r := httptest.NewRecorder()
			s.Handler().ServeHTTP(r, req)
			if r.Code != 200 || !strings.Contains(r.Body.String(), ": connected") {
				t.Fatalf("valid stream=%d %s", r.Code, r.Body)
			}
		})
	}
}

func TestNonLocalEmptyCredentialsFailClosed(t *testing.T) {
	for _, mode := range []string{"lan", "public"} {
		s := NewServer(config.Config{ExposureMode: mode}, events.NewBus(4))
		for _, path := range []string{"/api/events/history", "/api/events/stream", "/api/overlay/display", "/api/dashboard/overlay-token"} {
			r := httptest.NewRecorder()
			s.Handler().ServeHTTP(r, httptest.NewRequest("GET", path, nil))
			if r.Code != 401 {
				t.Fatalf("%s %s=%d", mode, path, r.Code)
			}
		}
		found := false
		for _, check := range s.buildExposureReadinessChecks() {
			if check.Name == "overlay_token" && check.Status == "degraded" {
				found = true
			}
		}
		if !found {
			t.Fatal("missing token readiness degradation")
		}
	}
}

func TestOverlayDisplayOnlyAndHistoryCopies(t *testing.T) {
	s := NewServer(config.Config{OverlayToken: "overlay", OperatorSecret: "operator"}, events.NewBus(4))
	s.dashboardConfig = displayConfigStub{raw: json.RawMessage(`{"display":{"chatMaxVisible":2},"quickActions":[{"secret":"private"}]}`)}
	r := httptest.NewRecorder()
	s.Handler().ServeHTTP(r, httptest.NewRequest("GET", "/api/overlay/display?key=overlay", nil))
	if r.Code != 200 || strings.Contains(r.Body.String(), "private") || strings.Contains(r.Body.String(), "quickActions") || !strings.Contains(r.Body.String(), `"chatMaxVisible":2`) {
		t.Fatalf("display: %d %s", r.Code, r.Body)
	}
	event := events.NewAlert("twitch", "Chat", "hello", events.SeverityInfo)
	event.Data = map[string]any{"twitchEvent": map[string]any{"private": "payload"}, "kind": "chat"}
	store := &legacyHistoryStub{event: event}
	s.history = store
	req := httptest.NewRequest("GET", "/api/events/history", nil)
	req.Header.Set("X-Overlay-Operator-Secret", "operator")
	r = httptest.NewRecorder()
	s.Handler().ServeHTTP(r, req)
	if r.Code != 200 || strings.Contains(r.Body.String(), "twitchEvent") || !strings.Contains(r.Body.String(), "hello") {
		t.Fatalf("history: %d %s", r.Code, r.Body)
	}
	if store.List(1)[0].Data["twitchEvent"] == nil {
		t.Fatal("sanitizing destroyed stored evidence")
	}
}

type legacyHistoryStub struct{ event events.Event }

func (s *legacyHistoryStub) Load() error               { return nil }
func (s *legacyHistoryStub) Append(events.Event) error { return nil }
func (s *legacyHistoryStub) List(int) []events.Event   { return []events.Event{s.event} }

func TestOverlayDisplayMissingStorageUsesDefaults(t *testing.T) {
	s := NewServer(config.Config{}, events.NewBus(4))
	for _, path := range []string{"/api/overlay/display", "/api/dashboard/config"} {
		r := httptest.NewRecorder()
		s.Handler().ServeHTTP(r, httptest.NewRequest("GET", path, nil))
		if r.Code != 200 {
			t.Fatalf("local empty store %s=%d", path, r.Code)
		}
	}
	s.dashboardConfigLoadError = "storage unavailable"
	r := httptest.NewRecorder()
	s.Handler().ServeHTTP(r, httptest.NewRequest("GET", "/api/overlay/display", nil))
	if r.Code != 503 {
		t.Fatalf("unavailable storage=%d", r.Code)
	}
}
