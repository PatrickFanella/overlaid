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

func TestRuntimeAllowsRoadmapRoleMatrix(t *testing.T) {
	runtime := NewRuntime(nil)
	roles := []Role{RoleViewer, RoleModerator, RoleBroadcaster, RoleOperator, RoleSystem}
	for _, actor := range roles {
		for _, required := range roles {
			want := actor == RoleBroadcaster || actor == RoleOperator || actor == RoleSystem || required == RoleViewer || (actor == RoleModerator && required == RoleModerator)
			t.Run(string(actor)+"/"+string(required), func(t *testing.T) {
				if got := runtime.Allowed(actor, required); got != want {
					t.Fatalf("Allowed(%s,%s)=%v want %v", actor, required, got, want)
				}
				if got := RoleAllows(actor, required); got != want {
					t.Fatalf("RoleAllows(%s,%s)=%v want %v", actor, required, got, want)
				}
			})
		}
	}
}

func TestRuntimeRoleNormalizationDoesNotElevateUnknownActors(t *testing.T) {
	if !RoleAllows(Role(" BROADCASTER "), RoleSystem) {
		t.Fatal("normalized broadcaster cannot run system role command")
	}
	for _, actor := range []Role{"", "admin", "unknown"} {
		if RoleAllows(actor, RoleModerator) || RoleAllows(actor, RoleSystem) {
			t.Fatalf("unknown actor %q was elevated", actor)
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
