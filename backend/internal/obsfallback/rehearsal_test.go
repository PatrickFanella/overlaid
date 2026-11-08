package obsfallback

import (
	"context"
	"errors"
	"git.subcult.tv/PatrickFanella/over-laid/internal/engine"
	"reflect"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) (Rule, Observation) {
	t.Helper()
	r, err := NewRule(Spec{TenantID: "11111111-1111-4111-8111-111111111111", AutomationID: "22222222-2222-4222-8222-222222222222", VersionID: "33333333-3333-4333-8333-333333333333", ConnectionID: strings.Repeat("c", 64), Collection: "Test collection", Input: "Test camera", Scene: "Safe scene"})
	if err != nil {
		t.Fatal(err)
	}
	s := r.Spec()
	return r, Observation{ID: strings.Repeat("a", 64), RuleVersionID: s.VersionID, GraphDigest: r.Graph().Digest(), ConnectionID: s.ConnectionID, Collection: s.Collection, Input: s.Input, Scene: s.Scene, ObservedAt: time.Date(2026, 11, 8, 4, 0, 0, 0, time.UTC), State: Missing, TargetAvailable: true}
}
func TestRehearseConditionsAndFreshness(t *testing.T) {
	r, o := fixture(t)
	for _, tc := range []struct {
		name     string
		state    InputState
		offset   time.Duration
		target   bool
		reason   string
		proposed bool
	}{
		{"missing", Missing, 0, true, "input_missing", true},
		{"present", Present, 0, true, "input_present", false},
		{"muted", Muted, 0, true, "input_muted", false},
		{"hidden", Hidden, 0, true, "input_hidden", false},
		{"lost", Disconnected, 0, true, "connection_lost", false},
		{"unknown", Unknown, 0, true, "input_unknown", false},
		{"no target", Missing, 0, false, "target_unavailable", false},
		{"before expiry", Missing, Freshness - time.Nanosecond, true, "input_missing", true},
		{"at expiry", Missing, Freshness, true, "observation_stale", false},
		{"future observation", Missing, -time.Nanosecond, true, "observation_stale", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := o
			in.State = tc.state
			in.TargetAvailable = tc.target
			got, err := Rehearse(t.Context(), r, in, o.ObservedAt.Add(tc.offset))
			if err != nil || got.Reason != tc.reason || (got.ProposalID != "") != tc.proposed || !got.Deadline.Equal(o.ObservedAt.Add(Freshness)) {
				t.Fatal(got, err)
			}
			if tc.proposed {
				if len(got.Flow.Proposals) != 1 || got.Flow.Proposals[0].Capability != "obs.scene.set" || got.Flow.Proposals[0].Arguments[0].Value != "Safe scene" {
					t.Fatal(got.Flow)
				}
			} else if len(got.Flow.Proposals) != 0 {
				t.Fatal("unqualified effect proposal")
			}
		})
	}
}
func TestReplayBindingAndSyntheticIsolation(t *testing.T) {
	r, o := fixture(t)
	first, err := Rehearse(t.Context(), r, o, o.ObservedAt)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := RestoreRule(r.Graph(), r.Spec().ConnectionID)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Rehearse(t.Context(), restored, o, o.ObservedAt)
	if err != nil || !reflect.DeepEqual(first, again) {
		t.Fatal("replay changed", err)
	}
	o.Synthetic = true
	synthetic, err := Rehearse(t.Context(), r, o, o.ObservedAt)
	if err != nil || !synthetic.Synthetic || synthetic.ProposalID == first.ProposalID || synthetic.ProposalID == "" {
		t.Fatal("synthetic identity", err)
	}
	for _, change := range []func(*Observation){
		func(x *Observation) { x.ConnectionID = strings.Repeat("d", 64) }, func(x *Observation) { x.RuleVersionID = r.Spec().TenantID }, func(x *Observation) { x.GraphDigest = strings.Repeat("e", 64) }, func(x *Observation) { x.Collection = "Other" }, func(x *Observation) { x.Input = "Other" }, func(x *Observation) { x.Scene = "Other" }, func(x *Observation) { x.ID = "bad" }, func(x *Observation) { x.State = "bad" }, func(x *Observation) { x.ObservedAt = time.Time{} },
	} {
		bad := o
		change(&bad)
		if got, err := Rehearse(t.Context(), r, bad, o.ObservedAt); err == nil || got.ProposalID != "" || got.Flow.Proposals != nil {
			t.Fatal("accepted changed binding")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if got, err := Rehearse(ctx, r, o, o.ObservedAt); !errors.Is(err, context.Canceled) || got.Flow.Proposals != nil {
		t.Fatal("canceled result", err)
	}
}
func TestRuleValidationAndImmutableGraph(t *testing.T) {
	r, _ := fixture(t)
	for _, change := range []func(*Spec){func(s *Spec) { s.ConnectionID = "bad" }, func(s *Spec) { s.Collection = "" }, func(s *Spec) { s.Scene = strings.Repeat("x", 256) }, func(s *Spec) { s.Input = "nul\x00name" }, func(s *Spec) { s.TenantID = "bad" }} {
		s := r.Spec()
		change(&s)
		if _, err := NewRule(s); err == nil {
			t.Fatal("invalid rule accepted")
		}
	}
	doc := r.Graph().Root().Document()
	doc.Nodes[0].Arguments[0].Value = "Changed"
	if r.Graph().Root().Document().Nodes[0].Arguments[0].Value != "Safe scene" {
		t.Fatal("graph mutated")
	}
	if _, err := RestoreRule(engine.PinnedGraph{}, r.Spec().ConnectionID); err == nil {
		t.Fatal("zero graph")
	}
}
