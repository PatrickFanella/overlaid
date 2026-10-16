package platformoutbound

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
	"git.subcult.tv/PatrickFanella/over-laid/internal/newsroom"
)

func TestBoundNewsroomAnnouncementChangesAndReceiptLookups(t *testing.T) {
	f, db, _ := fixture(t)
	s, _ := New(db)
	d := newsroom.Empty()
	d.CurrentID = "story"
	d.Items = []newsroom.Item{{ID: "story", Status: "approved", Title: "Approved source", URL: "https://example.org/story", ViewerID: "7", SubmittedBy: "viewer"}}
	write := func() {
		t.Helper()
		raw, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(t.Context(), `INSERT INTO newsroom_documents(id,document) VALUES('default',$1) ON CONFLICT(tenant_id,id) DO UPDATE SET document=excluded.document`, raw); err != nil {
			t.Fatal(err)
		}
	}
	write()
	r := manualRequest()
	r.Kind = "newsroom_current"
	projected, err := newsroom.BuildAnnouncement(d, r.Kind, "")
	if err != nil {
		t.Fatal(err)
	}
	r.Text = projected.Text
	r.ViewerID = projected.ViewerID
	r.ViewerLogin = projected.ViewerLogin
	injected := r
	injected.Text = "Injected body"
	if _, err := s.AdmitNewsroomAnnouncement(t.Context(), injected); !errors.Is(err, ErrConflict) {
		t.Fatal("unbound projected text admitted", err)
	}
	manualCounts(t, db, 0)
	// A status lookup for a key never attempts admission.
	if _, err := s.LookupManual(t.Context(), owner, r.RequestKey, r.Kind, ManualFingerprint(r)); !errors.Is(err, ErrManualMissing) {
		t.Fatal(err)
	}
	manualCounts(t, db, 0)
	ctx, stop := context.WithTimeout(t.Context(), 10*time.Second)
	defer stop()
	blocker, err := f.Admin.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = blocker.Rollback() }()
	d.Items[0].Status = "rejected"
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := blocker.ExecContext(ctx, `UPDATE newsroom_documents SET document=$1 WHERE tenant_id=$2 AND id='default'`, raw, db.TenantID()); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := s.AdmitNewsroomAnnouncement(ctx, r); done <- err }()
	waitRegistryLock(t, f.Admin, f.RuntimeURL)
	if err := blocker.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, ErrConflict) {
		t.Fatal("changed approved selection admitted", err)
	}
	manualCounts(t, db, 0)
	d.Items[0].Status = "approved"
	write()
	admitted, err := s.AdmitNewsroomAnnouncement(ctx, r)
	if err != nil || !admitted.Created {
		t.Fatal(admitted, err)
	}
	d.Items[0].Status = "rejected"
	write()
	status, err := s.LookupManual(ctx, owner, r.RequestKey, r.Kind, ManualFingerprint(r))
	if err != nil || status.State != "pending" || status.ActionID != admitted.ActionID {
		t.Fatal(status, err)
	}
	if _, err := s.LookupManual(ctx, owner, r.RequestKey, r.Kind, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); !errors.Is(err, ErrConflict) {
		t.Fatal("foreign fingerprint read receipt", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM platform_manual_sources WHERE /*tenant:platform_manual_sources*/`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LookupManual(ctx, owner, r.RequestKey, r.Kind, ManualFingerprint(r)); !errors.Is(err, ErrManualMissing) {
		t.Fatal("purged receipt reconstructed", err)
	}
	manualCounts(t, db, 0)
	// An open raffle's chosen shared-registry entry command is part of the
	// projected content, checked from a saved row held through admission.
	registry, err := bot.NewTenant(db)
	if err != nil {
		t.Fatal(err)
	}
	command, err := registry.SaveCommand(ctx, bot.Command{Name: "joinfixture", Action: "newsroom_raffle", Enabled: true, RoleRequired: "viewer"})
	if err != nil {
		t.Fatal(err)
	}
	d.Raffle.Open = true
	write()
	r.Kind = "newsroom_raffle"
	r.RequestKey = "00000000-0000-4000-8000-000000005678"
	projected, err = newsroom.BuildAnnouncement(d, r.Kind, "!joinfixture")
	if err != nil {
		t.Fatal(err)
	}
	r.Text = projected.Text
	r.ViewerID = projected.ViewerID
	r.ViewerLogin = projected.ViewerLogin
	if _, err := registry.SetCommandEnabled(ctx, command.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AdmitNewsroomAnnouncement(ctx, r); !errors.Is(err, ErrConflict) {
		t.Fatal("disabled raffle command announced", err)
	}
	manualCounts(t, db, 0)
}

func TestNewsroomAnnouncementHoldsDocumentThroughAdmission(t *testing.T) {
	f, db, _ := fixture(t)
	s, _ := New(db)
	d := newsroom.Empty()
	d.CurrentID = "story"
	d.Items = []newsroom.Item{{ID: "story", Status: "approved", Title: "Approved", URL: "https://example.org/story"}}
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `INSERT INTO newsroom_documents(id,document) VALUES('default',$1)`, raw); err != nil {
		t.Fatal(err)
	}
	projected, err := newsroom.BuildAnnouncement(d, "newsroom_current", "")
	if err != nil {
		t.Fatal(err)
	}
	r := manualRequest()
	r.Kind = "newsroom_current"
	r.Text = projected.Text
	r.ViewerID = projected.ViewerID
	r.ViewerLogin = projected.ViewerLogin
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
	go func() { _, err := s.AdmitNewsroomAnnouncement(ctx, r); done <- err }()
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
		_, err := editor.ExecContext(ctx, `UPDATE newsroom_documents SET document=jsonb_set(document,'{currentId}','""') WHERE tenant_id=$1 AND id='default'`, db.TenantID())
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
			t.Fatal("editor escaped retained document lock")
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
	manualCounts(t, db, 1)
}
