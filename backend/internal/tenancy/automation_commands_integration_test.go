package tenancy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
)

func TestCommandMigrationCanonicalInventoryAndStaleSource(t *testing.T) {
	s, a, b, owner, other, f := tenantPair(t)
	store, scope := automationStore(t, s, a.ID)
	registry, err := bot.NewTenant(scope)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	saved := map[string]bot.Command{}
	for i, action := range []string{"ticker", "alert", "newsroom_submit", "newsroom_capture", "newsroom_current", "newsroom_question", "newsroom_raffle"} {
		c, e := registry.SaveCommand(ctx, bot.Command{Name: fmt.Sprintf("cmd-%d", i), Enabled: i%2 == 0, Action: action, Response: "PRIVATE_RESPONSE {user}", ResponseMode: []string{"overlay", "chat", "both"}[i%3], RoleRequired: []string{"viewer", "supporter", "moderator", "broadcaster", "operator", "system"}[i%6], CooldownSeconds: 31 * i})
		if e != nil {
			t.Fatal(e)
		}
		saved[c.ID] = c
	}
	alias, err := registry.SaveCommand(ctx, bot.Command{Name: "alias", Enabled: true, Action: "newsroom_current", ResponseMode: "chat", RoleRequired: "moderator", CooldownSeconds: 31})
	if err != nil {
		t.Fatal(err)
	}
	saved[alias.ID] = alias
	before := ""
	snapshot := `SELECT jsonb_agg(to_jsonb(c) ORDER BY id)::text FROM bot_commands c WHERE tenant_id=$1`
	if err = f.Admin.QueryRow(snapshot, a.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	items, err := store.ListCommandMigration(ctx, owner)
	if err != nil || len(items) != 8 || items[0].ID != alias.ID {
		t.Fatal("whole ordered catalogue", len(items), err)
	}
	for _, item := range items {
		c := saved[item.ID]
		kind := "template"
		if bot.IsNewsroomAction(c.Action) {
			kind = "editorial"
		}
		if item.Name != c.Name || item.Action != c.Action || item.ResponseMode != c.ResponseMode || item.RoleRequired != c.RoleRequired || item.CooldownSeconds != c.CooldownSeconds || item.Enabled != c.Enabled || item.Kind != kind || item.Snapshot != bot.CommandSnapshotFingerprint(a.ID, c) {
			t.Fatal("saved behavior changed", item.ID)
		}
		source, e := store.ReadCommandMigration(ctx, owner, item.ID, item.Snapshot)
		if e != nil || source.CommandMigrationSummary != item || source.Response != c.Response {
			t.Fatal("canonical source mismatch", item.ID, e)
		}
		raw, e := json.Marshal(source)
		if e != nil || strings.Contains(string(raw), "PRIVATE_RESPONSE") || strings.Contains(string(raw), "Response\"") {
			t.Fatal("source template escaped metadata serialization", e)
		}
	}
	var after string
	if err = f.Admin.QueryRow(snapshot, a.ID).Scan(&after); err != nil || before != after {
		t.Fatal("reads changed saved commands", err)
	}
	for _, table := range []string{"engine_automations", "engine_automation_versions", "river_job", "bot_counters", "bot_audit_events", "tenant_usage"} {
		var count int
		if err = f.Admin.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatal("preview wrote product state", table, count, err)
		}
	}
	foreign, _ := automationStore(t, s, b.ID)
	if list, e := foreign.ListCommandMigration(ctx, other); e != nil || list == nil || len(list) != 0 {
		t.Fatal("foreign catalogue", e)
	}
	item := items[1]
	if source, e := foreign.ReadCommandMigration(ctx, other, item.ID, item.Snapshot); !errors.Is(e, sql.ErrNoRows) || source.ID != "" || source.Response != "" {
		t.Fatal("foreign source", e)
	}
	c := saved[item.ID]
	c.Response = "CHANGED_PRIVATE"
	if _, err = registry.SaveCommand(ctx, c); err != nil {
		t.Fatal(err)
	}
	if source, e := store.ReadCommandMigration(ctx, owner, item.ID, item.Snapshot); !errors.Is(e, ErrAutomationConflict) || source.ID != "" || source.Response != "" {
		t.Fatal("stale edit accepted", e)
	}
	if err = registry.DeleteCommand(ctx, item.ID); err != nil {
		t.Fatal(err)
	}
	if source, e := store.ReadCommandMigration(ctx, owner, item.ID, item.Snapshot); !errors.Is(e, sql.ErrNoRows) || source.ID != "" {
		t.Fatal("deleted source accepted", e)
	}
	c.ID = ""
	if _, err = registry.SaveCommand(ctx, c); err != nil {
		t.Fatal(err)
	}
	if source, e := store.ReadCommandMigration(ctx, owner, item.ID, item.Snapshot); !errors.Is(e, sql.ErrNoRows) || source.ID != "" {
		t.Fatal("recreated name reused deleted identity", e)
	}
	for _, role := range []string{"editor", "moderator", ""} {
		if err = s.SetMember(ctx, owner, a.ID, other, role); err != nil {
			t.Fatal(err)
		}
		list, e := store.ListCommandMigration(ctx, other)
		source, re := store.ReadCommandMigration(ctx, other, alias.ID, items[0].Snapshot)
		if role == "editor" {
			if e != nil || re != nil || len(list) != 8 || source.ID != alias.ID {
				t.Fatal("configure read refused", e, re)
			}
		} else if !errors.Is(e, ErrDenied) || !errors.Is(re, ErrDenied) || list != nil || source.ID != "" {
			t.Fatal("membership was not fresh", role, e, re)
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if list, e := store.ListCommandMigration(canceled, owner); !errors.Is(e, context.Canceled) || list != nil {
		t.Fatal("canceled catalogue", e)
	}
	if source, e := store.ReadCommandMigration(canceled, owner, alias.ID, items[0].Snapshot); !errors.Is(e, context.Canceled) || source.ID != "" {
		t.Fatal("canceled source", e)
	}
}

func TestCommandMigrationBoundsAndMalformedRowsReturnNoPartialResult(t *testing.T) {
	s, a, _, owner, _, f := tenantPair(t)
	store, _ := automationStore(t, s, a.ID)
	if _, err := f.Admin.Exec(`INSERT INTO bot_commands(tenant_id,id,name,response) SELECT $1,'fixture-'||i,'command-'||lpad(i::text,4,'0'),'private response' FROM generate_series(1,$2) i`, a.ID, MaxCommandMigrationItems+1); err != nil {
		t.Fatal(err)
	}
	if list, err := store.ListCommandMigration(t.Context(), owner); !errors.Is(err, ErrLimit) || list != nil {
		t.Fatal("overflow returned partial catalogue", len(list), err)
	}
	if _, err := f.Admin.Exec(`DELETE FROM bot_commands WHERE tenant_id=$1 AND id NOT IN('fixture-1','fixture-2')`, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Admin.Exec(`UPDATE bot_commands SET role_required='invalid' WHERE tenant_id=$1 AND id='fixture-2'`, a.ID); err != nil {
		t.Fatal(err)
	}
	if list, err := store.ListCommandMigration(t.Context(), owner); !errors.Is(err, ErrAutomationInvalid) || list != nil {
		t.Fatal("malformed row returned partial catalogue", len(list), err)
	}
	for _, input := range []struct{ id, snapshot string }{{"", strings.Repeat("a", 64)}, {strings.Repeat("i", 129), strings.Repeat("a", 64)}, {"fixture-1", strings.Repeat("A", 64)}, {"fixture-1", strings.Repeat("a", 63)}} {
		if source, err := store.ReadCommandMigration(t.Context(), owner, input.id, input.snapshot); !errors.Is(err, ErrAutomationInvalid) || source.ID != "" {
			t.Fatal("invalid source binding accepted", err)
		}
	}
}
