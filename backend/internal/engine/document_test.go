package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
)

func fixture() Document {
	return Document{SchemaVersion: 1, TenantID: "11111111-1111-4111-8111-111111111111", AutomationID: "22222222-2222-4222-8222-222222222222", VersionID: "33333333-3333-4333-8333-333333333333", Trigger: Trigger{ID: "trigger", Kind: "command"}, Capabilities: []platform.Capability{"chat.send", "chat.reply"}, Root: "root", Nodes: []Node{{ID: "root", Kind: "sequential", Children: []string{"send", "done"}}, {ID: "send", Kind: "action", Capability: "chat.send", Arguments: []Argument{{Name: "text", Value: "hello $(user)"}}}, {ID: "done", Kind: "stop"}}}
}
func mustValidate(t *testing.T, d Document) Validated {
	t.Helper()
	v, err := Validate(d)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func refused(t *testing.T, d Document) {
	t.Helper()
	v, err := Validate(d)
	if !errors.Is(err, ErrDocument) || v.Digest() != "" || len(v.JSON()) != 0 {
		t.Fatal("invalid document returned snapshot", err)
	}
}

func TestImmutableCanonicalDocumentAndIdentity(t *testing.T) {
	d := fixture()
	v := mustValidate(t, d)
	original := v.JSON()
	id, err := v.RunID(strings.Repeat("a", 64), ModeExecute)
	if err != nil || len(id) != 64 {
		t.Fatal(id, err)
	}
	d.Nodes[1].Arguments[0].Value = "CHANGED"
	d.Capabilities[0] = "stream.read"
	raw := v.JSON()
	raw[0] = '!'
	copyDoc := v.Document()
	for i := range copyDoc.Nodes {
		if copyDoc.Nodes[i].ID == "root" {
			copyDoc.Nodes[i].Children[0] = "CHANGED"
		}
		if copyDoc.Nodes[i].ID == "send" {
			copyDoc.Nodes[i].Arguments[0].Value = "CHANGED"
		}
	}
	if !bytes.Equal(original, v.JSON()) {
		t.Fatal("snapshot mutated")
	}
	reconstructed, err := Parse(original)
	if err != nil || reconstructed.Digest() != v.Digest() {
		t.Fatal("reconstruction", err)
	}
	again, _ := reconstructed.RunID(strings.Repeat("a", 64), ModeExecute)
	if again != id {
		t.Fatal("unstable run")
	}
	reordered := fixture()
	reordered.Capabilities[0], reordered.Capabilities[1] = reordered.Capabilities[1], reordered.Capabilities[0]
	reordered.Nodes[0], reordered.Nodes[2] = reordered.Nodes[2], reordered.Nodes[0]
	if mustValidate(t, reordered).Digest() != v.Digest() {
		t.Fatal("unordered registries changed digest")
	}
	for _, change := range []func(*Document){func(d *Document) { d.TenantID = "44444444-4444-4444-8444-444444444444" }, func(d *Document) { d.AutomationID = "44444444-4444-4444-8444-444444444444" }, func(d *Document) { d.VersionID = "44444444-4444-4444-8444-444444444444" }, func(d *Document) { d.Trigger.Kind = "timer" }, func(d *Document) { d.Nodes[1].Arguments[0].Value = "other" }, func(d *Document) { d.Nodes[0].Children = []string{"done", "send"} }} {
		changed := fixture()
		change(&changed)
		other, _ := mustValidate(t, changed).RunID(strings.Repeat("a", 64), ModeExecute)
		if other == id {
			t.Fatal("lost binding")
		}
	}
	modeIDs := map[string]bool{id: true}
	for _, mode := range []Mode{ModeReproduce, ModeCurrentConfig} {
		other, err := v.RunID(strings.Repeat("a", 64), mode)
		if err != nil || modeIDs[other] {
			t.Fatal("mode binding", err)
		}
		modeIDs[other] = true
	}
	other, _ := v.RunID(strings.Repeat("b", 64), ModeExecute)
	if other == id {
		t.Fatal("occurrence binding")
	}
	for _, input := range []struct {
		v     Validated
		event string
		mode  Mode
	}{{Validated{}, strings.Repeat("a", 64), ModeExecute}, {v, "native-private-id", ModeExecute}, {v, strings.Repeat("A", 64), ModeExecute}, {v, strings.Repeat("a", 64), "unknown"}} {
		if _, err := input.v.RunID(input.event, input.mode); !errors.Is(err, ErrIdentity) {
			t.Fatal("invalid identity accepted")
		}
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 100 {
				got, err := v.RunID(strings.Repeat("a", 64), ModeExecute)
				if err != nil || got != id {
					t.Error("concurrent identity")
				}
				_ = v.Document()
				_ = v.JSON()
			}
		})
	}
	wg.Wait()
}

func TestDocumentRefusals(t *testing.T) {
	cases := map[string]func(*Document){
		"version": func(d *Document) { d.SchemaVersion = 2 }, "tenant": func(d *Document) { d.TenantID = "bad" }, "zero tenant": func(d *Document) { d.TenantID = "00000000-0000-0000-0000-000000000000" }, "automation": func(d *Document) { d.AutomationID = "" }, "version id": func(d *Document) { d.VersionID = "" }, "trigger": func(d *Document) { d.Trigger.Kind = "arbitrary" }, "trigger id": func(d *Document) { d.Trigger.ID = "" }, "root": func(d *Document) { d.Root = "missing" }, "nodes": func(d *Document) { d.Nodes = nil }, "duplicate": func(d *Document) { d.Nodes[2].ID = "send" }, "global duplicate": func(d *Document) { d.Nodes[2].ID = "trigger" }, "dangling": func(d *Document) { d.Nodes[0].Children[0] = "missing" }, "cycle": func(d *Document) { d.Nodes[0].Children[0] = "root" }, "orphan": func(d *Document) { d.Nodes = append(d.Nodes, Node{ID: "orphan", Kind: "stop"}) }, "kind": func(d *Document) { d.Nodes[1].Kind = "eval" }, "capability": func(d *Document) { d.Capabilities = append(d.Capabilities, "arbitrary.execute") }, "duplicate cap": func(d *Document) { d.Capabilities = append(d.Capabilities, "chat.send") }, "undeclared cap": func(d *Document) { d.Nodes[1].Capability = "stream.read" }, "empty cap": func(d *Document) { d.Nodes[1].Capability = "" }, "stop child": func(d *Document) { d.Nodes[2].Children = []string{"send"} }, "empty sequence": func(d *Document) { d.Nodes[0].Children = nil }, "iterations": func(d *Document) { d.Nodes[1].Iterations = 1 }, "delay": func(d *Document) { d.Nodes[1].DelayMS = 1 }, "parallel": func(d *Document) { d.Nodes[1].Parallel = true }, "concurrency": func(d *Document) { d.Nodes[1].Concurrency = 1 }, "random": func(d *Document) { d.Nodes[1].NoRepeat = true }, "weights": func(d *Document) { d.Nodes[1].Weights = []int{1} }, "conditions": func(d *Document) { d.Nodes[1].Conditions = []string{"gate"} }, "preset": func(d *Document) { d.Nodes[1].Preset = &Preset{} }, "arguments": func(d *Document) { d.Nodes[2].Arguments = []Argument{{Name: "text", Value: "x"}} }, "duplicate args": func(d *Document) { d.Nodes[1].Arguments = append(d.Nodes[1].Arguments, d.Nodes[1].Arguments[0]) }, "invalid arg": func(d *Document) { d.Nodes[1].Arguments[0].Name = " BAD" }, "large arg": func(d *Document) { d.Nodes[1].Arguments[0].Value = strings.Repeat("x", MaxTextBytes+1) }, "unicode": func(d *Document) { d.Nodes[1].Arguments[0].Value = string([]byte{0xff}) }, "nul": func(d *Document) { d.Nodes[1].Arguments[0].Value = "private\x00value" }, "child id": func(d *Document) { d.Nodes[0].Children[0] = "bad id" }, "restriction mode": func(d *Document) { d.Restrictions = []Restriction{{ID: "gate", Mode: "some"}} },
	}
	for label, change := range cases {
		t.Run(label, func(t *testing.T) { d := fixture(); change(&d); refused(t, d) })
	}
}

func TestControlShapesAndPresets(t *testing.T) {
	for _, kind := range []string{"sequential", "all", "random", "condition", "switch", "loop", "delay", "stop", "set_output", "preset"} {
		t.Run(kind, func(t *testing.T) {
			d := fixture()
			d.Restrictions = []Restriction{{ID: "gate", Mode: "all", Predicates: []Predicate{{ID: "role", Field: "actor.role", Operator: "eq", Value: "moderator"}}}}
			d.Nodes = []Node{{ID: "root", Kind: kind, Children: []string{"done"}}, {ID: "done", Kind: "stop"}}
			switch kind {
			case "all":
				d.Nodes[0].Parallel = true
				d.Nodes[0].Concurrency = 4
			case "random":
				d.Nodes[0].Weights = []int{1}
				d.Nodes[0].NoRepeat = true
			case "condition", "switch":
				d.Nodes[0].Children = []string{"done", "other"}
				d.Nodes[0].Conditions = []string{"gate"}
				d.Nodes = append(d.Nodes, Node{ID: "other", Kind: "stop"})
			case "loop":
				d.Nodes[0].Iterations = 20
			case "delay":
				d.Nodes[0].DelayMS = MaxDelayMS
			case "stop", "set_output", "preset":
				d.Nodes = d.Nodes[:1]
				d.Nodes[0].Children = nil
			}
			if kind == "set_output" {
				d.Nodes[0].Arguments = []Argument{{Name: "result", Value: "value"}}
			}
			if kind == "preset" {
				d.Nodes[0].Preset = &Preset{TenantID: d.TenantID, AutomationID: "55555555-5555-4555-8555-555555555555", VersionID: d.VersionID}
			}
			mustValidate(t, d)
			mutations := []func(*Node){}
			switch kind {
			case "sequential":
				mutations = append(mutations, func(n *Node) { n.Children = nil })
			case "all":
				mutations = append(mutations, func(n *Node) { n.Concurrency = 5 }, func(n *Node) { n.Parallel = false })
			case "random":
				mutations = append(mutations, func(n *Node) { n.Weights = nil }, func(n *Node) { n.Weights = []int{0} }, func(n *Node) { n.Weights = []int{1_000_001} })
			case "condition", "switch":
				mutations = append(mutations, func(n *Node) { n.Conditions = nil }, func(n *Node) { n.Conditions = []string{"missing"} }, func(n *Node) { n.Children = n.Children[:1] })
			case "loop":
				mutations = append(mutations, func(n *Node) { n.Iterations = 0 }, func(n *Node) { n.Iterations = 21 })
			case "delay":
				mutations = append(mutations, func(n *Node) { n.DelayMS = 0 }, func(n *Node) { n.DelayMS = MaxDelayMS + 1 })
			case "set_output":
				mutations = append(mutations, func(n *Node) { n.Arguments = nil })
			case "preset":
				mutations = append(mutations, func(n *Node) { n.Preset = nil }, func(n *Node) { n.Preset = &Preset{} }, func(n *Node) { p := *n.Preset; p.TenantID = "66666666-6666-4666-8666-666666666666"; n.Preset = &p }, func(n *Node) { p := *n.Preset; p.AutomationID = d.AutomationID; n.Preset = &p })
			}
			for _, change := range mutations {
				copy := mustValidate(t, d).Document()
				for i := range copy.Nodes {
					if copy.Nodes[i].ID == "root" {
						change(&copy.Nodes[i])
					}
				}
				refused(t, copy)
			}
		})
	}
	for _, mode := range []string{"all", "any", "none"} {
		d := fixture()
		d.Restrictions = []Restriction{{ID: "gate", Mode: mode}}
		mustValidate(t, d)
	}
	for _, p := range []Predicate{{ID: "p", Field: "bad field", Operator: "eq"}, {ID: "p", Field: "actor.role", Operator: "eval"}, {ID: "p", Field: "actor.role", Operator: "exists", Value: "wrong"}, {ID: "trigger", Field: "actor.role", Operator: "eq"}} {
		d := fixture()
		d.Restrictions = []Restriction{{ID: "gate", Mode: "all", Predicates: []Predicate{p}}}
		refused(t, d)
	}
}

func TestGraphBudgets(t *testing.T) {
	d := fixture()
	d.Nodes = []Node{{ID: "root", Kind: "loop", Children: []string{"inner"}, Iterations: 20}, {ID: "inner", Kind: "loop", Children: []string{"done"}, Iterations: 5}, {ID: "done", Kind: "stop"}}
	refused(t, d)
	d.Nodes[0].Iterations = 16
	mustValidate(t, d)
	d = fixture()
	d.Nodes = []Node{{ID: "root", Kind: "sequential"}}
	for i := 0; i < 99; i++ {
		id := fmt.Sprintf("leaf%d", i)
		d.Nodes[0].Children = append(d.Nodes[0].Children, id)
		d.Nodes = append(d.Nodes, Node{ID: id, Kind: "stop"})
	}
	mustValidate(t, d)
	d.Nodes[0].Children = append(d.Nodes[0].Children, "leaf0")
	refused(t, d)
	d = fixture()
	d.Nodes = nil
	d.Root = "node0"
	for i := 0; i < 16; i++ {
		n := Node{ID: fmt.Sprintf("node%d", i), Kind: "delay", DelayMS: 1, Children: []string{fmt.Sprintf("node%d", i+1)}}
		if i == 15 {
			n = Node{ID: n.ID, Kind: "stop"}
		}
		d.Nodes = append(d.Nodes, n)
	}
	mustValidate(t, d)
	d.Nodes[15] = Node{ID: "node15", Kind: "delay", DelayMS: 1, Children: []string{"node16"}}
	d.Nodes = append(d.Nodes, Node{ID: "node16", Kind: "stop"})
	refused(t, d)
	d = fixture()
	d.Nodes[1].Arguments = nil
	for i := 0; i < 32; i++ {
		d.Nodes[1].Arguments = append(d.Nodes[1].Arguments, Argument{Name: fmt.Sprintf("arg%d", i), Value: strings.Repeat("x", MaxTextBytes)})
	}
	refused(t, d)
}

func TestParseRefusals(t *testing.T) {
	raw, _ := json.Marshal(fixture())
	for _, input := range [][]byte{nil, []byte("null"), []byte("[]"), []byte("{"), append(append([]byte(nil), raw...), raw...), []byte(strings.Repeat(" ", MaxDocumentBytes+1)), append([]byte{0xff}, raw...), bytes.Replace(raw, []byte(`"schemaVersion":1`), []byte(`"schemaVersion":1,"schemaVersion":1`), 1), bytes.Replace(raw, []byte(`"trigger":{`), []byte(`"unknown":true,"trigger":{`), 1), bytes.Replace(raw, []byte(`"kind":"command"`), []byte(`"kind":"command","kind":"command"`), 1), []byte(strings.Repeat("[", 18) + strings.Repeat("]", 18))} {
		if _, err := Parse(input); !errors.Is(err, ErrDocument) {
			t.Fatalf("malformed input accepted: %q", input)
		}
	}
}

func FuzzParse(f *testing.F) {
	raw, _ := json.Marshal(fixture())
	f.Add(raw)
	f.Add([]byte(`{"schemaVersion":1}`))
	f.Add([]byte(`null`))
	f.Add([]byte(`[[[[`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		v, err := Parse(raw)
		if err != nil {
			if !errors.Is(err, ErrDocument) {
				t.Fatal(err)
			}
			return
		}
		if len(v.JSON()) > MaxDocumentBytes {
			t.Fatal("oversized snapshot")
		}
		again, err := Parse(v.JSON())
		if err != nil || again.Digest() != v.Digest() {
			t.Fatal("noncanonical reconstruction", err)
		}
		a, err := v.RunID(strings.Repeat("a", 64), ModeExecute)
		b, e := again.RunID(strings.Repeat("a", 64), ModeExecute)
		if err != nil || e != nil || a != b {
			t.Fatal("unstable identity")
		}
	})
}

func TestFilterAndRestrictionBudget(t *testing.T) {
	d := fixture()
	d.Filters = []Predicate{{ID: "filter", Field: "event.category", Operator: "eq", Value: "Just Chatting"}}
	mustValidate(t, d)
	d.Filters[0].ID = "trigger"
	refused(t, d)
	d = fixture()
	for i := 0; i < MaxPredicates; i++ {
		d.Filters = append(d.Filters, Predicate{ID: fmt.Sprintf("filter%d", i), Field: "event.category", Operator: "exists"})
	}
	mustValidate(t, d)
	d.Restrictions = []Restriction{{ID: "gate", Mode: "all", Predicates: []Predicate{{ID: "extra", Field: "actor.role", Operator: "eq", Value: "moderator"}}}}
	refused(t, d)
	d = fixture()
	d.Filters = make([]Predicate, MaxPredicates+1)
	refused(t, d)
	d = fixture()
	d.Restrictions = make([]Restriction, MaxPredicates+1)
	refused(t, d)
	d = fixture()
	d.Capabilities = make([]platform.Capability, 65)
	refused(t, d)
	d = fixture()
	d.Nodes = make([]Node, MaxSteps+1)
	refused(t, d)
	d = fixture()
	d.Nodes[1].Arguments = make([]Argument, 33)
	refused(t, d)
	d = fixture()
	d.Nodes[0].Children = make([]string, MaxSteps+1)
	refused(t, d)
}

func TestLargeNumbersAndPresetSnapshot(t *testing.T) {
	for _, n := range []Node{
		{ID: "root", Kind: "loop", Children: []string{"done"}, Iterations: int(^uint(0) >> 1)},
		{ID: "root", Kind: "random", Children: []string{"done"}, Weights: []int{int(^uint(0) >> 1)}},
		{ID: "root", Kind: "delay", Children: []string{"done"}, DelayMS: 1 << 62},
	} {
		d := fixture()
		d.Nodes = []Node{n, {ID: "done", Kind: "stop"}}
		refused(t, d)
	}
	d := fixture()
	d.Nodes = []Node{{ID: "root", Kind: "preset", Preset: &Preset{TenantID: d.TenantID, AutomationID: "55555555-5555-4555-8555-555555555555", VersionID: d.VersionID}}}
	v := mustValidate(t, d)
	before := v.JSON()
	d.Nodes[0].Preset.VersionID = "CHANGED"
	copy := v.Document()
	copy.Nodes[0].Preset.TenantID = "CHANGED"
	if !bytes.Equal(before, v.JSON()) {
		t.Fatal("preset pointer escaped immutable snapshot")
	}
}

func TestJSONFieldAliasesCannotOverwrite(t *testing.T) {
	raw, _ := json.Marshal(fixture())
	for _, alias := range []string{"SchemaVersion", "SCHEMAVERSION", "schemaVerſion"} {
		input := bytes.Replace(raw, []byte(`"schemaVersion":1`), []byte(`"schemaVersion":1,"`+alias+`":1`), 1)
		if _, err := Parse(input); !errors.Is(err, ErrDocument) {
			t.Fatal("duplicate alias accepted", alias)
		}
	}
}
