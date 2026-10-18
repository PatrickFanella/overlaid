package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platformmoderation"
)

func operatorPreview(t *testing.T, h *moderationOwnerFixture) moderationPreview {
	t.Helper()
	w := executeApplication(h.app, accountRequest("GET", "/api/bot/moderation/action", "", h.account))
	var p moderationPreview
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &p) != nil || p.Mode != "retained" {
		t.Fatal(w.Code, w.Body.String())
	}
	return p
}
func operatorRequest(p moderationPreview) botModerationActionRequest {
	return botModerationActionRequest{Mode: p.Mode, TenantID: p.TenantID, ActorID: p.ActorID, ConnectionID: p.ConnectionID, RequestKey: "00000000-0000-4000-8000-000000001234", Action: "delete", MessageID: "owner-source"}
}
func operatorPost(h *moderationOwnerFixture, r botModerationActionRequest) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(r)
	return executeApplication(h.app, accountRequest("POST", "/api/bot/moderation/action", string(raw), h.account))
}

func TestOperatorModerationHTTPDeliveryAndReceiptOnly(t *testing.T) {
	for _, shared := range []bool{false, true} {
		t.Run(map[bool]string{false: "broadcaster", true: "shared"}[shared], func(t *testing.T) {
			h := ownedModerationFixture(t, shared)
			h.source(t, time.Now().Add(-time.Hour))
			consumer, err := h.app.prepareModeration(t.Context(), h.database.ModerationWorkerURL, h.database.ModerationPacerURL)
			if err != nil {
				t.Fatal(err)
			}
			request := operatorRequest(operatorPreview(t, h))
			if w := operatorPost(h, request); w.Code != 503 {
				t.Fatal("prepared owner admitted", w.Code, w.Body.String())
			}
			if err := consumer.start(t.Context()); err != nil {
				t.Fatal(err)
			}
			entered, release := make(chan struct{}), make(chan struct{})
			h.onAttempt = func(r *http.Request) (*http.Response, error) {
				close(entered)
				select {
				case <-release:
				case <-r.Context().Done():
					return nil, r.Context().Err()
				}
				return &http.Response{StatusCode: 204, Header: http.Header{}, Body: http.NoBody}, nil
			}
			released := false
			defer func() {
				if !released {
					close(release)
				}
			}()
			w := operatorPost(h, request)
			if w.Code != 202 || !strings.Contains(w.Body.String(), `"queued":true`) {
				t.Fatal("not queued", w.Code, w.Body.String())
			}
			var accepted struct {
				Receipt platformmoderation.ManualStatus `json:"receipt"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &accepted); err != nil || len(accepted.Receipt.ActionIDs) != 1 {
				t.Fatal(err, w.Body.String())
			}
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("worker did not attempt")
			}
			request.Retry = true
			if w := operatorPost(h, request); w.Code != 202 || !strings.Contains(w.Body.String(), `"state":"running"`) {
				t.Fatal("running status", w.Code, w.Body.String())
			}
			close(release)
			released = true
			waitOwnedModeration(t, h, "delivered")
			requests := h.requests.Load()
			if err := consumer.close(t.Context()); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				w := operatorPost(h, request)
				if w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"delivered"`) || h.requests.Load() != requests {
					t.Fatal("stopped receipt replayed", w.Code, w.Body.String())
				}
				for _, private := range []string{"owner-source", "PRIVATE", "owner-access", "reason", "target_actor_id", "payload_hash"} {
					if strings.Contains(w.Body.String(), private) {
						t.Fatal("private HTTP receipt", private)
					}
				}
			}
			var actions, jobs int
			var deadline time.Time
			if err := h.database.Admin.QueryRow(`SELECT count(*),min(deadline) FROM platform_moderation_actions`).Scan(&actions, &deadline); err != nil || actions != 1 || !deadline.Equal(accepted.Receipt.Deadline) {
				t.Fatal("changed action/deadline", actions, err)
			}
			if err := h.database.Admin.QueryRow(`SELECT count(*) FROM river_job WHERE kind='overlaid_moderation'`).Scan(&jobs); err != nil || jobs != 1 {
				t.Fatal(jobs, err)
			}
			changed := request
			changed.Action = "ban"
			changed.TargetUserID = "777"
			if w := operatorPost(h, changed); w.Code != 409 {
				t.Fatal("changed payload", w.Code, w.Body.String())
			}
			if _, err := h.database.Admin.Exec(`DELETE FROM platform_moderation_actions`); err != nil {
				t.Fatal(err)
			}
			if w := operatorPost(h, request); w.Code != 409 || !strings.Contains(w.Body.String(), "keep this request unresolved") {
				t.Fatal("purged receipt", w.Code, w.Body.String())
			}
			if h.attempts.Load() != 1 {
				t.Fatal("replayed", h.attempts.Load())
			}
		})
	}
}

func TestOperatorModerationHTTPRefusals(t *testing.T) {
	for _, mode := range []string{"retry-missing", "csrf", "editor", "actor", "tenant", "channel", "mode", "missing-target", "stopped", "stale-runtime", "controller"} {
		t.Run(mode, func(t *testing.T) {
			h := ownedModerationFixture(t, false)
			h.source(t, time.Now())
			consumer, err := h.app.prepareModeration(t.Context(), h.database.ModerationWorkerURL, h.database.ModerationPacerURL)
			if err != nil {
				t.Fatal(err)
			}
			if err := consumer.start(t.Context()); err != nil {
				t.Fatal(err)
			}
			request := operatorRequest(operatorPreview(t, h))
			expected := 409
			switch mode {
			case "retry-missing":
				request.Retry = true
			case "editor":
				if _, err := h.database.Admin.Exec(`UPDATE tenant_memberships SET role='editor' WHERE tenant_id=$1 AND account_id=$2`, h.account.tenant, h.account.id); err != nil {
					t.Fatal(err)
				}
				expected = 403
			case "actor":
				request.ActorID = "00000000-0000-4000-8000-000000000002"
			case "tenant":
				request.TenantID = "00000000-0000-4000-8000-000000000001"
			case "channel":
				request.ConnectionID = "twitch:999"
			case "mode":
				request.Mode = "direct"
			case "missing-target":
				request.MessageID = "not-retained"
				expected = 400
			case "stopped":
				if err := consumer.close(t.Context()); err != nil {
					t.Fatal(err)
				}
				expected = 503
			case "stale-runtime":
				server, _ := h.app.runtime(t.Context(), h.account.tenant)
				h.app.mu.Lock()
				runtime := h.app.runtimes[h.account.tenant]
				delete(h.app.runtimes, h.account.tenant)
				h.app.mu.Unlock()
				defer func() { h.app.mu.Lock(); h.app.runtimes[h.account.tenant] = runtime; h.app.mu.Unlock() }()
				ctx := server.tenantContext
				if _, cancel, err := consumer.manualContext(ctx, server); err == nil {
					cancel()
					t.Fatal("stale runtime admitted")
				}
				return
			case "controller":
				unlockChatController(t, h.app)
				expected = 503
			case "csrf":
				expected = 403
			}
			raw, _ := json.Marshal(request)
			r := accountRequest("POST", "/api/bot/moderation/action", string(raw), h.account)
			if mode == "csrf" {
				r.Header.Del("X-Overlaid-CSRF")
			}
			w := executeApplication(h.app, r)
			if w.Code != expected {
				t.Fatal(mode, w.Code, w.Body.String())
			}
			var count int
			if err := h.database.Admin.QueryRow(`SELECT count(*) FROM platform_moderation_actions`).Scan(&count); err != nil || count != 0 || h.attempts.Load() != 0 {
				t.Fatal("refused work had effects", count, h.attempts.Load(), err)
			}
		})
	}
}
