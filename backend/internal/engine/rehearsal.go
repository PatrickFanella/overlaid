package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
)

const MaxProposalBytes = 64 * 1024

var ErrRehearsal = errors.New("engine_rehearsal_invalid")
var ErrRehearsalLimit = errors.New("engine_rehearsal_limit")

// RehearsalInput is supplied evidence, never authenticated runtime authority.
// Facts owns its captured values. Reproduction requires agreement with the
// supplied original digest; a trusted caller must load the original evidence.
type RehearsalInput struct {
	OccurrenceID, OriginalGraphDigest string
	Mode                              Mode
	Clock                             time.Time
	Seed                              uint64
	Facts                             Facts
	TraceLimit                        int
}

// FlowStep contains authored identifiers and fixed outcome codes, not values or
// action arguments. It remains private workspace metadata, not a public receipt.
type FlowStep struct {
	Sequence, Parent                                       int
	AutomationID, VersionID, NodeID, Kind, Outcome, Reason string
}

// FlowProposal holds private, opaque authored arguments. They are unrendered:
// this is not a transport payload or an executable/authorized effect intent.
type FlowProposal struct {
	Sequence                        int
	AutomationID, VersionID, NodeID string
	Capability                      platform.Capability
	Arguments                       []Argument
}
type FlowRehearsal struct {
	ID, GraphDigest, OriginalGraphDigest, CaptureDigest string
	Mode                                                Mode
	Clock                                               time.Time
	Seed                                                uint64
	Status, Reason                                      string
	Steps                                               []FlowStep
	DroppedSteps                                        int
	Proposals                                           []FlowProposal
}

// RehearseFlow traverses supported captured control flow only. Its API has no
// provider, persistence, clock, variable resolver or effect callback. Root
// predicates are simulated; cooldown, entitlement and permission are not checked.
func RehearseFlow(ctx context.Context, g PinnedGraph, in RehearsalInput) (out FlowRehearsal, failure error) {
	defer func() {
		if ctx != nil && ctx.Err() != nil {
			out = FlowRehearsal{}
			failure = ctx.Err()
		}
		if failure != nil {
			out = FlowRehearsal{}
		}
	}()
	if ctx == nil || g.Digest() == "" || !occurrence.MatchString(in.OccurrenceID) || !occurrence.MatchString(in.OriginalGraphDigest) || !oneOf(string(in.Mode), string(ModeReproduce), string(ModeCurrentConfig)) || in.Clock.IsZero() || in.Clock.Year() < 1 || in.Clock.Year() > 9999 || !in.Facts.valid || in.Facts.tenant != g.Root().tenant || in.TraceLimit < 1 || in.TraceLimit > MaxSteps {
		return out, ErrRehearsal
	}
	if in.Mode == ModeReproduce && in.OriginalGraphDigest != g.Digest() {
		return out, ErrRehearsal
	}
	capture, err := json.Marshal(struct {
		Clock time.Time
		Seed  uint64
		Facts map[string]Fact
	}{in.Clock.UTC(), in.Seed, in.Facts.values})
	if err != nil {
		return out, ErrRehearsal
	}
	captureDigest := rehearsalHash("overlaid.engine.capture.v1", string(capture))
	out = FlowRehearsal{GraphDigest: g.Digest(), OriginalGraphDigest: in.OriginalGraphDigest, CaptureDigest: captureDigest, Mode: in.Mode, Clock: in.Clock.UTC(), Seed: in.Seed, Status: "proposed", Reason: "supported_flow", Steps: []FlowStep{}, Proposals: []FlowProposal{}}
	out.ID = rehearsalHash("overlaid.engine.rehearsal.v1", g.Digest(), in.OriginalGraphDigest, in.OccurrenceID, string(in.Mode), captureDigest, strconv.Itoa(in.TraceLimit))
	w := flowWalker{ctx: ctx, in: in, out: &out, nodes: map[versionKey]map[string]Node{}, roots: map[versionKey]string{}, groups: map[versionKey]map[string]Truth{}}
	for _, v := range g.Versions() {
		d := v.Document()
		key := versionKey{d.AutomationID, d.VersionID}
		w.roots[key] = d.Root
		w.nodes[key] = map[string]Node{}
		for _, n := range d.Nodes {
			w.nodes[key][n.ID] = n
		}
		report, e := EvaluatePredicates(ctx, v, in.Facts)
		if e != nil {
			return FlowRehearsal{}, e
		}
		w.groups[key] = map[string]Truth{}
		for _, group := range report.Groups {
			w.groups[key][group.ID] = group.Decision
		}
	}
	check, err := CheckAdmissionPredicates(ctx, g.Root(), in.Facts)
	if err != nil {
		return FlowRehearsal{}, err
	}
	root := g.Root().Document()
	key := versionKey{root.AutomationID, root.VersionID}
	w.record(key, root.Trigger.ID, "admission", "checked", check.Reason, 0)
	if !check.Matched {
		out.Status = "blocked"
		out.Reason = check.Reason
		return out, nil
	}
	if err = w.walk(key, root.Root, 0, 1); err != nil {
		return FlowRehearsal{}, err
	}
	return out, nil
}
func rehearsalHash(parts ...string) string {
	raw, _ := json.Marshal(parts)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

type flowWalker struct {
	ctx                              context.Context
	in                               RehearsalInput
	out                              *FlowRehearsal
	nodes                            map[versionKey]map[string]Node
	roots                            map[versionKey]string
	groups                           map[versionKey]map[string]Truth
	sequence, visited, proposalBytes int
	halted                           bool
}

func (w *flowWalker) record(k versionKey, node, kind, outcome, reason string, parent int) int {
	w.sequence++
	if len(w.out.Steps) < w.in.TraceLimit {
		w.out.Steps = append(w.out.Steps, FlowStep{Sequence: w.sequence, Parent: parent, AutomationID: k.automation, VersionID: k.version, NodeID: node, Kind: kind, Outcome: outcome, Reason: reason})
	} else {
		w.out.DroppedSteps++
	}
	return w.sequence
}
func (w *flowWalker) halt(status, reason string) {
	w.halted = true
	w.out.Status = status
	w.out.Reason = reason
}
func (w *flowWalker) walk(k versionKey, id string, parent, depth int) error {
	if err := w.ctx.Err(); err != nil {
		return err
	}
	if w.halted {
		return nil
	}
	if depth > MaxDepth || w.visited >= MaxSteps {
		return ErrRehearsalLimit
	}
	w.visited++
	n, ok := w.nodes[k][id]
	if !ok {
		return ErrRehearsal
	}
	kind, outcome, reason := n.Kind, "visited", "supported"
	switch {
	case n.Kind == "random" || n.Kind == "delay" || n.Kind == "set_output" || n.Parallel || n.Kind == "preset" && len(n.Arguments) > 0:
		w.record(k, id, kind, "unsupported", "unsupported_node", parent)
		w.halt("unsupported", "unsupported_node")
		return nil
	case n.Kind == "stop":
		w.record(k, id, kind, "stopped", "explicit_stop", parent)
		w.halt("stopped", "explicit_stop")
		return nil
	case n.Kind == "action":
		bytes := 0
		for _, a := range n.Arguments {
			bytes += len(a.Name) + len(a.Value)
		}
		if bytes > MaxProposalBytes-w.proposalBytes {
			return ErrRehearsalLimit
		}
		w.proposalBytes += bytes
		seq := w.record(k, id, kind, "proposed", "opaque_arguments", parent)
		w.out.Proposals = append(w.out.Proposals, FlowProposal{Sequence: seq, AutomationID: k.automation, VersionID: k.version, NodeID: id, Capability: n.Capability, Arguments: slices.Clone(n.Arguments)})
		return nil
	}
	seq := w.record(k, id, kind, outcome, reason, parent)
	switch n.Kind {
	case "sequential", "all":
		for _, child := range n.Children {
			if err := w.walk(k, child, seq, depth+1); err != nil {
				return err
			}
		}
	case "loop":
		for range n.Iterations {
			if err := w.walk(k, n.Children[0], seq, depth+1); err != nil {
				return err
			}
		}
	case "condition", "switch":
		selected := len(n.Children) - 1
		for i, group := range n.Conditions {
			value, ok := w.groups[k][group]
			if !ok {
				return ErrRehearsal
			}
			w.record(k, group, "condition_group", "checked", truthReason(value), seq)
			if value == TruthUnknown {
				w.halt("blocked", "condition_unavailable")
				return nil
			}
			if value == TruthTrue {
				selected = i
				break
			}
		}
		return w.walk(k, n.Children[selected], seq, depth+1)
	case "preset":
		target := versionKey{n.Preset.AutomationID, n.Preset.VersionID}
		return w.walk(target, w.roots[target], seq, depth+1)
	default:
		return ErrRehearsal
	}
	return nil
}
func truthReason(v Truth) string {
	switch v {
	case TruthTrue:
		return "matched"
	case TruthFalse:
		return "not_matched"
	default:
		return "unavailable"
	}
}
