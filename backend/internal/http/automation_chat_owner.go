package httpapi

import (
	"context"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

// operatorChatContext binds explicit new admission to the existing retained
// sender and exact already-live runtime. It never creates a runtime or pool.
func (a *Application) operatorChatContext(ctx context.Context, s *Server) (context.Context, context.CancelFunc, error) {
	if ctx == nil || a == nil || a.ctx == nil || a.store == nil || s == nil || s.tenantContext == nil || s.retainedReplies == nil || s.retainedReplies.db == nil || s.retainedReplies.db.TenantID() != s.tenantID || s.tenantID == tenantdb.BootstrapID || a.config.TwitchChatSendMode != "retained" || a.config.TwitchChatDispatchMode != "retained" || !a.outboundReady() {
		return nil, nil, errAutomationWriterUnavailable
	}
	bound, cancel := a.outboundContext(ctx)
	bounded, timeout := context.WithTimeout(bound, 5*time.Second)
	stopTenant := context.AfterFunc(s.tenantContext, cancel)
	release := func() { stopTenant(); timeout(); cancel() }
	a.mu.Lock()
	current, ok := a.runtimes[s.tenantID]
	live := ok && current.server == s && !a.deleting[s.tenantID] && s.tenantContext.Err() == nil
	a.mu.Unlock()
	if !live {
		release()
		return nil, nil, errAutomationWriterUnavailable
	}
	if err := a.guardOutboundChat(bounded); err != nil {
		release()
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, errAutomationWriterUnavailable
	}
	if bounded.Err() != nil {
		release()
		return nil, nil, bounded.Err()
	}
	return bounded, release, nil
}

// admitOperatorChatPlan is the explicit application operation used by account admission.
// Callers supply authenticated actor and a frozen original request/digest.
// The store rechecks configure, source, target, limits and original deadline.
func (a *Application) admitOperatorChatPlan(ctx context.Context, s *Server, actor, request, digest string) (tenancy.OperatorChatAdmission, error) {
	bound, release, err := a.operatorChatContext(ctx, s)
	if err != nil {
		return tenancy.OperatorChatAdmission{}, err
	}
	defer release()
	scope, err := a.store.Product.Scoped(s.tenantID)
	if err != nil {
		return tenancy.OperatorChatAdmission{}, errAutomationWriterUnavailable
	}
	store, err := tenancy.NewAutomationStore(bound, scope)
	if err != nil {
		return tenancy.OperatorChatAdmission{}, err
	}
	return store.AdmitOperatorChatPlan(bound, actor, request, digest)
}
