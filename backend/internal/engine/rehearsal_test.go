package engine

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func rehearsalFixture(t *testing.T, d Document, deps ...Validated) (PinnedGraph, RehearsalInput) {
	t.Helper()
	v := mustValidate(t, d)
	g, err := ResolvePinned(v, deps)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := NewFacts(d.TenantID, nil)
	if err != nil {
		t.Fatal(err)
	}
	return g, RehearsalInput{OccurrenceID: strings.Repeat("a", 64), OriginalGraphDigest: g.Digest(), Mode: ModeReproduce, Clock: time.Date(2026, 10, 21, 18, 0, 0, 0, time.UTC), Seed: 42, Facts: facts, TraceLimit: 100}
}
func rehearsalDocument() Document {
	d := fixture()
	d.SchemaVersion = AdmissionSchemaVersion
	d.Admission = &AdmissionPolicy{Groups: []string{}}
	return d
}
func TestFlowRehearsalDeterminismIdentityAndOpaquePrivacy(t *testing.T) {
	g, in := rehearsalFixture(t, rehearsalDocument())
	ctx := t.Context()
	first, err := RehearseFlow(ctx, g, in)
	if err != nil || first.Status != "stopped" || len(first.Proposals) != 1 {
		t.Fatal(first, err)
	}
	again, err := RehearseFlow(ctx, g, in)
	if err != nil || !reflect.DeepEqual(first, again) {
		t.Fatal("not reproducible", err)
	}
	// Reconstruct through the actual canonical bytes, not a shared document pointer.
	root, err := Parse(g.Root().JSON())
	if err != nil {
		t.Fatal(err)
	}
	rebuilt, err := ResolvePinned(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	again, err = RehearseFlow(ctx, rebuilt, in)
	if err != nil || !reflect.DeepEqual(first, again) {
		t.Fatal("reconstruction changed trace", err)
	}
	if first.Proposals[0].Arguments[0].Value != "hello $(user)" {
		t.Fatal("silently rendered opaque argument")
	}
	raw, _ := json.Marshal(first.Steps)
	if strings.Contains(string(raw), "hello") || strings.Contains(string(raw), "$(user)") {
		t.Fatal("trace leaked authored arguments")
	}
	for _, change := range []func(*RehearsalInput){func(x *RehearsalInput) { x.Seed++ }, func(x *RehearsalInput) { x.Clock = x.Clock.Add(time.Second) }, func(x *RehearsalInput) { x.Mode = ModeCurrentConfig }, func(x *RehearsalInput) { x.OccurrenceID = strings.Repeat("b", 64) }, func(x *RehearsalInput) { x.TraceLimit = 2 }, func(x *RehearsalInput) {
		x.Facts = mustFacts(t, map[string]Fact{"secret": {State: FactAvailable, Kind: FactText, Text: "PRIVATE_VALUE"}})
	}} {
		other := in
		change(&other)
		r, e := RehearseFlow(ctx, g, other)
		if e != nil || r.ID == first.ID {
			t.Fatal("unbound capture/mode/budget", e)
		}
	}
	first.Steps[0].Reason = "mutated"
	first.Proposals[0].Arguments[0].Value = "mutated"
	again, err = RehearseFlow(ctx, g, in)
	if err != nil || again.Proposals[0].Arguments[0].Value == "mutated" || again.Steps[0].Reason == "mutated" {
		t.Fatal("returned aliases", err)
	}
	changed := rehearsalDocument()
	changed.VersionID = "44444444-4444-4444-8444-444444444444"
	other, _ := rehearsalFixture(t, changed)
	if r, e := RehearseFlow(ctx, other, in); !errors.Is(e, ErrRehearsal) || r.ID != "" {
		t.Fatal("changed graph reproduced", r, e)
	}
	in.Mode = ModeCurrentConfig
	r, e := RehearseFlow(ctx, other, in)
	if e != nil || r.Mode != ModeCurrentConfig || r.OriginalGraphDigest != g.Digest() || r.GraphDigest == g.Digest() {
		t.Fatal("current config mislabeled", e)
	}
}
func TestFlowRehearsalBranchesUnknownAndLazyTraversal(t *testing.T) {
	for _, kind := range []string{"condition", "switch"} {
		t.Run(kind, func(t *testing.T) {
			d := rehearsalDocument()
			d.Restrictions = []Restriction{{ID: "choose", Mode: "all", Predicates: []Predicate{{ID: "check", Field: "ready", Operator: "eq", Value: "true"}}}}
			d.Nodes = []Node{{ID: "root", Kind: kind, Children: []string{"send", "otherwise"}, Conditions: []string{"choose"}}, {ID: "send", Kind: "action", Capability: "chat.send"}, {ID: "otherwise", Kind: "random", Children: []string{"send"}, Weights: []int{1}}}
			g, in := rehearsalFixture(t, d)
			r, e := RehearseFlow(t.Context(), g, in)
			if e != nil || r.Status != "blocked" || r.Reason != "condition_unavailable" || len(r.Proposals) != 0 {
				t.Fatal("unknown selected default", r, e)
			}
			in.Facts = mustFacts(t, map[string]Fact{"ready": {State: FactAvailable, Kind: FactBoolean, Boolean: true}})
			r, e = RehearseFlow(t.Context(), g, in)
			if e != nil || r.Status != "proposed" || len(r.Proposals) != 1 {
				t.Fatal("evaluated unselected branch", r, e)
			}
			in.Facts = mustFacts(t, map[string]Fact{"ready": {State: FactAvailable, Kind: FactBoolean}})
			r, e = RehearseFlow(t.Context(), g, in)
			if e != nil || r.Status != "unsupported" {
				t.Fatal("false skipped default", r, e)
			}
		})
	}
	d := rehearsalDocument()
	d.Restrictions = []Restriction{{ID: "first", Mode: "all", Predicates: []Predicate{{ID: "check", Field: "missing", Operator: "exists"}}}, {ID: "second", Mode: "all"}}
	d.Nodes = []Node{{ID: "root", Kind: "switch", Conditions: []string{"first", "second"}, Children: []string{"a", "b", "c"}}, {ID: "a", Kind: "stop"}, {ID: "b", Kind: "action", Capability: "chat.send"}, {ID: "c", Kind: "stop"}}
	g, in := rehearsalFixture(t, d)
	r, e := RehearseFlow(t.Context(), g, in)
	if e != nil || r.Reason != "condition_unavailable" || len(r.Proposals) != 0 {
		t.Fatal("later true bypassed unknown first case", r, e)
	}
}
func TestFlowRehearsalLoopPresetStopAndTruncation(t *testing.T) {
	leaf := rehearsalDocument()
	leaf.AutomationID = "44444444-4444-4444-8444-444444444444"
	leaf.VersionID = "55555555-5555-4555-8555-555555555555"
	leaf.Nodes = []Node{{ID: "root", Kind: "action", Capability: "chat.reply"}}
	d := rehearsalDocument()
	d.Nodes = []Node{{ID: "root", Kind: "sequential", Children: []string{"repeat", "done", "unreachable"}}, {ID: "repeat", Kind: "loop", Iterations: 3, Children: []string{"call"}}, {ID: "call", Kind: "preset", Preset: &Preset{TenantID: d.TenantID, AutomationID: leaf.AutomationID, VersionID: leaf.VersionID}}, {ID: "done", Kind: "stop"}, {ID: "unreachable", Kind: "action", Capability: "chat.send"}}
	g, in := rehearsalFixture(t, d, mustValidate(t, leaf))
	r, e := RehearseFlow(t.Context(), g, in)
	if e != nil || r.Status != "stopped" || len(r.Proposals) != 3 {
		t.Fatal(r, e)
	}
	for i, p := range r.Proposals {
		if p.AutomationID != leaf.AutomationID || p.VersionID != leaf.VersionID || i > 0 && p.Sequence <= r.Proposals[i-1].Sequence {
			t.Fatal("preset invocation not qualified", p)
		}
	}
	full := len(r.Steps)
	in.TraceLimit = 2
	r, e = RehearseFlow(t.Context(), g, in)
	if e != nil || len(r.Steps) != 2 || r.DroppedSteps != full-2 || len(r.Proposals) != 3 {
		t.Fatal("truncation hid work", r, e)
	}
}
func TestFlowRehearsalRefusalsBudgetsAndCancellation(t *testing.T) {
	g, in := rehearsalFixture(t, rehearsalDocument())
	ctx := t.Context()
	for _, change := range []func(*RehearsalInput){func(x *RehearsalInput) { x.Mode = ModeExecute }, func(x *RehearsalInput) { x.Facts = Facts{} }, func(x *RehearsalInput) { x.TraceLimit = 0 }, func(x *RehearsalInput) { x.TraceLimit = 101 }, func(x *RehearsalInput) { x.Clock = time.Time{} }, func(x *RehearsalInput) { x.OccurrenceID = "bad" }, func(x *RehearsalInput) { x.OriginalGraphDigest = "bad" }, func(x *RehearsalInput) { x.Facts, _ = NewFacts("99999999-9999-4999-8999-999999999999", nil) }} {
		other := in
		change(&other)
		r, e := RehearseFlow(ctx, g, other)
		if !errors.Is(e, ErrRehearsal) || r.ID != "" {
			t.Fatal("invalid capture accepted", r, e)
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if r, e := RehearseFlow(canceled, g, in); !errors.Is(e, context.Canceled) || r.ID != "" || len(r.Proposals) != 0 {
		t.Fatal("partial canceled result", r, e)
	}
	//nolint:staticcheck // Intentionally exercise invalid nil context input.
	if r, e := RehearseFlow(nil, g, in); !errors.Is(e, ErrRehearsal) || r.ID != "" {
		t.Fatal("nil context", e)
	}
	if r, e := RehearseFlow(ctx, PinnedGraph{}, in); !errors.Is(e, ErrRehearsal) || r.ID != "" {
		t.Fatal("zero graph", e)
	}
	d := fixture()
	legacy, li := rehearsalFixture(t, d)
	r, e := RehearseFlow(ctx, legacy, li)
	if e != nil || r.Status != "blocked" || r.Reason != "policy_missing" || len(r.Proposals) != 0 {
		t.Fatal("legacy implicit policy", r, e)
	}
	d = rehearsalDocument()
	d.Filters = []Predicate{{ID: "filter", Field: "private", Operator: "eq", Value: "SECRET"}}
	blocked, bi := rehearsalFixture(t, d)
	r, e = RehearseFlow(ctx, blocked, bi)
	if e != nil || r.Status != "blocked" || r.Reason != "filters_unavailable" {
		t.Fatal(r, e)
	}
	// A compact authored value expanded by a loop must hit the proposal-byte cap.
	d = rehearsalDocument()
	d.Nodes = []Node{{ID: "root", Kind: "loop", Iterations: 20, Children: []string{"send"}}, {ID: "send", Kind: "action", Capability: "chat.send", Arguments: []Argument{{Name: "text", Value: strings.Repeat("s", 4000)}}}}
	bounded, bIn := rehearsalFixture(t, d)
	r, e = RehearseFlow(ctx, bounded, bIn)
	if !errors.Is(e, ErrRehearsalLimit) || r.ID != "" || len(r.Proposals) != 0 || len(r.Steps) != 0 {
		t.Fatal("partial budget result", r, e)
	}
}
func TestFlowRehearsalUnsupportedNodes(t *testing.T) {
	for _, n := range []Node{{ID: "root", Kind: "random", Children: []string{"send"}, Weights: []int{1}, NoRepeat: true}, {ID: "root", Kind: "random", Children: []string{"send"}, Weights: []int{1}}} {
		d := rehearsalDocument()
		d.Nodes = []Node{n}
		if len(n.Children) > 0 {
			d.Nodes = append(d.Nodes, Node{ID: "send", Kind: "action", Capability: "chat.send"})
		}
		g, in := rehearsalFixture(t, d)
		r, e := RehearseFlow(t.Context(), g, in)
		if e != nil || r.Status != "unsupported" || r.Reason != "unsupported_node" || len(r.Proposals) != 0 {
			t.Fatal(n.Kind, r, e)
		}
	}
	d := rehearsalDocument()
	d.Nodes[0].Kind = "all"
	g, in := rehearsalFixture(t, d)
	if r, e := RehearseFlow(t.Context(), g, in); e != nil || len(r.Proposals) != 1 {
		t.Fatal("serial all", r, e)
	}
}
func FuzzRehearsalCapture(f *testing.F) {
	f.Add("ready", "private", uint64(1), uint8(2))
	f.Fuzz(func(t *testing.T, name, value string, seed uint64, limit uint8) {
		d := rehearsalDocument()
		facts, e := NewFacts(d.TenantID, map[string]Fact{name: {State: FactAvailable, Kind: FactText, Text: value}})
		if e != nil {
			return
		}
		g, in := rehearsalFixture(t, d)
		in.Facts = facts
		in.Seed = seed
		in.TraceLimit = int(limit)%100 + 1
		first, e := RehearseFlow(t.Context(), g, in)
		if e != nil {
			t.Fatal(e)
		}
		again, e := RehearseFlow(t.Context(), g, in)
		if e != nil || !reflect.DeepEqual(first, again) || len(first.Steps) > in.TraceLimit {
			t.Fatal("capture determinism/bound", e)
		}
	})
}

type cancelDuringFlow struct {
	context.Context
	calls, limit int
}

func (c *cancelDuringFlow) Err() error {
	c.calls++
	if c.calls >= c.limit {
		return context.Canceled
	}
	return nil
}
func TestFlowRehearsalMidEvaluationCancellationAndPresetArguments(t *testing.T) {
	g, in := rehearsalFixture(t, rehearsalDocument())
	for limit := 2; limit < 30; limit++ {
		ctx := &cancelDuringFlow{Context: t.Context(), limit: limit}
		r, e := RehearseFlow(ctx, g, in)
		if e != nil && (!errors.Is(e, context.Canceled) || r.ID != "" || len(r.Proposals) > 0 || len(r.Steps) > 0) {
			t.Fatal("partial cancellation", limit, r, e)
		}
	}
	leaf := rehearsalDocument()
	leaf.AutomationID = "44444444-4444-4444-8444-444444444444"
	leaf.VersionID = "55555555-5555-4555-8555-555555555555"
	d := rehearsalDocument()
	d.Nodes = []Node{{ID: "root", Kind: "preset", Preset: &Preset{TenantID: d.TenantID, AutomationID: leaf.AutomationID, VersionID: leaf.VersionID}, Arguments: []Argument{{Name: "private", Value: "uninterpreted"}}}}
	g, in = rehearsalFixture(t, d, mustValidate(t, leaf))
	r, e := RehearseFlow(t.Context(), g, in)
	if e != nil || r.Status != "unsupported" || len(r.Proposals) != 0 {
		t.Fatal("preset arguments silently discarded", r, e)
	}
}

func TestFlowRehearsalSimulatedDelayParallelAndOutput(t *testing.T) {
	d := rehearsalDocument()
	d.Nodes = []Node{
		{ID: "root", Kind: "sequential", Children: []string{"declare", "fan", "later"}},
		{ID: "declare", Kind: "set_output", Arguments: []Argument{{Name: "result", Value: "PRIVATE-OUTPUT"}}},
		{ID: "fan", Kind: "all", Parallel: true, Concurrency: 2, Children: []string{"a", "b"}},
		{ID: "a", Kind: "action", Capability: "chat.send", Arguments: []Argument{{Name: "text", Value: "first"}}},
		{ID: "b", Kind: "action", Capability: "chat.send", Arguments: []Argument{{Name: "text", Value: "second"}}},
		{ID: "later", Kind: "delay", DelayMS: 5000, Children: []string{"c"}},
		{ID: "c", Kind: "action", Capability: "chat.send", Arguments: []Argument{{Name: "text", Value: "third"}}},
	}
	g, in := rehearsalFixture(t, d)
	r, err := RehearseFlow(t.Context(), g, in)
	if err != nil || r.Status != "proposed" || r.Reason != "supported_flow" || !r.Deferred || len(r.Proposals) != 3 {
		t.Fatal(r, err)
	}
	got := []string{}
	for _, s := range r.Steps {
		got = append(got, s.NodeID+":"+s.Kind+":"+s.Outcome+":"+s.Reason)
	}
	want := []string{"trigger:admission:checked:matched", "root:sequential:visited:supported", "declare:set_output:visited:output_declared", "fan:all:visited:parallel_branches", "a:action:proposed:opaque_arguments", "b:action:proposed:opaque_arguments", "later:delay:visited:simulated_delay", "c:action:proposed:opaque_arguments"}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	// The delayed child's parent is the delay step; set_output never becomes a proposal.
	if r.Steps[7].Parent != r.Steps[6].Sequence || r.Proposals[2].NodeID != "c" {
		t.Fatal(r.Steps, r.Proposals)
	}
	raw, _ := json.Marshal(r.Steps)
	if strings.Contains(string(raw), "PRIVATE-OUTPUT") || strings.Contains(string(raw), "5000") {
		t.Fatal("trace leaked authored values", string(raw))
	}
	// Without a delay the flow is not deferred, and a stop after a delay keeps the flag.
	d.Nodes[0].Children = []string{"declare", "fan"}
	d.Nodes = d.Nodes[:5]
	g, in = rehearsalFixture(t, d)
	if r, err = RehearseFlow(t.Context(), g, in); err != nil || r.Deferred || len(r.Proposals) != 2 {
		t.Fatal("undelayed flow deferred", r, err)
	}
	d = rehearsalDocument()
	d.Nodes = []Node{{ID: "root", Kind: "delay", DelayMS: 1, Children: []string{"halt"}}, {ID: "halt", Kind: "stop"}}
	g, in = rehearsalFixture(t, d)
	if r, err = RehearseFlow(t.Context(), g, in); err != nil || r.Status != "stopped" || !r.Deferred {
		t.Fatal("stop after delay", r, err)
	}
}

// Offsets follow authored timing: sequential children start where the previous
// one ended, parallel branches share a start and end at the latest, loops
// repeat sequentially and a delay shifts only its own subtree.
func TestFlowRehearsalDelayOffsets(t *testing.T) {
	send := func(id string) Node {
		return Node{ID: id, Kind: "action", Capability: "chat.send", Arguments: []Argument{{Name: "text", Value: id}}}
	}
	d := rehearsalDocument()
	d.Nodes = []Node{
		{ID: "root", Kind: "sequential", Children: []string{"now", "fan", "after", "repeat"}},
		send("now"),
		{ID: "fan", Kind: "all", Parallel: true, Concurrency: 2, Children: []string{"short", "long"}},
		{ID: "short", Kind: "delay", DelayMS: 1000, Children: []string{"s"}},
		send("s"),
		{ID: "long", Kind: "delay", DelayMS: 3000, Children: []string{"l"}},
		send("l"),
		send("after"),
		{ID: "repeat", Kind: "loop", Iterations: 2, Children: []string{"tick"}},
		{ID: "tick", Kind: "delay", DelayMS: 500, Children: []string{"t"}},
		send("t"),
	}
	g, in := rehearsalFixture(t, d)
	r, err := RehearseFlow(t.Context(), g, in)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, p := range r.Proposals {
		got = append(got, p.NodeID+"@"+strconv.FormatInt(p.DelayMS, 10))
	}
	if want := []string{"now@0", "s@1000", "l@3000", "after@3000", "t@3500", "t@4000"}; !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	// Total offset is capped at MaxDelayMS even when each delay is valid alone.
	d = rehearsalDocument()
	d.Nodes = []Node{{ID: "root", Kind: "loop", Iterations: 2, Children: []string{"wait"}}, {ID: "wait", Kind: "delay", DelayMS: MaxDelayMS, Children: []string{"x"}}, send("x")}
	g, in = rehearsalFixture(t, d)
	if r, err = RehearseFlow(t.Context(), g, in); !errors.Is(err, ErrRehearsalLimit) || r.ID != "" || r.Proposals != nil {
		t.Fatal("offset cap", r, err)
	}
	d.Nodes[0].Iterations = 1
	g, in = rehearsalFixture(t, d)
	if r, err = RehearseFlow(t.Context(), g, in); err != nil || r.Proposals[0].DelayMS != MaxDelayMS {
		t.Fatal("exact cap", r, err)
	}
}
