package engine

import (
	"context"
	"errors"
	"strconv"
	"strings"
)

const (
	MaxFacts     = 128
	MaxFactBytes = 64 * 1024 // Names plus text values; not an exact heap limit.
)

type FactState uint8

const (
	FactMissing FactState = iota
	FactAvailable
	FactStale
	FactDisconnected
)

type FactKind string

const (
	FactText    FactKind = "text"
	FactBoolean FactKind = "boolean"
	FactInteger FactKind = "integer"
)

type Fact struct {
	State   FactState
	Kind    FactKind
	Text    string
	Boolean bool
	Integer int64
}

var ErrFacts = errors.New("engine_facts_invalid")
var ErrPredicateEvaluation = errors.New("engine_predicate_evaluation_invalid")

// Facts owns supplied data only. A producer must establish source, availability
// and freshness independently; construction authenticates no provider or tenant.
type Facts struct {
	tenant string
	values map[string]Fact
	valid  bool
}

func NewFacts(tenant string, input map[string]Fact) (Facts, error) {
	if !validUUID(tenant) || len(input) > MaxFacts {
		return Facts{}, ErrFacts
	}
	f := Facts{tenant: strings.Clone(tenant), values: make(map[string]Fact, len(input))}
	bytes := 0
	for name, value := range input {
		if !validName(name) || value.State > FactDisconnected {
			return Facts{}, ErrFacts
		}
		if value.State != FactAvailable {
			if value.Kind != "" || value.Text != "" || value.Boolean || value.Integer != 0 {
				return Facts{}, ErrFacts
			}
		} else {
			switch value.Kind {
			case FactText:
				if !validText(value.Text) || value.Boolean || value.Integer != 0 {
					return Facts{}, ErrFacts
				}
			case FactBoolean:
				if value.Text != "" || value.Integer != 0 {
					return Facts{}, ErrFacts
				}
			case FactInteger:
				if value.Text != "" || value.Boolean {
					return Facts{}, ErrFacts
				}
			default:
				return Facts{}, ErrFacts
			}
		}
		bytes += len(name) + len(value.Text)
		if bytes > MaxFactBytes {
			return Facts{}, ErrFacts
		}
		value.Text = strings.Clone(value.Text)
		f.values[strings.Clone(name)] = value
	}
	f.valid = true
	return f, nil
}

// Truth is a three-valued predicate outcome. Unknown is never inverted to true.
type Truth uint8

const (
	TruthUnknown Truth = iota
	TruthFalse
	TruthTrue
)

type PredicateOutcome struct {
	ID       string
	Decision Truth
	Reason   string
}
type GroupOutcome struct {
	ID, Mode   string
	Decision   Truth
	Predicates []PredicateOutcome
}
type PredicateReport struct {
	VersionID, DocumentDigest string
	Filters                   GroupOutcome
	Groups                    []GroupOutcome
}

// EvaluatePredicates reports filters and every named group independently. It
// does not choose which groups are admission gates versus condition selectors,
// combine them into authorization, reserve work or evaluate node effects.
func EvaluatePredicates(ctx context.Context, v Validated, facts Facts) (PredicateReport, error) {
	if ctx == nil || v.raw == "" || !facts.valid || facts.tenant != v.tenant {
		return PredicateReport{}, ErrPredicateEvaluation
	}
	if err := ctx.Err(); err != nil {
		return PredicateReport{}, err
	}
	d := v.Document()
	report := PredicateReport{VersionID: d.VersionID, DocumentDigest: v.Digest()}
	var err error
	report.Filters, err = evaluateGroup(ctx, Restriction{Mode: "all", Predicates: d.Filters}, facts)
	if err != nil {
		return PredicateReport{}, err
	}
	for _, group := range d.Restrictions {
		outcome, err := evaluateGroup(ctx, group, facts)
		if err != nil {
			return PredicateReport{}, err
		}
		report.Groups = append(report.Groups, outcome)
	}
	if err := ctx.Err(); err != nil {
		return PredicateReport{}, err
	}
	return report, nil
}

func evaluateGroup(ctx context.Context, g Restriction, f Facts) (GroupOutcome, error) {
	out := GroupOutcome{ID: g.ID, Mode: g.Mode}
	for _, p := range g.Predicates {
		if err := ctx.Err(); err != nil {
			return GroupOutcome{}, err
		}
		out.Predicates = append(out.Predicates, evaluatePredicate(p, f.values[p.Field]))
	}
	out.Decision = combinePredicates(g.Mode, out.Predicates)
	return out, nil
}

func combinePredicates(mode string, outcomes []PredicateOutcome) Truth {
	unknown := false
	for _, p := range outcomes {
		if mode == "all" && p.Decision == TruthFalse {
			return TruthFalse
		}
		if mode != "all" && p.Decision == TruthTrue {
			if mode == "none" {
				return TruthFalse
			}
			return TruthTrue
		}
		unknown = unknown || p.Decision == TruthUnknown
	}
	if unknown {
		return TruthUnknown
	}
	if mode == "any" {
		return TruthFalse
	}
	return TruthTrue
}

func evaluatePredicate(p Predicate, f Fact) PredicateOutcome {
	out := PredicateOutcome{ID: p.ID, Decision: TruthUnknown}
	switch f.State {
	case FactMissing:
		out.Reason = "missing"
		return out
	case FactStale:
		out.Reason = "stale"
		return out
	case FactDisconnected:
		out.Reason = "disconnected"
		return out
	}
	if p.Operator == "exists" {
		out.Decision = TruthTrue
		out.Reason = "matched"
		return out
	}
	matched := false
	if p.Operator == "gte" || p.Operator == "lte" {
		if f.Kind != FactInteger {
			out.Reason = "type_mismatch"
			return out
		}
		value, ok := predicateInteger(p.Value)
		if !ok {
			out.Reason = "invalid_literal"
			return out
		}
		if p.Operator == "gte" {
			matched = f.Integer >= value
		} else {
			matched = f.Integer <= value
		}
	} else {
		switch f.Kind {
		case FactText:
			matched = f.Text == p.Value
		case FactBoolean:
			if p.Value != "true" && p.Value != "false" {
				out.Reason = "invalid_literal"
				return out
			}
			matched = f.Boolean == (p.Value == "true")
		case FactInteger:
			value, ok := predicateInteger(p.Value)
			if !ok {
				out.Reason = "invalid_literal"
				return out
			}
			matched = f.Integer == value
		}
		if p.Operator == "neq" {
			matched = !matched
		}
	}
	if matched {
		out.Decision = TruthTrue
		out.Reason = "matched"
	} else {
		out.Decision = TruthFalse
		out.Reason = "not_matched"
	}
	return out
}

func predicateInteger(text string) (int64, bool) {
	value, err := strconv.ParseInt(text, 10, 64)
	return value, err == nil && strconv.FormatInt(value, 10) == text
}
