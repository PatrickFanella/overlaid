package engine

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
)

func graphDoc(index int) Document {
	d := fixture()
	d.AutomationID = fmt.Sprintf("%08x-1111-4111-8111-111111111111", index+10)
	d.VersionID = fmt.Sprintf("%08x-2222-4222-8222-222222222222", index+10)
	d.Capabilities = nil
	d.Restrictions = nil
	d.Filters = nil
	d.Nodes = []Node{{ID: "root", Kind: "stop"}}
	return d
}
func presetNode(id string, target Document) Node {
	return Node{ID: id, Kind: "preset", Preset: &Preset{TenantID: target.TenantID, AutomationID: target.AutomationID, VersionID: target.VersionID}}
}
func graphRefused(t *testing.T, root Validated, dependencies ...Validated) {
	t.Helper()
	g, err := ResolvePinned(root, dependencies)
	if !errors.Is(err, ErrPinnedGraph) || g.Digest() != "" || g.Steps() != 0 || len(g.Versions()) != 0 {
		t.Fatal("invalid graph returned", err)
	}
}

func TestPinnedGraphIdentityAndCopies(t *testing.T) {
	a, b, c := graphDoc(0), graphDoc(1), graphDoc(2)
	a.Nodes = []Node{{ID: "root", Kind: "sequential", Children: []string{"b", "c"}}, presetNode("b", b), presetNode("c", c)}
	b.Capabilities = []platform.Capability{"chat.send"}
	b.Nodes = []Node{{ID: "root", Kind: "action", Capability: "chat.send"}}
	c.Capabilities = []platform.Capability{"stream.read", "chat.send"}
	root := mustValidate(t, a)
	vb, vc := mustValidate(t, b), mustValidate(t, c)
	deps := []Validated{vb, vc}
	g, err := ResolvePinned(root, deps)
	if err != nil || g.Depth() != 3 || g.Steps() != 5 || len(g.Versions()) != 3 {
		t.Fatal("graph", g, err)
	}
	if got := g.Capabilities(); len(got) != 2 || got[0] != "chat.send" || got[1] != "stream.read" {
		t.Fatal("capability union", got)
	}
	id, err := g.RunID(strings.Repeat("a", 64), ModeExecute)
	if err != nil || len(id) != 64 {
		t.Fatal("run identity", err)
	}
	deps[0] = Validated{}
	copies := g.Versions()
	copies[0] = Validated{}
	caps := g.Capabilities()
	caps[0] = "changed"
	d := g.Root().Document()
	d.Nodes[0].Kind = "changed"
	again, err := ResolvePinned(root, []Validated{vc, vb})
	if err != nil || again.Digest() != g.Digest() {
		t.Fatal("order/copy affected identity")
	}
	againID, _ := again.RunID(strings.Repeat("a", 64), ModeExecute)
	if againID != id || g.Root().Digest() != root.Digest() {
		t.Fatal("unstable identity")
	}
	// Same version identifiers with a different supplied body must change the
	// graph identity; persistence must independently enforce body immutability.
	b.Nodes[0].Arguments = []Argument{{Name: "text", Value: "changed"}}
	changed, err := ResolvePinned(root, []Validated{mustValidate(t, b), vc})
	if err != nil || changed.Digest() == g.Digest() {
		t.Fatal("dependency digest lost")
	}
	changedID, _ := changed.RunID(strings.Repeat("a", 64), ModeExecute)
	if changedID == id {
		t.Fatal("dependency run binding lost")
	}
	for _, mode := range []Mode{ModeReproduce, ModeCurrentConfig} {
		other, err := g.RunID(strings.Repeat("a", 64), mode)
		if err != nil || other == id {
			t.Fatal("mode lost")
		}
	}
	if other, _ := g.RunID(strings.Repeat("b", 64), ModeExecute); other == id {
		t.Fatal("occurrence lost")
	}
	if _, err := (PinnedGraph{}).RunID(strings.Repeat("a", 64), ModeExecute); !errors.Is(err, ErrIdentity) {
		t.Fatal("zero graph")
	}
	if _, err := g.RunID("private-id", ModeExecute); !errors.Is(err, ErrIdentity) {
		t.Fatal("invalid occurrence")
	}
}

func TestPinnedGraphRejectsReferencesAndCycles(t *testing.T) {
	a, b, c := graphDoc(0), graphDoc(1), graphDoc(2)
	a.Nodes = []Node{presetNode("root", b)}
	va, vb, vc := mustValidate(t, a), mustValidate(t, b), mustValidate(t, c)
	graphRefused(t, Validated{})
	graphRefused(t, va)
	graphRefused(t, va, Validated{})
	graphRefused(t, va, vb, vb)
	graphRefused(t, va, va, vb)
	graphRefused(t, va, vb, vc)
	b.VersionID = c.VersionID
	graphRefused(t, va, mustValidate(t, b))
	b = graphDoc(1)
	b.TenantID = "99999999-1111-4111-8111-111111111111"
	graphRefused(t, va, mustValidate(t, b))
	b = graphDoc(1)
	b.Nodes = []Node{presetNode("root", a)}
	graphRefused(t, va, mustValidate(t, b))
}

func TestPinnedGraphExpandedCost(t *testing.T) {
	a, b := graphDoc(0), graphDoc(1)
	// Two calls to the same pinned version cost twice despite memoized analysis.
	a.Nodes = []Node{{ID: "root", Kind: "sequential", Children: []string{"b", "c"}}, presetNode("b", b), presetNode("c", b)}
	g, err := ResolvePinned(mustValidate(t, a), []Validated{mustValidate(t, b)})
	if err != nil || g.Steps() != 5 {
		t.Fatal("repeat count", err, g.Steps())
	}
	// Exactly 100 expanded steps are allowed; one more stop is refused.
	a.Nodes = []Node{{ID: "root", Kind: "sequential"}}
	for i := range 49 {
		id := fmt.Sprintf("call%d", i)
		a.Nodes[0].Children = append(a.Nodes[0].Children, id)
		a.Nodes = append(a.Nodes, presetNode(id, b))
	}
	a.Nodes[0].Children = append(a.Nodes[0].Children, "end")
	a.Nodes = append(a.Nodes, Node{ID: "end", Kind: "stop"})
	g, err = ResolvePinned(mustValidate(t, a), []Validated{mustValidate(t, b)})
	if err != nil || g.Steps() != 100 {
		t.Fatal("exact budget", err, g.Steps())
	}
	a.Nodes[0].Children = append(a.Nodes[0].Children, "extra")
	a.Nodes = append(a.Nodes, Node{ID: "extra", Kind: "stop"})
	graphRefused(t, mustValidate(t, a), mustValidate(t, b))
	// The local loop costs 21, but its expanded target makes it cost 101.
	a = graphDoc(0)
	a.Nodes = []Node{{ID: "root", Kind: "loop", Iterations: 20, Children: []string{"call"}}, presetNode("call", b)}
	b.Nodes = []Node{{ID: "root", Kind: "sequential", Children: []string{"x", "y", "z"}}, {ID: "x", Kind: "stop"}, {ID: "y", Kind: "stop"}, {ID: "z", Kind: "stop"}}
	graphRefused(t, mustValidate(t, a), mustValidate(t, b))
	// Branching uses worst-case cost, not the sum of mutually exclusive paths.
	b.Nodes = []Node{{ID: "root", Kind: "sequential"}}
	for i := range 79 {
		id := fmt.Sprintf("s%d", i)
		b.Nodes[0].Children = append(b.Nodes[0].Children, id)
		b.Nodes = append(b.Nodes, Node{ID: id, Kind: "stop"})
	}
	a.Nodes = []Node{{ID: "root", Kind: "random", Children: []string{"x", "y"}, Weights: []int{1, 1}}, presetNode("x", b), presetNode("y", b)}
	g, err = ResolvePinned(mustValidate(t, a), []Validated{mustValidate(t, b)})
	if err != nil || g.Steps() != 82 {
		t.Fatal("branch budget", err, g.Steps())
	}
	a.Nodes[0].Kind = "sequential"
	a.Nodes[0].Weights = nil
	graphRefused(t, mustValidate(t, a), mustValidate(t, b))
}

func pinnedChain(t *testing.T, count int, wrapped, large bool) []Validated {
	t.Helper()
	docs := make([]Document, count)
	for i := range docs {
		docs[i] = graphDoc(i)
	}
	for i := range docs {
		if i+1 < count {
			docs[i].Nodes = []Node{presetNode("root", docs[i+1])}
			if wrapped {
				docs[i].Nodes[0].ID = "call"
				docs[i].Nodes = append([]Node{{ID: "root", Kind: "sequential", Children: []string{"call"}}}, docs[i].Nodes...)
			}
		}
		if large {
			if i+1 == count {
				docs[i].Capabilities = []platform.Capability{"chat.send"}
				docs[i].Nodes = []Node{{ID: "root", Kind: "action", Capability: "chat.send"}}
			}
			for j := range 8 {
				docs[i].Nodes[len(docs[i].Nodes)-1].Arguments = append(docs[i].Nodes[len(docs[i].Nodes)-1].Arguments, Argument{Name: fmt.Sprintf("arg%d", j), Value: strings.Repeat("x", 4096)})
			}
		}
	}
	versions := make([]Validated, count)
	for i, d := range docs {
		versions[i] = mustValidate(t, d)
	}
	return versions
}

func TestPinnedGraphDepthAndRetentionBounds(t *testing.T) {
	v := pinnedChain(t, 16, false, false)
	g, err := ResolvePinned(v[0], v[1:])
	if err != nil || g.Depth() != 16 || g.Steps() != 16 {
		t.Fatal("exact depth", err)
	}
	v = pinnedChain(t, 17, false, false)
	graphRefused(t, v[0], v[1:]...)
	v = pinnedChain(t, 9, true, false)
	graphRefused(t, v[0], v[1:]...)
	v = pinnedChain(t, 8, false, true)
	graphRefused(t, v[0], v[1:]...)
}

func TestPinnedGraphConcurrentReads(t *testing.T) {
	v := pinnedChain(t, 8, false, false)
	g, err := ResolvePinned(v[0], v[1:])
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 20 {
				other, err := ResolvePinned(v[0], v[1:])
				if err != nil || other.Digest() != g.Digest() {
					t.Error("concurrent graph")
				}
				_ = g.Versions()
				_ = g.Capabilities()
				_, _ = g.RunID(strings.Repeat("a", 64), ModeExecute)
			}
		})
	}
	wg.Wait()
}

func FuzzPinnedGraph(f *testing.F) {
	f.Add(uint8(3), false, false)
	f.Add(uint8(16), true, false)
	f.Fuzz(func(t *testing.T, n uint8, wrapped, cycle bool) {
		count := int(n%18) + 1
		v := pinnedChain(t, count, wrapped, false)
		if cycle && count > 1 {
			d := v[count-1].Document()
			d.Nodes = []Node{presetNode("root", v[0].Document())}
			v[count-1] = mustValidate(t, d)
		}
		g, err := ResolvePinned(v[0], v[1:])
		if err != nil {
			if g.Digest() != "" {
				t.Fatal("partial graph")
			}
			return
		}
		if cycle && count > 1 || g.Depth() > 16 || g.Steps() > 100 || len(g.Versions()) > 16 {
			t.Fatal("graph bound/cycle")
		}
		again, err := ResolvePinned(g.Root(), v[1:])
		if err != nil || again.Digest() != g.Digest() {
			t.Fatal("unstable graph")
		}
	})
}
