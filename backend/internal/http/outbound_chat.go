package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"sync/atomic"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/chatpacing"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformoutbound"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformqueue"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitchoutbound"
	"github.com/riverqueue/river"
)

var errOutboundController = errors.New("retained chat sender unavailable")

// Owned by the existing chat controller. No independent dispatcher or parser.
type outboundChat struct {
	pool          *sql.DB
	pacing        *chatpacing.Store
	shared        *tenancy.BotExecutionStore
	worker        *river.Client[*sql.Tx]
	workerStarted bool
	ready         atomic.Bool
	started       chan struct{}
}

func (a *Application) prepareOutboundChat(ctx context.Context) error {
	if a.config.TwitchChatSendMode != "retained" {
		return nil
	}
	b := &outboundChat{started: make(chan struct{})}
	a.outbound = b // Partial startup is cleaned up by Application.Close.
	var err error
	b.pacing, err = chatpacing.Open(ctx, a.config.PacingDatabaseURL)
	if err != nil {
		return errors.New("retained chat pacing configuration invalid")
	}
	if err := b.pacing.CheckState(ctx); err != nil {
		return errors.New("retained chat pacing state incompatible")
	}
	b.pool, err = sql.Open("pgx", a.config.OutboundWorkerDatabaseURL)
	if err != nil {
		return errors.New("retained chat worker configuration invalid")
	}
	b.pool.SetMaxOpenConns(16)
	b.pool.SetMaxIdleConns(4)
	c := twitchoutbound.Config{ClientID: a.config.TwitchClientID, ClientSecret: a.config.TwitchClientSecret, Client: twitchHTTPClient}
	var adapter *twitchoutbound.Adapter
	if a.config.BotExecutionDatabaseURL != "" {
		b.shared, err = tenancy.OpenBotExecutionStore(ctx, a.config.BotExecutionDatabaseURL)
		if err != nil {
			return errors.New("retained shared execution configuration invalid")
		}
		adapter, err = twitchoutbound.NewShared(ctx, a.store, a.keys, c, b.shared)
	} else {
		adapter, err = twitchoutbound.New(a.store, a.keys, c)
	}
	if err != nil {
		return errors.New("retained chat authorization configuration invalid")
	}
	executor, err := platformoutbound.NewExecutorWithFairPreparation(ctx, a.store.Product, b.pacing, a.outboundAuthorize(adapter.Authorize), a.outboundSendGate, a.outboundPacingIdentity(adapter.PacingSender))
	if err != nil {
		return errors.New("retained chat execution configuration invalid")
	}
	b.worker, err = platformoutbound.NewControlledClient(ctx, b.pool, executor, a.outboundLeaseReady)
	if err != nil {
		return errors.New("retained chat worker configuration invalid")
	}
	return a.validateOutboundChat(ctx)
}

func (a *Application) outboundLeaseReady() bool {
	return a.outbound != nil && a.outbound.ready.Load() && a.chatReady() && a.moderationLeasingReady()
}

func (a *Application) outboundReady() bool {
	b := a.outbound
	if b == nil || !b.ready.Load() || !a.chatReady() {
		return false
	}
	select {
	case <-b.worker.Stopped():
		return false
	default:
	}
	select {
	case <-a.chatWorker.Stopped():
		return false
	default:
		return true
	}
}

func (a *Application) validateOutboundChat(ctx context.Context) error {
	b := a.outbound
	if b == nil {
		return errOutboundController
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := validateOutboundProduct(ctx, a.store.Product); err != nil {
		return errors.New("retained chat product role invalid")
	}
	if _, err := platformqueue.Validate(ctx, a.chatControl.pool, a.store.Product, "overlaid_chat_dispatch"); err != nil {
		return errOutboundController
	}
	// NewFairDriver validates the sequence and exact-kind policy without leasing.
	if _, _, err := platformqueue.NewFairDriver(ctx, b.pool, a.store.Product, "overlaid_chat_send"); err != nil {
		return errOutboundController
	}
	if err := b.pacing.Validate(ctx); err != nil {
		return errOutboundController
	}
	if b.shared != nil {
		if err := b.shared.Validate(ctx); err != nil {
			return errOutboundController
		}
	}
	return nil
}

func (a *Application) guardOutboundChat(ctx context.Context) error {
	if err := a.checkChatController(ctx); err != nil {
		return err
	}
	if !a.outboundReady() {
		a.failOutboundChat()
		return errOutboundController
	}
	if err := a.validateOutboundChat(ctx); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		a.failOutboundChat()
		return errOutboundController
	}
	return nil
}

func (a *Application) failOutboundChat() {
	a.outbound.ready.Store(false)
	a.chatControl.live.Store(false)
	a.cancel()
}

// Runtime references and active attempts keep one tenant gate across runtime
// invalidation. Entries disappear when neither owns a reference, so historical
// tenant churn cannot grow this map without bound.
type tenantSendGate struct {
	channel    chan struct{}
	references int
}

func (a *Application) retainRuntimeSendGateLocked(tenant string, server *Server) {
	if a.sendGates == nil {
		a.sendGates = map[string]*tenantSendGate{}
	}
	gate := a.sendGates[tenant]
	if gate == nil {
		gate = &tenantSendGate{channel: make(chan struct{}, 1)}
		a.sendGates[tenant] = gate
	}
	gate.references++
	server.chatSendGate = gate.channel
}

func (a *Application) releaseSendGateLocked(tenant string, channel chan struct{}) {
	gate := a.sendGates[tenant]
	if gate == nil || gate.channel != channel {
		return
	}
	gate.references--
	if gate.references == 0 {
		delete(a.sendGates, tenant)
	}
}

// Pair provider outcome and durable echo writes with every current consumer,
// including one rebuilt after a runtime invalidation. The original attempt is
// canceled on tenant invalidation; a confirmed late response still keeps its ID.
func (a *Application) outboundSendGate(ctx context.Context, target platformoutbound.Target, cancelAttempt context.CancelFunc) (func(), error) {
	if err := a.guardOutboundChat(ctx); err != nil {
		return nil, err
	}
	server, err := a.runtime(ctx, target.TenantID)
	if err != nil || server.retainedReplies == nil {
		return nil, errOutboundController
	}
	a.mu.Lock()
	current, ok := a.runtimes[target.TenantID]
	gate := a.sendGates[target.TenantID]
	if !ok || current.server != server || gate == nil || server.tenantContext.Err() != nil {
		a.mu.Unlock()
		return nil, errOutboundController
	}
	gate.references++
	a.mu.Unlock()
	stopTenant := context.AfterFunc(server.tenantContext, cancelAttempt)
	if server.tenantContext.Err() != nil {
		cancelAttempt()
	}
	drop := func() {
		stopTenant()
		a.mu.Lock()
		a.releaseSendGateLocked(target.TenantID, gate.channel)
		a.mu.Unlock()
	}
	release, err := acquireGate(ctx, gate.channel)
	if err != nil {
		drop()
		return nil, err
	}
	return func() { release(); drop() }, nil
}

func (a *Application) outboundContext(ctx context.Context) (context.Context, context.CancelFunc) {
	child, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(a.ctx, cancel)
	if a.ctx.Err() != nil {
		cancel()
	}
	return child, func() { stop(); cancel() }
}

func (a *Application) outboundPacingIdentity(identify platformoutbound.PacingIdentity) platformoutbound.PacingIdentity {
	return func(ctx context.Context, target platformoutbound.Target) (string, error) {
		ctx, cancel := a.outboundContext(ctx)
		defer cancel()
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-a.outbound.started:
		}
		if a.config.DeploymentMode == "hosted" && target.TenantID == tenantdb.BootstrapID {
			return "", platformoutbound.ErrDenied
		}
		if err := a.guardOutboundChat(ctx); err != nil {
			return "", err
		}
		return identify(ctx, target)
	}
}

func (a *Application) outboundAuthorize(authorize platformoutbound.Authorize) platformoutbound.Authorize {
	return func(ctx context.Context, target platformoutbound.Target) (platformoutbound.Authorization, error) {
		ctx, cancel := a.outboundContext(ctx)
		defer cancel()
		// A client can lease during startup; no provider work starts until both
		// validated clients have started. Startup failure cancels this wait.
		select {
		case <-ctx.Done():
			return platformoutbound.Authorization{}, ctx.Err()
		case <-a.outbound.started:
		}
		if a.config.DeploymentMode == "hosted" && target.TenantID == tenantdb.BootstrapID {
			return platformoutbound.Authorization{}, platformoutbound.ErrDenied
		}
		if err := a.guardOutboundChat(ctx); err != nil {
			return platformoutbound.Authorization{}, err
		}
		auth, err := authorize(ctx, target)
		if err != nil {
			return platformoutbound.Authorization{}, err
		}
		send := auth.Send
		if send == nil {
			return platformoutbound.Authorization{}, errOutboundController
		}
		auth.Send = func(ctx context.Context, message platform.OutboundMessage) (platform.Receipt, error) {
			ctx, cancel := a.outboundContext(ctx)
			defer cancel()
			if err := a.guardOutboundChat(ctx); err != nil {
				return platform.Receipt{}, err
			}
			return send(ctx, message)
		}
		return auth, nil
	}
}

func (b *outboundChat) close(ctx context.Context) error {
	b.ready.Store(false)
	var result error
	if b.workerStarted {
		result = errors.Join(result, b.worker.Stop(ctx))
	}
	if b.pool != nil {
		result = errors.Join(result, b.pool.Close())
	}
	if b.pacing != nil {
		result = errors.Join(result, b.pacing.Close())
	}
	if b.shared != nil {
		result = errors.Join(result, b.shared.Close())
	}
	return result
}

// Product credentials must not reach a global vault, pacing pool or worker
// policy, even through NOINHERIT/SET ROLE membership. No secret rows are read.
func validateOutboundProduct(ctx context.Context, product *tenantdb.DB) error {
	var valid bool
	err := product.QueryRowContext(ctx, `WITH reachable AS (
 SELECT r.* FROM pg_roles r WHERE r.rolname IN(current_user,session_user) OR pg_has_role(current_user,r.oid,'MEMBER') OR pg_has_role(session_user,r.oid,'MEMBER')
 ), tables AS (SELECT c.* FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relkind IN('r','p'))
 SELECT NOT EXISTS(SELECT 1 FROM reachable r WHERE r.rolsuper OR r.rolbypassrls OR r.rolcreaterole OR r.rolcreatedb OR r.rolreplication OR has_schema_privilege(r.oid,current_schema(),'CREATE') OR EXISTS(SELECT 1 FROM tables c WHERE c.relowner=r.oid))
 AND NOT EXISTS(SELECT 1 FROM tables c,reachable r WHERE c.relname <> 'service_bot_catalog' AND c.relname ~ '^(service_bot_|chat_pacing_|moderation_pacing_)' AND (has_table_privilege(r.oid,c.oid,'SELECT,INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER') OR has_any_column_privilege(r.oid,c.oid,'SELECT,INSERT,UPDATE,REFERENCES')))
 AND NOT EXISTS(SELECT 1 FROM tables c,reachable r WHERE c.relname='service_bot_catalog' AND (has_table_privilege(r.oid,c.oid,'INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER') OR has_any_column_privilege(r.oid,c.oid,'INSERT,UPDATE,REFERENCES')))
 AND (SELECT count(*)=1 AND bool_and(p.polname='catalog_runtime' AND p.polcmd='r' AND p.polpermissive AND pg_get_expr(p.polqual,p.polrelid)='available') FROM pg_policy p WHERE p.polrelid='service_bot_catalog'::regclass AND EXISTS(SELECT 1 FROM unnest(p.polroles) role_id WHERE CASE WHEN role_id=0 THEN true ELSE EXISTS(SELECT 1 FROM reachable r WHERE r.oid=role_id) END))
 AND (SELECT count(*)=1 AND bool_and(p.polname='tenant_isolation' AND p.polpermissive AND p.polcmd='*') FROM pg_policy p WHERE p.polrelid='river_job'::regclass AND EXISTS(SELECT 1 FROM unnest(p.polroles) role_id WHERE CASE WHEN role_id=0 THEN true ELSE EXISTS(SELECT 1 FROM reachable r WHERE r.oid=role_id) END))
 AND EXISTS(SELECT 1 FROM pg_constraint c WHERE c.conrelid='platform_outbound_actions'::regclass AND c.conname='platform_outbound_actions_failure_check' AND c.convalidated AND position('''plan_limited''::text' in pg_get_constraintdef(c.oid))>0)
 AND (SELECT count(*)=7 AND bool_and(c.relrowsecurity AND c.relforcerowsecurity) FROM tables c WHERE c.relname IN('platform_outbound_actions','normalized_chat_events','platform_manual_sources','platform_connections','tenant_usage','bot_commands','newsroom_documents'))`).Scan(&valid)
	if err != nil || !valid {
		return errOutboundController
	}
	return nil
}
