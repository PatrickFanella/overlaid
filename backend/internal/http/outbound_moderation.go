package httpapi

import (
	"context"
	"errors"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformmoderation"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

var errModerationController = errors.New("retained moderation execution unavailable")

// moderationContext binds explicitly constructed moderation to the existing
// controller and current tenant runtime. No echo gate is acquired. A saved
// invocation closure must retain its exact runtime, not rebuild after invalidation.
func (a *Application) moderationContext(ctx context.Context, target platformmoderation.Target, expected *Server) (context.Context, context.CancelFunc, *Server, error) {
	if a.outbound == nil || a.config.TwitchChatSendMode != "retained" || !tenantdb.ValidID(target.TenantID) || target.Action.ValidateTwitch() != nil || target.ConnectionID != "twitch:"+target.Action.ChannelID || !target.Deadline.After(time.Now()) {
		return nil, nil, nil, errModerationController
	}
	if a.config.DeploymentMode == "hosted" && target.TenantID == tenantdb.BootstrapID {
		return nil, nil, nil, platformmoderation.ErrDenied
	}
	owned, stopApp := a.outboundContext(ctx)
	child, stopDeadline := context.WithDeadline(owned, target.Deadline)
	cancel := func() { stopDeadline(); stopApp() }
	select {
	case <-child.Done():
		cancel()
		return nil, nil, nil, errModerationController
	case <-a.outbound.started:
	}
	if err := a.guardOutboundChat(child); err != nil {
		cancel()
		return nil, nil, nil, errModerationController
	}
	server := expected
	if server == nil {
		var err error
		server, err = a.runtime(child, target.TenantID)
		if err != nil {
			cancel()
			return nil, nil, nil, errModerationController
		}
	}
	a.mu.Lock()
	current, ok := a.runtimes[target.TenantID]
	if !ok || current.server != server || server.tenantContext.Err() != nil {
		a.mu.Unlock()
		cancel()
		return nil, nil, nil, errModerationController
	}
	stopTenant := context.AfterFunc(server.tenantContext, cancel)
	a.mu.Unlock()
	if child.Err() != nil {
		stopTenant()
		cancel()
		return nil, nil, nil, errModerationController
	}
	return child, func() { stopTenant(); cancel() }, server, nil
}

// These wrappers are explicit foundations, not startup registration. They rely
// on the retained controller's validated roles and barrier; the future consumer
// must stop before its supplied pools or controller ownership are released.
func (a *Application) moderationAuthorize(authorize platformmoderation.Authorize) platformmoderation.Authorize {
	return func(ctx context.Context, target platformmoderation.Target) (platformmoderation.Authorization, error) {
		bound, release, server, err := a.moderationContext(ctx, target, nil)
		if err != nil {
			return platformmoderation.Authorization{}, err
		}
		defer release()
		auth, err := authorize(bound, target)
		if err != nil {
			return platformmoderation.Authorization{}, err
		}
		if bound.Err() != nil || auth.Attempt == nil {
			return platformmoderation.Authorization{}, errModerationController
		}
		attempt := auth.Attempt
		auth.Attempt = func(ctx context.Context, action platform.ModerationAction) (platform.ModerationReceipt, error) {
			rejected := platform.ModerationReceipt{ActionID: action.ActionID, Status: platform.Rejected, Failure: platform.Retryable}
			if action != target.Action {
				rejected.Failure = platform.Unauthorized
				return rejected, platformmoderation.ErrDenied
			}
			bound, release, _, err := a.moderationContext(ctx, target, server)
			if err != nil {
				return rejected, err
			}
			defer release()
			return attempt(bound, action)
		}
		return auth, nil
	}
}

func (a *Application) moderationAttemptGate(ctx context.Context, target platformmoderation.Target, cancelAttempt context.CancelFunc) (func(), error) {
	bound, release, _, err := a.moderationContext(ctx, target, nil)
	if err != nil {
		return nil, err
	}
	stop := context.AfterFunc(bound, cancelAttempt)
	if bound.Err() != nil {
		stop()
		release()
		cancelAttempt()
		return nil, errModerationController
	}
	return func() { stop(); release() }, nil
}
