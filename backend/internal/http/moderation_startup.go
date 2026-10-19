package httpapi

import (
	"context"
	"database/sql"
	"errors"
)

const retainedModerationMode = "retained-v1"
const moderationSenderMode = "retained-v2-moderation"

// A separate atomic barrier avoids reading the owner pointer from River leasing
// callbacks while preparation installs it under the application lock.
func (a *Application) moderationLeasingReady() bool {
	return a.config.TwitchModerationMode != "retained" || a.moderationStarted.Load()
}

func (a *Application) checkModerationStartupMode(ctx context.Context, sender string) error {
	var mode string
	err := a.store.Product.QueryRowContext(ctx, `SELECT value FROM app_metadata WHERE /*tenant:app_metadata*/ AND key='moderation_mode'`).Scan(&mode)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return errors.New("moderation mode unavailable")
	}
	retained := a.config.TwitchModerationMode == "retained"
	if err == nil && (mode != retainedModerationMode || !retained || sender != moderationSenderMode) {
		return errors.New("retained moderation guard blocks incompatible startup")
	}
	if sender == moderationSenderMode && (!retained || mode != retainedModerationMode) {
		return errors.New("retained moderation guard blocks incompatible startup")
	}
	if sender != "" && (a.config.TwitchChatSendMode != "retained" || sender != "retained-v1" && sender != moderationSenderMode) {
		return errors.New("retained chat sender guard blocks incompatible startup")
	}
	return nil
}

// Called only under the actual installation controller lock after construction
// validation, before moderation starts or the shared leasing barrier opens.
// The sender version also makes binaries predating moderation opt-in refuse
// startup through their existing exact retained-v1 check.
func (a *Application) persistModerationStartupMode(ctx context.Context) error {
	if a.checkChatController(ctx) != nil || a.config.TwitchModerationMode != "retained" {
		return errModerationController
	}
	tx, err := a.store.Product.BeginTx(ctx, nil)
	if err != nil {
		return errModerationController
	}
	defer func() { _ = tx.Rollback() }()
	var sender string
	if err := tx.QueryRowContext(ctx, `SELECT value FROM app_metadata WHERE /*tenant:app_metadata*/ AND key='chat_send_mode' FOR UPDATE`).Scan(&sender); err != nil || sender != "retained-v1" && sender != moderationSenderMode {
		return errModerationController
	}
	var mode string
	err = tx.QueryRowContext(ctx, `SELECT value FROM app_metadata WHERE /*tenant:app_metadata*/ AND key='moderation_mode' FOR UPDATE`).Scan(&mode)
	if err != nil && !errors.Is(err, sql.ErrNoRows) || err == nil && mode != retainedModerationMode {
		return errModerationController
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO app_metadata(key,value) VALUES('moderation_mode',$1) ON CONFLICT(tenant_id,key) DO NOTHING`, retainedModerationMode); err != nil {
		return errModerationController
	}
	if _, err := tx.ExecContext(ctx, `UPDATE app_metadata SET value=$1,updated_at=now() WHERE /*tenant:app_metadata*/ AND key='chat_send_mode'`, moderationSenderMode); err != nil {
		return errModerationController
	}
	if err := tx.Commit(); err != nil {
		return errModerationController
	}
	return nil
}
