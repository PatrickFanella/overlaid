package engine

import "context"

const MaxPerStreamLimit = 10000

// AdmissionPolicy is immutable root-version configuration, not caller-supplied
// permission. Zero limits disable those declared restrictions, not provider/plan
// budgets. Reserving them still requires a separate transaction boundary.
type AdmissionPolicy struct {
	Groups         []string `json:"groups"`
	CooldownMS     int64    `json:"cooldownMs"`
	PerStreamLimit int      `json:"perStreamLimit"`
}

func validAdmission(d Document, groups map[string]bool) bool {
	if d.SchemaVersion == SchemaVersion {
		return d.Admission == nil
	}
	p := d.Admission
	if p == nil || p.Groups == nil || len(p.Groups) > MaxPredicates || p.CooldownMS < 0 || p.CooldownMS > MaxDelayMS || p.PerStreamLimit < 0 || p.PerStreamLimit > MaxPerStreamLimit {
		return false
	}
	seen := map[string]bool{}
	for _, id := range p.Groups {
		if !groups[id] || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}

type AdmissionPredicateCheck struct {
	Matched         bool
	Reason, GroupID string
	Report          PredicateReport
}

// CheckAdmissionPredicates checks only this version's explicitly selected groups
// plus its filters. Matching proves no current permission/entitlement, stream
// identity, cooldown reservation, run uniqueness or effect authorization.
func CheckAdmissionPredicates(ctx context.Context, v Validated, facts Facts) (out AdmissionPredicateCheck, err error) {
	defer func() {
		if ctx != nil && ctx.Err() != nil {
			out = AdmissionPredicateCheck{}
			err = ctx.Err()
		}
	}()
	report, err := EvaluatePredicates(ctx, v, facts)
	if err != nil {
		return AdmissionPredicateCheck{}, err
	}
	out.Report = report
	d := v.Document()
	if d.Admission == nil {
		out.Reason = "policy_missing"
		return out, nil
	}
	if report.Filters.Decision != TruthTrue {
		out.Reason = "filters_rejected"
		if report.Filters.Decision == TruthUnknown {
			out.Reason = "filters_unavailable"
		}
		return out, nil
	}
	groups := make(map[string]GroupOutcome, len(report.Groups))
	for _, g := range report.Groups {
		groups[g.ID] = g
	}
	for _, id := range d.Admission.Groups {
		g, exists := groups[id]
		if !exists {
			return AdmissionPredicateCheck{}, ErrPredicateEvaluation
		}
		if g.Decision != TruthTrue {
			out.GroupID = id
			out.Reason = "group_rejected"
			if g.Decision == TruthUnknown {
				out.Reason = "group_unavailable"
			}
			return out, nil
		}
	}
	out.Matched = true
	out.Reason = "matched"
	return out, nil
}
