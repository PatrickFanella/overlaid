package moderationpacing

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/permitturns"
)

// CheckState validates retained formats and capacity without mutation. Expired
// entries remain intact; only normal admission may prune eligible state.
// Callers separately validate privileges and product database identity.
func (s *Store) CheckState(ctx context.Context) error {
	if s == nil || s.db == nil {
		return ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// One query observes a consistent snapshot and at most one over capacity.
	rows, err := tx.QueryContext(ctx, `SELECT kind,fair_state FROM moderation_pacing_buckets LIMIT 12049`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	var senders, channels, clients int
	for rows.Next() {
		var kind string
		var raw []byte
		if err := rows.Scan(&kind, &raw); err != nil {
			return err
		}
		state, err := permitturns.Decode(raw)
		if err != nil {
			return err
		}
		switch kind {
		case "sender":
			senders++
		case "channel":
			channels++
		case "client":
			clients++
		default:
			return ErrFairState
		}
		if senders > 1024 || channels > 10000 || clients > 1024 {
			return ErrCapacity
		}
		if kind != "client" && state.Version != 0 {
			return ErrFairState
		}
		for _, waiter := range state.Waiters {
			// The shared codec also supports a single chat resource hash.
			// Moderation requires its sender:channel pair, in both versions.
			if !strings.Contains(waiter.Channel, ":") {
				return ErrFairState
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	return tx.Commit()
}
