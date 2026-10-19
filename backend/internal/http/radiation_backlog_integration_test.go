package httpapi

import (
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformingest"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitch"
)

func TestRadiationBacklogDoesNotExpandNukeBatch(t *testing.T) {
	h := ownedModerationFixture(t, false)
	h.messageIDs = map[string]bool{"owner-source": true, "future-source": true}
	server, err := h.app.runtime(t.Context(), h.account.tenant)
	if err != nil {
		t.Fatal(err)
	}
	// Delay only fixture chat jobs within the real ingest transaction. The
	// actual dispatcher/controller/pools remain healthy and fully validated.
	if _, err := h.database.Admin.Exec(`CREATE FUNCTION hold_radiation_backlog() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.kind='overlaid_chat_dispatch' THEN NEW.state='scheduled'; NEW.scheduled_at=clock_timestamp()+interval '1 hour'; END IF; RETURN NEW; END $$; CREATE TRIGGER hold_radiation_backlog BEFORE INSERT ON river_job FOR EACH ROW EXECUTE FUNCTION hold_radiation_backlog()`); err != nil {
		t.Fatal(err)
	}
	old := h.source(t, time.Now().Add(-time.Minute))
	consumer, err := h.app.prepareModeration(t.Context(), h.database.ModerationWorkerURL, h.database.ModerationPacerURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := consumer.start(t.Context()); err != nil {
		t.Fatal(err)
	}
	p := nukeHTTPPreview(t, h, "PRIVATE_OWNER_SOURCE", 5)
	if len(p.Targets) != 1 {
		t.Fatal("old preview target missing")
	}
	if w := nukePost(h, "request", nukeHTTPRequest(p)); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	waitOwnedModeration(t, h, "delivered")
	// Release the original native row without changing its identity, args,
	// deadline or attempt. The normal dispatcher now consumes the old input.
	if _, err := h.database.Admin.Exec(`DROP TRIGGER hold_radiation_backlog ON river_job; UPDATE river_job SET state='available',scheduled_at=clock_timestamp() WHERE kind='overlaid_chat_dispatch'`); err != nil {
		t.Fatal(err)
	}
	waitReceipt(t, h.app, h.account.tenant, 1)
	var automaticOld int
	if err := h.database.Admin.QueryRow(`SELECT count(*) FROM platform_moderation_actions WHERE event_id=$1`, old.ID).Scan(&automaticOld); err != nil || automaticOld != 0 {
		t.Fatal("pre-creation backlog gained radiation work", automaticOld, err)
	}
	// A projection's mutable display timestamp cannot make old input eligible.
	projected := twitch.EventsForNormalizedChat(old, server.twitchCommandProcessor, server.interactions.Rules())[0]
	bound := server.moderationSourceContext(t.Context(), old, projected)
	projected.CreatedAt = time.Now().Add(time.Minute)
	if err := server.enforceModerationForEvent(bound, projected); err != nil {
		t.Fatal(err)
	}
	db, _ := h.app.store.Product.Scoped(h.account.tenant)
	now := time.Now().UTC()
	future, err := platform.NewChatEvent(platform.Connection{TenantID: h.account.tenant, ID: "twitch:123", Platform: "twitch"}, "future-source", now, now, platform.ChatMessage{MessageID: "future-source", Text: "PRIVATE_OWNER_SOURCE", Actor: platform.Identity{ID: "888"}, Channel: platform.Identity{ID: "123"}})
	if err != nil {
		t.Fatal(err)
	}
	ingest, _ := platformingest.New(db)
	for range 2 {
		if _, err := ingest.IngestChat(t.Context(), future, nil); err != nil {
			t.Fatal(err)
		}
	}
	waitReceipt(t, h.app, h.account.tenant, 2)
	deadline := time.Now().Add(10 * time.Second)
	for {
		var delivered int
		if err := h.database.Admin.QueryRow(`SELECT count(*) FROM platform_moderation_actions a JOIN river_job j ON j.args->>'action_id'=a.id WHERE a.state='delivered' AND j.state='completed'`).Scan(&delivered); err != nil {
			t.Fatal(err)
		}
		if delivered == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("future radiation failed to settle", delivered)
		}
		time.Sleep(20 * time.Millisecond)
	}
	var actions, jobs, later int
	if err := h.database.Admin.QueryRow(`SELECT (SELECT count(*) FROM platform_moderation_actions),(SELECT count(*) FROM river_job WHERE kind='overlaid_moderation'),(SELECT count(*) FROM platform_moderation_actions WHERE event_id=$1)`, future.ID).Scan(&actions, &jobs, &later); err != nil || actions != 2 || jobs != 2 || later != 1 || h.attempts.Load() != 2 {
		t.Fatal("batch/radiation replay or expansion", actions, jobs, later, h.attempts.Load(), err)
	}
}
