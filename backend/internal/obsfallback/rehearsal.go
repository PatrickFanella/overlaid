// Package obsfallback binds a missing-input rehearsal to an exact engine graph.
// Supplied observations are evidence only; this package has no effect callback.
package obsfallback

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"git.subcult.tv/PatrickFanella/over-laid/internal/engine"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
)

// Freshness is the exclusive lifetime of a missing-input proposal.
const Freshness = 10 * time.Second

// ErrInvalid reports malformed or mismatched private rule/observation input.
var ErrInvalid = errors.New("obs_fallback_invalid")
var hash = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Spec is private workspace configuration, never a provider permission.
type Spec struct {
	TenantID, AutomationID, VersionID, ConnectionID string
	Collection, Input, Scene                        string
}

// Rule owns one exact immutable scene-action graph and connection binding.
type Rule struct {
	spec  Spec
	graph engine.PinnedGraph
}

// Spec returns a copy of the rule configuration.
func (r Rule) Spec() Spec { return r.spec }

// Graph returns the immutable pinned graph.
func (r Rule) Graph() engine.PinnedGraph { return r.graph }

// NewRule builds the bounded engine graph used by the fallback rule. It opens no
// connection and does not publish a version or register an executor.
func NewRule(s Spec) (Rule, error) {
	if !hash.MatchString(s.ConnectionID) || !validText(s.Collection) || !validText(s.Input) || !validText(s.Scene) {
		return Rule{}, ErrInvalid
	}
	d := engine.Document{SchemaVersion: engine.AdmissionSchemaVersion, TenantID: s.TenantID, AutomationID: s.AutomationID, VersionID: s.VersionID,
		Trigger: engine.Trigger{ID: "obs_missing_input", Kind: "event"}, Capabilities: []platform.Capability{"obs.scene.set"},
		Filters: []engine.Predicate{
			{ID: "collection", Field: "obs.collection", Operator: "eq", Value: s.Collection},
			{ID: "input", Field: "obs.input", Operator: "eq", Value: s.Input},
			{ID: "missing", Field: "obs.missing", Operator: "eq", Value: "true"},
			{ID: "target", Field: "obs.target", Operator: "eq", Value: "true"},
		}, Restrictions: []engine.Restriction{}, Admission: &engine.AdmissionPolicy{Groups: []string{}}, Root: "fallback",
		Nodes: []engine.Node{{ID: "fallback", Kind: "action", Capability: "obs.scene.set", Arguments: []engine.Argument{{Name: "scene", Value: s.Scene}}}}}
	v, err := engine.Validate(d)
	if err != nil {
		return Rule{}, ErrInvalid
	}
	g, err := engine.ResolvePinned(v, nil)
	if err != nil {
		return Rule{}, ErrInvalid
	}
	return Rule{spec: s, graph: g}, nil
}

// RestoreRule refuses graphs outside the exact rule shape, including additional
// effects, presets or predicates. Saved graph validation is not effect approval.
func RestoreRule(g engine.PinnedGraph, connection string) (Rule, error) {
	d := g.Root().Document()
	if len(d.Filters) != 4 || len(d.Nodes) != 1 || len(d.Nodes[0].Arguments) != 1 {
		return Rule{}, ErrInvalid
	}
	r, err := NewRule(Spec{TenantID: d.TenantID, AutomationID: d.AutomationID, VersionID: d.VersionID, ConnectionID: connection, Collection: d.Filters[0].Value, Input: d.Filters[1].Value, Scene: d.Nodes[0].Arguments[0].Value})
	if err != nil || g.Digest() != r.graph.Digest() {
		return Rule{}, ErrInvalid
	}
	return r, nil
}

// InputState distinguishes an absent input from intentionally suppressed input
// and from evidence that cannot establish absence.
type InputState string

// Fixed observed states never imply effect authorization.
const (
	Missing      InputState = "missing"
	Present      InputState = "present"
	Muted        InputState = "muted"
	Hidden       InputState = "hidden"
	Disconnected InputState = "disconnected"
	Unknown      InputState = "unknown"
)

// Observation is supplied original evidence. Trusted persistence must establish
// the source and connection binding; Synthetic input can never authorize effects.
type Observation struct {
	ID, RuleVersionID, GraphDigest, ConnectionID string
	Collection, Input, Scene                     string
	ObservedAt                                   time.Time
	State                                        InputState
	TargetAvailable, Synthetic                   bool
}

// Report explains one pure rehearsal; even a nonsynthetic proposal grants no
// effect claim. Approval must recheck current state and retain a one-use claim.
type Report struct {
	ProposalID, ObservationID, RuleVersionID, GraphDigest string
	Condition                                             InputState
	Reason, Scene                                         string
	ObservedAt, Deadline                                  time.Time
	Synthetic                                             bool
	Flow                                                  engine.FlowRehearsal
}

// Rehearse evaluates at an explicitly supplied clock. Saved replay supplies the
// original evaluation clock; live approval must use a fresh trusted clock and
// observation. No runtime clock, provider, persistence or effect is consulted.
func Rehearse(ctx context.Context, r Rule, o Observation, now time.Time) (out Report, failure error) {
	defer func() {
		if ctx != nil && ctx.Err() != nil {
			failure = ctx.Err()
		}
		if failure != nil {
			out = Report{}
		}
	}()
	s := r.spec
	if ctx == nil || r.graph.Digest() == "" || !hash.MatchString(o.ID) || o.RuleVersionID != s.VersionID || o.GraphDigest != r.graph.Digest() || o.ConnectionID != s.ConnectionID || o.Collection != s.Collection || o.Input != s.Input || o.Scene != s.Scene || !validTime(o.ObservedAt) || !validTime(now) {
		return out, ErrInvalid
	}
	switch o.State {
	case Missing, Present, Muted, Hidden, Disconnected, Unknown:
	default:
		return out, ErrInvalid
	}
	deadline := o.ObservedAt.Add(Freshness)
	if !validTime(deadline) {
		return out, ErrInvalid
	}
	reason := "input_" + string(o.State)
	available := o.State != Disconnected && o.State != Unknown
	if now.Before(o.ObservedAt) || !now.Before(deadline) {
		reason = "observation_stale"
		available = false
	} else if o.State == Disconnected {
		reason = "connection_lost"
	} else if o.State == Missing && !o.TargetAvailable {
		reason = "target_unavailable"
	}
	facts := map[string]engine.Fact{}
	if available {
		facts["obs.collection"] = engine.Fact{State: engine.FactAvailable, Kind: engine.FactText, Text: o.Collection}
		facts["obs.input"] = engine.Fact{State: engine.FactAvailable, Kind: engine.FactText, Text: o.Input}
		facts["obs.missing"] = engine.Fact{State: engine.FactAvailable, Kind: engine.FactBoolean, Boolean: o.State == Missing}
		facts["obs.target"] = engine.Fact{State: engine.FactAvailable, Kind: engine.FactBoolean, Boolean: o.TargetAvailable}
	}
	captured, err := engine.NewFacts(s.TenantID, facts)
	if err != nil {
		return out, ErrInvalid
	}
	flow, err := engine.RehearseFlow(ctx, r.graph, engine.RehearsalInput{OccurrenceID: o.ID, OriginalGraphDigest: r.graph.Digest(), Mode: engine.ModeReproduce, Clock: o.ObservedAt, Facts: captured, TraceLimit: engine.MaxSteps})
	if err != nil {
		return out, err
	}
	out = Report{ObservationID: o.ID, RuleVersionID: s.VersionID, GraphDigest: r.graph.Digest(), Condition: o.State, Reason: reason, Scene: s.Scene, ObservedAt: o.ObservedAt, Deadline: deadline, Synthetic: o.Synthetic, Flow: flow}
	if flow.Status == "proposed" {
		raw, err := json.Marshal([]any{"overlaid.engine.obs-fallback-proposal.v1", r.graph.Digest(), s.ConnectionID, o.ID, flow.ID, o.Synthetic})
		if err != nil {
			return Report{}, ErrInvalid
		}
		sum := sha256.Sum256(raw)
		out.ProposalID = hex.EncodeToString(sum[:])
	}
	return out, nil
}
func validText(v string) bool {
	return len(v) > 0 && len(v) <= 255 && utf8.ValidString(v) && !strings.ContainsRune(v, 0)
}
func validTime(t time.Time) bool { return !t.IsZero() && t.Year() >= 1 && t.Year() <= 9999 }
