package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/obsfallback"
	"git.subcult.tv/PatrickFanella/over-laid/internal/obsws"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

var errOBSOwner = errors.New("obs_controller_unavailable")

// One application owner serializes its configured OBS mutations. Its lifetime
// includes observers and detached receipt writes, before pool/controller release.
type obsEffectOwner struct {
	app     *Application
	gate    chan struct{}
	mu      sync.Mutex
	closing bool
	active  int
	done    chan struct{}
}

func newOBSEffectOwner(a *Application) *obsEffectOwner {
	return &obsEffectOwner{app: a, gate: make(chan struct{}, 1), done: make(chan struct{})}
}
func (o *obsEffectOwner) begin() (func(), error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closing || o.app.ctx.Err() != nil {
		return nil, errOBSOwner
	}
	o.active++
	var once sync.Once
	return func() {
		once.Do(func() {
			o.mu.Lock()
			defer o.mu.Unlock()
			o.active--
			if o.closing && o.active == 0 {
				close(o.done)
			}
		})
	}, nil
}
func (o *obsEffectOwner) close(ctx context.Context) error {
	o.mu.Lock()
	if !o.closing {
		o.closing = true
		if o.active == 0 {
			close(o.done)
		}
	}
	o.mu.Unlock()
	select {
	case <-o.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (o *obsEffectOwner) guard(ctx context.Context, s *Server) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if s == nil || s.tenantContext == nil || s.tenantContext.Err() != nil || o.app.ctx.Err() != nil || o.app.config.DeploymentMode != "selfhost" || s.obs.URL == "" {
		return errOBSOwner
	}
	o.app.mu.Lock()
	r, current := o.app.runtimes[s.tenantID]
	owned := current && r.server == s && !o.app.deleting[s.tenantID]
	o.app.mu.Unlock()
	if !owned || o.app.checkChatController(ctx) != nil {
		return errOBSOwner
	}
	return nil
}
func (o *obsEffectOwner) run(ctx context.Context, s *Server, effect func(context.Context) error) error {
	if ctx == nil || effect == nil || s == nil || s.tenantContext == nil {
		return errOBSOwner
	}
	done, err := o.begin()
	if err != nil {
		return err
	}
	defer done()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	stop := context.AfterFunc(o.app.ctx, cancel)
	defer stop()
	stopTenant := context.AfterFunc(s.tenantContext, cancel)
	defer stopTenant()
	release, err := acquireGate(ctx, o.gate)
	if err != nil {
		return err
	}
	defer release()
	if err = o.guard(ctx, s); err != nil {
		return err
	}
	return effect(ctx)
}

func obsConnectionID(client obsws.Client) string {
	h := sha256.New()
	_, _ = h.Write([]byte("overlaid.engine.obs-connection.v1\x00"))
	_, _ = h.Write([]byte(client.URL))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(client.Password))
	return hex.EncodeToString(h.Sum(nil))
}

func (o *obsEffectOwner) observe(s *Server, scope *tenantdb.DB) {
	done, err := o.begin()
	if err != nil {
		return
	}
	go func() {
		defer done()
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			if s.tenantContext.Err() != nil || o.app.ctx.Err() != nil {
				return
			}
			// No schema validation/provider work for runtimes with no enabled rule.
			ctx, cancel := context.WithTimeout(s.tenantContext, 5*time.Second)
			var enabled bool
			err := scope.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM obs_fallback_rules WHERE /*tenant:obs_fallback_rules*/ AND enabled)`).Scan(&enabled)
			if err == nil && enabled {
				_ = o.observeOnce(ctx, s, scope)
			}
			cancel()
			select {
			case <-s.tenantContext.Done():
				return
			case <-o.app.ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
func (o *obsEffectOwner) observeOnce(ctx context.Context, s *Server, scope *tenantdb.DB) error {
	return o.run(ctx, s, func(ctx context.Context) error {
		store, err := tenancy.NewOBSFallbackStore(ctx, scope)
		if err != nil {
			return err
		}
		rule, err := store.CurrentOwned(ctx)
		if err != nil {
			return err
		}
		if !rule.Enabled || rule.Rule.Spec().ConnectionID != obsConnectionID(s.obs) {
			return errOBSOwner
		}
		observation, _, err := readOBSEvidence(ctx, scope, s.obs, rule.Rule, rule.Rule.Spec().Scene)
		if err != nil {
			return err
		}
		if err = o.guard(ctx, s); err != nil {
			return err
		}
		_, err = store.Observe(ctx, rule.ConfiguredBy, observation)
		return err
	})
}

func readOBSEvidence(ctx context.Context, scope *tenantdb.DB, client obsws.Client, rule obsfallback.Rule, selected string) (out obsfallback.Observation, sceneAvailable bool, err error) {
	var at time.Time
	if err = scope.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&at); err != nil {
		return out, false, err
	}
	var id [32]byte
	if _, err = rand.Read(id[:]); err != nil {
		return out, false, err
	}
	spec := rule.Spec()
	out = obsfallback.Observation{ID: hex.EncodeToString(id[:]), RuleVersionID: spec.VersionID, GraphDigest: rule.Graph().Digest(), ConnectionID: obsConnectionID(client), Collection: spec.Collection, Input: spec.Input, Scene: spec.Scene, ObservedAt: at.UTC(), State: obsfallback.Unknown}
	evidence, problem := client.InspectFallback(ctx, spec.Collection, spec.Input, selected)
	if ctx.Err() != nil {
		return obsfallback.Observation{}, false, ctx.Err()
	}
	if errors.Is(problem, obsws.ErrFallbackConnection) {
		out.State = obsfallback.Disconnected
		return out, false, nil
	}
	if problem != nil {
		return out, false, nil
	}
	out.TargetAvailable = evidence.SceneAvailable && selected == spec.Scene
	switch {
	case !evidence.InputPresent:
		out.State = obsfallback.Missing
	case evidence.InputMuted:
		out.State = obsfallback.Muted
	case !evidence.InputActive:
		out.State = obsfallback.Hidden
	default:
		out.State = obsfallback.Present
	}
	return out, evidence.SceneAvailable, nil
}

func (o *obsEffectOwner) approve(ctx context.Context, s *Server, scope *tenantdb.DB, store *tenancy.OBSFallbackStore, actor, id, request, version, scene string, sessionGuard func(context.Context) error) (out tenancy.OBSFallbackProposal, failure error) {
	failure = o.run(ctx, s, func(ctx context.Context) error {
		rule, err := store.Current(ctx, actor)
		if err != nil {
			return err
		}
		if !rule.Enabled || rule.Rule.Spec().VersionID != version || rule.Rule.Spec().ConnectionID != obsConnectionID(s.obs) {
			return tenancy.ErrAutomationConflict
		}
		observation, available, err := readOBSEvidence(ctx, scope, s.obs, rule.Rule, scene)
		if err != nil {
			return err
		}
		if err = sessionGuard(ctx); err != nil {
			return err
		}
		if err = o.guard(ctx, s); err != nil {
			return err
		}
		attempt, receipt, err := store.Claim(ctx, actor, id, request, scene, tenancy.OBSFallbackAuthorization{Observation: observation, Scene: scene, SceneAvailable: available})
		if err != nil {
			return err
		}
		if !attempt.IsNew() {
			out = receipt
			return nil
		}
		// Bookkeeping remains inside the owned lifetime, even when the original
		// deadline, browser, session, runtime or application is canceled.
		state, code := "refused", "guard_refused"
		bound, cancel := context.WithDeadline(ctx, receipt.Deadline)
		defer cancel()
		guard := func(check context.Context) error {
			if e := sessionGuard(check); e != nil {
				return e
			}
			if e := o.guard(check, s); e != nil {
				return e
			}
			if obsConnectionID(s.obs) != rule.Rule.Spec().ConnectionID {
				return errOBSOwner
			}
			return store.CheckAttempt(check, attempt)
		}
		if err = guard(bound); err == nil {
			guardRefused := false
			err = s.obs.SetFallbackScene(bound, rule.Rule.Spec().Collection, rule.Rule.Spec().Input, scene, func(check context.Context) error { e := guard(check); guardRefused = e != nil; return e })
			switch {
			case err == nil:
				state, code = "succeeded", ""
			case bound.Err() != nil:
				state, code = "uncertain", "interrupted"
			case guardRefused:
				state, code = "refused", "guard_refused"
			default:
				state, code = "uncertain", "obs_unavailable"
			}
		}
		out, err = store.Finish(ctx, attempt, state, code)
		return err
	})
	if failure != nil {
		out = tenancy.OBSFallbackProposal{}
	}
	return out, failure
}
