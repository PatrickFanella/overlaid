package bot

import (
	"context"
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
