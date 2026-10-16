// Package chatpacing supplies a separate durable sender budget. It never
// authorizes effects, reads product/vault data or sends provider requests.
package chatpacing

import (
	"errors"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/permitturns"
)

const accountLimit = 20
const accountWindow = 30 * time.Second
const channelInterval = time.Second
const leaseDuration = 30 * time.Second

var ErrInvalid = permitturns.ErrInvalid
var ErrFence = errors.New("pacing completion fence rejected")
var ErrCapacity = permitturns.ErrCapacity

type accountState struct {
	Attempts                 []int64
	BlockedUntil, LeaseUntil time.Time
}

func (a *accountState) readyAt(now, channelNext time.Time) time.Time {
	kept := a.Attempts[:0]
	for _, at := range a.Attempts {
		if at > now.Add(-accountWindow).UnixMicro() {
			kept = append(kept, at)
		}
	}
	a.Attempts = kept
	ready := now
	for _, at := range []time.Time{channelNext, a.BlockedUntil, a.LeaseUntil} {
		if at.After(ready) {
			ready = at
		}
	}
	if len(kept) > 0 && time.UnixMicro(kept[len(kept)-1]).After(now) {
		// A backwards database clock cannot reopen a prior window.
		if at := time.UnixMicro(kept[len(kept)-1]).Add(accountWindow); at.After(ready) {
			ready = at
		}
	}
	if len(kept) >= accountLimit {
		if at := time.UnixMicro(kept[0]).Add(accountWindow); at.After(ready) {
			ready = at
		}
	}
	return ready
}

func (a *accountState) begin(now time.Time) {
	a.Attempts = append(a.Attempts, now.UnixMicro())
	a.LeaseUntil = now.Add(leaseDuration)
}

func (a *accountState) complete(now time.Time, retryAfter time.Duration) {
	// Charge at completion as well as start. Slow sends cannot release a
	// budget early, and unsuccessful attempts never refund a slot.
	if n := len(a.Attempts); n > 0 && now.UnixMicro() > a.Attempts[n-1] {
		a.Attempts[n-1] = now.UnixMicro()
	}
	if at := now.Add(retryAfter); at.After(a.BlockedUntil) {
		a.BlockedUntil = at
	}
	a.LeaseUntil = time.Time{}
}
