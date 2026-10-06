package bot

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

type timerSchedulerFixture struct {
	due   func(context.Context, time.Time, int) ([]Timer, error)
	mark  func(context.Context, string, time.Time) error
	audit func(context.Context, AuditEvent) (AuditEvent, error)
}

func (f timerSchedulerFixture) DueTimers(ctx context.Context, now time.Time, limit int) ([]Timer, error) {
	return f.due(ctx, now, limit)
}
func (f timerSchedulerFixture) MarkTimerRun(ctx context.Context, id string, at time.Time) error {
	return f.mark(ctx, id, at)
}
func (f timerSchedulerFixture) RecordAudit(ctx context.Context, event AuditEvent) (AuditEvent, error) {
	return f.audit(ctx, event)
}

func TestTimerSchedulerPublishesBeforeWritesAndLinksAudit(t *testing.T) {
	now := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	operations := []string{}
	published := []events.Event{}
	store := timerSchedulerFixture{
		due: func(_ context.Context, at time.Time, limit int) ([]Timer, error) {
			if !at.Equal(now) || limit != 10 {
				t.Fatalf("due query=%s/%d", at, limit)
			}
			return []Timer{{ID: "empty", Name: "empty", Response: "{missing}", Action: "ticker"}, {ID: "ticker", Name: "welcome", Response: "Welcome {channel} {missing}", Action: "ticker"}, {ID: "alert", Name: "hydrate", Response: "Hydrate", Action: "alert"}}, nil
		},
		mark: func(_ context.Context, id string, at time.Time) error {
			operations = append(operations, "mark:"+id)
			if !at.Equal(now) {
				t.Fatalf("mark at=%s", at)
			}
			return nil
		},
		audit: func(_ context.Context, event AuditEvent) (AuditEvent, error) {
			operations = append(operations, "audit:"+event.Command)
			if event.Command == "empty" {
				if event.Source != "timer" || event.ActorRole != "system" || event.Result != "failed" || event.Reason != "timer_response_empty" || event.EventID != "" || event.StreamSessionID != "active-session" || !strings.Contains(string(event.Data), "missing") {
					t.Fatalf("incorrect failed-render audit: %#v", event)
				}
				return event, nil
			}
			if event.Source != "timer" || event.ActorRole != "system" || event.Reason != "timer_due" || event.Result != "allowed" || event.StreamSessionID != "active-session" || event.EventID != published[len(published)-1].ID {
				t.Fatalf("unlinked audit=%#v", event)
			}
			if event.Command == "welcome" {
				var data struct {
					UnknownVariables []string `json:"unknownVariables"`
				}
				if err := json.Unmarshal(event.Data, &data); err != nil || len(data.UnknownVariables) != 1 || data.UnknownVariables[0] != "missing" {
					t.Fatalf("unknown variables=%s, %v", event.Data, err)
				}
			}
			return event, nil
		},
	}
	scheduler := TimerScheduler{Store: store, Channel: "fixture-channel", StreamSessionID: func() string { return "active-session" }, Publish: func(event events.Event) {
		published = append(published, event)
		operations = append(operations, "publish:"+event.Data["timer"].(string))
	}}
	scheduler.runOnce(context.Background(), now)
	if strings.Join(operations, ",") != "mark:empty,audit:empty,publish:welcome,mark:ticker,audit:welcome,publish:hydrate,mark:alert,audit:hydrate" {
		t.Fatalf("publication/write order=%v", operations)
	}
	if len(published) != 2 || published[0].Message != "Welcome fixture-channel" || published[0].Type != events.EventTypeCommand || published[1].Type != events.EventTypeAlert {
		t.Fatalf("published=%#v", published)
	}
}

func TestTimerSchedulerStorageFailuresDoNotSuppressPublishedTimers(t *testing.T) {
	published, marked, audited := 0, 0, 0
	store := timerSchedulerFixture{
		due: func(context.Context, time.Time, int) ([]Timer, error) {
			return []Timer{{ID: "first", Name: "first", Response: "Hello"}, {ID: "second", Name: "second", Response: "Again"}}, nil
		},
		mark: func(context.Context, string, time.Time) error { marked++; return errors.New("mark unavailable") },
		audit: func(context.Context, AuditEvent) (AuditEvent, error) {
			audited++
			return AuditEvent{}, errors.New("audit unavailable")
		},
	}
	TimerScheduler{Store: store, Publish: func(events.Event) { published++ }}.runOnce(context.Background(), time.Now())
	if published != 2 || marked != 2 || audited != 2 {
		t.Fatalf("published=%d marked=%d audited=%d", published, marked, audited)
	}
	store.due = func(context.Context, time.Time, int) ([]Timer, error) { return nil, errors.New("lookup unavailable") }
	TimerScheduler{Store: store, Publish: func(events.Event) { published++ }}.runOnce(context.Background(), time.Now())
	if published != 2 || marked != 2 || audited != 2 {
		t.Fatal("lookup failure produced timer activity")
	}
}

func TestTimerSchedulerStopsPublishingAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	published := 0
	store := timerSchedulerFixture{
		due: func(context.Context, time.Time, int) ([]Timer, error) {
			return []Timer{{ID: "first", Name: "first", Response: "Hello"}, {ID: "second", Name: "second", Response: "Again"}}, nil
		},
		mark:  func(context.Context, string, time.Time) error { return nil },
		audit: func(context.Context, AuditEvent) (AuditEvent, error) { return AuditEvent{}, nil },
	}
	scheduler := TimerScheduler{Store: store, Publish: func(events.Event) { published++; cancel() }}
	scheduler.runOnce(ctx, time.Now())
	if published != 1 {
		t.Fatalf("published %d timers after cancellation, want only the first", published)
	}
	store.due = func(context.Context, time.Time, int) ([]Timer, error) {
		t.Fatal("canceled scheduler queried storage")
		return nil, nil
	}
	scheduler.Store = store
	scheduler.Run(ctx)
}

func TestTimerSchedulerWithoutDependenciesDoesNoWork(t *testing.T) {
	store := timerSchedulerFixture{due: func(context.Context, time.Time, int) ([]Timer, error) {
		t.Fatal("scheduler without publisher queried storage")
		return nil, nil
	}}
	TimerScheduler{Store: store}.runOnce(context.Background(), time.Now())
	TimerScheduler{Publish: func(events.Event) { t.Fatal("scheduler without storage published") }}.runOnce(context.Background(), time.Now())
}

func TestTimerSchedulerPollsUntilCanceled(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	lookups := 0
	store := timerSchedulerFixture{due: func(context.Context, time.Time, int) ([]Timer, error) {
		lookups++
		if lookups == 2 {
			cancel()
		}
		return nil, nil
	}}
	TimerScheduler{Store: store, Interval: time.Millisecond, Publish: func(events.Event) { t.Fatal("empty due list published") }}.Run(ctx)
	if lookups != 2 {
		t.Fatalf("scheduled polls=%d, want initial lookup and one subsequent poll", lookups)
	}
}

func TestTimerEventRendersTickerTemplate(t *testing.T) {
	event, render := TimerEvent(Timer{Name: "stream_hype", Response: "Welcome to {channel}", Action: "ticker"}, "patrick__eff")
	if event.Type != events.EventTypeCommand || event.Source != "timer" || event.Message != "Welcome to patrick__eff" {
		t.Fatalf("unexpected timer event: %#v", event)
	}
	if len(render.UnknownVariables) != 0 {
		t.Fatalf("unexpected unknown variables: %#v", render.UnknownVariables)
	}
}

func TestTimerEventRendersAlertTemplate(t *testing.T) {
	event, _ := TimerEvent(Timer{Name: "hydrate", Response: "Hydrate", Action: "alert"}, "patrick__eff")
	if event.Type != events.EventTypeAlert || event.Title != "Bot timer: hydrate" || event.Message != "Hydrate" {
		t.Fatalf("unexpected timer alert: %#v", event)
	}
}
