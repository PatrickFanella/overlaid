package tenancy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/obsfallback"
)

func TestOBSFallbackClaimRaceAndDetachedReceipt(t *testing.T) {
	s, a, _, owner, _, _ := tenantPair(t)
	ctx := t.Context()
	scope, _ := s.Product.Scoped(a.ID)
	store, err := NewOBSFallbackStore(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	r := obsRule(t, a.ID, 950)
	if _, err = store.Save(ctx, owner, r, "", 0, true); err != nil {
		t.Fatal(err)
	}
	o := obsEvidence(t, scope, r, 100, obsfallback.Missing)
	id, err := store.Observe(ctx, owner, o)
	if err != nil || id == "" {
		t.Fatal("observe", err)
	}
	auth := OBSFallbackAuthorization{Observation: o, Scene: "PRIVATE_OVERRIDE", SceneAvailable: true}
	// Duplicate approval calls share the original key; exactly one receives a
	// private attempt. A reconstructed store can recover, but cannot replay.
	var wg sync.WaitGroup
	var handles [2]OBSFallbackAttempt
	var receipts [2]OBSFallbackProposal
	var failures [2]error
	for i := range handles {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			handles[i], receipts[i], failures[i] = store.Claim(ctx, owner, id, operatorRequest(951), auth.Scene, auth)
		}(i)
	}
	wg.Wait()
	var attempt OBSFallbackAttempt
	newClaims := 0
	for i := range handles {
		if failures[i] != nil || receipts[i].State != "running" || receipts[i].ApprovedScene != auth.Scene {
			t.Fatal("claim", failures[i])
		}
		if handles[i].IsNew() {
			newClaims++
			attempt = handles[i]
		}
	}
	if newClaims != 1 {
		t.Fatal("multiple effect permissions", newClaims)
	}
	raw, err := json.Marshal(attempt)
	if err != nil || string(raw) != "{}" || strings.Contains(fmt.Sprintf("%+v", attempt), "PRIVATE_") {
		t.Fatal("private claim serialization", err)
	}
	restarted, err := NewOBSFallbackStore(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if retry, receipt, err := restarted.Claim(ctx, owner, id, operatorRequest(951), auth.Scene, OBSFallbackAuthorization{}); err != nil || retry.IsNew() || !receipt.Unchanged {
		t.Fatal("restart replay", err)
	}
	if _, _, err = store.Claim(ctx, owner, id, operatorRequest(952), auth.Scene, auth); !errors.Is(err, ErrAutomationConflict) {
		t.Fatal("replacement key", err)
	}
	if _, _, err = store.Claim(ctx, owner, id, operatorRequest(951), r.Spec().Scene, auth); !errors.Is(err, ErrAutomationConflict) {
		t.Fatal("changed override", err)
	}
	if _, err = restarted.Finish(ctx, attempt, "succeeded", ""); !errors.Is(err, ErrAutomationInvalid) {
		t.Fatal("foreign completion handle", err)
	}
	wrong := attempt
	wrong.token[0] ^= 1
	if _, err = store.Finish(ctx, wrong, "succeeded", ""); !errors.Is(err, ErrAutomationConflict) {
		t.Fatal("wrong claim token", err)
	}
	if _, err = store.Finish(ctx, attempt, "succeeded", "obs_unavailable"); !errors.Is(err, ErrAutomationInvalid) {
		t.Fatal("contradictory receipt", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = store.Finish(canceled, attempt, "uncertain", "interrupted"); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation identity", err)
	}
	after, err := restarted.Lookup(ctx, owner, id)
	if err != nil || after.State != "uncertain" || after.Failure != "interrupted" || after.FinishedAt == nil {
		t.Fatal("detached completion", err)
	}
	if retry, _, err := restarted.Claim(ctx, owner, id, operatorRequest(951), auth.Scene, auth); err != nil || retry.IsNew() {
		t.Fatal("uncertain replay", err)
	}
	if _, err = store.Finish(ctx, attempt, "succeeded", ""); !errors.Is(err, ErrAutomationConflict) {
		t.Fatal("overwritten receipt", err)
	}
	unknown := obsEvidence(t, scope, r, 101, obsfallback.Disconnected)
	if _, err = store.Observe(ctx, owner, unknown); err != nil {
		t.Fatal(err)
	}
	current, err := store.Current(ctx, owner)
	if err != nil || !current.Latched {
		t.Fatal("disconnect cleared episode", err)
	}
}

func TestOBSFallbackClaimFreshGuards(t *testing.T) {
	for _, guard := range []string{"scene", "missing", "synthetic", "binding", "stale", "disabled", "permission", "plan", "cooldown"} {
		t.Run(guard, func(t *testing.T) {
			s, a, _, owner, _, f := tenantPair(t)
			ctx := t.Context()
			scope, _ := s.Product.Scoped(a.ID)
			store, err := NewOBSFallbackStore(ctx, scope)
			if err != nil {
				t.Fatal(err)
			}
			r := obsRule(t, a.ID, 953)
			if _, err = store.Save(ctx, owner, r, "", 0, true); err != nil {
				t.Fatal(err)
			}
			o := obsEvidence(t, scope, r, 102, obsfallback.Missing)
			id, err := store.Observe(ctx, owner, o)
			if err != nil || id == "" {
				t.Fatal("observe", err)
			}
			auth := OBSFallbackAuthorization{Observation: o, Scene: r.Spec().Scene, SceneAvailable: true}
			var want error
			switch guard {
			case "scene":
				auth.SceneAvailable = false
				want = ErrDenied
			case "missing":
				auth.Observation.State = obsfallback.Hidden
				want = ErrOBSFallbackStale
			case "synthetic":
				auth.Observation.Synthetic = true
				want = ErrDenied
			case "binding":
				auth.Observation.ConnectionID = strings.Repeat("d", 64)
				want = ErrOBSFallbackStale
			case "stale":
				auth.Observation.ObservedAt = o.ObservedAt.Add(-obsfallback.Freshness)
				want = ErrOBSFallbackStale
			case "disabled":
				_, err = store.Disable(ctx, owner, r.Spec().VersionID, 1)
				want = ErrAutomationConflict
			case "permission":
				_, err = f.Admin.Exec(`UPDATE tenant_memberships SET role='moderator' WHERE tenant_id=$1 AND account_id=$2`, a.ID, owner)
				want = ErrDenied
			case "plan":
				_, err = f.Admin.Exec(`UPDATE plans SET limits=jsonb_set(limits,'{storedEvents}','0') WHERE id='free'`)
				want = ErrLimit
			case "cooldown":
				_, err = f.Admin.Exec(`UPDATE obs_fallback_rules SET last_claim_at=clock_timestamp() WHERE tenant_id=$1`, a.ID)
				want = ErrOBSFallbackCooldown
			}
			if err != nil {
				t.Fatal(err)
			}
			attempt, receipt, err := store.Claim(ctx, owner, id, operatorRequest(954), auth.Scene, auth)
			if !errors.Is(err, want) || attempt.IsNew() || receipt.ID != "" {
				t.Fatal("guard admitted effect", err)
			}
			var starts int
			if err = f.Admin.QueryRow(`SELECT count(*) FROM obs_fallback_proposals WHERE tenant_id=$1 AND started_at IS NOT NULL`, a.ID).Scan(&starts); err != nil || starts != 0 {
				t.Fatal("partial claim", err)
			}
		})
	}
}

func TestOBSFallbackDismissAndOverride(t *testing.T) {
	s, a, _, owner, _, f := tenantPair(t)
	ctx := t.Context()
	scope, _ := s.Product.Scoped(a.ID)
	store, err := NewOBSFallbackStore(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	r := obsRule(t, a.ID, 955)
	if _, err = store.Save(ctx, owner, r, "", 0, true); err != nil {
		t.Fatal(err)
	}
	o := obsEvidence(t, scope, r, 103, obsfallback.Missing)
	id, err := store.Observe(ctx, owner, o)
	if err != nil || id == "" {
		t.Fatal(err)
	}
	if receipt, err := store.Dismiss(ctx, owner, id); err != nil || receipt.State != "dismissed" {
		t.Fatal("dismiss", err)
	}
	if receipt, err := store.Dismiss(ctx, owner, id); err != nil || !receipt.Unchanged {
		t.Fatal("duplicate dismiss", err)
	}
	auth := OBSFallbackAuthorization{Observation: o, Scene: "PRIVATE_OVERRIDE", SceneAvailable: true}
	if attempt, _, err := store.Claim(ctx, owner, id, operatorRequest(956), auth.Scene, auth); !errors.Is(err, ErrAutomationConflict) || attempt.IsNew() {
		t.Fatal("dismissed replay", err)
	}
	present := obsEvidence(t, scope, r, 104, obsfallback.Present)
	if _, err = store.Observe(ctx, owner, present); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Admin.Exec(`UPDATE obs_fallback_rules SET last_proposal_at='epoch' WHERE tenant_id=$1`, a.ID); err != nil {
		t.Fatal(err)
	}
	next := obsEvidence(t, scope, r, 105, obsfallback.Missing)
	nextID, err := store.Observe(ctx, owner, next)
	if err != nil || nextID == "" {
		t.Fatal("new episode", err)
	}
	// The original destination disappeared. An explicitly selected, available
	// override can qualify without silently replacing the saved rule.
	next.TargetAvailable = false
	auth.Observation = next
	attempt, receipt, err := store.Claim(ctx, owner, nextID, operatorRequest(957), auth.Scene, auth)
	if err != nil || !attempt.IsNew() || receipt.ApprovedScene != auth.Scene {
		t.Fatal("override", err)
	}
	if _, err = store.Dismiss(ctx, owner, nextID); !errors.Is(err, ErrAutomationConflict) {
		t.Fatal("running dismiss", err)
	}
	if receipt, err = store.Finish(ctx, attempt, "succeeded", ""); err != nil || receipt.State != "succeeded" || receipt.StartedAt == nil || time.Since(*receipt.StartedAt) > obsfallback.Freshness {
		t.Fatal("success", err)
	}
}

func TestOBSFallbackClaimRechecksOriginalDeadlineAfterLock(t *testing.T) {
	s, a, _, owner, _, f := tenantPair(t)
	ctx := t.Context()
	scope, _ := s.Product.Scoped(a.ID)
	store, err := NewOBSFallbackStore(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	r := obsRule(t, a.ID, 958)
	if _, err = store.Save(ctx, owner, r, "", 0, true); err != nil {
		t.Fatal(err)
	}
	o := obsEvidence(t, scope, r, 106, obsfallback.Missing)
	o.ObservedAt = o.ObservedAt.Add(-8 * time.Second)
	id, err := store.Observe(ctx, owner, o)
	if err != nil || id == "" {
		t.Fatal("original observation", err)
	}
	held, err := f.Admin.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Rollback() }()
	if _, err = held.ExecContext(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, a.ID); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		attempt, _, e := store.Claim(ctx, owner, id, operatorRequest(959), r.Spec().Scene, OBSFallbackAuthorization{Observation: o, Scene: r.Spec().Scene, SceneAvailable: true})
		if attempt.IsNew() {
			result <- errors.New("expired source received attempt")
			return
		}
		result <- e
	}()
	// Expire through the database clock while admission is blocked on the
	// tenant lock. A fresh observation must not renew this source's deadline.
	if _, err = held.ExecContext(ctx, `SELECT pg_sleep(greatest(0,extract(epoch FROM ($1::timestamptz-clock_timestamp())))+0.02)`, o.ObservedAt.Add(obsfallback.Freshness)); err != nil {
		t.Fatal(err)
	}
	if err = held.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = <-result; !errors.Is(err, ErrOBSFallbackStale) {
		t.Fatal("pre-lock deadline", err)
	}
	receipt, err := store.Lookup(ctx, owner, id)
	if err != nil || receipt.State != "pending" || receipt.StartedAt != nil || !receipt.Deadline.Equal(o.ObservedAt.Add(obsfallback.Freshness)) {
		t.Fatal("expired source changed", err)
	}
}
