package engine

import (
	"reflect"
	"testing"
)

func TestNoRepeatCapturedMemory(t *testing.T) {
	for _, children := range [][]string{{"heads", "tails"}, {"heads"}, {"heads", "heads"}} {
		t.Run(children[len(children)-1], func(t *testing.T) {
			d := rehearsalDocument()
			d.Nodes = []Node{{ID: "root", Kind: "loop", Iterations: 10, Children: []string{"pick"}}, {ID: "pick", Kind: "random", NoRepeat: true, Children: children, Weights: []int{1}}}
			if len(children) == 2 {
				d.Nodes[1].Weights = []int{1, 999999}
			}
			for _, child := range children {
				if child == "heads" && len(d.Nodes) > 2 {
					continue
				}
				d.Nodes = append(d.Nodes, Node{ID: child, Kind: "action", Capability: "chat.send"})
			}
			g, in := rehearsalFixture(t, d)
			in.Seed = 42
			key := d.AutomationID + "/pick"
			in.Recent = map[string]string{key: "heads"}
			original := map[string]string{key: "heads"}
			r, err := RehearseFlow(t.Context(), g, in)
			if err != nil || r.Status != "proposed" || len(r.Proposals) != 10 {
				t.Fatal(r, err)
			}
			prior := "heads"
			for _, p := range r.Proposals {
				if children[0] != children[len(children)-1] && p.NodeID == prior {
					t.Fatal("repeated child", prior)
				}
				prior = p.NodeID
			}
			if r.Recent[key] != prior || !reflect.DeepEqual(in.Recent, original) {
				t.Fatal("memory mutated or wrong final choice")
			}
			for _, step := range r.Steps {
				if step.Kind == "random" && step.Reason != "no_repeat_choice" {
					t.Fatal(step)
				}
			}
			again, err := RehearseFlow(t.Context(), g, in)
			if err != nil || !reflect.DeepEqual(r, again) {
				t.Fatal("replay drift", err)
			}
			in.Recent = map[string]string{key: "removed"}
			changed, err := RehearseFlow(t.Context(), g, in)
			if err != nil || changed.CaptureDigest == r.CaptureDigest {
				t.Fatal("memory not bound", err)
			}
			in.Recent = nil
			legacy, err := RehearseFlow(t.Context(), g, in)
			if err != nil || legacy.Status != "unsupported" {
				t.Fatal("legacy memory invented", err)
			}
		})
	}
}

func TestCaptureRecentChoicesBoundsAndPruning(t *testing.T) {
	d := rehearsalDocument()
	d.Nodes = []Node{{ID: "root", Kind: "random", NoRepeat: true, Children: []string{"send"}, Weights: []int{1}}, {ID: "send", Kind: "action", Capability: "chat.send"}}
	g, in := rehearsalFixture(t, d)
	key := d.AutomationID + "/root"
	for _, prior := range []map[string]string{nil, {}, {key: "send", d.AutomationID + "/removed": "old"}} {
		got, err := CaptureRecentChoices(g, prior)
		if err != nil || len(got) != 1 || got[key] != prior[key] {
			t.Fatal(got, err)
		}
	}
	for _, recent := range []map[string]string{{"bad": "send"}, {key: "PRIVATE INPUT"}, {d.AutomationID + "/bad/path": "send"}} {
		in.Seed, in.Recent = 42, recent
		if got, err := RehearseFlow(t.Context(), g, in); err == nil || got.ID != "" {
			t.Fatal("malformed memory accepted")
		}
		if _, err := CaptureRecentChoices(g, recent); err == nil {
			t.Fatal("malformed memory captured")
		}
	}
	in.Seed, in.Recent = 0, map[string]string{}
	if _, err := RehearseFlow(t.Context(), g, in); err == nil {
		t.Fatal("memory without seed")
	}
}
