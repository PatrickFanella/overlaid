package bot

import (
	"context"
	"log"
	"strings"
	"sync"
)

type Role string

const (
	RoleViewer      Role = "viewer"
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

type Runtime struct {
	recorder AuditRecorder
	mu       sync.RWMutex
	lastErr  string
}

func NewRuntime(recorder AuditRecorder) *Runtime {
	return &Runtime{recorder: recorder}
}

func (r *Runtime) Allowed(actor Role, required Role) bool {
	return roleRank(NormalizeRole(actor)) >= roleRank(NormalizeRole(required))
}

func (r *Runtime) Record(ctx context.Context, event AuditEvent) {
	if r == nil || r.recorder == nil {
		return
	}
	if _, err := r.recorder.RecordAudit(ctx, event); err != nil {
		r.setLastError(err.Error())
		log.Printf("bot audit record failed: %v", err)
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
	default:
		return RoleViewer
	}
}

func roleRank(role Role) int {
	switch NormalizeRole(role) {
	case RoleSystem:
		return 5
	case RoleOperator:
		return 4
	case RoleBroadcaster:
		return 3
	case RoleModerator:
		return 2
	default:
		return 1
	}
}
