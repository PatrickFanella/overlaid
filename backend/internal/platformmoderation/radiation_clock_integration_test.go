package platformmoderation

import (
	"testing"
	"time"
)

func TestNukeRadiationCreationUsesPostLockOccurrenceClock(t *testing.T) {
	f, db, _ := fixture(t)
	s, _ := New(db)
	p := nukePreview(t, s, NukeOptions{Pattern: "spam", RadiationMinutes: 5})
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	var tenant string
	var locker int
	if err := tx.QueryRowContext(t.Context(), `SELECT id,pg_backend_pid() FROM tenants WHERE id=$1 FOR UPDATE`, db.TenantID()).Scan(&tenant, &locker); err != nil {
		t.Fatal(err)
	}
	type result struct {
		admission NukeAdmission
		err       error
	}
	done := make(chan result, 1)
	go func() { a, err := s.AdmitNuke(t.Context(), owner, nukeRequestKey, p); done <- result{a, err} }()
	by := time.Now().Add(5 * time.Second)
	for {
		var blocked bool
		if err := f.Admin.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`, locker).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		if time.Now().After(by) {
			t.Fatal("admission did not wait on tenant lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	var releasedAt time.Time
	if err := tx.QueryRowContext(t.Context(), `SELECT clock_timestamp()`).Scan(&releasedAt); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	r := <-done
	if r.err != nil || !r.admission.Created {
		t.Fatal(r.admission, r.err)
	}
	var created, updated, expires time.Time
	if err := db.QueryRowContext(t.Context(), `SELECT created_at,updated_at,expires_at FROM bot_moderation_rules WHERE /*tenant:bot_moderation_rules*/ AND id=$1`, r.admission.RadiationRuleID).Scan(&created, &updated, &expires); err != nil || !created.Equal(r.admission.CreatedAt) || !updated.Equal(created) || created.Before(releasedAt) || !expires.Equal(created.Add(5*time.Minute)) {
		t.Fatal("radiation used transaction-start clock", created, updated, expires, releasedAt, r.admission.CreatedAt, err)
	}
	if _, err := s.AdmitNuke(t.Context(), owner, nukeRequestKey, p); err != nil {
		t.Fatal(err)
	}
	var repeated time.Time
	if err := db.QueryRowContext(t.Context(), `SELECT created_at FROM bot_moderation_rules WHERE /*tenant:bot_moderation_rules*/ AND id=$1`, r.admission.RadiationRuleID).Scan(&repeated); err != nil || !repeated.Equal(created) {
		t.Fatal("duplicate renewed rule start", repeated, err)
	}
}
