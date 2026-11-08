package tenancy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/obsfallback"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

// OBSFallbackStore borrows one immutable tenant product scope. It grants no
// controller ownership, provider authorization, effect claim or transport.
type OBSFallbackStore struct {
	automation *AutomationStore
	db         *tenantdb.DB
}

var (
	ErrOBSFallback         = errors.New("obs_fallback_unavailable")
	ErrOBSFallbackStale    = errors.New("obs_fallback_stale")
	ErrOBSFallbackCooldown = errors.New("obs_fallback_cooldown")
)

const obsProposalLimit = 1000
const obsProposalSpacing = 30 * time.Second

// OBSFallbackRuleRecord is private configure metadata; HTTP callers must project
// an allowlist rather than serialize this internal binding.
type OBSFallbackRuleRecord struct {
	Rule                        obsfallback.Rule
	ConfiguredBy                string
	Enabled, Latched, Unchanged bool
	Revision                    int64
	UpdatedAt                   time.Time
	LatestObservation           *obsfallback.Observation
	ActiveProposal              string
	lastProposalAt, lastClaimAt time.Time
}

// OBSFallbackProposal reports an original proposal, not delivery or permission.
// The rehearsal is the original saved evaluation, even after its deadline.
type OBSFallbackProposal struct {
	ID, State, Failure, RequestID, ApprovedScene string
	ObservedAt, Deadline                         time.Time
	StartedAt, FinishedAt                        *time.Time
	Rehearsal                                    obsfallback.Report
	Unchanged                                    bool
	observation                                  obsfallback.Observation
	actor                                        string
	claim                                        []byte
}

// NewOBSFallbackStore validates separate rule/receipt grants and schema. No pool,
// observer or executor is created by construction.
func NewOBSFallbackStore(ctx context.Context, db *tenantdb.DB) (*OBSFallbackStore, error) {
	a, err := NewAutomationStore(ctx, db)
	if err != nil {
		return nil, err
	}
	s := &OBSFallbackStore{automation: a, db: db}
	if err = s.validate(ctx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *OBSFallbackStore) begin(ctx context.Context, actor string, write bool) (*tenantdb.Tx, error) {
	if ctx == nil || s == nil || s.automation == nil {
		return nil, ErrOBSFallback
	}
	if err := s.validate(ctx); err != nil {
		return nil, err
	}
	tx, err := s.automation.begin(ctx, actor, write)
	if err != nil {
		return nil, err
	}
	var role string
	if err = tx.QueryRowContext(ctx, `SELECT role FROM tenant_memberships WHERE tenant_id=current_setting('app.tenant_id')::uuid AND account_id=$1`, actor).Scan(&role); err != nil || !Allowed(role, "configure") {
		_ = tx.Rollback()
		return nil, ErrDenied
	}
	return tx, nil
}
func obsFallbackError(ctx context.Context, err error) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	for _, fixed := range []error{ErrDenied, ErrLimit, ErrOBSFallbackStale, ErrOBSFallbackCooldown, ErrAutomationInvalid, ErrAutomationConflict, ErrAutomationPurged, ErrAutomationSchema, sql.ErrNoRows} {
		if errors.Is(err, fixed) {
			return fixed
		}
	}
	return ErrOBSFallback
}

// Current reads the saved rule without choosing today's engine pointer or
// requiring an active observer/controller. Configure permission stays fresh.
func (s *OBSFallbackStore) Current(ctx context.Context, actor string) (out OBSFallbackRuleRecord, failure error) {
	callerCtx := ctx
	defer func() {
		if failure != nil {
			out = OBSFallbackRuleRecord{}
			failure = obsFallbackError(callerCtx, failure)
		}
	}()
	if ctx == nil {
		return out, ErrOBSFallback
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := s.begin(ctx, actor, false)
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback() }()
	out, err = s.readRule(ctx, tx)
	if err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	return out, nil
}

// CurrentOwned is for the trusted application observer only. It rechecks the
// configuring account's current configure membership before returning a rule.
func (s *OBSFallbackStore) CurrentOwned(ctx context.Context) (OBSFallbackRuleRecord, error) {
	if ctx == nil || s == nil || s.db == nil {
		return OBSFallbackRuleRecord{}, ErrOBSFallback
	}
	if err := s.validate(ctx); err != nil {
		return OBSFallbackRuleRecord{}, obsFallbackError(ctx, err)
	}
	var actor string
	err := s.db.QueryRowContext(ctx, `SELECT configured_by FROM obs_fallback_rules WHERE /*tenant:obs_fallback_rules*/`).Scan(&actor)
	if err != nil {
		return OBSFallbackRuleRecord{}, obsFallbackError(ctx, err)
	}
	out, err := s.Current(ctx, actor)
	if err != nil {
		return OBSFallbackRuleRecord{}, err
	}
	if out.ConfiguredBy != actor {
		return OBSFallbackRuleRecord{}, ErrAutomationConflict
	}
	return out, nil
}

func (s *OBSFallbackStore) readRule(ctx context.Context, tx *tenantdb.Tx) (out OBSFallbackRuleRecord, err error) {
	var automation, version, connection, latest string
	err = tx.QueryRowContext(ctx, `SELECT automation_id,version_id,connection_id,configured_by,enabled,revision,updated_at,latest_observation,latched,active_proposal,last_proposal_at,last_claim_at FROM obs_fallback_rules WHERE /*tenant:obs_fallback_rules*/`).Scan(&automation, &version, &connection, &out.ConfiguredBy, &out.Enabled, &out.Revision, &out.UpdatedAt, &latest, &out.Latched, &out.ActiveProposal, &out.lastProposalAt, &out.lastClaimAt)
	if err != nil {
		return out, err
	}
	v, _, err := storedAutomation(ctx, tx, automation, version)
	if err != nil {
		return out, err
	}
	g, err := loadAutomationGraph(ctx, tx, v)
	if err != nil {
		return out, err
	}
	out.Rule, err = obsfallback.RestoreRule(g, connection)
	if err != nil {
		return out, ErrAutomationInvalid
	}
	if latest != "" {
		var o obsfallback.Observation
		if json.Unmarshal([]byte(latest), &o) != nil {
			return out, ErrAutomationInvalid
		}
		canonical, e := json.Marshal(o)
		if e != nil || string(canonical) != latest {
			return out, ErrAutomationInvalid
		}
		if _, e = obsfallback.Rehearse(ctx, out.Rule, o, o.ObservedAt); e != nil || o.Synthetic {
			return out, ErrAutomationInvalid
		}
		out.LatestObservation = &o
	}
	return out, nil
}

// Save commits an immutable engine version, current pointer, audit and typed
// rule together. Deleted rules cannot be recreated through an old automation ID.
// Re-enabling needs a fresh version; disabling is independent revocation.
func (s *OBSFallbackStore) Save(ctx context.Context, actor string, rule obsfallback.Rule, expected string, revision int64, enabled bool) (out OBSFallbackRuleRecord, failure error) {
	callerCtx := ctx
	defer func() {
		if failure != nil {
			out = OBSFallbackRuleRecord{}
			failure = obsFallbackError(callerCtx, failure)
		}
	}()
	if ctx == nil || s == nil || s.db == nil || rule.Graph().Digest() == "" || rule.Spec().TenantID != s.db.TenantID() || revision < 0 || expected != "" && !automationID(expected) {
		return out, ErrAutomationInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := s.begin(ctx, actor, true)
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback() }()
	old, err := s.readRule(ctx, tx)
	exists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	spec := rule.Spec()
	if exists {
		if old.Rule.Spec().VersionID == spec.VersionID {
			if old.Rule.Graph().Digest() != rule.Graph().Digest() || old.Rule.Spec().ConnectionID != spec.ConnectionID || old.Enabled != enabled || old.ConfiguredBy != actor {
				return out, ErrAutomationConflict
			}
			old.Unchanged = true
			if err = tx.Commit(); err != nil {
				return out, err
			}
			return old, nil
		}
		if old.Rule.Spec().AutomationID != spec.AutomationID || old.Rule.Spec().VersionID != expected || old.Revision != revision || revision >= MaxOperatorSeed {
			return out, ErrAutomationConflict
		}
	} else {
		if expected != "" || revision != 0 {
			return out, ErrAutomationConflict
		}
		if _, err = currentAutomation(ctx, tx, spec.AutomationID); err == nil {
			return out, ErrAutomationPurged
		} else if !errors.Is(err, sql.ErrNoRows) {
			return out, err
		}
	}
	if _, err = s.automation.publishTx(ctx, tx, actor, rule.Graph().Root(), expected); err != nil {
		return out, err
	}
	if exists {
		if _, err = tx.ExecContext(ctx, `UPDATE obs_fallback_proposals SET state='refused',failure='rule_changed',finished_at=clock_timestamp() WHERE /*tenant:obs_fallback_proposals*/ AND state='pending'`); err != nil {
			return out, err
		}
		_, err = tx.ExecContext(ctx, `UPDATE obs_fallback_rules SET version_id=$1,connection_id=$2,configured_by=$3,enabled=$4,revision=revision+1,updated_at=clock_timestamp(),latest_observation='',latched=false,active_proposal='' WHERE /*tenant:obs_fallback_rules*/`, spec.VersionID, spec.ConnectionID, actor, enabled)
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO obs_fallback_rules(automation_id,version_id,connection_id,configured_by,enabled) VALUES($1,$2,$3,$4,$5)`, spec.AutomationID, spec.VersionID, spec.ConnectionID, actor, enabled)
	}
	if err != nil {
		return out, err
	}
	out, err = s.readRule(ctx, tx)
	if err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	return out, nil
}

// Disable revokes pending approvals even if the general engine pointer drifted.
// Running attempts are never recalled or made replayable.
func (s *OBSFallbackStore) Disable(ctx context.Context, actor, version string, revision int64) (out OBSFallbackRuleRecord, failure error) {
	callerCtx := ctx
	defer func() {
		if failure != nil {
			out = OBSFallbackRuleRecord{}
			failure = obsFallbackError(callerCtx, failure)
		}
	}()
	if ctx == nil || !automationID(version) || revision < 1 {
		return out, ErrAutomationInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := s.begin(ctx, actor, true)
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback() }()
	old, err := s.readRule(ctx, tx)
	if err != nil {
		return out, err
	}
	if old.Rule.Spec().VersionID != version {
		return out, ErrAutomationConflict
	}
	if !old.Enabled {
		old.Unchanged = true
		if err = tx.Commit(); err != nil {
			return out, err
		}
		return old, nil
	}
	if old.Revision != revision || revision >= MaxOperatorSeed {
		return out, ErrAutomationConflict
	}
	if _, err = tx.ExecContext(ctx, `UPDATE obs_fallback_rules SET enabled=false,revision=revision+1,updated_at=clock_timestamp(),active_proposal='' WHERE /*tenant:obs_fallback_rules*/`); err != nil {
		return out, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE obs_fallback_proposals SET state='refused',failure='rule_changed',finished_at=clock_timestamp() WHERE /*tenant:obs_fallback_proposals*/ AND state='pending'`); err != nil {
		return out, err
	}
	out, err = s.readRule(ctx, tx)
	if err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	return out, nil
}

// Observe accepts only the trusted owned adapter's complete current snapshot.
// It grants no effect permission. Unknown/disconnected evidence cannot unlatch a
// prior attempt; only a confirmed present/muted/hidden observation can do that.
func (s *OBSFallbackStore) Observe(ctx context.Context, actor string, o obsfallback.Observation) (proposalID string, failure error) {
	callerCtx := ctx
	defer func() {
		if failure != nil {
			proposalID = ""
			failure = obsFallbackError(callerCtx, failure)
		}
	}()
	if ctx == nil || o.Synthetic || !o.ObservedAt.Equal(o.ObservedAt.Truncate(time.Microsecond)) {
		return "", ErrAutomationInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := s.begin(ctx, actor, true)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	r, err := s.readRule(ctx, tx)
	if err != nil {
		return "", err
	}
	if !r.Enabled || r.ConfiguredBy != actor {
		return "", ErrDenied
	}
	current, err := currentAutomation(ctx, tx, r.Rule.Spec().AutomationID)
	if err != nil || current != r.Rule.Spec().VersionID {
		return "", ErrAutomationConflict
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return "", err
	}
	report, err := obsfallback.Rehearse(ctx, r.Rule, o, now)
	if err != nil {
		return "", err
	}
	if now.Before(o.ObservedAt) || !now.Before(report.Deadline) {
		return "", ErrOBSFallbackStale
	}
	if r.LatestObservation != nil && (o.ObservedAt.Before(r.LatestObservation.ObservedAt) || o.ObservedAt.Equal(r.LatestObservation.ObservedAt) && o.ID != r.LatestObservation.ID) {
		return "", ErrOBSFallbackStale
	}
	o.ObservedAt = o.ObservedAt.UTC()
	raw, err := json.Marshal(o)
	if err != nil || len(raw) > 8192 {
		return "", ErrAutomationInvalid
	}
	// Refuse altered duplicate source IDs before changing the current episode.
	var savedID, savedBody string
	err = tx.QueryRowContext(ctx, `SELECT id,document FROM obs_fallback_proposals WHERE /*tenant:obs_fallback_proposals*/ AND observation_id=$1`, o.ID).Scan(&savedID, &savedBody)
	if err == nil {
		if savedBody != string(raw) {
			return "", ErrAutomationConflict
		}
		if err = tx.Commit(); err != nil {
			return "", err
		}
		return savedID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE obs_fallback_rules SET latest_observation=$1 WHERE /*tenant:obs_fallback_rules*/`, string(raw)); err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE obs_fallback_proposals SET state='expired',failure='expired',finished_at=clock_timestamp() WHERE /*tenant:obs_fallback_proposals*/ AND state='pending' AND deadline<=clock_timestamp()`); err != nil {
		return "", err
	}
	if o.State == obsfallback.Present || o.State == obsfallback.Muted || o.State == obsfallback.Hidden {
		if _, err = tx.ExecContext(ctx, `UPDATE obs_fallback_proposals SET state='refused',failure='condition_cleared',finished_at=clock_timestamp() WHERE /*tenant:obs_fallback_proposals*/ AND state='pending'`); err != nil {
			return "", err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE obs_fallback_rules SET latched=false,active_proposal='' WHERE /*tenant:obs_fallback_rules*/`); err != nil {
			return "", err
		}
	} else if report.ProposalID != "" && !r.Latched {

		var active bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM obs_fallback_proposals WHERE /*tenant:obs_fallback_proposals*/ AND state='pending')`).Scan(&active); err != nil {
			return "", err
		}
		if active || now.Before(r.lastProposalAt.Add(obsProposalSpacing)) {
			if err = tx.Commit(); err != nil {
				return "", err
			}
			return r.ActiveProposal, nil
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM obs_fallback_proposals WHERE /*tenant:obs_fallback_proposals*/ AND started_at IS NULL AND finished_at<clock_timestamp()-interval '1 hour'`); err != nil {
			return "", err
		}
		if err = obsFallbackCapacity(ctx, tx); err != nil {
			return "", err
		}
		// A plan edit may have held the shared plan lock. Preserve the original
		// observation deadline using database time after every admission lock.
		if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			return "", err
		}
		if now.Before(o.ObservedAt) || !now.Before(report.Deadline) {
			return "", ErrOBSFallbackStale
		}

		_, err = tx.ExecContext(ctx, `INSERT INTO obs_fallback_proposals(id,observation_id,automation_id,version_id,graph_digest,connection_id,document,observed_at,deadline) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, report.ProposalID, o.ID, r.Rule.Spec().AutomationID, o.RuleVersionID, o.GraphDigest, o.ConnectionID, string(raw), o.ObservedAt, report.Deadline)
		if err != nil {
			return "", err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE obs_fallback_rules SET active_proposal=$1,last_proposal_at=$2 WHERE /*tenant:obs_fallback_rules*/`, report.ProposalID, now); err != nil {
			return "", err
		}
		proposalID = report.ProposalID
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return proposalID, nil
}

// Lookup is controller-independent original-proposal recovery. It never repairs,
// expires, admits or retries work and always requires fresh configure permission.
func (s *OBSFallbackStore) Lookup(ctx context.Context, actor, id string) (out OBSFallbackProposal, failure error) {
	callerCtx := ctx
	defer func() {
		if failure != nil {
			out = OBSFallbackProposal{}
			failure = obsFallbackError(callerCtx, failure)
		}
	}()
	if ctx == nil || !traceHash.MatchString(id) {
		return out, ErrAutomationInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := s.begin(ctx, actor, false)
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback() }()
	out, err = s.readProposal(ctx, tx, id)
	if err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	return out, nil
}
func (s *OBSFallbackStore) readProposal(ctx context.Context, tx *tenantdb.Tx, id string) (out OBSFallbackProposal, err error) {
	var automation, version, graph, connection, raw, observationID string
	var actor, request, scene sql.NullString
	var started, finished sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT id,automation_id,version_id,graph_digest,connection_id,document,observation_id,observed_at,deadline,state,failure,approved_by,request_id,approved_scene,claim_hash,started_at,finished_at FROM obs_fallback_proposals WHERE /*tenant:obs_fallback_proposals*/ AND id=$1`, id).Scan(&out.ID, &automation, &version, &graph, &connection, &raw, &observationID, &out.ObservedAt, &out.Deadline, &out.State, &out.Failure, &actor, &request, &scene, &out.claim, &started, &finished)
	if err != nil {
		return out, err
	}
	var o obsfallback.Observation
	if len(raw) > 8192 || json.Unmarshal([]byte(raw), &o) != nil {
		return out, ErrAutomationInvalid
	}
	canonical, e := json.Marshal(o)
	if e != nil || string(canonical) != raw || o.Synthetic || o.ID != observationID || o.RuleVersionID != version || o.GraphDigest != graph || o.ConnectionID != connection || !o.ObservedAt.Equal(out.ObservedAt) || !out.Deadline.Equal(out.ObservedAt.Add(obsfallback.Freshness)) {
		return out, ErrAutomationInvalid
	}
	v, _, err := storedAutomation(ctx, tx, automation, version)
	if err != nil {
		return out, err
	}
	g, err := loadAutomationGraph(ctx, tx, v)
	if err != nil {
		return out, err
	}
	rule, err := obsfallback.RestoreRule(g, connection)
	if err != nil {
		return out, ErrAutomationInvalid
	}
	out.Rehearsal, err = obsfallback.Rehearse(ctx, rule, o, o.ObservedAt)
	if err != nil || out.Rehearsal.ProposalID != id {
		return out, ErrAutomationInvalid
	}
	out.observation = o
	if started.Valid {
		t := started.Time
		out.StartedAt = &t
	}
	if finished.Valid {
		t := finished.Time
		out.FinishedAt = &t
	}
	if started.Valid {
		if !actor.Valid || !automationID(actor.String) || !request.Valid || !automationID(request.String) || !scene.Valid || !obsfallback.ValidName(scene.String) || len(out.claim) != 32 || started.Time.Before(out.ObservedAt) || !started.Time.Before(out.Deadline) {
			return out, ErrAutomationInvalid
		}
		out.actor = actor.String
		out.RequestID = request.String
		out.ApprovedScene = scene.String
		switch out.State {
		case "running":
			if finished.Valid || out.Failure != "" {
				return out, ErrAutomationInvalid
			}
		case "succeeded":
			if !finished.Valid || out.Failure != "" {
				return out, ErrAutomationInvalid
			}
		case "refused":
			if !finished.Valid || out.Failure != "guard_refused" {
				return out, ErrAutomationInvalid
			}
		case "uncertain":
			if !finished.Valid || !traceOneOf(out.Failure, "obs_unavailable", "interrupted", "completion_unknown") {
				return out, ErrAutomationInvalid
			}
		default:
			return out, ErrAutomationInvalid
		}
	} else {
		if actor.Valid || request.Valid || scene.Valid || out.claim != nil {
			return out, ErrAutomationInvalid
		}
		switch out.State {
		case "pending":
			if finished.Valid || out.Failure != "" {
				return out, ErrAutomationInvalid
			}
		case "expired", "dismissed":
			if !finished.Valid || out.Failure != out.State {
				return out, ErrAutomationInvalid
			}
		case "refused":
			if !finished.Valid || !traceOneOf(out.Failure, "rule_changed", "condition_cleared") {
				return out, ErrAutomationInvalid
			}
		default:
			return out, ErrAutomationInvalid
		}
	}
	if started.Valid && finished.Valid && finished.Time.Before(started.Time) {
		return out, ErrAutomationInvalid
	}
	return out, nil
}

func obsFallbackCapacity(ctx context.Context, tx *tenantdb.Tx) error {
	var raw []byte
	if err := tx.QueryRowContext(ctx, `SELECT p.limits FROM tenants t JOIN plans p ON p.id=t.plan_id WHERE t.id=current_setting('app.tenant_id')::uuid FOR SHARE OF p`).Scan(&raw); err != nil {
		return err
	}
	limits := map[string]int64{}
	if json.Unmarshal(raw, &limits) != nil {
		return ErrLimit
	}
	limit, ok := limits["storedEvents"]
	if !ok || limit < -1 {
		return ErrLimit
	}
	if limit == -1 || limit > obsProposalLimit {
		limit = obsProposalLimit
	}
	var n int64
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM obs_fallback_proposals WHERE /*tenant:obs_fallback_proposals*/`).Scan(&n); err != nil {
		return err
	}
	if n >= limit {
		return ErrLimit
	}
	return nil
}
