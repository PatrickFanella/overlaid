package tenantdb

import (
	"context"
	"errors"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
)

// EnqueueStreamDispatch shares only the owning admission transaction. Callers
// cannot select a tenant, private job data, queue or replacement deadline.
func (t *Tx) EnqueueStreamDispatch(ctx context.Context, connectionID, eventID string) (int64, error) {
	if !t.db.IsScoped() || connectionID == "" || len(connectionID) > 256 || !eventIDPattern.MatchString(eventID) {
		return 0, errors.New("scoped stream dispatch identity required")
	}
	var deadline int64
	if err := t.QueryRowContext(ctx, `SELECT dispatch_deadline FROM normalized_stream_events WHERE /*tenant:normalized_stream_events*/ AND id=$1 AND connection_id=$2`, eventID, connectionID).Scan(&deadline); err != nil {
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
	result, err := client.InsertTx(ctx, t.raw, platform.StreamDispatchArgs{TenantID: t.db.tenant, ConnectionID: connectionID, EventID: eventID, Deadline: deadline}, &river.InsertOpts{Queue: "stream_followup", MaxAttempts: 3, Priority: 4})
	if err != nil {
		return 0, err
	}
	return result.Job.ID, nil
}
