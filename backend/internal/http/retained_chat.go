package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync/atomic"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformdispatch"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitch"
)

const chatControllerKey = `hashtextextended('overlaid:chat-controller:'||current_database()||':'||current_schema(),0)`

// A live session is insufficient: its advisory lock can have been released.
// PostgreSQL represents a bigint advisory key as two unsigned OID halves.
const chatControllerHeld = `SELECT EXISTS(SELECT 1 FROM pg_locks
 WHERE locktype='advisory' AND pid=pg_backend_pid() AND granted
 AND mode='ExclusiveLock' AND objsubid=1
 AND classid=((` + chatControllerKey + ` >> 32) & 4294967295)::oid
 AND objid=(` + chatControllerKey + ` & 4294967295)::oid)`

type chatController struct {
	pool  *sql.DB
	lease *sql.Conn
	live  atomic.Bool
}

func (a *Application) initChatDispatch() error {
	retained := a.config.TwitchChatDispatchMode == "retained"
	// Disabling ingestion does not disable manual announcement endpoints. Every
	// managed chat writer participates in the same installation-level ownership.
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
	defer cancel()
	dsn := a.config.DatabaseURL
	if retained {
		dsn = a.config.WorkerDatabaseURL
	}
	pool, err := sql.Open("pgx", dsn)
	if err != nil {
		return errors.New("chat controller unavailable")
	}
	pool.SetMaxOpenConns(16)
	pool.SetMaxIdleConns(4)
	a.chatControl = &chatController{pool: pool}
	if retained {
		if a.config.TwitchChatSendMode == "retained" {
			a.chatWorker, err = platformdispatch.NewControlledClient(ctx, pool, a.store.Product, a.handleRetainedChat, a.outboundLeaseReady)
		} else {
			a.chatWorker, err = platformdispatch.NewClient(ctx, pool, a.store.Product, a.handleRetainedChat)
		}
		if err != nil {
			return errors.New("retained chat worker configuration invalid")
		}
	}
	lease, err := pool.Conn(ctx)
	if err != nil {
		return errors.New("chat controller unavailable")
	}
	a.chatControl.lease = lease
	var acquired bool
	if err = lease.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(`+chatControllerKey+`)`).Scan(&acquired); err != nil || !acquired {
		return errors.New("another chat controller is active")
	}
	var mode string
	err = a.store.Product.QueryRowContext(ctx, `SELECT value FROM app_metadata WHERE /*tenant:app_metadata*/ AND key='chat_dispatch_mode'`).Scan(&mode)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return errors.New("chat dispatch mode unavailable")
	}
	if !retained && mode == "retained" {
		return errors.New("retained chat mode guard blocks legacy ingestion")
	}
	var senderMode string
	err = a.store.Product.QueryRowContext(ctx, `SELECT value FROM app_metadata WHERE /*tenant:app_metadata*/ AND key='chat_send_mode'`).Scan(&senderMode)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return errors.New("chat sender mode unavailable")
	}
	if senderMode != "" && (senderMode != "retained-v1" || a.config.TwitchChatSendMode != "retained") {
		return errors.New("retained chat sender guard blocks incompatible startup")
	}
	if err := a.prepareOutboundChat(ctx); err != nil {
		return err
	}
	if retained {
		tx, err := a.store.Product.BeginTx(ctx, nil)
		if err != nil {
			return errors.New("chat mode persistence unavailable")
		}
		defer func() { _ = tx.Rollback() }()
		if _, err := tx.ExecContext(ctx, `INSERT INTO app_metadata(key,value) VALUES('chat_dispatch_mode','retained') ON CONFLICT(tenant_id,key) DO UPDATE SET value=excluded.value,updated_at=now()`); err != nil {
			return errors.New("chat mode persistence unavailable")
		}
		if a.outbound != nil {
			if _, err := tx.ExecContext(ctx, `INSERT INTO app_metadata(key,value) VALUES('chat_send_mode','retained-v1') ON CONFLICT(tenant_id,key) DO UPDATE SET value=excluded.value,updated_at=now()`); err != nil {
				return errors.New("chat mode persistence unavailable")
			}
		}
		if err := tx.Commit(); err != nil {
			return errors.New("chat mode persistence unavailable")
		}
	}
	a.chatControl.live.Store(true)
	// Start retains the supplied context for worker lifetime. Bound startup by
	// canceling the application on timeout, rather than expiring a successful
	// worker's context after this function returns.
	startupTimer := time.AfterFunc(10*time.Second, a.cancel)
	defer startupTimer.Stop()
	if retained {
		if err = a.chatWorker.Start(a.ctx); err != nil {
			return errors.New("retained chat worker could not start")
		}
		a.chatWorkerStarted = true
	}
	if a.outbound != nil {
		if err := a.outbound.worker.Start(a.ctx); err != nil {
			return errors.New("retained chat sender could not start")
		}
		a.outbound.workerStarted = true
	}
	if !startupTimer.Stop() || a.ctx.Err() != nil {
		return errors.New("chat worker startup unavailable")
	}
	if a.outbound != nil {
		a.outbound.ready.Store(true)
		close(a.outbound.started)
	}
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-a.ctx.Done():
				return
			case <-ticker.C:
				if a.checkChatController(a.ctx) != nil {
					return
				}
				if a.outbound != nil {
					if !a.outboundReady() || a.validateOutboundChat(a.ctx) != nil {
						a.failOutboundChat()
						return
					}
				}
			}
		}
	}()
	return nil
}

func (a *Application) chatReady() bool {
	return a.chatControl != nil && a.chatControl.live.Load() && a.ctx.Err() == nil
}

func (a *Application) checkChatController(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !a.chatReady() {
		return errors.New("chat controller unavailable")
	}
	check, stop := context.WithTimeout(ctx, time.Second)
	defer stop()
	var held bool
	err := a.chatControl.lease.QueryRowContext(check, chatControllerHeld).Scan(&held)
	if err != nil && ctx.Err() != nil {
		// A canceled caller must not stop unrelated tenant work.
		return ctx.Err()
	}
	if err != nil || !held {
		a.chatControl.live.Store(false)
		a.cancel()
		return errors.New("chat controller unavailable")
	}
	return nil
}

func (a *Application) handleRetainedChat(ctx context.Context, event platform.ChatEvent) error {
	if !a.chatReady() || a.config.DeploymentMode == "hosted" && event.TenantID == tenantdb.BootstrapID {
		return errors.New("chat controller unavailable")
	}
	if err := a.checkChatController(ctx); err != nil {
		return err
	}
	s, err := a.runtime(ctx, event.TenantID)
	if err != nil {
		return errors.New("tenant runtime unavailable")
	}
	return s.dispatchRetainedChat(ctx, event)
}

func (s *Server) ingestRetainedChat(ctx context.Context, message twitch.EventSubMessage, token twitch.Token) error {
	if s.platformIngest == nil || s.chatReady == nil || !s.chatReady() {
		return errors.New("retained chat unavailable")
	}
	if _, ok := twitch.MapNotification(message); !ok {
		return nil
	}
	event, err := twitch.NormalizeChatIngest(platform.Connection{TenantID: s.tenantID, ID: "twitch:" + token.UserID, Platform: "twitch"}, message, time.Now().UTC())
	if err != nil {
		return err
	}
	if event.Chat.Channel.ID != token.UserID {
		return errors.New("chat broadcaster mismatch")
	}
	if sent, err := s.knownSentMessage(ctx, event.Chat.MessageID); err != nil {
		return err
	} else if sent {
		return nil
	}
	raw, err := json.Marshal(message)
	if err != nil {
		return err
	}
	_, err = s.platformIngest.IngestChat(ctx, event, raw)
	return err
}

func acquireGate(ctx context.Context, gate chan struct{}) (func(), error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case gate <- struct{}{}:
		return func() { <-gate }, nil
	}
}

func (s *Server) knownSentMessage(ctx context.Context, id string) (bool, error) {
	if s.twitchSentMessages.Contains(id) {
		return true, nil
	}
	if s.platformIngest != nil {
		return s.platformIngest.IsSentMessage(ctx, id)
	}
	return false, nil
}

func (s *Server) dispatchRetainedChat(ctx context.Context, event platform.ChatEvent) error {
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	if s.tenantContext != nil {
		stopTenant := context.AfterFunc(s.tenantContext, stop)
		defer stopTenant()
	}
	release, err := acquireGate(ctx, s.retainedChatGate)
	if err != nil {
		return err
	}
	defer release()
	// A known echo may arrive while a successful send is still storing its ID.
	// Wait for that send before checking the shared durable marker.
	sendRelease, err := acquireGate(ctx, s.chatSendGate)
	if err != nil {
		return err
	}
	sent, err := s.knownSentMessage(ctx, event.Chat.MessageID)
	sendRelease()
	if err != nil {
		return err
	}
	if sent {
		return nil
	}
	var replies *registryReplyCollection
	if s.retainedReplies != nil {
		replies, err = s.beginRegistryReplies(ctx, event)
		if err != nil {
			return err
		}
		defer replies.close()
	}
	rules := s.interactions.Rules()
	for _, out := range twitch.EventsForNormalizedChat(event, s.twitchCommandProcessor, rules) {
		if err := ctx.Err(); err != nil {
			return err
		}
		outputContext := ctx
		if replies != nil {
			outputContext = replies.outputContext(ctx, out)
		}
		if err := s.publishEventSubContext(outputContext, out, true); err != nil {
			return err
		}
	}
	if replies != nil {
		return replies.admit(ctx)
	}
	return nil
}
