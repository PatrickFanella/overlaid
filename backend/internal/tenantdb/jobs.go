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
