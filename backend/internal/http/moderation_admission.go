package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformmoderation"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

// admit is an explicit source-bound writer, not a route or startup registration.
// The caller supplies neither a source envelope nor a deadline, sender or claim.
// Existing direct workflows must be cut over together before activation.
func (b *moderationConsumer) admit(ctx context.Context, server *Server, sourceID, key string, request botModerationActionRequest) (string, bool, error) {
	if b == nil || server == nil || server.tenantContext == nil || !tenantdb.ValidID(server.tenantID) || len(sourceID) != 64 {
		return "", false, platformmoderation.ErrInvalid
	}
	if b.app.config.DeploymentMode == "hosted" && server.tenantID == tenantdb.BootstrapID {
		return "", false, platformmoderation.ErrDenied
	}
	if !b.isReady() {
		return "", false, errModerationController
	}
	bound, cancel := b.bind(ctx)
	defer cancel()
	bound, stop := context.WithTimeout(bound, 5*time.Second)
	defer stop()
	stopTenant := context.AfterFunc(server.tenantContext, cancel)
	defer stopTenant()
	b.app.mu.Lock()
	current, ok := b.app.runtimes[server.tenantID]
	live := ok && current.server == server && server.tenantContext.Err() == nil
	b.app.mu.Unlock()
	if !live || b.guard(bound) != nil {
		return "", false, errModerationController
	}
	db, err := b.app.store.Product.Scoped(server.tenantID)
	if err != nil {
		return "", false, errModerationController
	}
	var raw []byte
	if err := db.QueryRowContext(bound, `SELECT envelope FROM normalized_chat_events WHERE /*tenant:normalized_chat_events*/ AND id=$1`, sourceID).Scan(&raw); err != nil {
		return "", false, platformmoderation.ErrInvalid
	}
	var source platform.ChatEvent
	if json.Unmarshal(raw, &source) != nil || source.ID != sourceID || source.TenantID != server.tenantID {
		return "", false, platformmoderation.ErrInvalid
	}
	input, err := moderationRequestAction(source.Chat.Channel.ID, request)
	if err != nil {
		return "", false, err
	}
	intent, err := platform.NewTwitchModerationIntent(source, key, input)
	if err != nil {
		return "", false, platformmoderation.ErrInvalid
	}
	// Rebind to this exact runtime after source reconstruction. The ledger then
	// rechecks the retained source/connection and database-clock deadline inside
	// its atomic action/job transaction. No provider authorization happens here.
	target := platformmoderation.Target{TenantID: intent.TenantID(), ConnectionID: intent.ConnectionID(), Deadline: intent.Deadline(), Action: intent.Action()}
	owned, release, _, err := b.app.moderationContext(bound, target, server)
	if err != nil {
		return "", false, err
	}
	defer release()
	if err := b.guard(owned); err != nil {
		return "", false, errModerationController
	}
	ledger, err := platformmoderation.New(db)
	if err != nil {
		return "", false, errModerationController
	}
	id, created, err := ledger.Admit(owned, intent)
	if err != nil && !errors.Is(err, platformmoderation.ErrInvalid) && !errors.Is(err, platformmoderation.ErrConflict) && !errors.Is(err, tenantdb.ErrLimit) {
		return "", false, errModerationController
	}
	return id, created, err
}

// Preserve the legacy request's operation semantics: stale timeout, message,
// user and reason draft fields do not change another operation. The typed
// intent constructor validates the selected fields before persistence.
func moderationRequestAction(channel string, request botModerationActionRequest) (platform.ModerationAction, error) {
	input := platform.ModerationAction{ChannelID: channel}
	switch strings.ToLower(strings.TrimSpace(request.Action)) {
	case "delete":
		input.Capability, input.MessageID = "message.delete", strings.TrimSpace(request.MessageID)
	case "timeout", "ban":
		input.Capability, input.UserID, input.Reason = "user.ban", strings.TrimSpace(request.TargetUserID), request.Reason
		if strings.EqualFold(strings.TrimSpace(request.Action), "timeout") {
			// Validate before converting int seconds to nanoseconds, including
			// on 64-bit callers with values that would overflow time.Duration.
			if request.DurationSeconds < 1 || request.DurationSeconds > 1209600 {
				return platform.ModerationAction{}, platformmoderation.ErrInvalid
			}
			input.Capability, input.Duration = "user.timeout", time.Duration(request.DurationSeconds)*time.Second
		}
	case "unban":
		input.Capability, input.UserID = "user.unban", strings.TrimSpace(request.TargetUserID)
	default:
		return platform.ModerationAction{}, platformmoderation.ErrInvalid
	}
	return input, nil
}
