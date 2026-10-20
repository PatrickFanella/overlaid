package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/newsroom"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformstreamdispatch"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

var errRetainedStream = errors.New("retained stream owner unavailable")

// prepareRetainedStream is explicit: no startup path selects this adapter.
// It binds one canonical source to one runtime, never a later replacement.
func (a *Application) prepareRetainedStream(ctx context.Context, input platform.StreamEvent) (platformstreamdispatch.Prepared, error) {
	empty := platformstreamdispatch.Prepared{}
	if a.store == nil || a.config.TwitchChatDispatchMode != "retained" || !tenantdb.ValidID(input.TenantID) || a.config.DeploymentMode == "hosted" && input.TenantID == tenantdb.BootstrapID {
		return empty, errRetainedStream
	}
	event, err := platform.NewStreamEvent(platform.Connection{TenantID: input.TenantID, ID: input.ConnectionID, Platform: input.Platform}, input.NativeEventID, input.Kind, input.OccurredAt, input.ReceivedAt, input.Channel, input.Online, input.Offline, input.Update)
	raw, marshalErr := json.Marshal(event)
	original, originalErr := json.Marshal(input)
	if err != nil || marshalErr != nil || originalErr != nil || string(raw) != string(original) || event.Platform != "twitch" || event.ConnectionID != "twitch:"+event.Channel.ID {
		return empty, errRetainedStream
	}
	if err := a.checkChatController(ctx); err != nil {
		return empty, errRetainedStream
	}
	db, err := a.store.Product.Scoped(event.TenantID)
	if err != nil {
		return empty, errRetainedStream
	}
	current, err := retainedStreamSource(ctx, db, event, raw)
	if err != nil || !current {
		return empty, errRetainedStream
	}
	revision, err := retainedStreamRevision(ctx, db, event.Channel.ID)
	if err != nil || revision <= 0 {
		return empty, errRetainedStream
	}
	server, err := a.runtime(ctx, event.TenantID)
	if err != nil {
		return empty, errRetainedStream
	}
	owner, cancel := context.WithCancel(a.ctx)
	stopJob := context.AfterFunc(ctx, cancel)
	stopTenant := context.AfterFunc(server.tenantContext, cancel)
	release := sync.OnceFunc(func() { stopJob(); stopTenant(); cancel() })
	prepared := platformstreamdispatch.Prepared{Context: owner, Release: release}
	if ctx.Err() != nil || server.tenantContext.Err() != nil || !a.ownsStreamRuntime(event.TenantID, server) {
		release()
		return empty, errRetainedStream
	}
	bind := func(caller context.Context) (context.Context, func(), error) {
		bound, stop := context.WithCancel(caller)
		stopOwner := context.AfterFunc(owner, stop)
		cleanup := func() { stopOwner(); stop() }
		if owner.Err() != nil || !a.ownsStreamRuntime(event.TenantID, server) {
			cleanup()
			return nil, nil, errRetainedStream
		}
		if err := a.checkChatController(bound); err != nil {
			cleanup()
			return nil, nil, errRetainedStream
		}
		return bound, cleanup, nil
	}
	same := func(candidate platform.StreamEvent) bool {
		value, err := json.Marshal(candidate)
		return err == nil && string(value) == string(raw)
	}
	prepared.Capabilities = func(caller context.Context, candidate platform.StreamEvent) (platform.CapabilityReport, error) {
		if !same(candidate) {
			return platform.CapabilityReport{}, errRetainedStream
		}
		bound, stop, err := bind(caller)
		if err != nil {
			return platform.CapabilityReport{}, err
		}
		defer stop()
		current, err := retainedStreamSource(bound, db, event, raw)
		if err != nil {
			return platform.CapabilityReport{}, errRetainedStream
		}
		latest, err := retainedStreamRevision(bound, db, event.Channel.ID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return platform.CapabilityReport{}, errRetainedStream
		}
		limits, err := db.Limits(bound)
		if err != nil {
			return platform.CapabilityReport{}, errRetainedStream
		}
		doc, err := server.newsroom.Get(bound)
		if err != nil || owner.Err() != nil || !a.ownsStreamRuntime(event.TenantID, server) {
			return platform.CapabilityReport{}, errRetainedStream
		}
		// Twitch's reviewed lifecycle kinds require no additional OAuth scope.
		// This local report never opens credentials or authorizes provider effects.
		return platform.EvaluateCapabilities(map[platform.Capability]platform.CapabilityGates{
			"stream.read": {Supported: event.Kind == platform.StreamOnlineKind || event.Kind == platform.StreamOfflineKind, Granted: latest == revision && latest > 0, RoleAllowed: event.ConnectionID == "twitch:"+event.Channel.ID, StateAllowed: current && doc.Automation.Enabled, Entitled: limits["storedEvents"] != 0},
		}), nil
	}
	prepared.Handle = func(caller context.Context, candidate platform.StreamEvent) error {
		if !same(candidate) {
			return errRetainedStream
		}
		bound, stop, err := bind(caller)
		if err != nil {
			return err
		}
		defer stop()
		report, err := prepared.Capabilities(bound, candidate)
		if err != nil || !report.Check("stream.read").Available {
			return errRetainedStream
		}
		var running bool
		err = db.QueryRowContext(bound, `SELECT EXISTS(SELECT 1 FROM platform_stream_dispatch_receipts WHERE /*tenant:platform_stream_dispatch_receipts*/ AND event_id=$1 AND connection_id=$2 AND deadline=$3 AND state='running' AND deadline>extract(epoch FROM clock_timestamp()))`, event.ID, event.ConnectionID, event.OccurredAt.Unix()+120).Scan(&running)
		if err != nil || !running || owner.Err() != nil || !a.ownsStreamRuntime(event.TenantID, server) {
			return errRetainedStream
		}
		command := newsroom.Command{EventID: event.ID, EventAt: event.OccurredAt}
		switch event.Kind {
		case platform.StreamOnlineKind:
			started := event.Online.StartedAt
			command.Action, command.TwitchStreamID, command.StartedAt = "twitch-online", event.Online.StreamID, &started
		case platform.StreamOfflineKind:
			command.Action = "twitch-offline"
		default:
			return errRetainedStream
		}
		// The existing transactional command rechecks automation settings and
		// preserves watermarks, manual boundaries and editorial revisions.
		if _, err := server.newsroom.Internal(bound, command); err != nil {
			return errRetainedStream
		}
		return nil
	}
	return prepared, nil
}

func (a *Application) ownsStreamRuntime(tenant string, expected *Server) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	current, ok := a.runtimes[tenant]
	return ok && current.server == expected && !a.deleting[tenant] && expected.tenantContext.Err() == nil && a.chatReady()
}

func retainedStreamRevision(ctx context.Context, db *tenantdb.DB, channel string) (int64, error) {
	var revision int64
	err := db.QueryRowContext(ctx, `SELECT sender_revision FROM platform_connections WHERE /*tenant:platform_connections*/ AND provider='twitch' AND provider_user_id=$1 AND revoked_at IS NULL`, channel).Scan(&revision)
	return revision, err
}

func retainedStreamSource(ctx context.Context, db *tenantdb.DB, event platform.StreamEvent, raw []byte) (bool, error) {
	check, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var current bool
	err := db.QueryRowContext(check, `SELECT EXISTS(SELECT 1 FROM normalized_stream_events WHERE /*tenant:normalized_stream_events*/ AND id=$1 AND envelope=$2::jsonb AND platform='twitch' AND kind=$3 AND channel_id=$4 AND connection_id=$5 AND dispatch_deadline=$6 AND idempotency_key=$1 AND channel_login=lower($7) AND occurred_at=$8 AND received_at=$9 AND dispatch_deadline>extract(epoch FROM clock_timestamp()))`, event.ID, raw, event.Kind, event.Channel.ID, event.ConnectionID, event.OccurredAt.Unix()+120, event.Channel.Login, event.OccurredAt.Truncate(time.Microsecond), event.ReceivedAt.Truncate(time.Microsecond)).Scan(&current)
	return current, err
}
