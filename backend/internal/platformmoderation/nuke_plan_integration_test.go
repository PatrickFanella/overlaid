package platformmoderation

import (
	"errors"
	"testing"
	"time"
)

func TestNukePlanStoredRoundTrip(t *testing.T) {
	for _, mode := range []string{"delete", "timeout", "radiation"} {
		t.Run(mode, func(t *testing.T) {
			f, db, _ := fixture(t)
			s, _ := New(db)
			action := mode
			want := 1
			if mode == "radiation" {
				action, want = "timeout", 0
			} else {
				nukeSource(t, db, "selected", "777", "PRIVATE_PATTERN", time.Now().Add(-5*time.Minute))
			}
			p := nukePreview(t, s, NukeOptions{Pattern: "PRIVATE_PATTERN", Action: action, DurationSeconds: 90, RadiationMinutes: 7})
			scope := planScope()
			scope.TenantID = db.TenantID()
			token, err := SealNukePlan(planKeys(), scope, p)
			if err != nil {
				t.Fatal(err)
			}
			manualModerationCounts(t, db, 0, 0)
			nukeRuleCount(t, db, 0)
			nukeSource(t, db, "arrived-after-preview", "888", "PRIVATE_PATTERN", time.Now().Add(-time.Millisecond))
			restored, err := OpenNukePlan(planKeys(), scope, token)
			if err != nil || restored.Fingerprint() != p.Fingerprint() {
				t.Fatal("restored plan", err)
			}
			a, err := s.AdmitNuke(t.Context(), owner, nukeRequestKey, restored)
			if err != nil || !a.Created || len(a.ActionIDs) != want {
				t.Fatal("restored admission", a, err)
			}
			manualModerationCounts(t, db, 1, want)
			nukeRuleCount(t, db, 1)
			if _, err := f.Admin.Exec(`DELETE FROM bot_moderation_rules`); err != nil {
				t.Fatal(err)
			}
			// Simulate reload/restart by restoring again, with no remembered plan.
			reloaded, err := OpenNukePlan(planKeys(), scope, token)
			if err != nil {
				t.Fatal(err)
			}
			status, err := s.LookupNuke(t.Context(), owner, nukeRequestKey, reloaded)
			if err != nil || status.SourceID != a.SourceID || len(status.Receipts) != want || !status.Deadline.Equal(a.Deadline) {
				t.Fatal("restored receipt lookup", status, err)
			}
			duplicate, err := s.AdmitNuke(t.Context(), owner, nukeRequestKey, reloaded)
			if err != nil || duplicate.Created || duplicate.SourceID != a.SourceID || !duplicate.Deadline.Equal(a.Deadline) {
				t.Fatal("restored duplicate", duplicate, err)
			}
			manualModerationCounts(t, db, 1, want)
			nukeRuleCount(t, db, 0)
			if _, err := f.Admin.Exec(`UPDATE tenant_memberships SET role='editor' WHERE tenant_id=$1 AND account_id=$2`, db.TenantID(), owner); err != nil {
				t.Fatal(err)
			}
			if _, err := OpenNukePlan(planKeys(), scope, token); err != nil {
				t.Fatal("codec performed authorization", err)
			}
			if _, err := s.LookupNuke(t.Context(), owner, nukeRequestKey, reloaded); !errors.Is(err, ErrDenied) {
				t.Fatal("restored plan bypassed current permission", err)
			}
		})
	}
}

func TestNukePlanRestorationCannotRenewPreview(t *testing.T) {
	_, db, _ := fixture(t)
	s, _ := New(db)
	nukeSource(t, db, "old-preview", "777", "PRIVATE_PATTERN", time.Now().Add(-5*time.Minute))
	p := nukePreview(t, s, NukeOptions{Pattern: "PRIVATE_PATTERN", RadiationMinutes: 5})
	binding := *p.request.nuke
	binding.observedAt = binding.observedAt.Add(-3 * time.Minute)
	p.request.nuke = &binding
	scope := planScope()
	scope.TenantID = db.TenantID()
	token, err := SealNukePlan(planKeys(), scope, p)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := OpenNukePlan(planKeys(), scope, token)
	if err != nil || !restored.ObservedAt().Equal(p.ObservedAt()) {
		t.Fatal("old plan could not be restored", err)
	}
	if _, err := s.LookupNuke(t.Context(), owner, nukeRequestKey, restored); !errors.Is(err, ErrManualMissing) {
		t.Fatal("lookup created missing work", err)
	}
	if _, err := s.AdmitNuke(t.Context(), owner, nukeRequestKey, restored); !errors.Is(err, ErrInvalid) {
		t.Fatal("restoration renewed preview", err)
	}
	manualModerationCounts(t, db, 0, 0)
	nukeRuleCount(t, db, 0)
}
