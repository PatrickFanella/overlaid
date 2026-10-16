package permitturns

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

func fairRequest(tenant, action int, deadline time.Time) Request {
	return Request{TenantID: fmt.Sprintf("%08x-0000-0000-0000-000000000001", tenant), ActionID: fmt.Sprintf("%064x", action), Deadline: deadline}
}
func TestFairTurnsNoisyTenantBlockedChannelAndExpiry(t *testing.T) {
	now := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	var s State
	channels := map[string]time.Time{}
	actions := map[string]int{}
	for _, tenant := range []int{1, 2, 3} {
		for i := range 4 {
			channel := fmt.Sprintf("%064x", tenant*10+i)
			channels[channel] = now
			id, err := s.Register(now, fairRequest(tenant, tenant*10+i, now.Add(time.Minute)), channel)
			if err != nil {
				t.Fatal(err)
			}
			actions[id] = tenant
		}
	}
	var got []int
	for range 12 {
		id := s.Winner(now, channels)
		got = append(got, actions[id])
		if err := s.Grant(now, id); err != nil {
			t.Fatal(err)
		}
	}
	if fmt.Sprint(got) != "[1 2 3 1 2 3 1 2 3 1 2 3]" {
		t.Fatal("unbalanced turns", got)
	}
	// Granted actions remain tombstones; they cannot steal a second slot.
	if s.Winner(now, channels) != "" {
		t.Fatal("granted action selected twice")
	}
	raw, _ := json.Marshal(s)
	reloaded, err := Decode(raw)
	if err != nil || reloaded.Counter != s.Counter {
		t.Fatal("restart", err)
	}
	s.Prune(now.Add(3 * time.Minute))
	if len(s.Waiters) != 0 || len(s.Tenants) != 0 {
		t.Fatal("expiry did not bound history")
	}
	for _, tenant := range []int{1, 2} {
		channel := fmt.Sprintf("%064x", tenant)
		channels[channel] = now
		_, err = s.Register(now, fairRequest(tenant, tenant, now.Add(time.Minute)), channel)
		if err != nil {
			t.Fatal(err)
		}
	}
	channels[fmt.Sprintf("%064x", 1)] = now.Add(time.Second)
	id := s.Winner(now, channels)
	_, want := Keys(fairRequest(2, 2, now.Add(time.Minute)))
	if id != want {
		t.Fatal("blocked channel stalled eligible tenant")
	}
	// Wall-clock reversal does not rewrite durable tenant turns.
	if s.Winner(now.Add(-time.Second), channels) != "" {
		t.Fatal("future channel became ready on reversed clock")
	}
}
func TestFairIdentityBoundsAndMalformedState(t *testing.T) {
	now := time.Now().UTC()
	channel := strings.Repeat("1", 64)
	var s State
	r := fairRequest(1, 1, now.Add(time.Minute))
	id, err := s.Register(now, r, channel)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := s.Register(now, r, channel)
	if err != nil || duplicate != id || len(s.Waiters) != 1 {
		t.Fatal("duplicate changed ticket", err)
	}
	changed := r
	changed.Deadline = changed.Deadline.Add(time.Second)
	if _, err = s.Register(now, changed, channel); !errors.Is(err, ErrInvalid) {
		t.Fatal("deadline extended", err)
	}
	if _, err = s.Register(now, r, strings.Repeat("2", 64)); !errors.Is(err, ErrInvalid) {
		t.Fatal("channel changed", err)
	}
	for _, bad := range []Request{{}, fairRequest(1, 2, now), fairRequest(1, 2, now.Add(3*time.Minute))} {
		if _, err = s.Register(now, bad, channel); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid request", err)
		}
	}
	for i := 2; i <= WaitLimit; i++ {
		if _, err = s.Register(now, fairRequest(1, i, r.Deadline), channel); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.Register(now, fairRequest(1, 999, r.Deadline), channel); !errors.Is(err, ErrCapacity) {
		t.Fatal("wait capacity", err)
	}
	raw, _ := json.Marshal(s)
	if len(raw) > 131072 {
		t.Fatal("bounded state exceeds database envelope")
	}
	s = State{}
	for i := 1; i <= TenantLimit; i++ {
		if _, err = s.Register(now, fairRequest(i, i, r.Deadline), channel); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.Register(now, fairRequest(999, 999, r.Deadline), channel); !errors.Is(err, ErrCapacity) {
		t.Fatal("tenant capacity", err)
	}
	s.Counter = math.MaxInt64
	if _, err = s.tick(); !errors.Is(err, ErrState) {
		t.Fatal("counter wrapped", err)
	}
	for _, raw := range []string{`null`, `[]`, `{"version":2}`, `{"version":1,"counter":-1}`, `{"version":1,"extra":true}`, `{} {}`, strings.Repeat("x", 131073)} {
		if _, err := Decode([]byte(raw)); !errors.Is(err, ErrState) {
			t.Fatal("malformed state accepted", err)
		}
	}
}

func TestResourcePairsAndLegacyStateCompatibility(t *testing.T) {
	now := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	var s State
	r := fairRequest(1, 1, now.Add(time.Minute))
	channel := strings.Repeat("1", 64)
	id, err := s.Register(now, r, channel)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(s)
	// Existing chat version-one records retain the original channel field and
	// context-separated hash keys; extraction must not rename or salt them.
	var legacy struct {
		Version int `json:"version"`
		Waiters []struct {
			Channel, Action, Tenant string
			Deadline                time.Time
		}
	}
	if err = json.Unmarshal(raw, &legacy); err != nil || legacy.Version != 1 || len(legacy.Waiters) != 1 || legacy.Waiters[0].Channel != channel || legacy.Waiters[0].Action != id {
		t.Fatal("legacy record changed", err)
	}
	restored, err := Decode(raw)
	if err != nil || restored.Winner(now, map[string]time.Time{channel: now}) != id {
		t.Fatal("legacy readiness changed", err)
	}
	s = State{}
	for i := 1; i <= TenantLimit; i++ {
		pair := fmt.Sprintf("%064x:%064x", i, i)
		if _, err = s.Register(now, fairRequest(i, i, now.Add(time.Minute)), pair); err != nil {
			t.Fatal(err)
		}
	}
	for i := TenantLimit + 1; i <= WaitLimit; i++ {
		pair := fmt.Sprintf("%064x:%064x", i, i)
		if _, err = s.Register(now, fairRequest(1, i, now.Add(time.Minute)), pair); err != nil {
			t.Fatal(err)
		}
	}
	raw, _ = json.Marshal(s)
	if len(raw) > 262144 {
		t.Fatal("full paired state exceeds database bound")
	}
	if _, err = Decode(raw); err != nil {
		t.Fatal("full paired state rejected", err)
	}
	for _, bad := range []string{"", channel + ":bad", channel + ":" + channel + ":" + channel} {
		if _, err = s.Register(now, fairRequest(1, 999, now.Add(time.Minute)), bad); !errors.Is(err, ErrInvalid) {
			t.Fatal("bad resource accepted", err)
		}
	}
}
