package tenancy

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestOperatorChatCancelOnlyUnclaimedReplies(t *testing.T) {
	s, a, _, owner, other, f := tenantPair(t)
	store, scope := automationStore(t, s, a.ID)
	ctx := t.Context()
	if err := s.SaveConnection(ctx, botKeys(t), a.ID, owner, "twitch", "channel_a", "channel_a", nil, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	request, p := prepareChatAdmission(t, store, owner, 3)
	// Nothing admitted yet: nothing to cancel and nothing is created.
	if got, err := store.CancelOperatorChatAdmission(ctx, owner, request, p.Digest); err == nil || got.SourceID != "" {
		t.Fatal("canceled before admission", got, err)
	}
	admitted, err := store.AdmitOperatorChatPlan(ctx, owner, request, p.Digest)
	if err != nil || len(admitted.ActionIDs) != 3 {
		t.Fatal(admitted, err)
	}
	// The first reply is already claimed; it may be at the provider and is never recalled.
	if _, err = f.Admin.Exec(`UPDATE platform_outbound_actions SET state='running',code='started',claim_hash=repeat('e',64),started_at=now() WHERE tenant_id=$1 AND id=$2`, a.ID, admitted.ActionIDs[0]); err != nil {
		t.Fatal(err)
	}
	if _, err = store.CancelOperatorChatAdmission(ctx, other, request, p.Digest); err == nil {
		t.Fatal("foreign actor canceled")
	}
	if _, err = store.CancelOperatorChatAdmission(ctx, owner, request, strings.Repeat("a", 64)); !errors.Is(err, ErrAutomationConflict) {
		t.Fatal("digest", err)
	}
	got, err := store.CancelOperatorChatAdmission(ctx, owner, request, p.Digest)
	if err != nil || got.SourceID != p.ID || !reflect.DeepEqual(got.Canceled, admitted.ActionIDs[1:]) || got.CanceledAt.IsZero() {
		t.Fatal("cancel", got, err)
	}
	var states string
	if err = scope.QueryRowContext(ctx, `SELECT string_agg(state||'/'||code||'/'||failure||'/'||(started_at IS NULL)::text||'/'||(finished_at IS NOT NULL)::text,',' ORDER BY array_position($1::text[],id)) FROM platform_outbound_actions`, admitted.ActionIDs).Scan(&states); err != nil || states != "running/started//false/false,rejected/canceled//true/true,rejected/canceled//true/true" {
		t.Fatal("states", states, err)
	}
	again, err := store.CancelOperatorChatAdmission(ctx, owner, request, p.Digest)
	if err != nil || len(again.Canceled) != 0 {
		t.Fatal("repeat cancel", again, err)
	}
	status, err := store.LookupOperatorChatAdmission(ctx, owner, request, p.Digest)
	if err != nil || status.Actions[0].State != "running" || status.Actions[1].Code != "canceled" || status.Actions[2].Code != "canceled" {
		t.Fatal("lookup after cancel", status, err)
	}
	// A duplicate admission stays a no-op and never re-enqueues canceled work.
	if dup, err := store.AdmitOperatorChatPlan(ctx, owner, request, p.Digest); err != nil || dup.Created {
		t.Fatal("duplicate admission after cancel", dup, err)
	}
	var jobs int
	if err = f.Admin.QueryRow(`SELECT count(*) FROM river_job WHERE args->>'tenant_id'=$1`, a.ID).Scan(&jobs); err != nil || jobs != 3 {
		t.Fatal("jobs", jobs, err)
	}
	// The schema refuses canceled on a started reply or a non-engine row.
	if _, err = f.Admin.Exec(`UPDATE platform_outbound_actions SET state='rejected',code='canceled',failure='',claim_hash=NULL,finished_at=now() WHERE tenant_id=$1 AND id=$2`, a.ID, admitted.ActionIDs[0]); err == nil {
		t.Fatal("canceled a started reply")
	}
	if _, err = f.Admin.Exec(`UPDATE tenant_memberships SET role='moderator' WHERE tenant_id=$1 AND account_id=$2`, a.ID, owner); err != nil {
		t.Fatal(err)
	}
	if _, err = store.CancelOperatorChatAdmission(ctx, owner, request, p.Digest); !errors.Is(err, ErrDenied) {
		t.Fatal("moderator canceled", err)
	}
}
