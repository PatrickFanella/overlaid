package twitchoutbound

import (
	"context"
	"errors"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformmoderation"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformoutbound"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitchmoderation"
)

var errModerationUnavailable = errors.New("twitch moderation authorization unavailable")

func moderationError(err error) error {
	if errors.Is(err, platformoutbound.ErrDenied) {
		return platformmoderation.ErrDenied
	}
	return errModerationUnavailable
}

// AuthorizeModeration shares the existing credential custody boundary, with
// explicit moderation scopes and shared capability consent. No executor or
// startup registration is supplied. Target is private ledger input.
func (a *Adapter) AuthorizeModeration(ctx context.Context, t platformmoderation.Target) (platformmoderation.Authorization, error) {
	if !tenantdb.ValidID(t.TenantID) || t.Action.ValidateTwitch() != nil || t.ConnectionID != "twitch:"+t.Action.ChannelID || !t.Deadline.After(time.Now()) {
		return platformmoderation.Authorization{}, platformmoderation.ErrDenied
	}
	scope := "moderator:manage:banned_users"
	if t.Action.Capability == "message.delete" {
		scope = "moderator:manage:chat_messages"
	}
	deadline := time.Now().Add(10 * time.Second)
	if t.Deadline.Before(deadline) {
		deadline = t.Deadline
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	metadata := platformoutbound.Target{TenantID: t.TenantID, ActionID: t.Action.ActionID, ConnectionID: t.ConnectionID, ChannelID: t.Action.ChannelID, Capability: t.Action.Capability, Deadline: t.Deadline}
	first, err := a.resolveSender(ctx, metadata, scope)
	if err != nil {
		return platformmoderation.Authorization{}, moderationError(err)
	}
	moderatorID, binding := first.credential.UserID, first.selection.Binding
	gates := platform.CapabilityGates{Supported: true, Granted: true, RoleAllowed: true, StateAllowed: true, Entitled: true}
	return platformmoderation.Authorization{ModeratorID: moderatorID, Binding: binding,
		Capabilities: platform.EvaluateCapabilities(map[platform.Capability]platform.CapabilityGates{t.Action.Capability: gates}),
		Attempt: func(attemptCtx context.Context, action platform.ModerationAction) (platform.ModerationReceipt, error) {
			rejected := platform.ModerationReceipt{ActionID: action.ActionID, Status: platform.Rejected, Failure: platform.Unauthorized}
			fail := func(err error) (platform.ModerationReceipt, error) {
				mapped := moderationError(err)
				receipt := rejected
				if !errors.Is(mapped, platformmoderation.ErrDenied) {
					receipt.Failure = platform.Retryable
				}
				return receipt, mapped
			}
			if action != t.Action || !t.Deadline.After(time.Now()) {
				return rejected, platformmoderation.ErrDenied
			}
			attemptDeadline := time.Now().Add(10 * time.Second)
			if t.Deadline.Before(attemptDeadline) {
				attemptDeadline = t.Deadline
			}
			attemptCtx, stop := context.WithDeadline(attemptCtx, attemptDeadline)
			defer stop()
			// Re-resolve at invocation too: an old closure is not consent or a
			// live role/grant. Refresh-only updates preserve the binding revision.
			current, e := a.resolveSender(attemptCtx, metadata, scope)
			if e != nil {
				return fail(e)
			}
			if current.selection.Binding != binding || current.credential.UserID != moderatorID {
				return rejected, platformmoderation.ErrDenied
			}
			if e = a.reserve(attemptCtx, current.db); e != nil {
				return fail(e)
			}
			if e = a.current(attemptCtx, metadata, binding); e != nil {
				return fail(e)
			}
			if !t.Deadline.After(time.Now()) {
				return rejected, platformmoderation.ErrDenied
			}
			return twitchmoderation.AttemptUser(attemptCtx, a.client, a.config.ClientID, current.credential.AccessToken, current.credential.UserID, action)
		}}, nil
}
