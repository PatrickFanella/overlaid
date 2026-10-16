package tenantdb

import (
	"context"
	"errors"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"regexp"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
)

var eventIDPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// EnqueueChatDispatch bridges the private transaction to River without giving
// callers a pool, a raw transaction or control over tenant/queue/job arguments.
// It does not commit: the owning store commits event + native payload + job.
func (t *Tx) EnqueueChatDispatch(ctx context.Context, connectionID, eventID string) (int64, error) {
	if !t.db.IsScoped() || connectionID == "" || len(connectionID) > 256 || !eventIDPattern.MatchString(eventID) {
		return 0, errors.New("scoped chat dispatch identity required")
	}
	var occurred time.Time
	if err := t.QueryRowContext(ctx, `SELECT occurred_at FROM normalized_chat_events WHERE /*tenant:normalized_chat_events*/ AND id=$1 AND connection_id=$2`, eventID, connectionID).Scan(&occurred); err != nil {
		return 0, err
	}
	var schema string
	if err := t.raw.QueryRowContext(ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		return 0, err
	}
	client, err := river.NewClient(riverdatabasesql.New(nil), &river.Config{Schema: schema})
	if err != nil {
		return 0, err
	}
	result, err := client.InsertTx(ctx, t.raw, platform.ChatDispatchArgs{TenantID: t.db.tenant, ConnectionID: connectionID, EventID: eventID, Deadline: occurred.UTC().Add(2 * time.Minute).Unix()}, &river.InsertOpts{Queue: "ingest_followup", MaxAttempts: 3})
	if err != nil {
		return 0, err
	}
	return result.Job.ID, nil
}

// EnqueueChatSend commits with the owning ledger transaction. Callers cannot
// choose a tenant, queue, priority, deadline or private message job argument.
func (t *Tx) EnqueueChatSend(ctx context.Context, actionID string) (int64, error) {
	if !t.db.IsScoped() || !eventIDPattern.MatchString(actionID) {
		return 0, errors.New("scoped outbound action identity required")
	}
	var deadline time.Time
	if err := t.QueryRowContext(ctx, `SELECT deadline FROM platform_outbound_actions WHERE /*tenant:platform_outbound_actions*/ AND id=$1 AND state='pending' AND deadline>now()`, actionID).Scan(&deadline); err != nil {
		return 0, err
	}
	var schema string
	if err := t.raw.QueryRowContext(ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		return 0, err
	}
	client, err := river.NewClient(riverdatabasesql.New(nil), &river.Config{Schema: schema})
	if err != nil {
		return 0, err
	}
	result, err := client.InsertTx(ctx, t.raw, platform.ChatSendArgs{TenantID: t.db.tenant, ActionID: actionID, Deadline: deadline.UTC().Unix()}, &river.InsertOpts{Queue: "chat_outbound", MaxAttempts: 3, Priority: 4})
	if err != nil {
		return 0, err
	}
	return result.Job.ID, nil
}

// EnqueueModeration commits with the owning ledger transaction. Callers cannot
// choose a tenant, queue, priority, deadline or private moderation job argument.
func (t *Tx) EnqueueModeration(ctx context.Context, actionID string) (int64, error) {
	if !t.db.IsScoped() || !eventIDPattern.MatchString(actionID) {
		return 0, errors.New("scoped moderation action identity required")
	}
	var deadline time.Time
	if err := t.QueryRowContext(ctx, `SELECT deadline FROM platform_moderation_actions WHERE /*tenant:platform_moderation_actions*/ AND id=$1 AND state='pending' AND deadline>clock_timestamp() FOR SHARE`, actionID).Scan(&deadline); err != nil {
		return 0, err
	}
	var schema string
	if err := t.raw.QueryRowContext(ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		return 0, err
	}
	client, err := river.NewClient(riverdatabasesql.New(nil), &river.Config{Schema: schema})
	if err != nil {
		return 0, err
	}
	result, err := client.InsertTx(ctx, t.raw, platform.ModerationArgs{TenantID: t.db.tenant, ActionID: actionID, Deadline: deadline.UTC().Unix()}, &river.InsertOpts{Queue: "moderation_outbound", MaxAttempts: 3, Priority: 4})
	if err != nil {
		return 0, err
	}
	return result.Job.ID, nil
}
