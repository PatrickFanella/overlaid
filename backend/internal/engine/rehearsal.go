package engine

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
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
	// Recent is the captured pre-run no-repeat memory; nil retains legacy behavior.
	Recent     map[string]string
	Facts      Facts
	TraceLimit int
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
	// DelayMS is the fixed offset from the occurrence clock at which this
	// proposal would run: the sum of delays before it on its authored path.
	DelayMS int64
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
	// Deferred reports that a delay node was visited. Proposal DelayMS values
	// carry the resulting offsets; a zero offset still runs immediately.
	Deferred bool
	// Recent contains the final simulated picks, never provider authority.
	Recent map[string]string `json:"-"`
}

// RehearseFlow traverses supported captured control flow only. Its API has no
// provider, persistence, clock, variable resolver or effect callback. Root
// predicates are simulated; cooldown, entitlement and permission are not checked.
// Delays are simulated without waiting and mark the result Deferred. Each
// proposal records its offset: sequential children start where the previous
// child ended, a delay shifts its child, and parallel branches share their start
// and end at the latest branch. Offsets above MaxDelayMS fail with
// ErrRehearsalLimit. Parallel branches are walked in authored order. Set output records the declaration
// only. Weighted random picks one child from the captured seed; without a seed
// (capture 1) it stays unsupported. No-repeat additionally requires captured
// memory. Preset arguments remain unsupported without an argument binding.
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
	if ctx == nil || g.Digest() == "" || !occurrence.MatchString(in.OccurrenceID) || !occurrence.MatchString(in.OriginalGraphDigest) || !oneOf(string(in.Mode), string(ModeReproduce), string(ModeCurrentConfig)) || in.Clock.IsZero() || in.Clock.Year() < 1 || in.Clock.Year() > 9999 || !in.Facts.valid || in.Facts.tenant != g.Root().tenant || in.TraceLimit < 1 || in.TraceLimit > MaxSteps || !ValidRecentChoices(in.Recent) || in.Recent != nil && in.Seed == 0 {
		return out, ErrRehearsal
	}
	if in.Mode == ModeReproduce && in.OriginalGraphDigest != g.Digest() {
		return out, ErrRehearsal
	}
	captureDigest, err := in.CaptureDigest()
	if err != nil {
		return out, err
	}
	out = FlowRehearsal{GraphDigest: g.Digest(), OriginalGraphDigest: in.OriginalGraphDigest, CaptureDigest: captureDigest, Mode: in.Mode, Clock: in.Clock.UTC(), Seed: in.Seed, Status: "proposed", Reason: "supported_flow", Steps: []FlowStep{}, Proposals: []FlowProposal{}, Recent: maps.Clone(in.Recent)}
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
	if _, err = w.walk(key, root.Root, 0, 1, 0); err != nil {
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

// walk returns the offset at which the node's subtree finishes, starting at at.
func (w *flowWalker) walk(k versionKey, id string, parent, depth int, at int64) (int64, error) {
	if err := w.ctx.Err(); err != nil {
		return at, err
	}
	if w.halted {
		return at, nil
	}
	if depth > MaxDepth || w.visited >= MaxSteps {
		return at, ErrRehearsalLimit
	}
	w.visited++
	n, ok := w.nodes[k][id]
	if !ok {
		return at, ErrRehearsal
	}
	kind, outcome, reason := n.Kind, "visited", "supported"
	switch {
	case n.Kind == "random" && w.in.Seed > 0 && (!n.NoRepeat || w.in.Recent != nil):
		// A captured seed fixes the weighted choice for this visit, so the same
		// occurrence always takes the same branch. Loops get a fresh draw per visit.
		weights := n.Weights
		reason := "seeded_choice"
		memoryKey := k.automation + "/" + id
		if n.NoRepeat {
			reason = "no_repeat_choice"
			if slices.ContainsFunc(n.Children, func(child string) bool { return child != w.out.Recent[memoryKey] }) {
				weights = slices.Clone(weights)
				for i, child := range n.Children {
					if child == w.out.Recent[memoryKey] {
						weights[i] = 0
					}
				}
			}
		}
		seq := w.record(k, id, kind, "visited", reason, parent)
		child := n.Children[seededChoice(w.in.Seed, k, id, seq, weights)]
		if n.NoRepeat {
			if _, exists := w.out.Recent[memoryKey]; !exists && len(w.out.Recent) >= MaxRecentChoices {
				return at, ErrRehearsalLimit
			}
			w.out.Recent[memoryKey] = child
		}
		return w.walk(k, child, seq, depth+1, at)
	case n.Kind == "random" || n.Kind == "preset" && len(n.Arguments) > 0:
		w.record(k, id, kind, "unsupported", "unsupported_node", parent)
		w.halt("unsupported", "unsupported_node")
		return at, nil
	case n.Kind == "stop":
		w.record(k, id, kind, "stopped", "explicit_stop", parent)
		w.halt("stopped", "explicit_stop")
		return at, nil
	case n.Kind == "action":
		bytes := 0
		for _, a := range n.Arguments {
			bytes += len(a.Name) + len(a.Value)
		}
		if bytes > MaxProposalBytes-w.proposalBytes {
			return at, ErrRehearsalLimit
		}
		w.proposalBytes += bytes
		seq := w.record(k, id, kind, "proposed", "opaque_arguments", parent)
		w.out.Proposals = append(w.out.Proposals, FlowProposal{Sequence: seq, AutomationID: k.automation, VersionID: k.version, NodeID: id, Capability: n.Capability, Arguments: slices.Clone(n.Arguments), DelayMS: at})
		return at, nil
	case n.Kind == "set_output":
		// The authored value is never rendered or exposed by the trace.
		w.record(k, id, kind, "visited", "output_declared", parent)
		return at, nil
	case n.Kind == "delay":
		reason = "simulated_delay"
		w.out.Deferred = true
	case n.Parallel:
		reason = "parallel_branches"
	}
	seq := w.record(k, id, kind, outcome, reason, parent)
	switch n.Kind {
	case "sequential":
		for _, child := range n.Children {
			end, err := w.walk(k, child, seq, depth+1, at)
			if err != nil {
				return at, err
			}
			at = end
		}
		return at, nil
	case "all":
		latest := at
		for _, child := range n.Children {
			end, err := w.walk(k, child, seq, depth+1, at)
			if err != nil {
				return at, err
			}
			latest = max(latest, end)
		}
		return latest, nil
	case "delay":
		if n.DelayMS > MaxDelayMS-at {
			return at, ErrRehearsalLimit
		}
		return w.walk(k, n.Children[0], seq, depth+1, at+n.DelayMS)
	case "loop":
		for range n.Iterations {
			end, err := w.walk(k, n.Children[0], seq, depth+1, at)
			if err != nil {
				return at, err
			}
			at = end
		}
		return at, nil
	case "condition", "switch":
		selected := len(n.Children) - 1
		for i, group := range n.Conditions {
			value, ok := w.groups[k][group]
			if !ok {
				return at, ErrRehearsal
			}
			w.record(k, group, "condition_group", "checked", truthReason(value), seq)
			if value == TruthUnknown {
				w.halt("blocked", "condition_unavailable")
				return at, nil
			}
			if value == TruthTrue {
				selected = i
				break
			}
		}
		return w.walk(k, n.Children[selected], seq, depth+1, at)
	case "preset":
		target := versionKey{n.Preset.AutomationID, n.Preset.VersionID}
		return w.walk(target, w.roots[target], seq, depth+1, at)
	default:
		return at, ErrRehearsal
	}
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

// seededChoice picks a child index with probability proportional to its weight.
// The draw is a domain-separated hash of the captured seed, version, node and
// trace sequence; validated weights sum to at most 1,000,000.
func seededChoice(seed uint64, k versionKey, node string, seq int, weights []int) int {
	sum, _ := hex.DecodeString(rehearsalHash("overlaid.engine.random.v1", strconv.FormatUint(seed, 10), k.automation, k.version, node, strconv.Itoa(seq)))
	total := 0
	for _, w := range weights {
		total += w
	}
	r := int(binary.BigEndian.Uint64(sum[:8]) % uint64(total))
	for i, w := range weights {
		if r < w {
			return i
		}
		r -= w
	}
	return len(weights) - 1
}

func recentCapture(recent map[string]string) *map[string]string {
	if recent == nil {
		return nil
	}
	return &recent
}

// CaptureDigest binds the supplied clock, seed, facts and optional pre-run memory.
// It grants no historical provenance or effect permission.
func (in RehearsalInput) CaptureDigest() (string, error) {
	if in.Clock.IsZero() || !in.Facts.valid || !ValidRecentChoices(in.Recent) || in.Recent != nil && in.Seed == 0 {
		return "", ErrRehearsal
	}
	capture, err := json.Marshal(struct {
		Clock  time.Time
		Seed   uint64
		Facts  map[string]Fact
		Recent *map[string]string `json:",omitempty"`
	}{in.Clock.UTC(), in.Seed, in.Facts.values, recentCapture(in.Recent)})
	if err != nil {
		return "", ErrRehearsal
	}
	return rehearsalHash("overlaid.engine.capture.v1", string(capture)), nil
}
