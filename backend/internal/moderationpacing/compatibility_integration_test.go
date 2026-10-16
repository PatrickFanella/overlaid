package moderationpacing

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/permitturns"
	"git.subcult.tv/PatrickFanella/over-laid/internal/testpostgres"
)

func pacingSnapshot(t *testing.T, f testpostgres.TenantFixture) string {
	t.Helper()
	var snapshot string
	if err := f.Admin.QueryRow(`SELECT coalesce(jsonb_agg(to_jsonb(b)||jsonb_build_object('xmin',b.xmin::text) ORDER BY kind,key),'[]'::jsonb)::text FROM moderation_pacing_buckets b`).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestPreflightPreservesRetainedPacingState(t *testing.T) {
	f, s := fixture(t)
	if err := s.CheckState(t.Context()); err != nil {
		t.Fatal("empty state", err)
	}
	active := acquire(t, s, "active-client", "123", "456")
	if !active.Granted {
		t.Fatal("missing active permit")
	}
	cooldown := acquire(t, s, "cooldown-client", "777", "888")
	if err := s.Complete(t.Context(), cooldown.Permit, time.Minute); err != nil {
		t.Fatal(err)
	}
	// Version one and version two retain granted and pending entries, including
	// expired entries that admission could prune. Preflight must not prune them.
	for _, version := range []int{1, 2} {
		now := time.Now().UTC().Add(-time.Hour)
		state := permitturns.State{}
		r := FairRequest{TenantID: "00000000-0000-4000-8000-000000000002", ActionID: strings.Repeat("a", 64), Deadline: now.Add(time.Minute)}
		action, err := state.Register(now, r, strings.Repeat("b", 64)+":"+strings.Repeat("c", 64))
		if err != nil {
			t.Fatal(err)
		}
		if err = state.Grant(now, action); err != nil {
			t.Fatal(err)
		}
		r.ActionID = strings.Repeat("d", 64)
		if _, err = state.Register(now, r, strings.Repeat("e", 64)+":"+strings.Repeat("f", 64)); err != nil {
			t.Fatal(err)
		}
		state.Version = version
		if version == 1 {
			for i := range state.Waiters {
				state.Waiters[i].ActiveUntil = time.Time{}
			}
		}
		raw, _ := json.Marshal(state)
		if _, err = f.Admin.Exec(`INSERT INTO moderation_pacing_buckets(kind,key,fair_state,updated_at) VALUES('client',repeat(md5($1),2),$2,clock_timestamp()-interval '2 days')`, string(rune('0'+version)), raw); err != nil {
			t.Fatal(err)
		}
	}
	before := pacingSnapshot(t, f)
	for range 2 {
		if err := s.Validate(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := s.CheckState(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if after := pacingSnapshot(t, f); after != before {
		t.Fatal("preflight changed retained pacing state")
	}
	if err := s.Complete(t.Context(), active.Permit, 0); err != nil {
		t.Fatal("preflight damaged active permit", err)
	}
}

func TestPreflightRejectsInvalidRetainedStateWithoutRepair(t *testing.T) {
	for _, mode := range []string{"unknown-version", "unknown-field", "invalid-waiter", "chat-resource", "sender-state"} {
		t.Run(mode, func(t *testing.T) {
			f, s := fixture(t)
			raw := `{"version":99}`
			kind := "client"
			switch mode {
			case "unknown-field":
				raw = `{"version":2,"private":true}`
			case "invalid-waiter":
				raw = `{"version":2,"counter":1,"waiters":[{}]}`
			case "chat-resource":
				state := permitturns.State{}
				now := time.Now().UTC()
				_, err := state.Register(now, FairRequest{TenantID: "00000000-0000-4000-8000-000000000002", ActionID: strings.Repeat("a", 64), Deadline: now.Add(time.Minute)}, strings.Repeat("b", 64))
				if err != nil {
					t.Fatal(err)
				}
				encoded, _ := json.Marshal(state)
				raw = string(encoded)
			case "sender-state":
				kind = "sender"
				raw = `{"version":2}`
				// Simulate incompatible retained state, not a runtime writer.
				if _, err := f.Admin.Exec(`ALTER TABLE moderation_pacing_buckets DROP CONSTRAINT moderation_pacing_fair_state_bound`); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := f.Admin.Exec(`INSERT INTO moderation_pacing_buckets(kind,key,fair_state) VALUES($1,repeat('a',64),$2)`, kind, raw); err != nil {
				t.Fatal(err)
			}
			before := pacingSnapshot(t, f)
			if err := s.CheckState(t.Context()); !errors.Is(err, ErrFairState) {
				t.Fatal("incompatible state accepted", err)
			}
			if pacingSnapshot(t, f) != before {
				t.Fatal("failed preflight repaired state")
			}
		})
	}
}

func TestPreflightCapacityBoundsWithoutPruning(t *testing.T) {
	for _, kind := range []string{"sender", "channel", "client"} {
		t.Run(kind, func(t *testing.T) {
			f, s := fixture(t)
			limit := 1024
			if kind == "channel" {
				limit = 10000
			}
			if _, err := f.Admin.Exec(`INSERT INTO moderation_pacing_buckets(kind,key,updated_at) SELECT $1,repeat(md5(i::text),2),clock_timestamp()-interval '2 days' FROM generate_series(1,$2) i`, kind, limit); err != nil {
				t.Fatal(err)
			}
			before := pacingSnapshot(t, f)
			if err := s.CheckState(t.Context()); err != nil {
				t.Fatal("exact bound refused", err)
			}
			if pacingSnapshot(t, f) != before {
				t.Fatal("preflight pruned expired entries at capacity")
			}
			if _, err := f.Admin.Exec(`INSERT INTO moderation_pacing_buckets(kind,key) VALUES($1,repeat(md5('overflow'),2))`, kind); err != nil {
				t.Fatal(err)
			}
			before = pacingSnapshot(t, f)
			if err := s.CheckState(t.Context()); !errors.Is(err, ErrCapacity) {
				t.Fatal("capacity overflow accepted", err)
			}
			if pacingSnapshot(t, f) != before {
				t.Fatal("preflight repaired over-capacity state")
			}
		})
	}
}

func TestPreflightRechecksActivePoolPrivileges(t *testing.T) {
	for _, mode := range []string{"public-policy", "changed-policy", "forced-rls", "table-grant", "column-select", "column-update", "column-reference", "reachable-role", "own-truncate"} {
		t.Run(mode, func(t *testing.T) {
			f, s := fixture(t)
			u, _ := url.Parse(f.ModerationPacerURL)
			role := u.User.Username()
			query := ""
			switch mode {
			case "public-policy":
				query = `CREATE POLICY unexpected ON moderation_pacing_buckets TO PUBLIC USING(true)`
			case "changed-policy":
				query = `ALTER POLICY pacer_only ON moderation_pacing_buckets USING(false)`
			case "forced-rls":
				query = `ALTER TABLE moderation_pacing_buckets NO FORCE ROW LEVEL SECURITY`
			case "table-grant":
				query = `GRANT SELECT ON platform_connections TO ` + role
			case "column-select":
				query = `GRANT SELECT(provider_user_id) ON platform_connections TO ` + role
			case "column-update":
				query = `GRANT UPDATE(provider_user_id) ON platform_connections TO ` + role
			case "column-reference":
				query = `GRANT REFERENCES(provider_user_id) ON platform_connections TO ` + role
			case "reachable-role":
				runtime, _ := url.Parse(f.RuntimeURL)
				query = `GRANT ` + runtime.User.Username() + ` TO ` + role + ` WITH INHERIT FALSE`
			case "own-truncate":
				query = `GRANT TRUNCATE ON moderation_pacing_buckets TO ` + role
			}
			if _, err := f.Admin.Exec(query); err != nil {
				t.Fatal(err)
			}
			if err := s.Validate(t.Context()); err == nil || strings.Contains(err.Error(), role) {
				t.Fatal("active pool drift accepted or exposed", err)
			}
			if bad, err := Open(t.Context(), f.ModerationPacerURL); err == nil {
				_ = bad.Close()
				t.Fatal("new pool accepted same drift")
			}
		})
	}
}

func TestPreflightInvalidClosedAndCanceledPools(t *testing.T) {
	for _, s := range []*Store{nil, {}} {
		if err := s.Validate(t.Context()); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid validation pool", err)
		}
		if err := s.CheckState(t.Context()); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid state pool", err)
		}
	}
	_, s := fixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if s.Validate(ctx) == nil || s.CheckState(ctx) == nil {
		t.Fatal("canceled preflight accepted")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if s.Validate(t.Context()) == nil || s.CheckState(t.Context()) == nil {
		t.Fatal("closed preflight accepted")
	}
}
