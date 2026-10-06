package bot

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

type manualTimerStore struct {
	timer    Timer
	err      error
	auditErr error
	audit    []AuditEvent
}

func (s *manualTimerStore) GetTimer(_ context.Context, id string) (Timer, error) {
	if id != s.timer.ID {
		return Timer{}, sql.ErrNoRows
	}
	return s.timer, s.err
}
func (s *manualTimerStore) RecordAudit(_ context.Context, event AuditEvent) (AuditEvent, error) {
	s.audit = append(s.audit, event)
	return event, s.auditErr
}

func TestRunTimerOnceRehearsesPausedTimerWithoutChangingSchedule(t *testing.T) {
	lastRun := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	store := &manualTimerStore{timer: Timer{ID: "timer-1", Name: "hydrate", Enabled: false, IntervalSeconds: 300, Response: "Hello {channel}", Action: "ticker", LastRunAt: &lastRun}}
	var published []events.Event
	event, err := RunTimerOnce(context.Background(), store, NewRuntime(store), "timer-1", "channel", "session-1", func(e events.Event) { published = append(published, e) })
	if err != nil || len(published) != 1 || event.Message != "Hello channel" {
		t.Fatalf("unexpected execution: %#v, %v", published, err)
	}
	if store.timer.Enabled || !store.timer.LastRunAt.Equal(lastRun) {
		t.Fatal("manual run changed the timer schedule")
	}
	if len(store.audit) != 1 {
		t.Fatalf("expected audit, got %#v", store.audit)
	}
	audit := store.audit[0]
	if audit.Source != "dashboard" || audit.ActorRole != "operator" || audit.Reason != "timer_run_once" || audit.EventID != event.ID || audit.StreamSessionID != "session-1" {
		t.Fatalf("unexpected audit: %#v", audit)
	}
}

func TestRunTimerOnceRejectsMissingAndEmptyTimers(t *testing.T) {
	for _, test := range []struct {
		name, id, response string
		want               error
		audits             int
	}{
		{"missing", "missing", "Hello", sql.ErrNoRows, 0},
		{"empty", "timer-1", "{user}", ErrEmptyTimerResponse, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &manualTimerStore{timer: Timer{ID: "timer-1", Name: "hydrate", Response: test.response, Action: "alert"}}
			_, err := RunTimerOnce(context.Background(), store, NewRuntime(store), test.id, "channel", "", func(events.Event) { t.Fatal("unexpected publish") })
			if !errors.Is(err, test.want) || len(store.audit) != test.audits {
				t.Fatalf("got %v, audits %#v", err, store.audit)
			}
			if test.audits > 0 && store.audit[0].Result != "failed" {
				t.Fatal("empty response was not audited as failed")
			}
		})
	}
}

func TestRunTimerOnceStillPublishesWhenAuditFails(t *testing.T) {
	store := &manualTimerStore{timer: Timer{ID: "timer-1", Name: "hydrate", Response: "Hello", Action: "alert"}, auditErr: errors.New("audit offline")}
	runtime := NewRuntime(store)
	var published bool
	_, err := RunTimerOnce(context.Background(), store, runtime, "timer-1", "channel", "", func(events.Event) { published = true })
	if err != nil || !published || runtime.LastAuditError() == "" {
		t.Fatalf("audit failure blocked execution: %v", err)
	}
}
