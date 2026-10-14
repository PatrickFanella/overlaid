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

type chatController struct {
	pool  *sql.DB
	lease *sql.Conn
	live  atomic.Bool
}

func (a *Application) initChatDispatch() error {
	retained := a.config.TwitchChatDispatchMode == "retained"
	if !retained && !a.config.TwitchEventSub {
		return nil
	}
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
		a.chatWorker, err = platformdispatch.NewClient(a.ctx, pool, a.store.Product, a.handleRetainedChat)
		if err != nil {
			return errors.New("retained chat worker configuration invalid")
		}
	}
	lease, err := pool.Conn(a.ctx)
	if err != nil {
		return errors.New("chat controller unavailable")
	}
	a.chatControl.lease = lease
	var acquired bool
	if err = lease.QueryRowContext(a.ctx, `SELECT pg_try_advisory_lock(`+chatControllerKey+`)`).Scan(&acquired); err != nil || !acquired {
		return errors.New("another chat controller is active")
	}
	var mode string
	err = a.store.Product.QueryRowContext(a.ctx, `SELECT value FROM app_metadata WHERE /*tenant:app_metadata*/ AND key='chat_dispatch_mode'`).Scan(&mode)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return errors.New("chat dispatch mode unavailable")
	}
	if !retained && mode == "retained" {
		return errors.New("retained chat mode guard blocks legacy ingestion")
	}
	if retained {
		if _, err = a.store.Product.ExecContext(a.ctx, `INSERT INTO app_metadata(key,value) VALUES('chat_dispatch_mode','retained') ON CONFLICT(tenant_id,key) DO UPDATE SET value=excluded.value,updated_at=now()`); err != nil {
			return errors.New("chat dispatch mode unavailable")
		}
	}
	a.chatControl.live.Store(true)
	if retained {
		if err = a.chatWorker.Start(a.ctx); err != nil {
			a.chatControl.live.Store(false)
			return errors.New("retained chat worker could not start")
		}
		a.chatWorkerStarted = true
	}
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-a.ctx.Done():
				return
			case <-ticker.C:
				ctx, stop := context.WithTimeout(a.ctx, time.Second)
				err := lease.PingContext(ctx)
				stop()
				if err != nil {
					a.chatControl.live.Store(false)
					a.cancel()
					return
				}
			}
		}
	}()
	return nil
}

func (a *Application) chatReady() bool {
	return a.chatControl != nil && a.chatControl.live.Load() && a.ctx.Err() == nil
}

func (a *Application) handleRetainedChat(ctx context.Context, event platform.ChatEvent) error {
	if !a.chatReady() || a.config.DeploymentMode == "hosted" && event.TenantID == tenantdb.BootstrapID {
		return errors.New("chat controller unavailable")
	}
	if err := a.chatControl.lease.PingContext(ctx); err != nil {
		a.chatControl.live.Store(false)
		a.cancel()
		return errors.New("chat controller unavailable")
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
	rules := s.interactions.Rules()
	for _, out := range twitch.EventsForNormalizedChat(event, s.twitchCommandProcessor, rules) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.publishEventSubContext(ctx, out, true); err != nil {
			return err
		}
	}
	return nil
}
