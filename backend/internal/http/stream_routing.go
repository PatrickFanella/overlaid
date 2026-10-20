package httpapi

import (
	"context"
	"encoding/json"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformingest"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitch"
)

// One routing lock spans legacy effects or retained admission. Installation
// drains old effects; an installed prepared/stopped owner never falls back.
func (a *Application) routeStreamLifecycle(ctx context.Context, server *Server, message twitch.EventSubMessage, token twitch.Token) error {
	bound, cancel := a.outboundContext(ctx)
	defer cancel()
	release, err := acquireModerationRoute(bound, a.streamRouting.TryRLock, a.streamRouting.RUnlock)
	if err != nil {
		return errRetainedStream
	}
	defer release()
	a.mu.Lock()
	owner := a.stream
	current, ok := a.runtimes[server.tenantID]
	live := ok && current.server == server && !a.deleting[server.tenantID] && server.tenantContext.Err() == nil
	a.mu.Unlock()
	if !live {
		return errRetainedStream
	}
	stopTenant := context.AfterFunc(server.tenantContext, cancel)
	defer stopTenant()
	if server.tenantContext.Err() != nil {
		return errRetainedStream
	}
	if owner != nil {
		return owner.ingest(bound, server, message, token)
	}
	if a.config.TwitchStreamDispatchMode == "retained" || a.checkChatController(bound) != nil {
		return errRetainedStream
	}
	return server.newsroomTwitchLifecycleDirect(bound, message, token)
}

func (b *streamConsumer) ingest(ctx context.Context, server *Server, message twitch.EventSubMessage, token twitch.Token) error {
	if !b.isReady() {
		return errRetainedStream
	}
	if b.app.config.DeploymentMode == "hosted" && server.tenantID == tenantdb.BootstrapID {
		return errRetainedStream
	}
	bound, cancel := b.bind(ctx)
	defer cancel()
	bound, stop := context.WithTimeout(bound, 5*time.Second)
	defer stop()
	if !b.app.ownsStreamRuntime(server.tenantID, server) || b.guard(bound) != nil {
		return errRetainedStream
	}
	if message.Payload.Subscription.Type != "stream.online" && message.Payload.Subscription.Type != "stream.offline" {
		return errRetainedStream
	}
	event, err := twitch.NormalizeStreamNotification(platform.Connection{TenantID: server.tenantID, ID: "twitch:" + token.UserID, Platform: "twitch"}, token.UserID, message, time.Now().UTC())
	if err != nil {
		return errRetainedStream
	}
	native, err := json.Marshal(message)
	if err != nil || len(native) > 65536 {
		return errRetainedStream
	}
	db, err := b.app.store.Product.Scoped(server.tenantID)
	if err != nil {
		return errRetainedStream
	}
	store, err := platformingest.New(db)
	if err != nil {
		return errRetainedStream
	}
	// Admission locks/rechecks the current nonrevoked connection and original
	// source/deadline. Failure must reach EventSub; there is no direct fallback.
	if _, err := store.IngestStream(bound, event, native); err != nil {
		return errRetainedStream
	}
	return nil
}

func (a *Application) streamLifecycleReady(ctx context.Context) error {
	bound, cancel := a.outboundContext(ctx)
	defer cancel()
	release, err := acquireModerationRoute(bound, a.streamRouting.TryRLock, a.streamRouting.RUnlock)
	if err != nil {
		return errRetainedStream
	}
	defer release()
	a.mu.Lock()
	owner := a.stream
	a.mu.Unlock()
	if owner != nil {
		if !owner.isReady() {
			return errRetainedStream
		}
		return owner.guard(bound)
	}
	if a.config.TwitchStreamDispatchMode == "retained" || bound.Err() != nil || a.checkChatController(bound) != nil {
		return errRetainedStream
	}
	return nil
}
