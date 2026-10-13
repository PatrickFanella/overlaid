package bot

import (
	"context"
	"errors"
	"strings"

	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

var ErrEmptyTimerResponse = errors.New("timer response renders empty")

type TimerReader interface {
	GetTimer(context.Context, string) (Timer, error)
}

// RunTimerOnce rehearses a saved timer without enabling it or moving its next
// scheduled run. Manual executions are recorded separately in the bot audit.
func RunTimerOnce(ctx context.Context, store TimerReader, runtime *Runtime, id, channel, sessionID string, publish func(events.Event)) (events.Event, error) {
	timer, err := store.GetTimer(ctx, id)
	if err != nil {
		return events.Event{}, err
	}
	if gate, ok := store.(interface {
		AllowItem(context.Context, string, string) error
	}); ok {
		if err := gate.AllowItem(ctx, "timers", timer.ID); err != nil {
			return events.Event{}, err
		}
	}
	event, render := TimerEvent(timer, channel)
	outcome := AuditOutcome{
		Source: SourceDashboard, ActorRole: RoleOperator,
		Trigger: timer.Name, Command: timer.Name, Action: timer.Action,
		Result: ResultAllowed, Reason: "timer_run_once", StreamSessionID: sessionID,
		Data: map[string]any{"timerId": timer.ID, "unknownVariables": render.UnknownVariables},
	}
	if strings.TrimSpace(event.Message) == "" {
		outcome.Result = ResultFailed
		outcome.Reason = "timer_response_empty"
		runtime.RecordOutcomeAndWait(ctx, outcome)
		return events.Event{}, ErrEmptyTimerResponse
	}
	publish(event)
	outcome.EventID = event.ID
	runtime.RecordOutcomeAndWait(ctx, outcome)
	return event, nil
}
