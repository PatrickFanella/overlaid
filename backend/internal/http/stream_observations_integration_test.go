package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformingest"
)

func observationEvent(t *testing.T, app *Application, account accountFixture, native string, kind platform.StreamEventKind, occurred, received time.Time) platform.StreamEvent {
	t.Helper()
	var online *platform.StreamOnline
	var offline *platform.StreamOffline
	var update *platform.ChannelUpdate
	switch kind {
	case platform.StreamOnlineKind:
		online = &platform.StreamOnline{StreamID: "public-stream", Type: "live", StartedAt: occurred.Add(-time.Minute)}
	case platform.StreamOfflineKind:
		offline = &platform.StreamOffline{}
	case platform.ChannelUpdateKind:
		update = &platform.ChannelUpdate{Title: "  exact " + native + " title  ", Language: "en", CategoryID: "42", CategoryName: "Category", ContentClassificationLabels: []string{"MatureGame"}}
	}
	event, err := platform.NewStreamEvent(platform.Connection{TenantID: account.tenant, ID: "twitch:alpha", Platform: "twitch"}, native, kind, occurred, received, platform.Identity{ID: "alpha", Login: "Alpha", DisplayName: "Private display name"}, online, offline, update)
	if err != nil {
		t.Fatal(err)
	}
	db, _ := app.store.Product.Scoped(account.tenant)
	store, _ := platformingest.New(db)
	if created, err := store.IngestStream(t.Context(), event, json.RawMessage(`{"private_native_field":"PRIVATE-NATIVE-PAYLOAD"}`)); err != nil || !created {
		t.Fatal(created, err)
	}
	return event
}
func observationGet(t *testing.T, app *Application, account accountFixture) platformingest.StreamObservations {
	t.Helper()
	response := executeApplication(app, accountRequest(http.MethodGet, "/api/twitch/stream-observations", "", account))
	if response.Code != http.StatusOK {
		t.Fatal(response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("private observations are cacheable")
	}
	var report platformingest.StreamObservations
	if err := json.Unmarshal(response.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"PRIVATE-NATIVE-PAYLOAD", "private_native_field", "Private display name", "native_event_id", "trace_id", "claim_hash", "tenant_id", "connection_id", "access_token"} {
		if strings.Contains(response.Body.String(), secret) {
			t.Fatal("private field escaped observation projection", secret)
		}
	}
	return report
}

func TestStreamObservationsLatestOrderingIsolationAndReadOnly(t *testing.T) {
	app, alpha, beta, f, _ := ownedStreamFixture(t)
	now := time.Now().UTC()
	empty := observationGet(t, app, alpha)
	if empty.State != "empty" || empty.ChannelID != "alpha" || empty.Lifecycle != nil || empty.Metadata != nil || empty.ObservedAt.IsZero() {
		t.Fatal("missing-data state")
	}
	first := observationEvent(t, app, alpha, "PRIVATE-DELIVERY-NEWER", platform.ChannelUpdateKind, now.Add(-3*time.Minute), now.Add(-time.Second))
	observationEvent(t, app, alpha, "older", platform.ChannelUpdateKind, now.Add(-4*time.Minute), now)
	observationEvent(t, app, alpha, "online", platform.StreamOnlineKind, now.Add(-2*time.Minute), now)
	offline := observationEvent(t, app, alpha, "offline", platform.StreamOfflineKind, now.Add(-time.Minute), now)
	// The same broadcaster in two workspaces must still have isolated input.
	if err := app.store.SaveConnection(t.Context(), app.keys, beta.tenant, beta.id, "twitch", "alpha", "alpha", nil, json.RawMessage(`{"fixture":true}`)); err != nil {
		t.Fatal(err)
	}
	const snapshot = `SELECT jsonb_build_array((SELECT jsonb_agg(to_jsonb(s) ORDER BY id) FROM normalized_stream_events s),(SELECT jsonb_agg(to_jsonb(p) ORDER BY event_id) FROM platform_stream_payloads p),(SELECT jsonb_agg(to_jsonb(j) ORDER BY id) FROM river_job j WHERE kind='overlaid_stream_dispatch'),(SELECT jsonb_agg(to_jsonb(r) ORDER BY event_id) FROM platform_stream_dispatch_receipts r),(SELECT jsonb_agg(to_jsonb(u) ORDER BY tenant_id,kind,window_start) FROM tenant_usage u))::text`
	var before, after string
	if err := f.Admin.QueryRow(snapshot).Scan(&before); err != nil {
		t.Fatal(err)
	}
	report := observationGet(t, app, alpha)
	if report.State != "observed" || report.Lifecycle == nil || report.Lifecycle.Kind != platform.StreamOfflineKind || report.Lifecycle.EventID != offline.ID || !report.Lifecycle.OccurredAt.Equal(offline.OccurredAt) || report.Lifecycle.StartedAt != nil || report.Metadata == nil || report.Metadata.EventID != first.ID || report.Metadata.Data.Title != first.Update.Title || !report.Metadata.ReceivedAt.Equal(first.ReceivedAt) || report.ObservedAt.Before(now) {
		t.Fatal("latest original observations not preserved")
	}
	report.Metadata.Data.ContentClassificationLabels[0] = "changed locally"
	again := observationGet(t, app, alpha)
	if again.Metadata.Data.ContentClassificationLabels[0] != "MatureGame" {
		t.Fatal("observation mutation reached retained state")
	}
	other := observationGet(t, app, beta)
	if other.State != "empty" || other.ChannelID != "alpha" || other.Metadata != nil || other.Lifecycle != nil {
		t.Fatal("cross-tenant observation leak")
	}
	if err := f.Admin.QueryRow(snapshot).Scan(&after); err != nil || before != after {
		t.Fatal("observation reads changed retained rows/jobs/claims/usage")
	}
	tie := observationEvent(t, app, alpha, "tie-A", platform.ChannelUpdateKind, now, now)
	second := observationEvent(t, app, alpha, "tie-B", platform.ChannelUpdateKind, now, now)
	if second.ID > tie.ID {
		tie = second
	}
	if report := observationGet(t, app, alpha); report.Metadata.EventID != tie.ID {
		t.Fatal("unstable occurrence/ID tie")
	}
}

func TestStreamObservationsConnectionPlanRetentionAndSourceDrift(t *testing.T) {
	for _, mode := range []string{"revoked", "rebound", "reauthorized", "plan", "retention", "deleted", "row-login", "row-received", "trace", "unknown-field", "case-alias", "payload-kind"} {
		t.Run(mode, func(t *testing.T) {
			app, alpha, _, f, _ := ownedStreamFixture(t)
			now := time.Now().UTC()
			occurred := now.Add(-3 * time.Minute)
			if mode == "retention" {
				occurred = now.Add(-48 * time.Hour)
			}
			source := observationEvent(t, app, alpha, "metadata", platform.ChannelUpdateKind, occurred, now)
			var err error
			switch mode {
			case "revoked":
				_, err = f.Admin.Exec(`UPDATE platform_connections SET revoked_at=now() WHERE tenant_id=$1`, alpha.tenant)
			case "rebound", "reauthorized":
				channel := "alpha"
				if mode == "rebound" {
					channel = "replacement"
				}
				err = app.store.SaveConnection(t.Context(), app.keys, alpha.tenant, alpha.id, "twitch", channel, channel, nil, json.RawMessage(`{"fixture":true}`))
			case "plan":
				_, err = f.Admin.Exec(`UPDATE plans SET limits=jsonb_set(limits,'{storedEvents}','0') WHERE id='free'`)
			case "retention":
				_, err = f.Admin.Exec(`UPDATE tenants SET retention_days=1 WHERE id=$1`, alpha.tenant)
			case "deleted":
				_, err = f.Admin.Exec(`DELETE FROM normalized_stream_events WHERE id=$1`, source.ID)
			case "row-login":
				_, err = f.Admin.Exec(`UPDATE normalized_stream_events SET channel_login='wrong' WHERE id=$1`, source.ID)
			case "row-received":
				_, err = f.Admin.Exec(`UPDATE normalized_stream_events SET received_at=received_at+interval '1 second' WHERE id=$1`, source.ID)
			case "trace":
				_, err = f.Admin.Exec(`UPDATE normalized_stream_events SET envelope=jsonb_set(envelope,'{trace_id}','"wrong"') WHERE id=$1`, source.ID)
			case "unknown-field":
				_, err = f.Admin.Exec(`UPDATE normalized_stream_events SET envelope=envelope||'{"unexpected_private":"do not disclose"}' WHERE id=$1`, source.ID)
			case "case-alias":
				_, err = f.Admin.Exec(`UPDATE normalized_stream_events SET envelope=envelope||'{"Version":1}' WHERE id=$1`, source.ID)
			case "payload-kind":
				_, err = f.Admin.Exec(`UPDATE normalized_stream_events SET envelope=jsonb_set(envelope,'{kind}','"stream_online"') WHERE id=$1`, source.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			response := executeApplication(app, accountRequest(http.MethodGet, "/api/twitch/stream-observations", "", alpha))
			switch mode {
			case "plan":
				if response.Code != http.StatusForbidden {
					t.Fatal("plan denied observation read", response.Code)
				}
			case "row-login", "row-received", "trace", "unknown-field", "case-alias", "payload-kind":
				if response.Code != http.StatusServiceUnavailable || strings.Contains(response.Body.String(), "metadata") || strings.Contains(response.Body.String(), "do not disclose") {
					t.Fatal("malformed source was projected", response.Code)
				}
			default:
				report := observationGet(t, app, alpha)
				if mode == "reauthorized" {
					if report.Metadata == nil || report.Metadata.EventID != source.ID {
						t.Fatal("same-channel reauthorization erased historical observation")
					}
					return
				}
				expected := "empty"
				if mode == "revoked" {
					expected = "disconnected"
				}
				if report.State != expected || report.Metadata != nil || report.Lifecycle != nil {
					t.Fatal("ineligible source projected", mode)
				}
			}
		})
	}
}

func TestStreamObservationsPrivateRequestAndRuntimeBoundaries(t *testing.T) {
	app, alpha, _, _, server := ownedStreamFixture(t)
	observationEvent(t, app, alpha, "metadata", platform.ChannelUpdateKind, time.Now().UTC(), time.Now().UTC())
	request := httptest.NewRequest(http.MethodGet, "/api/twitch/stream-observations", nil)
	if response := executeApplication(app, request); response.Code == http.StatusOK {
		t.Fatal("anonymous observation read")
	}
	if response := executeApplication(app, accountRequest(http.MethodPost, "/api/twitch/stream-observations", "{}", alpha)); response.Code == http.StatusOK {
		t.Fatal("observation endpoint accepted mutation")
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := server.streamObservations(canceled); err == nil {
		t.Fatal("canceled observation read succeeded")
	}
	saved := server.streamObservations
	app.invalidate(alpha.tenant)
	if _, err := saved(t.Context()); err == nil {
		t.Fatal("stale runtime observation read succeeded")
	}
	response := observationGet(t, app, alpha)
	if response.Metadata == nil {
		t.Fatal("fresh runtime could not read retained input")
	}
}

func TestStreamObservationsExistingReadRolePolicy(t *testing.T) {
	for _, role := range []string{"owner", "manager", "editor", "moderator"} {
		t.Run(role, func(t *testing.T) {
			app, alpha, _, f, _ := ownedStreamFixture(t)
			if _, err := f.Admin.Exec(`UPDATE tenant_memberships SET role=$1 WHERE tenant_id=$2 AND account_id=$3`, role, alpha.tenant, alpha.id); err != nil {
				t.Fatal(err)
			}
			response := executeApplication(app, accountRequest(http.MethodGet, "/api/twitch/stream-observations", "", alpha))
			expected := http.StatusOK
			if role == "moderator" {
				expected = http.StatusForbidden
			}
			if response.Code != expected {
				t.Fatal("existing read policy changed", role, response.Code)
			}
		})
	}
}

func TestStreamObservationsFileModeHasNoProviderFallback(t *testing.T) {
	server := newTwitchServer(t)
	response := httptest.NewRecorder()
	server.retainedStreamObservations(response, httptest.NewRequest(http.MethodGet, "/api/twitch/stream-observations", nil))
	if response.Code != http.StatusNotImplemented || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("file-mode retained observation fallback", response.Code)
	}
}
