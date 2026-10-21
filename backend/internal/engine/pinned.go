package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
)

const (
	MaxPinnedVersions = 16         // Includes the root version.
	MaxPinnedBytes    = 256 * 1024 // Canonical document bytes, not Go heap size.
)

var ErrPinnedGraph = errors.New("engine_pinned_graph_invalid")

type versionKey struct{ automation, version string }
type pinnedNode struct {
	versionKey
	node string
}

// PinnedGraph owns immutable supplied versions. Resolution proves structure and
// bounded expanded cost only, not tenant authorization, persistence or effects.
type PinnedGraph struct {
	root         Validated
	versions     []Validated
	capabilities []platform.Capability
	digest       string
	depth, steps int
}

func (g PinnedGraph) Root() Validated                     { return g.root }
func (g PinnedGraph) Versions() []Validated               { return slices.Clone(g.versions) }
func (g PinnedGraph) Capabilities() []platform.Capability { return slices.Clone(g.capabilities) }
func (g PinnedGraph) Digest() string                      { return g.digest }
func (g PinnedGraph) Depth() int                          { return g.depth }
func (g PinnedGraph) Steps() int                          { return g.steps }

// ResolvePinned requires every transitively referenced exact version and rejects
// unused supplied bodies. A future tenant store must load immutable versions in
// its authorized scope; this API performs no lookup or current-pointer fallback.
func ResolvePinned(root Validated, dependencies []Validated) (PinnedGraph, error) {
	if root.raw == "" || len(dependencies) >= MaxPinnedVersions {
		return PinnedGraph{}, ErrPinnedGraph
	}
	versions := make(map[versionKey]Validated, len(dependencies)+1)
	nodes := make(map[versionKey]map[string]Node, len(dependencies)+1)
	roots := make(map[versionKey]string, len(dependencies)+1)
	all := make([]Validated, 0, len(dependencies)+1)
	all = append(all, root)
	all = append(all, dependencies...)
	bytes := 0
	for _, v := range all {
		if v.raw == "" || v.tenant != root.tenant || len(v.raw) > MaxPinnedBytes-bytes {
			return PinnedGraph{}, ErrPinnedGraph
		}
		bytes += len(v.raw)
		key := versionKey{v.automation, v.version}
		if _, exists := versions[key]; exists {
			return PinnedGraph{}, ErrPinnedGraph
		}
		versions[key] = v
		d := v.Document()
		roots[key] = d.Root
		nodes[key] = make(map[string]Node, len(d.Nodes))
		for _, n := range d.Nodes {
			nodes[key][n.ID] = n
		}
	}
	w := pinnedWalker{nodes: nodes, roots: roots, visiting: map[pinnedNode]bool{}, memo: map[pinnedNode]measure{}, used: map[versionKey]bool{}}
	m, ok := w.walk(pinnedNode{versionKey: versionKey{root.automation, root.version}, node: roots[versionKey{root.automation, root.version}]})
	if !ok || m.depth > MaxDepth || m.steps > MaxSteps || len(w.used) != len(versions) {
		return PinnedGraph{}, ErrPinnedGraph
	}
	slices.SortFunc(all, func(a, b Validated) int {
		if n := strings.Compare(a.automation, b.automation); n != 0 {
			return n
		}
		return strings.Compare(a.version, b.version)
	})
	caps := map[platform.Capability]bool{}
	identities := make([][3]string, 0, len(all))
	for _, v := range all {
		identities = append(identities, [3]string{v.automation, v.version, v.digest})
		for _, capability := range v.Document().Capabilities {
			caps[capability] = true
		}
	}
	capabilities := make([]platform.Capability, 0, len(caps))
	for c := range caps {
		capabilities = append(capabilities, c)
	}
	slices.Sort(capabilities)
	// Root selection and every exact body are bound independently of input order.
	raw, _ := json.Marshal(struct {
		Domain, Tenant, Automation, Version, RootDigest string
		Versions                                        [][3]string
	}{"overlaid.engine.pinned.v1", root.tenant, root.automation, root.version, root.digest, identities})
	digest := sha256.Sum256(raw)
	return PinnedGraph{root: root, versions: all, capabilities: capabilities, digest: hex.EncodeToString(digest[:]), depth: m.depth, steps: m.steps}, nil
}

type pinnedWalker struct {
	nodes    map[versionKey]map[string]Node
	roots    map[versionKey]string
	visiting map[pinnedNode]bool
	memo     map[pinnedNode]measure
	used     map[versionKey]bool
}

func (w *pinnedWalker) walk(key pinnedNode) (measure, bool) {
	if m, ok := w.memo[key]; ok {
		return m, true
	}
	n, ok := w.nodes[key.versionKey][key.node]
	if !ok || w.visiting[key] || len(w.visiting) >= MaxDepth {
		return measure{}, false
	}
	w.visiting[key] = true
	w.used[key.versionKey] = true
	m := measure{depth: 1, steps: 1}
	sum, maxChild := 0, 0
	for _, id := range n.Children {
		child, ok := w.walk(pinnedNode{versionKey: key.versionKey, node: id})
		if !ok {
			return measure{}, false
		}
		m.depth = max(m.depth, 1+child.depth)
		sum += child.steps
		maxChild = max(maxChild, child.steps)
	}
	switch n.Kind {
	case "preset":
		target := versionKey{n.Preset.AutomationID, n.Preset.VersionID}
		child, ok := w.walk(pinnedNode{versionKey: target, node: w.roots[target]})
		if !ok {
			return measure{}, false
		}
		m.depth = 1 + child.depth
		m.steps += child.steps
	case "random", "condition", "switch":
		m.steps += maxChild
	case "loop":
		if sum > (MaxSteps-1)/n.Iterations {
			return measure{}, false
		}
		m.steps += sum * n.Iterations
	default:
		m.steps += sum
	}
	delete(w.visiting, key)
	if m.depth > MaxDepth || m.steps > MaxSteps {
		return measure{}, false
	}
	w.memo[key] = m
	return m, true
}

// RunID additionally binds the complete pinned graph. A local Validated.RunID
// alone cannot distinguish changed dependency bodies under the same version IDs.
// Neither identity permits admission or replay of an already claimed effect.
func (g PinnedGraph) RunID(event string, mode Mode) (string, error) {
	if g.digest == "" {
		return "", ErrIdentity
	}
	root, err := g.root.RunID(event, mode)
	if err != nil {
		return "", err
	}
	raw, _ := json.Marshal([]string{"overlaid.engine.pinned-run.v1", root, g.digest})
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}
