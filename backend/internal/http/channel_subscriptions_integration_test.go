package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/twitch"
)

func TestRetainedChannelUpdateAdmissionObservationAndNoEditorialEffect(t *testing.T) {
	app, alpha, _, _ := configuredStream(t)
	server, err := app.runtime(t.Context(), alpha.tenant)
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	previous := twitchHTTPClient
	twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("unexpected provider call")
	})}
	t.Cleanup(func() { twitchHTTPClient = previous })
	before, err := server.newsroom.Get(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	initial, _ := json.Marshal(before)
	message := streamNotification("metadata-native", "channel.update", time.Now().UTC())
	message.Payload.Subscription.Version = "2"
	message.Payload.Event["title"] = "  provider metadata  "
	message.Payload.Event["language"] = "en"
	message.Payload.Event["category_id"] = "42"
	message.Payload.Event["category_name"] = "Category"
	message.Payload.Event["content_classification_labels"] = []any{"MatureGame"}
	event := notifyStream(t, app, alpha, message)
	waitOwnedStream(t, app, event, "skipped")
	report := observationGet(t, app, alpha)
	if report.Metadata == nil || report.Metadata.EventID != event.ID || report.Metadata.Data.Title != "  provider metadata  " || !report.Metadata.OccurredAt.Equal(message.Metadata.MessageTimestamp) {
		t.Fatal("retained metadata missing", report)
	}
	notifyStream(t, app, alpha, message)
	db, _ := app.store.Product.Scoped(alpha.tenant)
	var jobs, payloads int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM river_job WHERE overlaid_stream_event_id=$1`, event.ID).Scan(&jobs); err != nil || jobs != 1 {
		t.Fatal("duplicate job", jobs, err)
	}
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM platform_stream_payloads WHERE /*tenant:platform_stream_payloads*/ AND event_id=$1`, event.ID).Scan(&payloads); err != nil || payloads != 1 {
		t.Fatal("duplicate payload", payloads, err)
	}
	after, err := server.newsroom.Get(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	final, _ := json.Marshal(after)
	if string(initial) != string(final) || calls.Load() != 0 {
		t.Fatal("channel metadata changed editorial content or fetched provider", calls.Load())
	}
	// A changed duplicate and an invalid version must fail the admission boundary.
	message.Payload.Event["title"] = "changed"
	if err := server.newsroomTwitchLifecycle(t.Context(), message, twitch.Token{UserID: "alpha"}); err == nil {
		t.Fatal("changed duplicate admitted")
	}
	message.Metadata.MessageID = "wrong-version"
	message.Payload.Subscription.Version = "1"
	if err := server.newsroomTwitchLifecycle(t.Context(), message, twitch.Token{UserID: "alpha"}); err == nil {
		t.Fatal("wrong version admitted")
	}
	stop, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := app.stream.close(stop); err != nil {
		t.Fatal(err)
	}
	message.Metadata.MessageID = "stopped-update"
	message.Payload.Subscription.Version = "2"
	if err := server.newsroomTwitchLifecycle(t.Context(), message, twitch.Token{UserID: "alpha"}); err == nil {
		t.Fatal("stopped owner fell back")
	}
	if observationGet(t, app, alpha).Metadata.EventID != event.ID {
		t.Fatal("refused source replaced observation")
	}
}
