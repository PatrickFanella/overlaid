package bot

import (
	"context"
	"log"
	"strings"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

type TimerStore interface {
	DueTimers(context.Context, time.Time, int) ([]Timer, error)
	MarkTimerRun(context.Context, string, time.Time) error
	RecordAudit(context.Context, AuditEvent) (AuditEvent, error)
}

type TimerScheduler struct {
	Store           TimerStore
	Interval        time.Duration
	Channel         string
	Publish         func(events.Event)
	StreamSessionID func() string
}

func (s TimerScheduler) Run(ctx context.Context) {
	if s.Store == nil || s.Publish == nil || ctx.Err() != nil {
		return
	}
	interval := s.Interval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	s.runOnce(ctx, time.Now().UTC())
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.runOnce(ctx, now.UTC())
		}
	}
}

func (s TimerScheduler) runOnce(ctx context.Context, now time.Time) {
	if s.Store == nil || s.Publish == nil || ctx.Err() != nil {
		return
	}
	timers, err := s.Store.DueTimers(ctx, now, 10)
	if err != nil {
		log.Printf("failed to load due bot timers: %v", err)
		return
	}
	for _, timer := range timers {
		if ctx.Err() != nil {
			return
		}
		event, render := TimerEvent(timer, s.Channel)
		if strings.TrimSpace(event.Message) != "" {
			s.Publish(event)
		} else {
			// A failed render still consumes this scheduled attempt. Leaving it due
			// forever can crowd valid timers out of the bounded due batch.
			event = events.Event{}
		}
		if err := s.Store.MarkTimerRun(ctx, timer.ID, now); err != nil {
			log.Printf("failed to mark bot timer %s run: %v", timer.Name, err)
		}
		s.recordAudit(ctx, timer, event, render)
	}
}

func (s TimerScheduler) recordAudit(ctx context.Context, timer Timer, event events.Event, render TemplateResult) {
	if s.Store == nil {
		return
	}
	sessionID := ""
	if s.StreamSessionID != nil {
		sessionID = s.StreamSessionID()
	}
	result, reason := ResultAllowed, "timer_due"
	if strings.TrimSpace(render.Text) == "" {
		result, reason = ResultFailed, "timer_response_empty"
	}
	auditEvent := AuditEventFromOutcome(AuditOutcome{Source: SourceTimer, ActorRole: RoleSystem, Trigger: timer.Name, Command: timer.Name, Action: timer.Action, Result: result, Reason: reason, EventID: event.ID, StreamSessionID: sessionID, Data: map[string]any{"unknownVariables": render.UnknownVariables}})
	if _, err := s.Store.RecordAudit(ctx, auditEvent); err != nil {
		log.Printf("failed to record bot timer audit: %v", err)
	}
}

func TimerEvent(timer Timer, channel string) (events.Event, TemplateResult) {
	render := (TemplateRenderer{}).Render(timer.Response, TemplateContext{Channel: channel, Command: timer.Name})
	message := render.Text
	switch timer.Action {
	case "alert":
		event := events.NewAlert("timer", "Bot timer: "+timer.Name, message, events.SeverityInfo)
		event.Data = map[string]any{"timer": timer.Name, "action": "alert", "unknownVariables": render.UnknownVariables}
		return event, render
	default:
		event := events.NewCommand("timer", "Bot timer: "+timer.Name, message, events.SeverityInfo, map[string]any{
			"timer":            timer.Name,
			"action":           "ticker",
			"unknownVariables": render.UnknownVariables,
			"ticker": map[string]any{
				"label":   timer.Name,
				"message": message,
			},
		})
		return event, render
	}
}
