package bot

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
	"git.subcult.tv/PatrickFanella/over-laid/internal/history"
	"git.subcult.tv/PatrickFanella/over-laid/internal/sessions"
	"git.subcult.tv/PatrickFanella/over-laid/internal/testpostgres"
)

// Isolate global list queries and enabled timers from the running API worker.
// Only this test's schema is dropped; existing database rows remain untouched.
func isolatedPostgresBotStore(t *testing.T) (*Store, *history.PostgresStore) {
	t.Helper()
	databaseURL := os.Getenv("OVERLAID_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set OVERLAID_TEST_DATABASE_URL to a disposable Postgres database")
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		t.Fatal("OVERLAID_TEST_DATABASE_URL must be a Postgres URL")
	}
	admin, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal("open test database")
	}
	schema := "bot_test_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := testpostgres.EnsurePGCrypto(ctx, admin); err != nil {
		_ = admin.Close()
		t.Fatalf("initialize database-wide test extension: %v", err)
	}
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		_ = admin.Close()
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := admin.ExecContext(cleanupCtx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Errorf("remove test schema: %v", err)
		}
		_ = admin.Close()
	})
	query := parsed.Query()
	query.Set("options", strings.TrimSpace(query.Get("options")+" -csearch_path="+schema+",public"))
	parsed.RawQuery = query.Encode()
	scopedURL := parsed.String()
	eventStore, err := history.NewPostgres(scopedURL, 250)
	if err != nil {
		t.Fatalf("initialize isolated event schema: %v", err)
	}
	t.Cleanup(func() { _ = eventStore.Close() })
	sessionStore, err := sessions.NewPostgres(scopedURL)
	if err != nil {
		t.Fatalf("initialize isolated session schema: %v", err)
	}
	t.Cleanup(func() { _ = sessionStore.Close() })
	store, err := NewPostgres(scopedURL)
	if err != nil {
		t.Fatalf("initialize isolated bot schema: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	var currentSchema string
	if err := store.db.QueryRowContext(ctx, "SELECT current_schema()").Scan(&currentSchema); err != nil || currentSchema != schema {
		t.Fatalf("bot store not isolated: schema=%q err=%v", currentSchema, err)
	}
	return store, eventStore
}

func TestRecentAuditPersistencePostgres(t *testing.T) {
	store, _ := isolatedPostgresBotStore(t)
	ctx := context.Background()
	ids := []string{}
	base := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	for i := range 3 {
		event, err := store.RecordAudit(ctx, AuditEvent{Source: " CHAT ", ActorRole: " MODERATOR ", Command: "!hello", Action: "ticker", Result: " ALLOWED ", Data: json.RawMessage(`{"unknownVariables":["missing"],"nested":{"count":3}}`)})
		if err != nil || event.ID == "" || event.CreatedAt.IsZero() {
			t.Fatalf("insert audit: %#v, %v", event, err)
		}
		ids = append(ids, event.ID)
		at := base
		if i > 0 {
			at = base.Add(time.Second)
		}
		if _, err := store.db.ExecContext(ctx, "UPDATE bot_audit_events SET created_at=$2 WHERE id=$1", event.ID, at); err != nil {
			t.Fatal(err)
		}
	}
	tiedIDs := append([]string(nil), ids[1:]...)
	sort.Sort(sort.Reverse(sort.StringSlice(tiedIDs)))
	rows, err := store.RecentAudit(ctx, 2)
	if err != nil || len(rows) != 2 || rows[0].ID != tiedIDs[0] || rows[1].ID != tiedIDs[1] {
		t.Fatalf("bounded newest-first audit=%#v, %v", rows, err)
	}
	for _, event := range rows {
		if event.Source != "chat" || event.ActorRole != "moderator" || event.Command != "hello" || event.Result != "allowed" || event.EventID != "" || event.StreamSessionID != "" {
			t.Fatalf("audit fields did not round trip: %#v", event)
		}
		var data struct {
			UnknownVariables []string `json:"unknownVariables"`
		}
		if err := json.Unmarshal(event.Data, &data); err != nil || len(data.UnknownVariables) != 1 || data.UnknownVariables[0] != "missing" {
			t.Fatalf("audit metadata lost: %s, %v", event.Data, err)
		}
	}
	for _, payload := range []string{`[]`, `{"accessToken":"DO_NOT_STORE"}`, `{"items":[{"nested":{"Authorization":"DO_NOT_STORE"}}]}`, `{"nested":{"api_key":"DO_NOT_STORE"}}`, `{"items":[{"PRIVATE-KEY":"DO_NOT_STORE"}]}`} {
		if _, err := store.RecordAudit(ctx, AuditEvent{Source: "chat", Result: "allowed", Data: json.RawMessage(payload)}); err == nil {
			t.Fatalf("unsafe audit payload accepted: %s", payload)
		}
	}
	rows, err = store.RecentAudit(ctx, 250)
	if err != nil || len(rows) != 3 || rows[2].ID != ids[0] {
		t.Fatalf("rejected audit reached storage or ordering changed: %#v, %v", rows, err)
	}
}

func TestTimerCadencePostgres(t *testing.T) {
	store, _ := isolatedPostgresBotStore(t)
	ctx := context.Background()
	base := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	fixtures := map[string]Timer{}
	for _, config := range []Timer{{Name: "fresh", Enabled: true, IntervalSeconds: 30, Response: "Hello", Action: "ticker"}, {Name: "scheduled", Enabled: true, IntervalSeconds: 30, Response: "Again", Action: "alert"}, {Name: "paused", Enabled: false, IntervalSeconds: 30, Response: "Paused", Action: "ticker"}, {Name: "later", Enabled: true, IntervalSeconds: 60, Response: "Later", Action: "ticker"}} {
		timer, err := store.SaveTimer(ctx, config)
		if err != nil {
			t.Fatal(err)
		}
		fixtures[config.Name] = timer
		if config.Name == "scheduled" || config.Name == "later" {
			if err := store.MarkTimerRun(ctx, timer.ID, base); err != nil {
				t.Fatal(err)
			}
		}
	}
	check := func(at time.Time, limit int, want string) {
		t.Helper()
		rows, err := store.DueTimers(ctx, at, limit)
		if err != nil {
			t.Fatal(err)
		}
		names := []string{}
		for _, row := range rows {
			names = append(names, row.Name)
		}
		if strings.Join(names, ",") != want {
			t.Fatalf("due at %s: %v, want %q", at, names, want)
		}
	}
	check(base, 10, "fresh")
	check(base.Add(30*time.Second-time.Microsecond), 10, "fresh")
	check(base.Add(30*time.Second), 10, "fresh,scheduled")
	for _, name := range []string{"fresh", "scheduled"} {
		if err := store.MarkTimerRun(ctx, fixtures[name].ID, base.Add(30*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	check(base.Add(60*time.Second-time.Microsecond), 10, "")
	check(base.Add(60*time.Second), 10, "later,fresh,scheduled")
	check(base.Add(60*time.Second), 1, "later")
	scheduled, err := store.GetTimer(ctx, fixtures["scheduled"].ID)
	if err != nil || scheduled.LastRunAt == nil || !scheduled.LastRunAt.Equal(base.Add(30*time.Second)) {
		t.Fatalf("last run not persisted: %#v, %v", scheduled, err)
	}
	scheduled.IntervalSeconds = 60
	if _, err := store.SaveTimer(ctx, scheduled); err != nil {
		t.Fatal(err)
	}
	check(base.Add(60*time.Second), 10, "later,fresh")
	if _, err := store.SetTimerEnabled(ctx, fixtures["fresh"].ID, false); err != nil {
		t.Fatal(err)
	}
	check(base.Add(60*time.Second), 10, "later")
	if err := store.DeleteTimer(ctx, fixtures["later"].ID); err != nil {
		t.Fatal(err)
	}
	check(base.Add(60*time.Second), 10, "")
}

func TestTimerSchedulerEventAndAuditPersistencePostgres(t *testing.T) {
	store, eventStore := isolatedPostgresBotStore(t)
	ctx := context.Background()
	var sessionID string
	if err := store.db.QueryRowContext(ctx, `INSERT INTO stream_sessions (title, kind) VALUES ('Isolated scheduler fixture', 'rehearsal') RETURNING id`).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}
	eventStore.SetActiveSessionID(sessionID)
	timerIDs := []string{}
	for _, timer := range []Timer{{Name: "welcome", Enabled: true, IntervalSeconds: 30, Response: "Welcome {channel} {missing}", Action: "ticker"}, {Name: "hydrate", Enabled: true, IntervalSeconds: 30, Response: "Hydrate", Action: "alert"}, {Name: "paused", Enabled: false, IntervalSeconds: 30, Response: "Paused", Action: "ticker"}} {
		saved, err := store.SaveTimer(ctx, timer)
		if err != nil {
			t.Fatal(err)
		}
		if saved.Enabled {
			timerIDs = append(timerIDs, saved.ID)
		}
	}
	bus := events.NewBus(8)
	client := bus.Subscribe()
	defer bus.Unsubscribe(client.ID)
	published := map[string]events.Event{}
	scheduler := TimerScheduler{Store: store, Channel: "fixture-channel", StreamSessionID: eventStore.ActiveSessionID, Publish: func(event events.Event) {
		bus.Publish(event)
		if err := eventStore.Append(event); err != nil {
			t.Fatalf("persist published timer: %v", err)
		}
		published[event.ID] = event
	}}
	now := time.Now().UTC().Truncate(time.Microsecond)
	scheduler.runOnce(ctx, now)
	if len(published) != 2 || len(eventStore.List(10)) != 2 {
		t.Fatalf("timer publication/history count=%d/%d", len(published), len(eventStore.List(10)))
	}
	for range 2 {
		select {
		case event := <-client.Events:
			if _, ok := published[event.ID]; !ok {
				t.Fatalf("unexpected bus event: %#v", event)
			}
		default:
			t.Fatal("scheduled timer missing from event bus")
		}
	}
	audit, total, err := store.AuditForSession(ctx, sessionID, 10)
	if err != nil || total != 2 || len(audit) != 2 {
		t.Fatalf("scheduled audit=%#v total=%d err=%v", audit, total, err)
	}
	for _, row := range audit {
		event, ok := published[row.EventID]
		if !ok || row.Source != "timer" || row.ActorRole != "system" || row.Result != "allowed" || row.Reason != "timer_due" || row.StreamSessionID != sessionID || row.Action != event.Data["action"] {
			t.Fatalf("unlinked persisted audit: %#v", row)
		}
		if row.Command == "welcome" {
			var data struct {
				UnknownVariables []string `json:"unknownVariables"`
			}
			if err := json.Unmarshal(row.Data, &data); err != nil || len(data.UnknownVariables) != 1 || data.UnknownVariables[0] != "missing" {
				t.Fatalf("template diagnostics lost: %s, %v", row.Data, err)
			}
		}
	}
	for _, id := range timerIDs {
		timer, err := store.GetTimer(ctx, id)
		if err != nil || timer.LastRunAt == nil || !timer.LastRunAt.Equal(now) {
			t.Fatalf("scheduled last run missing: %#v, %v", timer, err)
		}
	}
	scheduler.runOnce(ctx, now.Add(30*time.Second-time.Microsecond))
	if len(published) != 2 {
		t.Fatal("scheduler repeated timers before their interval")
	}
	scheduler.runOnce(ctx, now.Add(30*time.Second))
	_, total, err = store.AuditForSession(ctx, sessionID, 10)
	if err != nil || len(published) != 4 || total != 4 {
		t.Fatalf("next scheduled run missing: published=%d audit=%d err=%v", len(published), total, err)
	}
}

func TestRuntimeQueuedAuditPersistencePostgres(t *testing.T) {
	store, eventStore := isolatedPostgresBotStore(t)
	runtime := NewRuntime(store)
	for _, command := range []string{"first", "second", "third"} {
		event := events.Event{ID: "queued-" + command, Type: events.EventTypeCommand, Source: "twitch", Message: command}
		if err := eventStore.Append(event); err != nil {
			t.Fatal(err)
		}
		runtime.RecordOutcome(context.Background(), AuditOutcome{Source: SourceChat, ActorRole: RoleViewer, Command: command, Result: ResultAllowed, EventID: event.ID})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := runtime.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	audits, err := store.RecentAudit(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(audits) != 3 {
		t.Fatalf("queued audit count=%d", len(audits))
	}
	for i, name := range []string{"third", "second", "first"} {
		if audits[i].Command != name || audits[i].EventID != "queued-"+name {
			t.Fatalf("queued audit order: %#v", audits)
		}
	}
	if runtime.LastAuditError() != "" {
		t.Fatal(runtime.LastAuditError())
	}
}

func TestCommandCRUDPostgres(t *testing.T) {
	store, _ := isolatedPostgresBotStore(t)
	ctx := context.Background()
	original, err := store.SaveCommand(ctx, Command{Name: " !HELLO ", Enabled: true, Response: "Hello {user}", Action: "ticker", ResponseMode: "both", RoleRequired: "moderator", CooldownSeconds: 30})
	if err != nil {
		t.Fatal(err)
	}
	if original.ID == "" || original.Name != "hello" || original.CreatedAt.IsZero() {
		t.Fatalf("invalid created command: %#v", original)
	}
	original.Name, original.Response, original.Action = "hello_2", "Welcome {displayName}", "alert"
	original.ResponseMode, original.RoleRequired, original.CooldownSeconds = "chat", "operator", 120
	updated, err := store.SaveCommand(ctx, original)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != original.ID || !updated.CreatedAt.Equal(original.CreatedAt) || updated.Name != "hello_2" || updated.Response != original.Response || updated.Action != "alert" || updated.ResponseMode != "chat" || updated.RoleRequired != "operator" || updated.CooldownSeconds != 120 {
		t.Fatalf("edit lost fields or identity: %#v", updated)
	}
	paused, err := store.SetCommandEnabled(ctx, updated.ID, false)
	if err != nil || paused.Enabled || paused.Response != updated.Response {
		t.Fatalf("pause=%#v err=%v", paused, err)
	}
	for _, invalid := range []Command{{Name: "_hidden", Response: "Hello"}, {Name: strings.Repeat("a", 33), Response: "Hello"}, {Name: "unsafe", Response: "Hello", Action: "obs"}} {
		if _, err := store.SaveCommand(ctx, invalid); err == nil {
			t.Fatalf("invalid command persisted: %#v", invalid)
		}
	}
	invalidUpdate := updated
	invalidUpdate.Name = "-invalid"
	if _, err := store.SaveCommand(ctx, invalidUpdate); err == nil {
		t.Fatal("invalid update accepted")
	}
	rows, err := store.ListCommands(ctx)
	if err != nil || len(rows) != 1 || rows[0].ID != paused.ID || rows[0].Name != paused.Name || rows[0].Enabled {
		t.Fatalf("invalid writes changed saved command: %#v, %v", rows, err)
	}
	if err := store.DeleteCommand(ctx, paused.ID); err != nil {
		t.Fatal(err)
	}
	rows, err = store.ListCommands(ctx)
	if err != nil || len(rows) != 0 {
		t.Fatalf("deleted command remains: %#v, %v", rows, err)
	}
	if err := store.DeleteCommand(ctx, paused.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing delete=%v", err)
	}
	if _, err := store.SaveCommand(ctx, updated); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing update=%v", err)
	}
	if _, err := store.SetCommandEnabled(ctx, paused.ID, true); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing enable=%v", err)
	}
}

func TestEmptyTimerAttemptsDoNotStarveValidTimersPostgres(t *testing.T) {
	store, eventStore := isolatedPostgresBotStore(t)
	ctx := context.Background()
	for i := range 10 {
		_, err := store.SaveTimer(ctx, Timer{Name: fmt.Sprintf("empty_%02d", i), Enabled: true, IntervalSeconds: 30, Response: "{missing}", Action: "ticker"})
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err := store.SaveTimer(ctx, Timer{Name: "valid", Enabled: true, IntervalSeconds: 30, Response: "Welcome", Action: "ticker"})
	if err != nil {
		t.Fatal(err)
	}
	// Names sort the empty fixtures ahead of the valid timer in the first batch.
	published := []events.Event{}
	scheduler := TimerScheduler{Store: store, Publish: func(event events.Event) {
		published = append(published, event)
		if err := eventStore.Append(event); err != nil {
			t.Fatal(err)
		}
	}}
	now := time.Now().UTC().Truncate(time.Microsecond)
	scheduler.runOnce(ctx, now)
	if len(published) != 0 {
		t.Fatalf("empty timers published: %#v", published)
	}
	audits, err := store.RecentAudit(ctx, 20)
	if err != nil || len(audits) != 10 {
		t.Fatalf("failed attempts=%#v, %v", audits, err)
	}
	for _, audit := range audits {
		if audit.Result != "failed" || audit.Reason != "timer_response_empty" || audit.EventID != "" || !strings.Contains(string(audit.Data), "missing") {
			t.Fatalf("incorrect failed attempt: %#v", audit)
		}
	}
	scheduler.runOnce(ctx, now.Add(5*time.Second))
	if len(published) != 1 || published[0].Data["timer"] != "valid" {
		t.Fatalf("valid timer starved: %#v", published)
	}
	due, err := store.DueTimers(ctx, now.Add(29*time.Second), 20)
	if err != nil || len(due) != 0 {
		t.Fatalf("failed attempts remain due: %#v, %v", due, err)
	}
	due, err = store.DueTimers(ctx, now.Add(30*time.Second), 20)
	if err != nil || len(due) != 10 {
		t.Fatalf("failed attempt cadence=%d, %v", len(due), err)
	}
}

func TestTimerCRUDAndRejectedJSONPostgres(t *testing.T) {
	store, _ := isolatedPostgresBotStore(t)
	ctx := context.Background()
	created, err := store.SaveTimer(ctx, Timer{Name: " JSON_NOTICE ", IntervalSeconds: 30, Response: `{"message":"Welcome {channel}"}`, Action: "ticker"})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Name != "json_notice" || created.Enabled || created.CreatedAt.IsZero() {
		t.Fatalf("created timer=%#v", created)
	}
	edited := created
	edited.Response, edited.Action, edited.IntervalSeconds = "Welcome again", "alert", 86400
	updated, err := store.SaveTimer(ctx, edited)
	if err != nil || updated.ID != created.ID || !updated.CreatedAt.Equal(created.CreatedAt) || updated.Response != edited.Response || updated.Action != "alert" || updated.IntervalSeconds != 86400 {
		t.Fatalf("updated timer=%#v, %v", updated, err)
	}
	enabled, err := store.SetTimerEnabled(ctx, created.ID, true)
	if err != nil || !enabled.Enabled || enabled.Response != edited.Response {
		t.Fatalf("enable=%#v, %v", enabled, err)
	}
	for _, response := range []string{`{"API_KEY":"value"}`, `[{"nested":{"cookie":"value"}}]`, `{"safe":{"privateKey":"value"},"safe":{}}`} {
		invalid := edited
		invalid.Response = response
		if _, err := store.SaveTimer(ctx, invalid); err == nil {
			t.Fatalf("invalid edit persisted: %s", response)
		}
		invalid.ID = ""
		if _, err := store.SaveTimer(ctx, invalid); err == nil {
			t.Fatalf("invalid create persisted: %s", response)
		}
	}
	rows, err := store.ListTimers(ctx)
	if err != nil || len(rows) != 1 || rows[0].Response != edited.Response || !rows[0].Enabled {
		t.Fatalf("rejected writes changed timers: %#v, %v", rows, err)
	}
	if err := store.DeleteTimer(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	rows, err = store.ListTimers(ctx)
	if err != nil || len(rows) != 0 {
		t.Fatalf("deleted timer remains: %#v, %v", rows, err)
	}
	if _, err := store.GetTimer(ctx, created.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing timer lookup=%v", err)
	}
}

func TestCommunityPersistenceAndConcurrentCounterPostgres(t *testing.T) {
	store, _ := isolatedPostgresBotStore(t)
	ctx := context.Background()
	counter, err := store.SaveCounter(ctx, Counter{Name: " HYPE ", Value: 7})
	if err != nil || counter.Name != "hype" || counter.ID == "" {
		t.Fatalf("create counter=%#v, %v", counter, err)
	}
	var workers sync.WaitGroup
	failures := make(chan error, 20)
	for range 20 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			_, err := store.IncrementCounter(ctx, " HYPE ", 1)
			if err != nil {
				failures <- err
			}
		}()
	}
	workers.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	counted, found, err := store.CounterValue(ctx, "HYPE")
	if err != nil || !found || counted.Value != 27 || counted.ID != counter.ID || !counted.CreatedAt.Equal(counter.CreatedAt) {
		t.Fatalf("lost concurrent increments: %#v, %t, %v", counted, found, err)
	}
	counted.Name, counted.Value = "hype_renamed", -3
	edited, err := store.SaveCounter(ctx, counted)
	if err != nil || edited.ID != counter.ID || edited.Name != "hype_renamed" || edited.Value != -3 {
		t.Fatalf("edit counter=%#v, %v", edited, err)
	}
	if _, found, err := store.CounterValue(ctx, "hype"); err != nil || found {
		t.Fatalf("old counter name remains: %t, %v", found, err)
	}
	if err := store.DeleteCounter(ctx, counter.ID); err != nil {
		t.Fatal(err)
	}
	counters, err := store.ListCounters(ctx)
	if err != nil || len(counters) != 0 {
		t.Fatalf("deleted counter remains: %#v, %v", counters, err)
	}
	if _, found, err := store.CounterValue(ctx, "hype_renamed"); err != nil || found {
		t.Fatalf("deleted counter lookup: %t, %v", found, err)
	}
	if _, found, err := store.RandomQuote(ctx); err != nil || found {
		t.Fatalf("empty quote lookup: %t, %v", found, err)
	}
	quote, err := store.SaveQuote(ctx, Quote{Text: " Original quote ", Author: " Author "})
	if err != nil || quote.ID == "" || quote.Text != "Original quote" || quote.Author != "Author" {
		t.Fatalf("create quote=%#v, %v", quote, err)
	}
	quote.Text, quote.Author = "Edited quote", "Edited author"
	editedQuote, err := store.SaveQuote(ctx, quote)
	if err != nil || editedQuote.ID != quote.ID || !editedQuote.CreatedAt.Equal(quote.CreatedAt) {
		t.Fatalf("edit quote identity=%#v, %v", editedQuote, err)
	}
	random, found, err := store.RandomQuote(ctx)
	if err != nil || !found || random.ID != quote.ID || random.Text != "Edited quote" || random.Author != "Edited author" {
		t.Fatalf("quote read=%#v, %t, %v", random, found, err)
	}
	quotes, err := store.ListQuotes(ctx)
	if err != nil || len(quotes) != 1 || quotes[0].Text != random.Text {
		t.Fatalf("quote list=%#v, %v", quotes, err)
	}
	if err := store.DeleteQuote(ctx, quote.ID); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.RandomQuote(ctx); err != nil || found {
		t.Fatalf("deleted quote lookup: %t, %v", found, err)
	}
	if err := store.DeleteQuote(ctx, quote.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing quote delete=%v", err)
	}
}

func TestModerationRulePersistenceMatchingAndRollbackPostgres(t *testing.T) {
	store, _ := isolatedPostgresBotStore(t)
	ctx := t.Context()
	created, err := store.SaveModerationRule(ctx, ModerationRule{Name: " SPAM ", Enabled: true, Pattern: " buy now ", MatchTarget: "MESSAGE", Action: "FLAG"})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Name != "spam" || created.Pattern != "buy now" || created.Source != "manual" || created.CreatedAt.IsZero() {
		t.Fatalf("created = %#v", created)
	}
	edited := created
	edited.Pattern, edited.MatchTarget, edited.Action = "^spam_[0-9]+$", "username", "delete"
	edited.Regex, edited.CaseSensitive, edited.Enforce = true, true, true
	updated, err := store.SaveModerationRule(ctx, edited)
	if err != nil || updated.ID != created.ID || !updated.CreatedAt.Equal(created.CreatedAt) || updated.Pattern != edited.Pattern || updated.MatchTarget != "username" || updated.Action != "delete" || !updated.Enforce || !updated.Regex || !updated.CaseSensitive {
		t.Fatalf("updated = %#v err=%v", updated, err)
	}
	matches, err := store.DryRunModeration(ctx, "unrelated", "spam_42")
	if err != nil || len(matches) != 1 || matches[0].RuleID != created.ID {
		t.Fatalf("username dry run = %#v err=%v", matches, err)
	}
	matches, err = store.DryRunModeration(ctx, "spam_42", "SPAM_42")
	if err != nil || len(matches) != 0 {
		t.Fatalf("case/target dry run = %#v err=%v", matches, err)
	}
	invalid := updated
	invalid.Pattern = "["
	if _, err := store.SaveModerationRule(ctx, invalid); err == nil {
		t.Fatal("invalid regex edit persisted")
	}
	invalid.ID = ""
	if _, err := store.SaveModerationRule(ctx, invalid); err == nil {
		t.Fatal("invalid regex create persisted")
	}
	rows, err := store.ListModerationRules(ctx)
	if err != nil || len(rows) != 1 || rows[0].Pattern != updated.Pattern || !rows[0].UpdatedAt.Equal(updated.UpdatedAt) {
		t.Fatalf("rejected writes changed saved rule = %#v err=%v", rows, err)
	}
	paused := updated
	paused.Enabled = false
	paused, err = store.SaveModerationRule(ctx, paused)
	if err != nil || paused.ID != created.ID || paused.Enabled || paused.Pattern != updated.Pattern {
		t.Fatalf("paused = %#v err=%v", paused, err)
	}
	matches, err = store.DryRunModeration(ctx, "", "spam_42")
	if err != nil || len(matches) != 0 {
		t.Fatalf("paused matched = %#v err=%v", matches, err)
	}
	if _, err := store.DisableNukeRadiationRule(ctx, created.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("manual rollback = %v", err)
	}
	future := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	radiation, err := store.SaveModerationRule(ctx, ModerationRule{Name: "radiation", Enabled: true, Enforce: true, Pattern: "spam", Action: "timeout", Source: "nuke_radiation", ExpiresAt: &future})
	if err != nil {
		t.Fatal(err)
	}
	// Dashboard edits omit source and expiry; those server-owned fields persist.
	radiation.Pattern, radiation.Source, radiation.ExpiresAt = "buy now", "", nil
	radiation, err = store.SaveModerationRule(ctx, radiation)
	if err != nil || radiation.Source != "nuke_radiation" || radiation.ExpiresAt == nil || !radiation.ExpiresAt.Equal(future) {
		t.Fatalf("radiation edit metadata = %#v err=%v", radiation, err)
	}
	matches, err = store.DryRunModeration(ctx, "BUY NOW", "viewer")
	if err != nil || len(matches) != 1 || matches[0].RuleID != radiation.ID {
		t.Fatalf("active radiation = %#v err=%v", matches, err)
	}
	rolledBack, err := store.DisableNukeRadiationRule(ctx, radiation.ID)
	if err != nil || rolledBack.ID != radiation.ID || rolledBack.Enabled || rolledBack.Enforce || rolledBack.ExpiresAt == nil || !rolledBack.ExpiresAt.Equal(future) {
		t.Fatalf("rollback = %#v err=%v", rolledBack, err)
	}
	matches, err = store.DryRunModeration(ctx, "buy now", "viewer")
	if err != nil || len(matches) != 0 {
		t.Fatalf("rollback still matches = %#v err=%v", matches, err)
	}
	past := time.Now().UTC().Add(-time.Minute)
	radiation.Enabled, radiation.Enforce, radiation.ExpiresAt = true, true, &past
	if _, err := store.SaveModerationRule(ctx, radiation); err != nil {
		t.Fatal(err)
	}
	matches, err = store.DryRunModeration(ctx, "buy now", "viewer")
	if err != nil || len(matches) != 0 {
		t.Fatalf("expired still matches = %#v err=%v", matches, err)
	}
	for _, id := range []string{created.ID, radiation.ID} {
		if err := store.DeleteModerationRule(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	rows, err = store.ListModerationRules(ctx)
	if err != nil || len(rows) != 0 {
		t.Fatalf("deleted rows = %#v err=%v", rows, err)
	}
	if err := store.DeleteModerationRule(ctx, created.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing delete = %v", err)
	}
	updated.ID = "missing"
	if _, err := store.SaveModerationRule(ctx, updated); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing edit = %v", err)
	}
}
