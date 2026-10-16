package platformoutbound

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

func savedRegistryReply(t *testing.T, db *tenantdb.DB, r Request, name string) (RegistryReply, bot.Command) {
	t.Helper()
	registry, err := bot.NewTenant(db)
	if err != nil {
		t.Fatal(err)
	}
	command, err := registry.SaveCommand(t.Context(), bot.Command{Name: name, Enabled: true, Response: "Saved reply", Action: "ticker", ResponseMode: "chat", RoleRequired: "viewer"})
	if err != nil {
		t.Fatal(err)
	}
	r.ReplyToID = ""
	binding := RegistryReply{Request: r, CommandID: command.ID, SnapshotFingerprint: bot.CommandSnapshotFingerprint(db.TenantID(), command), EffectKind: "chat-response-v1"}
	binding.Key = RegistryReplyKey(binding.CommandID, binding.SnapshotFingerprint, binding.EffectKind)
	return binding, command
}

func waitRegistryLock(t *testing.T, db *sql.DB, runtimeURL string) int {
	t.Helper()
	u, err := url.Parse(runtimeURL)
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithTimeout(t.Context(), 5*time.Second)
	defer stop()
	for {
		var pid int
		err = db.QueryRowContext(ctx, `SELECT pid FROM pg_stat_activity WHERE datname=current_database() AND usename=$1 AND wait_event_type='Lock' LIMIT 1`, u.User.Username()).Scan(&pid)
		if err == nil {
			return pid
		}
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatal(err)
		}
		select {
		case <-ctx.Done():
			t.Fatal("registry admission did not wait on fixture lock")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestRegistryAdmissionRejectsConcurrentSavedChanges(t *testing.T) {
	for _, mutation := range []string{"edit", "disable", "delete"} {
		t.Run(mutation, func(t *testing.T) {
			f, db, _ := fixture(t)
			s, _ := New(db)
			reply, command := savedRegistryReply(t, db, source(t, db, "before-"+mutation), "registryreply")
			blocker, err := f.Admin.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = blocker.Rollback() }()
			query := `UPDATE bot_commands SET response='Changed',updated_at=clock_timestamp() WHERE tenant_id=$1 AND id=$2`
			if mutation == "disable" {
				query = `UPDATE bot_commands SET enabled=false,updated_at=clock_timestamp() WHERE tenant_id=$1 AND id=$2`
			}
			if mutation == "delete" {
				query = `DELETE FROM bot_commands WHERE tenant_id=$1 AND id=$2`
			}
			if _, err := blocker.ExecContext(t.Context(), query, db.TenantID(), command.ID); err != nil {
				t.Fatal(err)
			}
			ctx, stop := context.WithTimeout(t.Context(), 10*time.Second)
			defer stop()
			done := make(chan error, 1)
			go func() { _, err := s.AdmitRegistryBatch(ctx, []RegistryReply{reply}); done <- err }()
			waitRegistryLock(t, f.Admin, f.RuntimeURL)
			if err := blocker.Commit(); err != nil {
				t.Fatal(err)
			}
			if err := <-done; !errors.Is(err, ErrInvalid) {
				t.Fatal("concurrent changed snapshot admitted", err)
			}
			sendJobs(t, db, 0)
			var count int
			if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM platform_outbound_actions WHERE /*tenant:platform_outbound_actions*/`).Scan(&count); err != nil || count != 0 {
				t.Fatal("partial action after stale snapshot", count, err)
			}
		})
	}
}

func TestRegistryAdmissionHoldsSnapshotUntilLedgerCommit(t *testing.T) {
	f, db, _ := fixture(t)
	s, _ := New(db)
	reply, command := savedRegistryReply(t, db, source(t, db, "through-commit"), "registryreply")
	ctx, stop := context.WithTimeout(t.Context(), 10*time.Second)
	defer stop()
	blocker, err := f.Admin.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = blocker.Rollback() }()
	if _, err := blocker.ExecContext(ctx, `LOCK TABLE platform_outbound_actions IN SHARE MODE`); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := s.AdmitRegistryBatch(ctx, []RegistryReply{reply}); done <- err }()
	admissionPID := waitRegistryLock(t, f.Admin, f.RuntimeURL)
	editor, err := f.Admin.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = editor.Close() }()
	var editorPID int
	if err := editor.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&editorPID); err != nil {
		t.Fatal(err)
	}
	edited := make(chan error, 1)
	go func() {
		_, err := editor.ExecContext(ctx, `UPDATE bot_commands SET response='After commit',updated_at=clock_timestamp() WHERE tenant_id=$1 AND id=$2`, db.TenantID(), command.ID)
		edited <- err
	}()
	for {
		var waiting bool
		if err := f.Admin.QueryRowContext(ctx, `SELECT $1::int=ANY(pg_blocking_pids($2))`, admissionPID, editorPID).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("command update escaped admission snapshot lock")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if err := blocker.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := <-edited; err != nil {
		t.Fatal(err)
	}
	// Existing immutable actions are noops even after their registry snapshot changes.
	results, err := s.AdmitRegistryBatch(ctx, []RegistryReply{reply})
	if err != nil || len(results) != 1 || results[0].Created {
		t.Fatal("existing action reauthorized or duplicated", results, err)
	}
	var text string
	if err := db.QueryRowContext(ctx, `SELECT message FROM platform_outbound_actions WHERE /*tenant:platform_outbound_actions*/`).Scan(&text); err != nil || text != reply.Text {
		t.Fatal("saved payload changed", text, err)
	}
	sendJobs(t, db, 1)
}

func TestRegistryAdmissionPlanAndBatchBindings(t *testing.T) {
	f, db, other := fixture(t)
	s, _ := New(db)
	r := source(t, db, "bindings")
	first, one := savedRegistryReply(t, db, r, "firstreply")
	second, two := savedRegistryReply(t, db, r, "secondreply")
	foreign, _ := savedRegistryReply(t, other, r, "foreignreply")
	for _, bad := range []RegistryReply{foreign, func() RegistryReply { r := first; r.Key = "unbound"; return r }(), func() RegistryReply {
		r := first
		r.EffectKind = "newsroom-current-v1"
		r.Key = RegistryReplyKey(r.CommandID, r.SnapshotFingerprint, r.EffectKind)
		return r
	}()} {
		if results, err := s.AdmitRegistryBatch(t.Context(), []RegistryReply{bad}); err == nil || results != nil {
			t.Fatal("unbound registry output accepted", results, err)
		}
	}
	if _, err := f.Admin.Exec(`UPDATE bot_commands SET enabled=false WHERE tenant_id=$1 AND id=$2`, db.TenantID(), two.ID); err != nil {
		t.Fatal(err)
	}
	if results, err := s.AdmitRegistryBatch(t.Context(), []RegistryReply{first, second}); err == nil || results != nil {
		t.Fatal("stale member left partial batch", results, err)
	}
	sendJobs(t, db, 0)
	// An assignment changed while the admission tenant lock is blocked must be
	// reread before checking command rank; the old joined plan is insufficient.
	if _, err := f.Admin.Exec(`UPDATE plans SET limits=jsonb_set(limits,'{commands}','0') WHERE id='free'`); err != nil {
		t.Fatal(err)
	}
	blocker, err := f.Admin.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = blocker.Rollback() }()
	if _, err := blocker.Exec(`UPDATE tenants SET plan_id='free' WHERE id=$1`, db.TenantID()); err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithTimeout(t.Context(), 10*time.Second)
	defer stop()
	done := make(chan error, 1)
	go func() { _, err := s.AdmitRegistryBatch(ctx, []RegistryReply{first}); done <- err }()
	waitRegistryLock(t, f.Admin, f.RuntimeURL)
	if err := blocker.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, tenantdb.ErrLimit) {
		t.Fatal("post-lock command plan not enforced", err)
	}
	sendJobs(t, db, 0)
	if _, err := f.Admin.Exec(`UPDATE tenants SET plan_id='unlimited' WHERE id=$1`, db.TenantID()); err != nil {
		t.Fatal(err)
	}
	admitted, err := s.AdmitRegistryBatch(ctx, []RegistryReply{first})
	if err != nil || !admitted[0].Created {
		t.Fatal(err)
	}
	if _, err := f.Admin.Exec(`DELETE FROM bot_commands WHERE tenant_id=$1 AND id=$2`, db.TenantID(), one.ID); err != nil {
		t.Fatal(err)
	}
	registry, err := bot.NewTenant(db)
	if err != nil {
		t.Fatal(err)
	}
	two.Enabled = true
	two, err = registry.SaveCommand(ctx, two)
	if err != nil {
		t.Fatal(err)
	}
	second.SnapshotFingerprint = bot.CommandSnapshotFingerprint(db.TenantID(), two)
	second.Key = RegistryReplyKey(second.CommandID, second.SnapshotFingerprint, second.EffectKind)
	mixed, err := s.AdmitRegistryBatch(ctx, []RegistryReply{first, second})
	if err != nil || len(mixed) != 2 || mixed[0].Created || !mixed[1].Created {
		t.Fatal("mixed existing/deleted and new binding failed", mixed, err)
	}
	sendJobs(t, db, 2)
}

func TestRegistryAdmissionCancellationWhileSnapshotBlocked(t *testing.T) {
	f, db, _ := fixture(t)
	s, _ := New(db)
	reply, command := savedRegistryReply(t, db, source(t, db, "cancel-snapshot"), "registryreply")
	blocker, err := f.Admin.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = blocker.Rollback() }()
	if _, err := blocker.ExecContext(t.Context(), `SELECT id FROM bot_commands WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, db.TenantID(), command.ID); err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	done := make(chan error, 1)
	go func() { _, err := s.AdmitRegistryBatch(ctx, []RegistryReply{reply}); done <- err }()
	waitRegistryLock(t, f.Admin, f.RuntimeURL)
	stop()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("snapshot wait cancellation lost", err)
	}
	sendJobs(t, db, 0)
}
