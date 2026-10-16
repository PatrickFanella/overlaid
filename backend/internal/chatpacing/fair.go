package chatpacing

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"regexp"
	"time"
)

// FairRequest identifies trusted retained work, never a browser-selected grant.
// Its deadline is immutable and may be at most two minutes ahead of DB time.
type FairRequest struct {
	TenantID, ActionID string
	Deadline           time.Time
}

var ErrFairRequired = errors.New("fair pacing admission required")
var ErrFairState = errors.New("invalid fair pacing state")
var canonicalTenant = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

const fairWaitLimit = 256
const fairTenantLimit = 128
const fairHorizon = 2 * time.Minute

type fairTenant struct {
	Key   string    `json:"key"`
	Turn  int64     `json:"turn"`
	Until time.Time `json:"until"`
}
type fairWait struct {
	Action   string    `json:"action"`
	Tenant   string    `json:"tenant"`
	Channel  string    `json:"channel"`
	Deadline time.Time `json:"deadline"`
	Ticket   int64     `json:"ticket"`
	Granted  bool      `json:"granted"`
}
type fairState struct {
	Version int          `json:"version"`
	Counter int64        `json:"counter"`
	Tenants []fairTenant `json:"tenants"`
	Waiters []fairWait   `json:"waiters"`
}

func hashID(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
func (r FairRequest) valid() bool {
	return canonicalTenant.MatchString(r.TenantID) && hashID(r.ActionID) && !r.Deadline.IsZero()
}
func fairKeys(r FairRequest) (string, string) {
	tenant, _ := json.Marshal([]string{"twitch", "pacing-tenant", r.TenantID})
	action, _ := json.Marshal([]string{"twitch", "pacing-action", r.TenantID, r.ActionID})
	return digest(tenant), digest(action)
}
func decodeFair(raw []byte) (fairState, error) {
	var s fairState
	if len(raw) > 131072 {
		return s, ErrFairState
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("{}")) {
		return s, nil
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&s) != nil || d.Decode(new(any)) != io.EOF || s.Version != 1 || s.Counter < 0 || len(s.Tenants) > fairTenantLimit || len(s.Waiters) > fairWaitLimit {
		return fairState{}, ErrFairState
	}
	tenants := map[string]bool{}
	for _, t := range s.Tenants {
		if !hashID(t.Key) || tenants[t.Key] || t.Turn < 0 || t.Turn > s.Counter || t.Until.IsZero() {
			return fairState{}, ErrFairState
		}
		tenants[t.Key] = true
	}
	actions := map[string]bool{}
	tickets := map[int64]bool{}
	for _, w := range s.Waiters {
		if !hashID(w.Action) || !hashID(w.Channel) || !tenants[w.Tenant] || actions[w.Action] || w.Ticket <= 0 || w.Ticket > s.Counter || tickets[w.Ticket] || w.Deadline.IsZero() {
			return fairState{}, ErrFairState
		}
		actions[w.Action] = true
		tickets[w.Ticket] = true
	}
	return s, nil
}
func (s *fairState) prune(now time.Time) {
	waits := s.Waiters[:0]
	active := map[string]bool{}
	for _, w := range s.Waiters {
		if w.Deadline.After(now) {
			waits = append(waits, w)
			active[w.Tenant] = true
		}
	}
	s.Waiters = waits
	tenants := s.Tenants[:0]
	for _, t := range s.Tenants {
		if t.Until.After(now) || active[t.Key] {
			tenants = append(tenants, t)
		}
	}
	s.Tenants = tenants
}
func (s *fairState) tick() (int64, error) {
	if s.Counter == math.MaxInt64 {
		return 0, ErrFairState
	}
	s.Counter++
	return s.Counter, nil
}
func (s *fairState) register(now time.Time, r FairRequest, channel string) (string, error) {
	if !r.valid() || !r.Deadline.After(now) || r.Deadline.After(now.Add(fairHorizon)) {
		return "", ErrInvalid
	}
	tenant, action := fairKeys(r)
	// Compare existing identity before pruning: an expired original request cannot
	// be renewed at the boundary while its old row is still retained.
	for _, w := range s.Waiters {
		if w.Action == action {
			if w.Tenant != tenant || w.Channel != channel || !w.Deadline.Equal(r.Deadline) {
				return "", ErrInvalid
			}
			s.prune(now)
			return action, nil
		}
	}
	s.prune(now)
	if len(s.Waiters) >= fairWaitLimit {
		return "", ErrCapacity
	}
	found := false
	for _, t := range s.Tenants {
		if t.Key == tenant {
			found = true
		}
	}
	if !found {
		if len(s.Tenants) >= fairTenantLimit {
			return "", ErrCapacity
		}
		s.Tenants = append(s.Tenants, fairTenant{Key: tenant, Turn: s.Counter, Until: now.Add(fairHorizon)})
	}
	ticket, err := s.tick()
	if err != nil {
		return "", err
	}
	s.Version = 1
	s.Waiters = append(s.Waiters, fairWait{Action: action, Tenant: tenant, Channel: channel, Deadline: r.Deadline.UTC(), Ticket: ticket})
	return action, nil
}

// winner considers ready channels only. Within a tenant the earliest eligible
// ticket wins; tenants take turns by their last grant, independent of clocks.
func (s *fairState) winner(now time.Time, channels map[string]time.Time) string {
	turns := map[string]int64{}
	for _, t := range s.Tenants {
		turns[t.Key] = t.Turn
	}
	var best *fairWait
	for i := range s.Waiters {
		w := &s.Waiters[i]
		next, ok := channels[w.Channel]
		if w.Granted || !w.Deadline.After(now) || !ok || next.After(now) {
			continue
		}
		if best == nil || turns[w.Tenant] < turns[best.Tenant] || turns[w.Tenant] == turns[best.Tenant] && w.Ticket < best.Ticket {
			best = w
		}
	}
	if best == nil {
		return ""
	}
	return best.Action
}
func (s *fairState) grant(now time.Time, action string) error {
	turn, err := s.tick()
	if err != nil {
		return err
	}
	for i := range s.Waiters {
		if s.Waiters[i].Action == action {
			s.Waiters[i].Granted = true
			for j := range s.Tenants {
				if s.Tenants[j].Key == s.Waiters[i].Tenant {
					s.Tenants[j].Turn = turn
					s.Tenants[j].Until = now.Add(fairHorizon)
					return nil
				}
			}
		}
	}
	return ErrFairState
}
