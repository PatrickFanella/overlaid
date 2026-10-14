package bot

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"
)

type Role string

const (
	RoleViewer      Role = "viewer"
	RoleSupporter   Role = "supporter"
	RoleModerator   Role = "moderator"
	RoleBroadcaster Role = "broadcaster"
	RoleOperator    Role = "operator"
	RoleSystem      Role = "system"
)

type Source string

const (
	SourceChat         Source = "chat"
	SourceChannelPoint Source = "channel_point"
	SourceDashboard    Source = "dashboard"
	SourceTimer        Source = "timer"
	SourceWebhook      Source = "webhook"
)

type Result string

const (
	ResultAllowed  Result = "allowed"
	ResultDenied   Result = "denied"
	ResultFailed   Result = "failed"
	ResultCooldown Result = "cooldown"
)

type AuditRecorder interface {
	RecordAudit(context.Context, AuditEvent) (AuditEvent, error)
}

type AuditOutcome struct {
	Source           Source
	ActorID          string
	ActorLogin       string
	ActorDisplayName string
	ActorRole        Role
	Trigger          string
	Command          string
	Action           string
	Result           Result
	Reason           string
	EventID          string
	StreamSessionID  string
	Data             map[string]any
}

type Runtime struct {
	recorder AuditRecorder
	mu       sync.RWMutex
	lastErr  string
	queue    []auditJob
	idle     chan struct{}
}

func NewRuntime(recorder AuditRecorder) *Runtime {
	return &Runtime{recorder: recorder}
}

func (r *Runtime) Allowed(actor Role, required Role) bool {
	return RoleAllows(actor, required)
}

const auditQueueCapacity = 128
const auditWriteTimeout = 5 * time.Second

type auditJob struct {
	context context.Context
	event   AuditEvent
}

// Record queues an immutable audit snapshot without waiting for storage. One
// worker preserves enqueue order and exits when idle; overload is observable.
func (r *Runtime) Record(ctx context.Context, event AuditEvent) {
	if r == nil || r.recorder == nil {
		return
	}
	event.Data = append(json.RawMessage(nil), event.Data...)
	r.mu.Lock()
	if len(r.queue) >= auditQueueCapacity {
		r.lastErr = "bot audit queue full; audit event dropped"
		r.mu.Unlock()
		return
	}
	r.queue = append(r.queue, auditJob{context.WithoutCancel(ctx), event})
	if r.idle == nil {
		r.idle = make(chan struct{})
		go r.drain()
	}
	r.mu.Unlock()
}

func (r *Runtime) drain() {
	for {
		r.mu.Lock()
		if len(r.queue) == 0 {
			close(r.idle)
			r.idle = nil
			r.mu.Unlock()
			return
		}
		job := r.queue[0]
		r.queue[0] = auditJob{}
		r.queue = r.queue[1:]
		r.mu.Unlock()
		ctx, cancel := context.WithTimeout(job.context, auditWriteTimeout)
		r.recordAndWait(ctx, job.event)
		cancel()
	}
}

// Flush waits for currently queued work, bounded by the caller's deadline.
func (r *Runtime) Flush(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	idle := r.idle
	r.mu.RUnlock()
	if idle == nil {
		return nil
	}
	select {
	case <-idle:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Runtime) recordAndWait(ctx context.Context, event AuditEvent) {
	if r == nil || r.recorder == nil {
		return
	}
	if _, err := r.recorder.RecordAudit(ctx, event); err != nil {
		r.setLastError(err.Error())
		log.Printf("bot audit record failed: %v", err)
	}
}

func (r *Runtime) RecordOutcome(ctx context.Context, outcome AuditOutcome) {
	r.Record(ctx, AuditEventFromOutcome(outcome))
}

// RecordOutcomeAndWait keeps dashboard mutation receipts readable immediately.
func (r *Runtime) RecordOutcomeAndWait(ctx context.Context, outcome AuditOutcome) {
	r.recordAndWait(ctx, AuditEventFromOutcome(outcome))
}

func AuditEventFromOutcome(outcome AuditOutcome) AuditEvent {
	encoded := json.RawMessage(nil)
	if outcome.Data != nil {
		data, err := json.Marshal(outcome.Data)
		if err != nil {
			data = json.RawMessage(`{}`)
		}
		encoded = data
	}
	return AuditEvent{
		Source:           string(outcome.Source),
		ActorID:          strings.TrimSpace(outcome.ActorID),
		ActorLogin:       strings.TrimSpace(outcome.ActorLogin),
		ActorDisplayName: strings.TrimSpace(outcome.ActorDisplayName),
		ActorRole:        string(NormalizeRole(outcome.ActorRole)),
		Trigger:          strings.TrimSpace(outcome.Trigger),
		Command:          strings.TrimSpace(outcome.Command),
		Action:           strings.TrimSpace(outcome.Action),
		Result:           string(outcome.Result),
		Reason:           strings.TrimSpace(outcome.Reason),
		EventID:          strings.TrimSpace(outcome.EventID),
		StreamSessionID:  strings.TrimSpace(outcome.StreamSessionID),
		Data:             encoded,
	}
}

func (r *Runtime) LastAuditError() string {
	if r == nil {
		return ""
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lastErr
}

func (r *Runtime) setLastError(message string) {
	r.mu.Lock()
	r.lastErr = strings.TrimSpace(message)
	r.mu.Unlock()
}

func NormalizeRole(role Role) Role {
	switch Role(strings.ToLower(strings.TrimSpace(string(role)))) {
	case RoleSystem:
		return RoleSystem
	case RoleOperator:
		return RoleOperator
	case RoleBroadcaster:
		return RoleBroadcaster
	case RoleModerator:
		return RoleModerator
	case RoleSupporter:
		return RoleSupporter
	default:
		return RoleViewer
	}
}

func RoleAllows(actor Role, required Role) bool {
	switch NormalizeRole(actor) {
	case RoleBroadcaster, RoleOperator, RoleSystem:
		return true
	}
	return roleRank(NormalizeRole(actor)) >= roleRank(NormalizeRole(required))
}

func roleRank(role Role) int {
	switch NormalizeRole(role) {
	case RoleSystem:
		return 6
	case RoleOperator:
		return 5
	case RoleBroadcaster:
		return 4
	case RoleModerator:
		return 3
	case RoleSupporter:
		return 2
	default:
		return 1
	}
}
