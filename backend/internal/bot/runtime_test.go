package bot

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type fakeAuditRecorder struct {
	events []AuditEvent
	err    error
}

func (f *fakeAuditRecorder) RecordAudit(_ context.Context, event AuditEvent) (AuditEvent, error) {
	if f.err != nil {
		return AuditEvent{}, f.err
	}
	f.events = append(f.events, event)
	return event, nil
}

func TestRuntimeAllowsRankedRoles(t *testing.T) {
	runtime := NewRuntime(nil)
	cases := []struct {
		actor    Role
		required Role
		want     bool
	}{
		{RoleViewer, RoleViewer, true},
		{RoleViewer, RoleModerator, false},
		{RoleModerator, RoleViewer, true},
		{RoleModerator, RoleBroadcaster, false},
		{RoleBroadcaster, RoleModerator, true},
		{RoleOperator, RoleBroadcaster, true},
		{RoleSystem, RoleOperator, true},
	}
	for _, tc := range cases {
		if got := runtime.Allowed(tc.actor, tc.required); got != tc.want {
			t.Fatalf("Allowed(%s,%s)=%v want %v", tc.actor, tc.required, got, tc.want)
		}
		if got := RoleAllows(tc.actor, tc.required); got != tc.want {
			t.Fatalf("RoleAllows(%s,%s)=%v want %v", tc.actor, tc.required, got, tc.want)
		}
	}
}

func TestAuditEventFromOutcomeNormalizesAndEncodesData(t *testing.T) {
	event := AuditEventFromOutcome(AuditOutcome{Source: SourceDashboard, ActorRole: Role(" MODERATOR "), Trigger: " trigger ", Command: " command ", Action: " action ", Result: ResultAllowed, Reason: " reason ", EventID: " event ", StreamSessionID: " session ", Data: map[string]any{"ok": true}})
	if event.Source != "dashboard" || event.ActorRole != "moderator" || event.Trigger != "trigger" || event.Command != "command" || event.StreamSessionID != "session" {
		t.Fatalf("unexpected normalized audit event: %#v", event)
	}
	var decoded map[string]bool
	if err := json.Unmarshal(event.Data, &decoded); err != nil || !decoded["ok"] {
		t.Fatalf("unexpected audit data: data=%s err=%v", event.Data, err)
	}
}

func TestRuntimeRecordsAuditOutcomes(t *testing.T) {
	recorder := &fakeAuditRecorder{}
	runtime := NewRuntime(recorder)
	runtime.RecordOutcome(context.Background(), AuditOutcome{Source: SourceChat, Command: "alert", Result: ResultAllowed, Data: map[string]any{"alias": true}})
	if len(recorder.events) != 1 || recorder.events[0].Command != "alert" {
		t.Fatalf("expected recorded audit outcome, got %#v", recorder.events)
	}
}

func TestRuntimeRecordsAuditEvents(t *testing.T) {
	recorder := &fakeAuditRecorder{}
	runtime := NewRuntime(recorder)
	runtime.Record(context.Background(), AuditEvent{Source: string(SourceChat), Command: "alert", Result: string(ResultAllowed)})
	if len(recorder.events) != 1 || recorder.events[0].Command != "alert" {
		t.Fatalf("expected recorded audit event, got %#v", recorder.events)
	}
}

func TestRuntimeAuditFailureDoesNotPanic(t *testing.T) {
	runtime := NewRuntime(&fakeAuditRecorder{err: errors.New("database unavailable")})
	runtime.Record(context.Background(), AuditEvent{Source: string(SourceChat), Command: "alert", Result: string(ResultAllowed)})
	if runtime.LastAuditError() != "database unavailable" {
		t.Fatalf("expected remembered audit error, got %q", runtime.LastAuditError())
	}
}
