package engine

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/variables"
)

func chatTextFixture(t testing.TB, text string) (PinnedGraph, ChatTextInput) {
	t.Helper()
	d := rehearsalDocument()
	d.Nodes = []Node{{ID: "root", Kind: "action", Capability: "chat.send", Arguments: []Argument{{Name: "text", Value: text}}}}
	v, e := Validate(d)
	if e != nil {
		t.Fatal(e)
	}
	g, e := ResolvePinned(v, nil)
	if e != nil {
		t.Fatal(e)
	}
	facts, e := NewFacts(d.TenantID, nil)
	if e != nil {
		t.Fatal(e)
	}
	clock := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	seed := uint64(7)
	snapshot, e := variables.NewSnapshot(variables.SnapshotInput{Values: map[string]variables.Value{"user": {Text: "$(channel)", Available: true}, "query": {Text: "one two", Available: true}}, Clock: &clock, Seed: &seed})
	if e != nil {
		t.Fatal(e)
	}
	return g, ChatTextInput{Flow: RehearsalInput{OccurrenceID: strings.Repeat("a", 64), OriginalGraphDigest: g.Digest(), Mode: ModeReproduce, Clock: clock, Seed: seed, Facts: facts, TraceLimit: 1}, Variables: snapshot, SyntaxVersion: 2}
}
func TestChatTextCapturedRenderingIdentityAndNoReparse(t *testing.T) {
	g, in := chatTextFixture(t, "  hi $(user) $(2) $(time) $(random a b)  ")
	first, e := RehearseChatText(t.Context(), g, in)
	if e != nil || len(first.Proposals) != 1 || !strings.HasPrefix(first.Proposals[0].Text, "  hi $(channel) two 2026-10-23T12:00:00Z ") || !strings.HasSuffix(first.Proposals[0].Text, "  ") {
		t.Fatal(first, e)
	}
	again, e := RehearseChatText(t.Context(), g, in)
	if e != nil || !reflect.DeepEqual(first, again) {
		t.Fatal("not reproducible", e)
	}
	parsed, e := Parse(g.Root().JSON())
	if e != nil {
		t.Fatal(e)
	}
	rebuilt, e := ResolvePinned(parsed, nil)
	if e != nil {
		t.Fatal(e)
	}
	again, e = RehearseChatText(t.Context(), rebuilt, in)
	if e != nil || !reflect.DeepEqual(first, again) {
		t.Fatal("reconstruction changed", e)
	}
	first.Proposals[0].Text = "mutated"
	again, e = RehearseChatText(t.Context(), g, in)
	if e != nil || again.Proposals[0].Text == "mutated" {
		t.Fatal("result alias", e)
	}
	changed := in
	changed.Flow.Mode = ModeCurrentConfig
	r, e := RehearseChatText(t.Context(), g, changed)
	if e != nil || r.ID == again.ID || r.Proposals[0].ID == again.Proposals[0].ID {
		t.Fatal("mode not bound", e)
	}
	changed = in
	changed.Variables, _ = variables.NewSnapshot(variables.SnapshotInput{Values: map[string]variables.Value{"user": {Text: "different", Available: true}, "query": {Text: "one two", Available: true}}, Clock: &in.Flow.Clock, Seed: &in.Flow.Seed})
	r, e = RehearseChatText(t.Context(), g, changed)
	if e != nil || r.ID == again.ID || r.VariableFingerprint == again.VariableFingerprint {
		t.Fatal("variable input not bound", e)
	}
}
func TestChatTextRejectsWholeOutputAndUnavailableCapture(t *testing.T) {
	for _, test := range []struct {
		text string
		want error
	}{{"$(missing)", ErrChatTextUnavailable}, {"$(channel)", ErrChatTextUnavailable}, {"$(user extra)", ErrChatTextUnavailable}, {"$(", ErrChatText}, {"  ", ErrChatText}, {strings.Repeat("雪", 501), ErrChatTextLimit}, {strings.Repeat("a", 500), nil}, {strings.Repeat("$(user)", 101), ErrChatTextLimit}} {
		g, in := chatTextFixture(t, test.text)
		r, e := RehearseChatText(t.Context(), g, in)
		if test.want == nil {
			if e != nil || len(r.Proposals) != 1 {
				t.Fatal("exact limit", e)
			}
			continue
		}
		if !errors.Is(e, test.want) || r.ID != "" || r.Proposals != nil {
			t.Fatal(test.text[:min(len(test.text), 20)], r, e)
		}
	}
	g, in := chatTextFixture(t, "$(random a b) $(time)")
	in.Variables, _ = variables.NewSnapshot(variables.SnapshotInput{})
	if r, e := RehearseChatText(t.Context(), g, in); !errors.Is(e, ErrChatTextUnavailable) || r.ID != "" {
		t.Fatal("invented clock/seed", r, e)
	}
	for _, change := range []func(*ChatTextInput){func(x *ChatTextInput) { x.SyntaxVersion = 1 }, func(x *ChatTextInput) { x.Variables = variables.Snapshot{} }, func(x *ChatTextInput) { x.Flow.Clock = x.Flow.Clock.Add(time.Second) }, func(x *ChatTextInput) { x.Flow.Seed++ }, func(x *ChatTextInput) { x.Flow.Mode = ModeExecute }} {
		g, in := chatTextFixture(t, "hello")
		change(&in)
		r, e := RehearseChatText(t.Context(), g, in)
		if e == nil || r.ID != "" || r.Proposals != nil {
			t.Fatal("invalid input accepted", r, e)
		}
	}
	//nolint:staticcheck // Deliberate invalid context contract check.
	if r, e := RehearseChatText(nil, g, in); !errors.Is(e, ErrChatText) || r.ID != "" {
		t.Fatal(r, e)
	}
}
func TestChatTextFlowPrefixLoopsPresetsAndAggregateLimits(t *testing.T) {
	for _, tail := range []Node{{ID: "tail", Kind: "random", Children: []string{"send"}, Weights: []int{1}}, {ID: "tail", Kind: "action", Capability: "chat.reply", Arguments: []Argument{{Name: "text", Value: "no parent"}}}, {ID: "tail", Kind: "action", Capability: "chat.send", Arguments: []Argument{{Name: "text", Value: "ok"}, {Name: "other", Value: "PRIVATE"}}}, {ID: "tail", Kind: "action", Capability: "chat.send"}} {
		d := rehearsalDocument()
		d.Nodes = []Node{{ID: "root", Kind: "sequential", Children: []string{"send", "tail"}}, {ID: "send", Kind: "action", Capability: "chat.send", Arguments: []Argument{{Name: "text", Value: "prefix"}}}, tail}
		g, in := rehearsalFixture(t, d)
		r, e := RehearseChatText(t.Context(), g, ChatTextInput{Flow: in, Variables: mustChatSnapshot(t), SyntaxVersion: 2})
		if !errors.Is(e, ErrChatTextUnsupported) || r.ID != "" || r.Proposals != nil {
			t.Fatal("partial unsupported prefix", r, e)
		}
	}
	leaf := rehearsalDocument()
	leaf.AutomationID = "44444444-4444-4444-8444-444444444444"
	leaf.VersionID = "55555555-5555-4555-8555-555555555555"
	leaf.Nodes = []Node{{ID: "root", Kind: "action", Capability: "chat.send", Arguments: []Argument{{Name: "text", Value: "hello"}}}}
	d := rehearsalDocument()
	d.Nodes = []Node{{ID: "root", Kind: "sequential", Children: []string{"loop", "stop"}}, {ID: "loop", Kind: "loop", Iterations: 3, Children: []string{"call"}}, {ID: "call", Kind: "preset", Preset: &Preset{TenantID: d.TenantID, AutomationID: leaf.AutomationID, VersionID: leaf.VersionID}}, {ID: "stop", Kind: "stop"}}
	g, flow := rehearsalFixture(t, d, mustValidate(t, leaf))
	r, e := RehearseChatText(t.Context(), g, ChatTextInput{Flow: flow, Variables: mustChatSnapshot(t), SyntaxVersion: 2})
	if e != nil || len(r.Proposals) != 3 || r.Proposals[0].ID == r.Proposals[1].ID || r.Proposals[0].AutomationID != leaf.AutomationID {
		t.Fatal("preset/loop step identity", r, e)
	}

	exact := rehearsalDocument()
	exact.Nodes = []Node{{ID: "root", Kind: "loop", Iterations: 20, Children: []string{"send"}}, {ID: "send", Kind: "action", Capability: "chat.send", Arguments: []Argument{{Name: "text", Value: strings.Repeat("$(user)", 5)}}}}
	exactGraph, exactFlow := rehearsalFixture(t, exact)
	exactSnapshot, _ := variables.NewSnapshot(variables.SnapshotInput{Values: map[string]variables.Value{"user": {Text: "x", Available: true}}})
	exactResult, exactErr := RehearseChatText(t.Context(), exactGraph, ChatTextInput{Flow: exactFlow, Variables: exactSnapshot, SyntaxVersion: 2})
	if exactErr != nil || len(exactResult.Proposals) != 20 {
		t.Fatal("exact resolution budget", exactErr)
	}
	// Compact authored templates can expand to more than the batch output bound.
	for _, text := range []string{"$(user)", strings.Repeat("$(user)", 6)} {
		d := rehearsalDocument()
		d.Nodes = []Node{{ID: "root", Kind: "loop", Iterations: 20, Children: []string{"send"}}, {ID: "send", Kind: "action", Capability: "chat.send", Arguments: []Argument{{Name: "text", Value: text}}}}
		// Two distinct loops produce 40 outputs within the 100-step graph budget.
		d.Nodes[0] = Node{ID: "root", Kind: "sequential", Children: []string{"a", "b"}}
		d.Nodes = append(d.Nodes, Node{ID: "a", Kind: "loop", Iterations: 20, Children: []string{"send"}}, Node{ID: "b", Kind: "loop", Iterations: 20, Children: []string{"send"}})
		g, flow := rehearsalFixture(t, d)
		value := strings.Repeat("😀", 500)
		if len(text) > 7 {
			value = "x"
		}
		snapshot, _ := variables.NewSnapshot(variables.SnapshotInput{Values: map[string]variables.Value{"user": {Text: value, Available: true}}})
		r, e := RehearseChatText(t.Context(), g, ChatTextInput{Flow: flow, Variables: snapshot, SyntaxVersion: 2})
		if !errors.Is(e, ErrChatTextLimit) || r.ID != "" || r.Proposals != nil {
			t.Fatal("aggregate output/resolution limit", e)
		}
	}
}
func mustChatSnapshot(t *testing.T) variables.Snapshot {
	t.Helper()
	s, e := variables.NewSnapshot(variables.SnapshotInput{})
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func TestChatTextCancellationNeverReturnsPrivatePrefix(t *testing.T) {
	g, in := chatTextFixture(t, "hello $(user)")
	for limit := 1; limit < 100; limit++ {
		ctx := &cancelDuringFlow{Context: t.Context(), limit: limit}
		r, e := RehearseChatText(ctx, g, in)
		if e != nil && (!errors.Is(e, context.Canceled) || r.ID != "" || r.Proposals != nil) {
			t.Fatal("partial cancellation", limit, r, e)
		}
	}
	expired, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	if r, e := RehearseChatText(expired, g, in); !errors.Is(e, context.DeadlineExceeded) || r.ID != "" {
		t.Fatal(r, e)
	}
}
func FuzzChatTextCapture(f *testing.F) {
	f.Add("hello $(user)", "viewer")
	f.Fuzz(func(t *testing.T, source, value string) {
		if len(source) > 4096 || len(value) > 8192 {
			return
		}
		d := rehearsalDocument()
		d.Nodes = []Node{{ID: "root", Kind: "action", Capability: "chat.send", Arguments: []Argument{{Name: "text", Value: source}}}}
		v, e := Validate(d)
		if e != nil {
			return
		}
		g, e := ResolvePinned(v, nil)
		if e != nil {
			t.Fatal(e)
		}
		facts, _ := NewFacts(d.TenantID, nil)
		snapshot, e := variables.NewSnapshot(variables.SnapshotInput{Values: map[string]variables.Value{"user": {Text: value, Available: true}}})
		if e != nil {
			return
		}
		in := ChatTextInput{Flow: RehearsalInput{OccurrenceID: strings.Repeat("a", 64), OriginalGraphDigest: g.Digest(), Mode: ModeReproduce, Clock: time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC), Facts: facts, TraceLimit: 1}, Variables: snapshot, SyntaxVersion: 2}
		a, e := RehearseChatText(t.Context(), g, in)
		b, other := RehearseChatText(t.Context(), g, in)
		if (e == nil) != (other == nil) || !reflect.DeepEqual(a, b) || e != nil && (a.ID != "" || a.Proposals != nil) {
			t.Fatal("determinism/partial failure", e, other)
		}
	})
}
