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

func nukePost(h *moderationOwnerFixture, path string, input any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(input)
	return executeApplication(h.app, accountRequest("POST", "/api/bot/moderation/nuke/"+path, string(raw), h.account))
}

func nukeHTTPPreview(t *testing.T, h *moderationOwnerFixture, pattern string, radiation int) retainedNukePreviewResponse {
	t.Helper()
	r := retainedNukePreviewRequest{moderationPreview: operatorPreview(t, h), Options: retainedNukeOptions{Pattern: pattern, RadiationMinutes: radiation}}
	w := nukePost(h, "preview", r)
	var p retainedNukePreviewResponse
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &p) != nil || p.Mode != "retained" || !p.ExpiresAt.Equal(p.ObservedAt.Add(2*time.Minute)) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("nuke preview", w.Code, w.Body.String())
	}
	return p
}

func nukeHTTPRequest(p retainedNukePreviewResponse) retainedNukeRequest {
	return retainedNukeRequest{moderationPreview: p.moderationPreview, Plan: p.Plan, RequestKey: "00000000-0000-4000-8000-000000009001"}
}

func noNukeWork(t *testing.T, h *moderationOwnerFixture) {
	t.Helper()
	var sources, actions, jobs, rules int
	err := h.database.Admin.QueryRow(`SELECT (SELECT count(*) FROM platform_moderation_sources),(SELECT count(*) FROM platform_moderation_actions),(SELECT count(*) FROM river_job WHERE kind='overlaid_moderation'),(SELECT count(*) FROM bot_moderation_rules)`).Scan(&sources, &actions, &jobs, &rules)
	if err != nil || sources != 0 || actions != 0 || jobs != 0 || rules != 0 || h.attempts.Load() != 0 {
		t.Fatal("partial nuke work", sources, actions, jobs, rules, h.attempts.Load(), err)
	}
}

func TestRetainedNukeHTTPDeliveryAndStoppedStatus(t *testing.T) {
	h := ownedModerationFixture(t, false)
	h.source(t, time.Now().Add(-time.Minute))
	consumer, err := h.app.prepareModeration(t.Context(), h.database.ModerationWorkerURL, h.database.ModerationPacerURL)
	if err != nil {
		t.Fatal(err)
	}
	p := nukeHTTPPreview(t, h, "PRIVATE_OWNER_SOURCE", 0)
	if len(p.Targets) != 1 || p.Targets[0].MessageID != "owner-source" || p.Options.Action != "delete" || p.Plan == "" || p.Fingerprint == "" {
		t.Fatal("preview projection", p.Options, len(p.Targets))
	}
	request := nukeHTTPRequest(p)
	noNukeWork(t, h)
	if w := nukePost(h, "request", request); w.Code != 503 {
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
	w := nukePost(h, "request", request)
	var accepted retainedNukeReceipt
	if w.Code != 202 || json.Unmarshal(w.Body.Bytes(), &accepted) != nil || !accepted.Queued || accepted.RadiationOnly || len(accepted.Receipt.ActionIDs) != 1 || w.Result().Header.Get("Content-Type") != "application/json" {
		t.Fatal("nuke admission", w.Code, w.Body.String())
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not attempt")
	}
	request.Retry = true
	if w := nukePost(h, "request", request); w.Code != 202 {
		t.Fatal("running lookup", w.Code, w.Body.String())
	}
	close(release)
	released = true
	waitOwnedModeration(t, h, "delivered")
	if err := consumer.close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := h.database.Admin.Exec(`DELETE FROM bot_moderation_rules`); err != nil {
		t.Fatal(err)
	}
	providerRequests := h.requests.Load()
	for range 2 {
		w := nukePost(h, "request", request)
		var status retainedNukeReceipt
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &status) != nil || status.Queued || status.Receipt.SourceID != accepted.Receipt.SourceID || !status.Receipt.Deadline.Equal(accepted.Receipt.Deadline) || status.Receipt.Receipts[0].State != "delivered" || h.requests.Load() != providerRequests {
			t.Fatal("stopped lookup", w.Code, w.Body.String())
		}
		for _, private := range []string{p.Plan, "PRIVATE_OWNER_SOURCE", "owner-source", request.RequestKey, "reason", "payload_hash"} {
			if strings.Contains(w.Body.String(), private) {
				t.Fatal("private receipt", private)
			}
		}
	}
	if _, err := h.database.Admin.Exec(`DELETE FROM platform_moderation_actions`); err != nil {
		t.Fatal(err)
	}
	if w := nukePost(h, "request", request); w.Code != 409 {
		t.Fatal("purged lookup", w.Code, w.Body.String())
	}
	if h.attempts.Load() != 1 {
		t.Fatal("replayed nuke", h.attempts.Load())
	}
}

func TestRetainedNukeHTTPRadiationOnlyAndEmpty(t *testing.T) {
	h := ownedModerationFixture(t, false)
	consumer, err := h.app.prepareModeration(t.Context(), h.database.ModerationWorkerURL, h.database.ModerationPacerURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := consumer.start(t.Context()); err != nil {
		t.Fatal(err)
	}
	empty := nukeHTTPPreview(t, h, "no matches", 0)
	if len(empty.Targets) != 0 || empty.Plan != "" || empty.Fingerprint != "" {
		t.Fatal("empty preview admitted input")
	}
	if w := nukePost(h, "request", nukeHTTPRequest(empty)); w.Code != 409 {
		t.Fatal("empty admitted", w.Code, w.Body.String())
	}
	noNukeWork(t, h)
	p := nukeHTTPPreview(t, h, "no matches", 5)
	r := nukeHTTPRequest(p)
	w := nukePost(h, "request", r)
	var receipt retainedNukeReceipt
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &receipt) != nil || receipt.Queued || !receipt.RadiationOnly || len(receipt.Receipt.ActionIDs) != 0 || receipt.Receipt.RadiationRuleID == "" {
		t.Fatal("radiation occurrence", w.Code, w.Body.String())
	}
	if err := consumer.close(t.Context()); err != nil {
		t.Fatal(err)
	}
	r.Retry = true
	if w := nukePost(h, "request", r); w.Code != 200 {
		t.Fatal("radiation status", w.Code, w.Body.String())
	}
	var actions, jobs, rules int
	if err := h.database.Admin.QueryRow(`SELECT (SELECT count(*) FROM platform_moderation_actions),(SELECT count(*) FROM river_job WHERE kind='overlaid_moderation'),(SELECT count(*) FROM bot_moderation_rules)`).Scan(&actions, &jobs, &rules); err != nil || actions != 0 || jobs != 0 || rules != 1 || h.attempts.Load() != 0 {
		t.Fatal("radiation made provider work", actions, jobs, rules, err)
	}
}

func TestRetainedNukeHTTPRefusals(t *testing.T) {
	for _, mode := range []string{"csrf", "editor", "actor", "tenant", "channel", "mode", "binding", "tamper", "missing-key", "retry-missing", "stopped", "controller", "deleted-target", "invalid-key", "unknown", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			h := ownedModerationFixture(t, false)
			h.source(t, time.Now().Add(-5*time.Minute))
			consumer, err := h.app.prepareModeration(t.Context(), h.database.ModerationWorkerURL, h.database.ModerationPacerURL)
			if err != nil {
				t.Fatal(err)
			}
			if err := consumer.start(t.Context()); err != nil {
				t.Fatal(err)
			}
			p := nukeHTTPPreview(t, h, "PRIVATE_OWNER_SOURCE", 5)
			r := nukeHTTPRequest(p)
			expected := 409
			switch mode {
			case "csrf", "editor":
				expected = 403
			case "actor":
				r.ActorID = "00000000-0000-4000-8000-000000000099"
			case "tenant":
				r.TenantID = "00000000-0000-4000-8000-000000000001"
			case "channel":
				r.ConnectionID = "twitch:999"
			case "mode":
				r.Mode = "direct"
			case "binding":
				_, err = h.database.Admin.Exec(`UPDATE platform_connections SET granted_at=granted_at+interval '1 second'`)
			case "tamper":
				r.Plan = r.Plan[:len(r.Plan)-3] + "abc"
			case "missing-key":
				server, runtimeErr := h.app.runtime(t.Context(), h.account.tenant)
				if runtimeErr != nil {
					t.Fatal(runtimeErr)
				}
				server.vaultKeys.Keys = nil
			case "retry-missing":
				r.Retry = true
			case "stopped":
				err = consumer.close(t.Context())
				expected = 503
			case "controller":
				unlockChatController(t, h.app)
				expected = 503
			case "deleted-target":
				_, err = h.database.Admin.Exec(`DELETE FROM normalized_chat_events`)
				expected = 400
			case "invalid-key":
				r.RequestKey = "new-key"
				expected = 400
			case "unknown", "oversize":
				expected = 400

			}
			if mode == "editor" {
				_, err = h.database.Admin.Exec(`UPDATE tenant_memberships SET role='editor' WHERE tenant_id=$1 AND account_id=$2`, h.account.tenant, h.account.id)
			}
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(r)
			if mode == "unknown" {
				raw = append(raw[:len(raw)-1], []byte(`,"targets":[]}`)...)
			}
			if mode == "oversize" {
				raw = []byte(strings.Repeat("x", platformmoderation.MaxNukePlanTokenBytes+8192))
			}
			req := accountRequest("POST", "/api/bot/moderation/nuke/request", string(raw), h.account)
			if mode == "csrf" {
				req.Header.Del("X-Overlaid-CSRF")
			}
			w := executeApplication(h.app, req)
			if w.Code != expected {
				t.Fatal(mode, w.Code, w.Body.String())
			}
			noNukeWork(t, h)
		})
	}
}

func TestRetainedNukeHTTPPreviewRefusals(t *testing.T) {
	for _, mode := range []string{"no-owner", "invalid-regex", "history-limit", "unknown", "csrf"} {
		t.Run(mode, func(t *testing.T) {
			h := ownedModerationFixture(t, false)
			p := moderationPreview{Mode: "retained", TenantID: h.account.tenant, ActorID: h.account.id, ConnectionID: "twitch:123"}
			if mode != "no-owner" {
				if _, err := h.app.prepareModeration(t.Context(), h.database.ModerationWorkerURL, h.database.ModerationPacerURL); err != nil {
					t.Fatal(err)
				}
				p = operatorPreview(t, h)
			}
			r := retainedNukePreviewRequest{moderationPreview: p, Options: retainedNukeOptions{Pattern: "spam", RadiationMinutes: 5}}
			expected := 400
			switch mode {
			case "no-owner":
				expected = 503
			case "invalid-regex":
				r.Options.Pattern, r.Options.Regex = "[", true
			case "history-limit":
				r.Options.HistoryLimit = 251
			case "csrf":
				expected = 403
			}
			raw, _ := json.Marshal(r)
			if mode == "unknown" {
				raw = append(raw[:len(raw)-1], []byte(`,"targetIds":[]}`)...)
			}
			req := accountRequest("POST", "/api/bot/moderation/nuke/preview", string(raw), h.account)
			if mode == "csrf" {
				req.Header.Del("X-Overlaid-CSRF")
			}
			w := executeApplication(h.app, req)
			if w.Code != expected {
				t.Fatal(mode, w.Code, w.Body.String())
			}
			noNukeWork(t, h)
		})
	}
}
