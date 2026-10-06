package bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
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
	flushRuntime(t, runtime)
	if len(recorder.events) != 1 || recorder.events[0].Command != "alert" {
		t.Fatalf("expected recorded audit outcome, got %#v", recorder.events)
	}
}

func TestRuntimeRecordsAuditEvents(t *testing.T) {
	recorder := &fakeAuditRecorder{}
	runtime := NewRuntime(recorder)
	runtime.Record(context.Background(), AuditEvent{Source: string(SourceChat), Command: "alert", Result: string(ResultAllowed)})
	flushRuntime(t, runtime)
	if len(recorder.events) != 1 || recorder.events[0].Command != "alert" {
		t.Fatalf("expected recorded audit event, got %#v", recorder.events)
	}
}

func TestRuntimeAuditFailureDoesNotPanic(t *testing.T) {
	runtime := NewRuntime(&fakeAuditRecorder{err: errors.New("database unavailable")})
	runtime.Record(context.Background(), AuditEvent{Source: string(SourceChat), Command: "alert", Result: string(ResultAllowed)})
	flushRuntime(t, runtime)
	if runtime.LastAuditError() != "database unavailable" {
		t.Fatalf("expected remembered audit error, got %q", runtime.LastAuditError())
	}
}

func flushRuntime(t *testing.T, runtime *Runtime) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := runtime.Flush(ctx); err != nil {
		t.Fatal(err)
	}
}

// stalledAuditRecorder holds the first write so queue behavior is deterministic.
type stalledAuditRecorder struct {
	started chan context.Context
	release chan struct{}
	events  []AuditEvent
}

func (r *stalledAuditRecorder) RecordAudit(ctx context.Context, event AuditEvent) (AuditEvent, error) {
	if len(r.events) == 0 {
		r.started <- ctx
		<-r.release
	}
	r.events = append(r.events, event)
	return event, nil
}

func TestRuntimeQueuesBoundedOrderedSnapshotsWithoutRequestCancellation(t *testing.T) {
	recorder := &stalledAuditRecorder{started: make(chan context.Context, 1), release: make(chan struct{})}
	runtime := NewRuntime(recorder)
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(recorder.release) }); flushRuntime(t, runtime) })
	request, cancel := context.WithCancel(context.Background())
	runtime.Record(request, AuditEvent{Command: "first"})
	var writeContext context.Context
	select {
	case writeContext = <-recorder.started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	cancel()
	if writeContext.Err() != nil {
		t.Fatal("request cancellation aborted accepted audit")
	}
	if deadline, ok := writeContext.Deadline(); !ok || time.Until(deadline) > auditWriteTimeout {
		t.Fatal("audit write has no bounded deadline")
	}
	payload := json.RawMessage(`{"value":"original"}`)
	runtime.Record(request, AuditEvent{Command: "snapshot", Data: payload})
	copy(payload, []byte(`{"value":"modified"}`))
	for i := 1; i < auditQueueCapacity; i++ {
		runtime.Record(request, AuditEvent{Command: fmt.Sprint(i)})
	}
	runtime.Record(request, AuditEvent{Command: "dropped"})
	if runtime.LastAuditError() != "bot audit queue full; audit event dropped" {
		t.Fatal("queue overflow not reported")
	}
	ctx, cancelFlush := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancelFlush()
	if err := runtime.Flush(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked flush=%v", err)
	}
	release.Do(func() { close(recorder.release) })
	flushRuntime(t, runtime)
	if len(recorder.events) != auditQueueCapacity+1 || recorder.events[0].Command != "first" || recorder.events[1].Command != "snapshot" || string(recorder.events[1].Data) != `{"value":"original"}` {
		t.Fatalf("incorrect queued snapshots: %#v", recorder.events)
	}
	for i := 2; i < len(recorder.events); i++ {
		if recorder.events[i].Command != fmt.Sprint(i-1) {
			t.Fatalf("audit order at %d: %s", i, recorder.events[i].Command)
		}
	}
	runtime.Record(context.Background(), AuditEvent{Command: "restarted"})
	flushRuntime(t, runtime)
	if recorder.events[len(recorder.events)-1].Command != "restarted" {
		t.Fatal("idle worker did not restart")
	}
}
