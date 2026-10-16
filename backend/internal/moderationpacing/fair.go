package moderationpacing

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/permitturns"
)

type FairRequest = permitturns.Request

var ErrFairRequired = permitturns.ErrRequired
var ErrFairState = permitturns.ErrState

func clientHash(client string) string {
	b, _ := json.Marshal([]string{"twitch", "moderation", "fair-client", client})
	return digest(b)
}
func fairError(err error) error {
	if errors.Is(err, permitturns.ErrInvalid) {
		return ErrInvalid
	}
	if errors.Is(err, permitturns.ErrCapacity) {
		return ErrCapacity
	}
	return err
}
func admitFair(ctx context.Context, tx *sql.Tx, client, resource string, request *FairRequest, ready, now time.Time) (string, bool, time.Time, error) {
	var raw []byte
	if err := tx.QueryRowContext(ctx, `SELECT coalesce((SELECT fair_state FROM moderation_pacing_buckets WHERE kind='client' AND key=$1),'{}'::jsonb)`, client).Scan(&raw); err != nil {
		return "", false, time.Time{}, err
	}
	state, err := permitturns.Decode(raw)
	if err != nil {
		return "", false, time.Time{}, err
	}
	if request == nil {
		if state.Version != 0 {
			return "", false, time.Time{}, ErrFairRequired
		}
		return "", !ready.After(now), ready, nil
	}
	action, err := state.Register(now, *request, resource)
	if err != nil {
		return "", false, time.Time{}, fairError(err)
	}
	// A single turn list per client avoids cycles from incompatible winners in
	// separate sender/channel lists. Both resource hashes must still be eligible.
	senders := []string{}
	channels := []string{}
	for _, w := range state.Waiters {
		sender, channel, ok := strings.Cut(w.Channel, ":")
		if !ok {
			return "", false, time.Time{}, ErrFairState
		}
		senders = append(senders, sender)
		channels = append(channels, channel)
	}
	senderJSON, _ := json.Marshal(senders)
	channelJSON, _ := json.Marshal(channels)
	rows, err := tx.QueryContext(ctx, `SELECT kind,key,greatest(next_at,lease_until,'0001-01-01 UTC'::timestamptz) FROM moderation_pacing_buckets WHERE (kind='sender' AND key IN(SELECT jsonb_array_elements_text($1::jsonb))) OR (kind='channel' AND key IN(SELECT jsonb_array_elements_text($2::jsonb)))`, senderJSON, channelJSON)
	if err != nil {
		return "", false, time.Time{}, err
	}
	resources := map[string]time.Time{}
	for rows.Next() {
		var kind, key string
		var at time.Time
		if err = rows.Scan(&kind, &key, &at); err != nil {
			break
		}
		resources[kind+":"+key] = at
	}
	rowsErr := rows.Err()
	closeErr := rows.Close()
	if err != nil || rowsErr != nil || closeErr != nil {
		return "", false, time.Time{}, errors.Join(err, rowsErr, closeErr)
	}
	eligibleResources := map[string]time.Time{}
	for i, w := range state.Waiters {
		a, hasSender := resources["sender:"+senders[i]]
		b, hasChannel := resources["channel:"+channels[i]]
		if !hasSender || !hasChannel {
			continue
		}
		if b.After(a) {
			a = b
		}
		eligibleResources[w.Channel] = a
	}
	eligible := !ready.After(now) && state.Winner(now, eligibleResources) == action
	if eligible {
		if err = state.Grant(now, action); err != nil {
			return "", false, time.Time{}, err
		}
	}
	raw, err = json.Marshal(state)
	if err != nil {
		return "", false, time.Time{}, ErrFairState
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO moderation_pacing_buckets(kind,key,fair_state,updated_at) VALUES('client',$1,$2,$3) ON CONFLICT(kind,key) DO UPDATE SET fair_state=excluded.fair_state,updated_at=excluded.updated_at`, client, raw, now); err != nil {
		return "", false, time.Time{}, err
	}
	retry := now.Add(time.Second)
	if ready.After(now) && ready.Before(retry) {
		retry = ready
	}
	if request.Deadline.Before(retry) {
		retry = request.Deadline
	}
	return action, eligible, retry, nil
}
