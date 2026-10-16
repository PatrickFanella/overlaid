package httpapi

import (
	"context"

	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

func (s *Server) enforceModerationForEvent(ctx context.Context, event events.Event) error {
	return s.moderationWorkflow().enforceModerationForEvent(ctx, event)
}
