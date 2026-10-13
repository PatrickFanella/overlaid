package tenancy

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
	"git.subcult.tv/PatrickFanella/over-laid/internal/history"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

func TestPlanConcurrencyAndDowngrade(t *testing.T) {
	s, a, _, _, _, f := tenantPair(t)
	ctx := t.Context()
	// A private fixture plan keeps concurrent tests independent of seeded plans.
	limits := `{"commands":2,"timers":2,"apiVariables":2,"httpCallsPerHour":3,"storedEvents":2,"mediaBytes":0}`
	if _, err := f.Admin.Exec(`UPDATE plans SET limits=$1 WHERE id='free'`, limits); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Admin.Exec(`UPDATE tenants SET plan_id='free' WHERE id=$1`, a.ID); err != nil {
		t.Fatal(err)
	}
	db, _ := s.Product.Scoped(a.ID)
	store, _ := bot.NewTenant(db)
	var accepted atomic.Int32
	var workers sync.WaitGroup
	for i := 0; i < 12; i++ {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			_, err := store.SaveCommand(ctx, bot.Command{Name: fmt.Sprintf("cmd%d", i), Enabled: true, Response: "hello", Action: "ticker", RoleRequired: "viewer"})
			if err == nil {
				accepted.Add(1)
			} else if !errors.Is(err, tenantdb.ErrLimit) {
				t.Errorf("insert: %v", err)
			}
		}(i)
	}
	workers.Wait()
	if accepted.Load() != 2 {
		t.Fatalf("accepted %d commands", accepted.Load())
	}
	accepted.Store(0)
	for i := 0; i < 12; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			err := db.ReserveHTTP(ctx)
			if err == nil {
				accepted.Add(1)
			} else if !errors.Is(err, tenantdb.ErrLimit) {
				t.Errorf("quota: %v", err)
			}
		}()
	}
	workers.Wait()
	if accepted.Load() != 3 {
		t.Fatalf("accepted %d HTTP calls", accepted.Load())
	}
	if _, err := f.Admin.Exec(`UPDATE plans SET limits=jsonb_set(limits,'{commands}','1') WHERE id='free'`); err != nil {
		t.Fatal(err)
	}
	commands, err := store.ListCommands(ctx)
	if err != nil || len(commands) != 2 {
		t.Fatalf("downgrade removed config: %v %v", commands, err)
	}
	allowed := 0
	for _, command := range commands {
		if err := db.AllowItem(ctx, "commands", command.Name); err == nil {
			allowed++
		} else if !errors.Is(err, tenantdb.ErrLimit) {
			t.Fatal(err)
		}
	}
	if allowed != 1 {
		t.Fatalf("downgrade allowed %d executions", allowed)
	}
	h, _ := history.NewTenant(db)
	for i := 0; i < 4; i++ {
		if err := h.Append(events.NewAlert("test", "limit", fmt.Sprint(i), events.SeverityInfo)); err != nil {
			t.Fatal(err)
		}
	}
	if got := h.List(10); len(got) != 2 {
		t.Fatalf("stored event cap %d", len(got))
	}
}

func TestRetentionExportAndDeletion(t *testing.T) {
	s, a, b, owner, other, f := tenantPair(t)
	ctx := t.Context()
	dbA, _ := s.Product.Scoped(a.ID)
	dbB, _ := s.Product.Scoped(b.ID)
	ha, _ := history.NewTenant(dbA)
	hb, _ := history.NewTenant(dbB)
	old := events.NewAlert("test", "old", "expired", events.SeverityInfo)
	old.CreatedAt = time.Now().Add(-48 * time.Hour)
	viewer := events.NewAlert("test", "viewer", "private", events.SeverityInfo)
	viewer.Data = map[string]any{"viewerId": "target-id", "viewerLogin": "target"}
	for i, h := range []*history.PostgresStore{ha, hb} {
		old.ID = fmt.Sprint("old-", i)
		viewer.ID = fmt.Sprint("viewer-", i)
		if err := h.Append(old); err != nil {
			t.Fatal(err)
		}
		if err := h.Append(viewer); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetRetention(ctx, owner, a.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRetention(ctx, other, a.ID, 1); !errors.Is(err, ErrDenied) {
		t.Fatal("foreign retention accepted", err)
	}
	if err := s.Expire(ctx); err != nil {
		t.Fatal(err)
	}
	if len(ha.List(10)) != 1 || len(hb.List(10)) != 2 {
		t.Fatal("retention ignored tenant policy")
	}
	credential, err := s.RotateCredential(ctx, a.ID, owner, "overlay")
	if err != nil {
		t.Fatal(err)
	}
	exported, err := s.Export(ctx, owner, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(exported)
	if strings.Contains(string(raw), Hash(credential)) || strings.Contains(string(raw), "tenant_credentials") || strings.Contains(string(raw), "platform_connections") {
		t.Fatal("export leaked credential material")
	}
	if _, err = s.Export(ctx, other, a.ID); !errors.Is(err, ErrDenied) {
		t.Fatal("foreign export accepted", err)
	}
	if err = s.DeleteViewer(ctx, owner, a.ID, "target-id", "target"); err != nil {
		t.Fatal(err)
	}
	if len(ha.List(10)) != 0 || len(hb.List(10)) != 2 {
		t.Fatal("viewer deletion crossed tenant boundary")
	}
	token, _, err := s.NewSession(ctx, owner, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SelectTenant(ctx, token, owner, a.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteTenant(ctx, other, a.ID); !errors.Is(err, ErrDenied) {
		t.Fatal("foreign tenant deletion accepted", err)
	}
	if err = s.DeleteTenant(ctx, owner, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Session(ctx, token); err == nil {
		t.Fatal("deleted tenant session remains")
	}
	if _, err = s.CredentialTenant(ctx, credential, "overlay"); err == nil {
		t.Fatal("deleted tenant credential remains")
	}
	var receipt int
	if err = f.Admin.QueryRow(`SELECT count(*) FROM tenant_change_audit WHERE tenant_id=$1 AND action='tenant.delete'`, a.ID).Scan(&receipt); err != nil || receipt != 1 {
		t.Fatal("missing deletion receipt", err)
	}
	if _, err = s.Resolve(ctx, other, b.ID); err != nil {
		t.Fatal("other tenant lost", err)
	}
}
