package newsroom

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func applyWorkflowTest(t *testing.T, s *Store, c Command) Document {
	t.Helper()
	d, err := s.Internal(context.Background(), c)
	if err != nil {
		t.Fatal(c.Action, err)
	}
	return d
}
func playedFixture(t *testing.T, s *Store, url, notes string) Document {
	t.Helper()
	d := applyWorkflowTest(t, s, Command{Action: "submit", URL: url, Title: "Fixture"})
	id := d.Items[len(d.Items)-1].ID
	applyWorkflowTest(t, s, Command{Action: "approve", ID: id})
	applyWorkflowTest(t, s, Command{Action: "focus", ID: id})
	return applyWorkflowTest(t, s, Command{Action: "play", ID: id, Title: "Fixture", Notes: notes, SessionID: "session"})
}
func TestDurableClaimsAndFencedRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desk.json")
	s, _ := NewFile(path)
	d := applyWorkflowTest(t, s, Command{Action: "submit", URL: "https://example.org/job"})
	id := d.Items[0].ID
	applyWorkflowTest(t, s, Command{Action: "job-queue", ID: id})
	reopened, err := NewFile(path)
	if err != nil {
		t.Fatal(err)
	}
	d = applyWorkflowTest(t, reopened, Command{Action: "job-claim", ID: id, Token: "first"})
	if d.Items[0].Processing.State != "running" {
		t.Fatal("queued work did not survive restart")
	}
	if _, err = reopened.Internal(context.Background(), Command{Action: "job-claim", ID: id, Token: "second"}); !errors.Is(err, ErrConflict) {
		t.Fatal("competing claim admitted", err)
	}
	if _, err = reopened.Internal(context.Background(), Command{Action: "job-complete", ID: id, Token: "other", Enrichment: &Enrichment{Summary: "forged"}}); !errors.Is(err, ErrConflict) {
		t.Fatal("stale completion admitted", err)
	}
	// A returned file snapshot must not mutate the live lease.
	d.Items[0].Processing.Lease.Until = time.Now().Add(-time.Minute)
	live, _ := reopened.Get(context.Background())
	if !live.Items[0].Processing.Lease.Until.After(time.Now()) {
		t.Fatal("snapshot mutated store")
	}
	// Expired claims can be recovered and their old completions are fenced.
	recovered := clone(d)
	recovered.Items[0].Processing.Lease.Until = time.Now().Add(-time.Minute)
	if err := recovered.Apply(Command{Action: "job-claim", ID: id, Token: "replacement", Revision: recovered.Revision}); err != nil {
		t.Fatal(err)
	}
	if err := recovered.Apply(Command{Action: "job-complete", ID: id, Token: "first", Revision: recovered.Revision, Enrichment: &Enrichment{Title: "Late"}}); !errors.Is(err, ErrConflict) {
		t.Fatal("expired worker overwrote replacement", err)
	}
	applyWorkflowTest(t, reopened, Command{Action: "job-complete", ID: id, Token: "first", Enrichment: &Enrichment{Title: "Source", AIStatus: "disabled"}})
	d = applyWorkflowTest(t, reopened, Command{Action: "settings", Settings: &Settings{AutoEnrich: true}})
	d = applyWorkflowTest(t, reopened, Command{Action: "approve", ID: id, ActorID: "mod"})
	if d.Items[0].Processing.State != "queued" || !d.Items[0].Processing.AI || d.Items[0].ApprovedBy != "mod" {
		t.Fatal("approval did not queue AI atomically", d.Items[0])
	}
	d = applyWorkflowTest(t, reopened, Command{Action: "job-claim", ID: id, Token: "ai"})
	applyWorkflowTest(t, reopened, Command{Action: "edit", ID: id, Title: "Editorial", Notes: "Keep notes", Warning: "Audience warning"})
	d = applyWorkflowTest(t, reopened, Command{Action: "job-complete", ID: id, Token: "ai", Enrichment: &Enrichment{Error: "fixture failure", AIStatus: "failed"}})
	if d.Items[0].Title != "Editorial" || d.Items[0].Notes != "Keep notes" || d.Items[0].Enrichment.Title != "Source" || d.Items[0].Processing.State != "failed" {
		t.Fatal("failure lost metadata/editorial state", d.Items[0])
	}
}
func TestPlaySnapshotRetryUndoReplayAndRemoval(t *testing.T) {
	s, _ := NewFile("")
	d := playedFixture(t, s, "https://example.org/play", "Original takeaway")
	id := d.Items[0].ID
	at := *d.Items[0].PlayedAt
	d = applyWorkflowTest(t, s, Command{Action: "play", ID: id, Title: "Changed retry", Notes: "must not overwrite"})
	if len(d.History) != 1 || !d.Items[0].PlayedAt.Equal(at) || d.History[0].Notes != "Original takeaway" {
		t.Fatal("play retry changed history")
	}
	d = applyWorkflowTest(t, s, Command{Action: "undo-play", ID: id})
	if len(d.ActiveNotes("")) != 0 {
		t.Fatal("undo left active note")
	}
	applyWorkflowTest(t, s, Command{Action: "focus", ID: id})
	d = applyWorkflowTest(t, s, Command{Action: "play", ID: id, Title: "Replay", Notes: "New takeaway"})
	if len(d.History) != 2 || d.History[0].ID == d.History[1].ID || !d.History[0].Cancelled {
		t.Fatal("replay did not create new snapshot")
	}
	d = applyWorkflowTest(t, s, Command{Action: "delete", ID: id})
	if len(d.Items) != 0 || !strings.Contains(d.Markdown(""), "New takeaway") || strings.Contains(d.Markdown(""), "Original takeaway") {
		t.Fatal("archive removal erased detached history")
	}
}
func TestDeliveryResumesChunksAndKeepsLateNotes(t *testing.T) {
	s, _ := NewFile("")
	playedFixture(t, s, "https://example.org/one", strings.Repeat("😀", 1300))
	calls := 0
	fail := true
	e := Enricher{Keys: ProviderKeys{Discord: "https://discord.com/api/webhooks/123/fixture"}, Client: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Query().Get("wait") != "true" {
			t.Fatal("missing delivery acknowledgement")
		}
		if fail && calls == 2 {
			reply := response("")
			reply.StatusCode = 429
			return reply, nil
		}
		return response(`{"id":"fixture-message"}`), nil
	})}}
	if err := s.DeliverNotes(context.Background(), e, ""); err == nil {
		t.Fatal("partial failure reported success")
	}
	fail = false
	if err := s.DeliverNotes(context.Background(), e, ""); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatal("retry repeated acknowledged chunk", calls)
	}
	playedFixture(t, s, "https://example.org/late", "Late note")
	if err := s.DeliverNotes(context.Background(), e, ""); err != nil {
		t.Fatal(err)
	}
	if calls != 4 {
		t.Fatal("new export resent old notes", calls)
	}
	if err := s.DeliverNotes(context.Background(), e, ""); !errors.Is(err, ErrConflict) {
		t.Fatal("duplicate export permitted", err)
	}
}
func TestUncertainDeliveryAndLeaseFailClosed(t *testing.T) {
	s, _ := NewFile("")
	playedFixture(t, s, "https://example.org/unknown", "Takeaway")
	calls := 0
	e := Enricher{Keys: ProviderKeys{Discord: "https://discord.com/api/webhooks/123/fixture"}, Client: &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("response lost") })}}
	if err := s.DeliverNotes(context.Background(), e, ""); !errors.Is(err, ErrDeliveryUncertain) {
		t.Fatal(err)
	}
	if err := s.DeliverNotes(context.Background(), e, ""); !errors.Is(err, ErrDeliveryUncertain) || calls != 1 {
		t.Fatal("uncertain post retried", err, calls)
	}
	var wg sync.WaitGroup
	accepted := make(chan bool, 2)
	for _, token := range []string{"one", "two"} {
		wg.Add(1)
		go func(token string) {
			defer wg.Done()
			_, err := s.Internal(context.Background(), Command{Action: "delivery-lock", Token: token})
			accepted <- err == nil
		}(token)
	}
	wg.Wait()
	close(accepted)
	count := 0
	for ok := range accepted {
		if ok {
			count++
		}
	}
	if count != 1 {
		t.Fatal("competing delivery leases admitted", count)
	}
}
func TestShelfImportPreservesTitleAndSegments(t *testing.T) {
	s, _ := NewFile("")
	d := applyWorkflowTest(t, s, Command{Action: "shelf-import", Name: "Shared", ImportSegments: []Segment{{ID: "source", Name: "Headlines"}}, ImportItems: []ShelfLink{{URL: "https://example.org/source", Title: "Original title", SegmentID: "source"}, {URL: "https://example.org/ungrouped", Title: "Ungrouped"}}})
	if d.Items[0].Title != "Original title" || d.Items[0].SegmentID == "source" || d.Items[0].SegmentID != d.Segments[0].ID || d.Items[1].SegmentID != "" {
		t.Fatal("segment import flattened or reused IDs")
	}
}

func TestLegacyPlayedCardsAcquireHistory(t *testing.T) {
	s, _ := NewFile("")
	original := playedFixture(t, s, "https://example.org/legacy", "Legacy takeaway")
	raw, _ := json.Marshal(original)
	var old map[string]json.RawMessage
	_ = json.Unmarshal(raw, &old)
	delete(old, "history")
	raw, _ = json.Marshal(old)
	path := filepath.Join(t.TempDir(), "legacy.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewFile(path)
	if err != nil {
		t.Fatal(err)
	}
	d, err := reopened.Get(context.Background())
	if err != nil || len(d.History) != 1 || !strings.Contains(d.Markdown(""), "Legacy takeaway") {
		t.Fatal("legacy notes disappeared", err)
	}
	d = applyWorkflowTest(t, reopened, Command{Action: "delete", ID: d.Items[0].ID})
	if !strings.Contains(d.Markdown(""), "Legacy takeaway") {
		t.Fatal("legacy backfill not retained on write")
	}
}

func TestLegacyDeliveryReceiptsDoNotResendAfterNewNotes(t *testing.T) {
	s, _ := NewFile("")
	playedFixture(t, s, "https://example.org/legacy-delivered", "Old notes")
	applyWorkflowTest(t, s, Command{Action: "delivery-claim", ID: "old-whole-export"})
	applyWorkflowTest(t, s, Command{Action: "delivery-complete", ID: "old-whole-export"})
	playedFixture(t, s, "https://example.org/new-after-legacy", "New notes")
	calls := 0
	e := Enricher{Keys: ProviderKeys{Discord: "https://discord.com/api/webhooks/123/fixture"}, Client: &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) { calls++; return response(`{"id":"fixture"}`), nil })}}
	if err := s.DeliverNotes(context.Background(), e, ""); !errors.Is(err, ErrDeliveryUncertain) || calls != 0 {
		t.Fatal("legacy export scope guessed", err, calls)
	}
}
