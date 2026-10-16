package platformmoderation

import (
	"context"
	"errors"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
)

var ErrDenied = errors.New("moderation authorization denied")

// Target is private, immutable effect input from the moderation ledger. It
// must not be projected into jobs, exports or logs. Credential selection must
// depend only on tenant, connection, channel and capability, never reason text.
type Target struct {
	TenantID, ConnectionID string
	Deadline               time.Time
	Action                 platform.ModerationAction
}

// Authorization supplies one selected identity and exact-action callback to a
// future fenced executor. It grants neither a pacing permit nor a ledger claim.
// The executor must compare identity/binding before and after its durable claim.
type Authorization struct {
	ClientID, ModeratorID, Binding string
	Capabilities                   platform.CapabilityReport
	Attempt                        func(context.Context, platform.ModerationAction) (platform.ModerationReceipt, error)
}

type Authorize func(context.Context, Target) (Authorization, error)
