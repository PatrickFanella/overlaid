package chatpacing

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

func fairRequest(tenant, action int, deadline time.Time) FairRequest {
	return FairRequest{TenantID: fmt.Sprintf("%08x-0000-0000-0000-000000000001", tenant), ActionID: fmt.Sprintf("%064x", action), Deadline: deadline}
}
func TestFairTurnsNoisyTenantBlockedChannelAndExpiry(t *testing.T) {
	now := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	var s fairState
	channels := map[string]time.Time{}
	actions := map[string]int{}
	for _, tenant := range []int{1, 2, 3} {
		for i := range 4 {
			channel := fmt.Sprintf("%064x", tenant*10+i)
			channels[channel] = now
			id, err := s.register(now, fairRequest(tenant, tenant*10+i, now.Add(time.Minute)), channel)
			if err != nil {
				t.Fatal(err)
			}
			actions[id] = tenant
		}
	}
	var got []int
	for range 12 {
		id := s.winner(now, channels)
		got = append(got, actions[id])
		if err := s.grant(now, id); err != nil {
			t.Fatal(err)
		}
	}
	if fmt.Sprint(got) != "[1 2 3 1 2 3 1 2 3 1 2 3]" {
		t.Fatal("unbalanced turns", got)
	}
	// Granted actions remain tombstones; they cannot steal a second slot.
	if s.winner(now, channels) != "" {
		t.Fatal("granted action selected twice")
	}
	raw, _ := json.Marshal(s)
	reloaded, err := decodeFair(raw)
	if err != nil || reloaded.Counter != s.Counter {
		t.Fatal("restart", err)
	}
	s.prune(now.Add(3 * time.Minute))
	if len(s.Waiters) != 0 || len(s.Tenants) != 0 {
		t.Fatal("expiry did not bound history")
	}
	for _, tenant := range []int{1, 2} {
		channel := fmt.Sprintf("%064x", tenant)
		channels[channel] = now
		_, err = s.register(now, fairRequest(tenant, tenant, now.Add(time.Minute)), channel)
		if err != nil {
			t.Fatal(err)
		}
	}
	channels[fmt.Sprintf("%064x", 1)] = now.Add(time.Second)
	id := s.winner(now, channels)
	_, want := fairKeys(fairRequest(2, 2, now.Add(time.Minute)))
	if id != want {
		t.Fatal("blocked channel stalled eligible tenant")
	}
	// Wall-clock reversal does not rewrite durable tenant turns.
	if s.winner(now.Add(-time.Second), channels) != "" {
		t.Fatal("future channel became ready on reversed clock")
	}
}
func TestFairIdentityBoundsAndMalformedState(t *testing.T) {
	now := time.Now().UTC()
	channel := strings.Repeat("1", 64)
	var s fairState
	r := fairRequest(1, 1, now.Add(time.Minute))
	id, err := s.register(now, r, channel)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := s.register(now, r, channel)
	if err != nil || duplicate != id || len(s.Waiters) != 1 {
		t.Fatal("duplicate changed ticket", err)
	}
	changed := r
	changed.Deadline = changed.Deadline.Add(time.Second)
	if _, err = s.register(now, changed, channel); !errors.Is(err, ErrInvalid) {
		t.Fatal("deadline extended", err)
	}
	if _, err = s.register(now, r, strings.Repeat("2", 64)); !errors.Is(err, ErrInvalid) {
		t.Fatal("channel changed", err)
	}
	for _, bad := range []FairRequest{{}, fairRequest(1, 2, now), fairRequest(1, 2, now.Add(3*time.Minute))} {
		if _, err = s.register(now, bad, channel); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid request", err)
		}
	}
	for i := 2; i <= fairWaitLimit; i++ {
		if _, err = s.register(now, fairRequest(1, i, r.Deadline), channel); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.register(now, fairRequest(1, 999, r.Deadline), channel); !errors.Is(err, ErrCapacity) {
		t.Fatal("wait capacity", err)
	}
	raw, _ := json.Marshal(s)
	if len(raw) > 131072 {
		t.Fatal("bounded state exceeds database envelope")
	}
	s = fairState{}
	for i := 1; i <= fairTenantLimit; i++ {
		if _, err = s.register(now, fairRequest(i, i, r.Deadline), channel); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.register(now, fairRequest(999, 999, r.Deadline), channel); !errors.Is(err, ErrCapacity) {
		t.Fatal("tenant capacity", err)
	}
	s.Counter = math.MaxInt64
	if _, err = s.tick(); !errors.Is(err, ErrFairState) {
		t.Fatal("counter wrapped", err)
	}
	for _, raw := range []string{`null`, `[]`, `{"version":2}`, `{"version":1,"counter":-1}`, `{"version":1,"extra":true}`, `{} {}`, strings.Repeat("x", 131073)} {
		if _, err := decodeFair([]byte(raw)); !errors.Is(err, ErrFairState) {
			t.Fatal("malformed state accepted", err)
		}
	}
}
