package platformdispatch

import (
	"context"
	"errors"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"time"
)

type Receipt struct {
	EventID      string     `json:"eventId"`
	ConnectionID string     `json:"connectionId"`
	State        string     `json:"state"`
	Code         string     `json:"code"`
	StartedAt    time.Time  `json:"startedAt"`
	FinishedAt   *time.Time `json:"finishedAt,omitempty"`
}

func Receipts(ctx context.Context, db *tenantdb.DB, limit int) ([]Receipt, error) {
	if db == nil || !db.IsScoped() || limit < 1 || limit > 100 {
		return nil, errors.New("scoped receipt limit must be 1 to 100")
	}
	rows, err := db.QueryContext(ctx, `SELECT event_id,connection_id,state,code,started_at,finished_at FROM platform_dispatch_receipts WHERE /*tenant:platform_dispatch_receipts*/ ORDER BY started_at DESC,event_id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Receipt{}
	for rows.Next() {
		var r Receipt
		if err := rows.Scan(&r.EventID, &r.ConnectionID, &r.State, &r.Code, &r.StartedAt, &r.FinishedAt); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}
