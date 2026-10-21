// Package engine defines structural automation contracts. It contains no run
// admission, evaluator, authorization, persistence or effect executor.
package engine

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
)

const (
	SchemaVersion     = 1
	MaxDocumentBytes  = 64 * 1024
	MaxDepth          = 16
	MaxSteps          = 100
	MaxLoopIterations = 20
	MaxPredicates     = 100
	MaxTextBytes      = 4096
	MaxDelayMS        = 24 * 60 * 60 * 1000
)

var ErrDocument = errors.New("engine_document_invalid")
var ErrIdentity = errors.New("engine_identity_invalid")
var uuid = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var name = regexp.MustCompile(`^[a-z][a-z0-9_.:-]{0,63}$`)
var occurrence = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Document is untrusted configuration. Validate before retaining its snapshot.
// A capability declaration never represents a current provider grant.
type Document struct {
	SchemaVersion int                   `json:"schemaVersion"`
	TenantID      string                `json:"tenantId"`
	AutomationID  string                `json:"automationId"`
	VersionID     string                `json:"versionId"`
	Trigger       Trigger               `json:"trigger"`
	Capabilities  []platform.Capability `json:"capabilities"`
	Filters       []Predicate           `json:"filters,omitempty"`
	Restrictions  []Restriction         `json:"restrictions"`
	Root          string                `json:"root"`
	Nodes         []Node                `json:"nodes"`
}
type Trigger struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
}
type Restriction struct {
	ID         string      `json:"id"`
	Mode       string      `json:"mode"`
	Predicates []Predicate `json:"predicates"`
}
type Predicate struct {
	ID       string `json:"id"`
	Field    string `json:"field"`
	Operator string `json:"operator"`
	Value    string `json:"value"`
}
type Argument struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}
type Preset struct {
	TenantID     string `json:"tenantId"`
	AutomationID string `json:"automationId"`
	VersionID    string `json:"versionId"`
}

// Node describes structure, not executable behavior. Conditions refer to saved
// restriction groups; switch children end with a required default branch.
// Preset bodies must be resolved and budgeted by a later tenant-scoped boundary.
type Node struct {
	ID          string              `json:"id"`
	Kind        string              `json:"kind"`
	Children    []string            `json:"children,omitempty"`
	Conditions  []string            `json:"conditions,omitempty"`
	Weights     []int               `json:"weights,omitempty"`
	Iterations  int                 `json:"iterations,omitempty"`
	DelayMS     int64               `json:"delayMs,omitempty"`
	Parallel    bool                `json:"parallel,omitempty"`
	Concurrency int                 `json:"concurrency,omitempty"`
	NoRepeat    bool                `json:"noRepeat,omitempty"`
	Capability  platform.Capability `json:"capability,omitempty"`
	Preset      *Preset             `json:"preset,omitempty"`
	Arguments   []Argument          `json:"arguments,omitempty"`
}

// Validated holds only privately owned canonical bytes and fixed metadata. Its
// zero value is invalid. Exported copies cannot alter a validated snapshot.
type Validated struct {
	raw        string
	digest     string
	tenant     string
	automation string
	version    string
}

func (v Validated) JSON() []byte   { return []byte(v.raw) }
func (v Validated) Digest() string { return v.digest }
func (v Validated) Document() Document {
	var d Document
	_ = json.Unmarshal([]byte(v.raw), &d)
	return d
}

// Parse refuses unknown and duplicate object fields, trailing input, invalid
// UTF-8 and oversized input before validating the typed document.
func Parse(raw []byte) (Validated, error) {
	if len(raw) == 0 || len(raw) > MaxDocumentBytes || !utf8.Valid(raw) {
		return Validated{}, ErrDocument
	}
	keys := json.NewDecoder(bytes.NewReader(raw))
	if err := uniqueKeys(keys, 0); err != nil {
		return Validated{}, ErrDocument
	}
	if _, err := keys.Token(); err != io.EOF {
		return Validated{}, ErrDocument
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var d Document
	if err := decoder.Decode(&d); err != nil {
		return Validated{}, ErrDocument
	}
	return Validate(d)
}

// Validate copies before normalization. Callers must not mutate input
// concurrently with this call; after return the result is independently owned.
func Validate(input Document) (Validated, error) {
	if !validDocument(input) {
		return Validated{}, ErrDocument
	}
	raw, err := json.Marshal(input)
	if err != nil || len(raw) > MaxDocumentBytes {
		return Validated{}, ErrDocument
	}
	var d Document
	if json.Unmarshal(raw, &d) != nil || !validDocument(d) {
		return Validated{}, ErrDocument
	}
	slices.Sort(d.Capabilities)
	slices.SortFunc(d.Nodes, func(a, b Node) int { return strings.Compare(a.ID, b.ID) })
	// Normalize semantically empty collections for stable reconstruction.
	if len(d.Capabilities) == 0 {
		d.Capabilities = nil
	}
	if len(d.Restrictions) == 0 {
		d.Restrictions = nil
	}
	for i := range d.Restrictions {
		if len(d.Restrictions[i].Predicates) == 0 {
			d.Restrictions[i].Predicates = nil
		}
	}
	raw, err = json.Marshal(d)
	if err != nil {
		return Validated{}, ErrDocument
	}
	digest := sha256.Sum256(append([]byte("overlaid.engine.document.v1\x00"), raw...))
	return Validated{raw: string(raw), digest: hex.EncodeToString(digest[:]), tenant: d.TenantID, automation: d.AutomationID, version: d.VersionID}, nil
}

func validDocument(d Document) bool {
	if len(d.Capabilities) > 64 || len(d.Restrictions) > MaxPredicates || !validName(d.Root) || d.SchemaVersion != SchemaVersion || !validUUID(d.TenantID) || !validUUID(d.AutomationID) || !validUUID(d.VersionID) || !validName(d.Trigger.ID) || !oneOf(d.Trigger.Kind, "command", "keyword", "event", "timer", "cron", "reward", "webhook", "dashboard", "counter", "api") || len(d.Nodes) == 0 || len(d.Nodes) > MaxSteps {
		return false
	}
	known := map[platform.Capability]bool{}
	for _, definition := range platform.Registry() {
		known[definition.Name] = true
	}
	caps := map[platform.Capability]bool{}
	for _, capability := range d.Capabilities {
		if !known[capability] || caps[capability] {
			return false
		}
		caps[capability] = true
	}
	ids := map[string]bool{d.Trigger.ID: true}
	groups := map[string]bool{}
	count := len(d.Filters)
	if count > MaxPredicates {
		return false
	}
	for _, p := range d.Filters {
		if !validPredicate(ids, p) {
			return false
		}
	}
	for _, g := range d.Restrictions {
		if !takeID(ids, g.ID) || !oneOf(g.Mode, "all", "any", "none") {
			return false
		}
		groups[g.ID] = true
		if len(g.Predicates) > MaxPredicates-count {
			return false
		}
		count += len(g.Predicates)
		if count > MaxPredicates {
			return false
		}
		for _, p := range g.Predicates {
			if !validPredicate(ids, p) {
				return false
			}
		}
	}
	nodes := map[string]Node{}
	for _, n := range d.Nodes {
		if !takeID(ids, n.ID) || !validNode(n, d, caps, groups) {
			return false
		}
		nodes[n.ID] = n
	}
	visiting := map[string]bool{}
	memo := map[string]measure{}
	m, ok := walk(d.Root, nodes, visiting, memo)
	return ok && m.depth <= MaxDepth && m.steps <= MaxSteps && len(memo) == len(nodes)
}

type measure struct{ depth, steps int }

func walk(id string, nodes map[string]Node, visiting map[string]bool, memo map[string]measure) (measure, bool) {
	if m, ok := memo[id]; ok {
		return m, true
	}
	n, ok := nodes[id]
	if !ok || visiting[id] || len(visiting) >= MaxDepth {
		return measure{}, false
	}
	visiting[id] = true
	m := measure{depth: 1, steps: 1}
	sum, maxChild := 0, 0
	for _, child := range n.Children {
		c, ok := walk(child, nodes, visiting, memo)
		if !ok {
			return measure{}, false
		}
		m.depth = max(m.depth, c.depth+1)
		sum += c.steps
		maxChild = max(maxChild, c.steps)
		if m.depth > MaxDepth || sum > MaxSteps && !oneOf(n.Kind, "random", "condition", "switch") {
			return measure{}, false
		}
	}
	delete(visiting, id)
	switch n.Kind {
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
	if m.steps > MaxSteps {
		return measure{}, false
	}
	memo[id] = m
	return m, true
}

func validNode(n Node, d Document, caps map[platform.Capability]bool, groups map[string]bool) bool {
	if len(n.Children) > MaxSteps || len(n.Arguments) > 32 || len(n.Conditions) > MaxPredicates || len(n.Weights) > MaxSteps {
		return false
	}
	for _, child := range n.Children {
		if !validName(child) {
			return false
		}
	}
	seen := map[string]bool{}
	for _, a := range n.Arguments {
		if !validName(a.Name) || seen[a.Name] || !validText(a.Value) {
			return false
		}
		seen[a.Name] = true
	}
	for _, g := range n.Conditions {
		if !validName(g) || !groups[g] {
			return false
		}
	}
	// Reject irrelevant fields instead of silently accepting another kind's
	// configuration. All effect kinds below are structural declarations only.
	if n.Iterations != 0 && n.Kind != "loop" || n.DelayMS != 0 && n.Kind != "delay" || (n.Parallel || n.Concurrency != 0) && n.Kind != "all" || n.NoRepeat && n.Kind != "random" || len(n.Weights) > 0 && n.Kind != "random" || len(n.Conditions) > 0 && !oneOf(n.Kind, "condition", "switch") || n.Preset != nil && n.Kind != "preset" || n.Capability != "" && n.Kind != "action" || len(n.Arguments) > 0 && !oneOf(n.Kind, "action", "preset", "set_output") {
		return false
	}
	switch n.Kind {
	case "sequential":
		return len(n.Children) > 0
	case "all":
		return len(n.Children) > 0 && ((!n.Parallel && n.Concurrency == 0) || (n.Parallel && n.Concurrency >= 1 && n.Concurrency <= 4))
	case "random":
		if len(n.Children) == 0 || len(n.Weights) != len(n.Children) {
			return false
		}
		total := 0
		for _, w := range n.Weights {
			if w < 1 || w > 1_000_000-total {
				return false
			}
			total += w
		}
		return true
	case "condition":
		return len(n.Children) == 2 && len(n.Conditions) == 1
	case "switch":
		return len(n.Conditions) > 0 && len(n.Children) == len(n.Conditions)+1
	case "loop":
		return len(n.Children) == 1 && n.Iterations >= 1 && n.Iterations <= MaxLoopIterations
	case "delay":
		return len(n.Children) == 1 && n.DelayMS >= 1 && n.DelayMS <= MaxDelayMS
	case "stop":
		return len(n.Children) == 0
	case "set_output":
		return len(n.Children) == 0 && len(n.Arguments) == 1
	case "action":
		return len(n.Children) == 0 && caps[n.Capability]
	case "preset":
		return len(n.Children) == 0 && n.Preset != nil && n.Preset.TenantID == d.TenantID && validUUID(n.Preset.AutomationID) && validUUID(n.Preset.VersionID) && n.Preset.AutomationID != d.AutomationID
	default:
		return false
	}
}

func validPredicate(ids map[string]bool, p Predicate) bool {
	return takeID(ids, p.ID) && validName(p.Field) && oneOf(p.Operator, "eq", "neq", "exists", "gte", "lte") && validText(p.Value) && (p.Operator != "exists" || p.Value == "")
}

func validName(s string) bool { return len(s) <= 64 && name.MatchString(s) }

func validUUID(s string) bool {
	return len(s) == 36 && uuid.MatchString(s) && s != "00000000-0000-0000-0000-000000000000"
}
func takeID(ids map[string]bool, id string) bool {
	if !validName(id) || ids[id] {
		return false
	}
	ids[id] = true
	return true
}
func validText(s string) bool {
	return len(s) <= MaxTextBytes && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}
func oneOf(s string, values ...string) bool { return slices.Contains(values, s) }

// encoding/json matches struct fields case-insensitively, including Unicode
// simple-fold aliases. Treat those aliases as duplicate fields too.
func foldField(s string) string {
	return strings.Map(func(r rune) rune {
		smallest := r
		for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
			smallest = min(smallest, next)
		}
		return smallest
	}, s)
}

func uniqueKeys(d *json.Decoder, depth int) error {
	if depth > 16 {
		return ErrDocument
	}
	token, err := d.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return err
			}
			s, ok := key.(string)
			if !ok || len(s) > 64 {
				return ErrDocument
			}
			keyName := foldField(s)
			if seen[keyName] {
				return ErrDocument
			}
			seen[keyName] = true
			if err := uniqueKeys(d, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err := uniqueKeys(d, depth+1); err != nil {
				return err
			}
		}
	default:
		return ErrDocument
	}
	_, err = d.Token()
	return err
}

// Mode binds live admission and the two distinct rehearsal identities. It does
// not select an executor, capture inputs or allow replay of an existing effect.
type Mode string

const (
	ModeExecute       Mode = "execute"
	ModeReproduce     Mode = "reproduce"
	ModeCurrentConfig Mode = "current_config"
)

func (v Validated) RunID(eventID string, mode Mode) (string, error) {
	if v.raw == "" || !occurrence.MatchString(eventID) || !oneOf(string(mode), string(ModeExecute), string(ModeReproduce), string(ModeCurrentConfig)) {
		return "", ErrIdentity
	}
	// JSON array encoding prevents ambiguous concatenation and pins field order.
	raw, err := json.Marshal([]string{"overlaid.engine.run.v1", v.tenant, eventID, v.automation, v.version, v.digest, string(mode)})
	if err != nil {
		return "", ErrIdentity
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
