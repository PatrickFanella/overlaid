package httpapi

import (
	"context"
	"database/sql"
	"errors"
)

const retainedStreamMode = "retained-v1"

func (a *Application) checkStreamStartupMode(ctx context.Context) error {
	var mode string
	err := a.store.Product.QueryRowContext(ctx, `SELECT value FROM app_metadata WHERE /*tenant:app_metadata*/ AND key='stream_dispatch_mode'`).Scan(&mode)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return errRetainedStream
	}
	if err == nil && (mode != retainedStreamMode || a.config.TwitchStreamDispatchMode != "retained" || !a.streamModeCurrent(ctx)) {
		return errRetainedStream
	}
	return nil
}

func (a *Application) streamModeCurrent(ctx context.Context) bool {
	if a.config.TwitchStreamDispatchMode != "retained" {
		return true
	}
	var valid bool
	err := a.store.Product.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM app_metadata WHERE /*tenant:app_metadata*/ AND key='stream_dispatch_mode' AND value=$1) AND EXISTS(SELECT 1 FROM app_metadata WHERE /*tenant:app_metadata*/ AND key='chat_dispatch_mode' AND value='retained')`, retainedStreamMode).Scan(&valid)
	return err == nil && valid
}

// Persist only after resources validate, under the actual controller. The
// trigger also fences pre-cutover binaries that overwrite the chat marker.
func (a *Application) persistStreamStartupMode(ctx context.Context) error {
	if a.config.TwitchStreamDispatchMode != "retained" || a.checkChatController(ctx) != nil || a.store.Product.ValidateStreamMode(ctx) != nil {
		return errRetainedStream
	}
	tx, err := a.store.Product.BeginTx(ctx, nil)
	if err != nil {
		return errRetainedStream
	}
	defer func() { _ = tx.Rollback() }()
	var mode string
	if err := tx.QueryRowContext(ctx, `SELECT value FROM app_metadata WHERE /*tenant:app_metadata*/ AND key='chat_dispatch_mode' FOR UPDATE`).Scan(&mode); err != nil || mode != "retained" {
		return errRetainedStream
	}
	err = tx.QueryRowContext(ctx, `SELECT value FROM app_metadata WHERE /*tenant:app_metadata*/ AND key='stream_dispatch_mode' FOR UPDATE`).Scan(&mode)
	if err != nil && !errors.Is(err, sql.ErrNoRows) || err == nil && mode != retainedStreamMode {
		return errRetainedStream
	}
	if _, err := tx.ExecContext(ctx, `SELECT set_config('app.stream_dispatch_mode','retained-v1',true)`); err != nil {
		return errRetainedStream
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO app_metadata(key,value) VALUES('stream_dispatch_mode',$1) ON CONFLICT(tenant_id,key) DO NOTHING`, retainedStreamMode); err != nil {
		return errRetainedStream
	}
	if err := tx.Commit(); err != nil {
		return errRetainedStream
	}
	return nil
}

func (a *Application) streamLeasingReady() bool {
	if a.config.TwitchStreamDispatchMode != "retained" {
		return true
	}
	select {
	case <-a.streamStarted:
		return true
	default:
		return false
	}
}
