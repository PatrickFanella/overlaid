package newsroom

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
)

func TestDeskLifecycleAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "newsroom.json")
	s, err := NewFile(path)
	if err != nil {
		t.Fatal(err)
	}
	apply := func(c Command) Document {
		t.Helper()
		d, _ := s.Get(context.Background())
		c.Revision = d.Revision
		out, err := s.Apply(context.Background(), c)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	d := apply(Command{Action: "submit", URL: "https://example.org/story?utm_source=chat", Title: "Headline"})
	id := d.Items[0].ID
	if _, err = s.Apply(context.Background(), Command{Action: "approve", ID: id, Revision: 0}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale write: %v", err)
	}
	if _, err = s.Internal(context.Background(), Command{Action: "submit", URL: "https://example.org/story"}); !errors.Is(err, ErrConflict) {
		t.Fatal("normalized duplicate accepted")
	}
	d = apply(Command{Action: "create-shelf", Name: "Next show"})
	shelf := d.Shelves[0].ID
	apply(Command{Action: "shelve", ID: id, ShelfID: shelf})
	apply(Command{Action: "approve", ID: id})
	apply(Command{Action: "focus", ID: id})
	d = apply(Command{Action: "play", ID: id, Title: "Headline", Notes: "Takeaway", SessionID: "session-a"})
	if d.CurrentID != "" || len(d.Order) != 0 || d.Items[0].Status != "played" {
		t.Fatalf("invalid played state: %+v", d)
	}
	reopened, err := NewFile(path)
	if err != nil {
		t.Fatal(err)
	}
	d, err = reopened.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.Markdown("session-a"), "Takeaway") || strings.Contains(d.Markdown("session-b"), "Headline") {
		t.Fatal("notes not session-scoped")
	}
}

func TestAtomicBatchAndConflictingWriters(t *testing.T) {
	s, _ := NewFile("")
	_, err := s.Apply(context.Background(), Command{Action: "batch-submit", URLs: []string{"https://example.org/valid", "javascript:alert(1)"}})
	if err == nil {
		t.Fatal("bad batch accepted")
	}
	d, _ := s.Get(context.Background())
	if d.Revision != 0 || len(d.Items) != 0 {
		t.Fatal("failed batch committed partial data")
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	success := 0
	conflicts := 0
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Apply(context.Background(), Command{Action: "create-shelf", Name: "Shared"})
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				success++
			} else if errors.Is(err, ErrConflict) {
				conflicts++
			} else {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if success != 1 || conflicts != 9 {
		t.Fatalf("success=%d conflicts=%d", success, conflicts)
	}
}

func TestFailedFileWriteKeepsState(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "blocked")
	if err := os.WriteFile(parent, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	s := &Store{path: filepath.Join(parent, "newsroom.json"), doc: Empty()}
	if _, err := s.Apply(context.Background(), Command{Action: "submit", URL: "https://example.org/story"}); err == nil {
		t.Fatal("expected write failure")
	}
	d, _ := s.Get(context.Background())
	if len(d.Items) != 0 || d.Revision != 0 {
		t.Fatal("failed persistence changed memory")
	}
}

func TestEncryptedKeysAndRotation(t *testing.T) {
	s, _ := NewFile(filepath.Join(t.TempDir(), "desk.json"))
	key := []byte(strings.Repeat("a", 32))
	old := tenancy.Keyring{Active: "v1", Keys: map[string][]byte{"v1": key}}
	if err := s.SaveKeys(context.Background(), old, ProviderKeys{Anthropic: "fixture-secret"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(s.path + ".keys")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "fixture-secret") {
		t.Fatal("plaintext key retained")
	}
	rotated := tenancy.Keyring{Active: "v2", Keys: map[string][]byte{"v1": key, "v2": []byte(strings.Repeat("b", 32))}}
	keys, err := s.Keys(context.Background(), rotated)
	if err != nil || keys.Anthropic != "fixture-secret" {
		t.Fatal(keys, err)
	}
	onlyNew := tenancy.Keyring{Active: "v2", Keys: map[string][]byte{"v2": rotated.Keys["v2"]}}
	if _, err = s.Keys(context.Background(), onlyNew); err != nil {
		t.Fatal("credential read did not rotate", err)
	}
}

func TestQuestionsRaffleAndDeliveryReceipt(t *testing.T) {
	d := Empty()
	apply := func(c Command) {
		t.Helper()
		c.Revision = d.Revision
		if err := d.Apply(c); err != nil {
			t.Fatal(err)
		}
	}
	apply(Command{Action: "question-submit", Text: "A question?", SubmittedBy: "viewer"})
	id := d.Questions[0].ID
	if err := d.Apply(Command{Action: "question-show", ID: id, Revision: d.Revision}); !errors.Is(err, ErrConflict) {
		t.Fatal("unapproved question shown")
	}
	apply(Command{Action: "question-approve", ID: id})
	apply(Command{Action: "question-show", ID: id})
	apply(Command{Action: "question-answer", ID: id})
	if d.CurrentQuestion != "" {
		t.Fatal("answered question still on overlay")
	}
	apply(Command{Action: "raffle-start"})
	apply(Command{Action: "raffle-enter", SubmittedBy: "one"})
	apply(Command{Action: "raffle-enter", SubmittedBy: "two"})
	apply(Command{Action: "raffle-draw"})
	first := d.Raffle.Winner
	apply(Command{Action: "raffle-reroll"})
	if d.Raffle.Open || d.Raffle.Winner == first {
		t.Fatal("raffle state invalid")
	}
	apply(Command{Action: "delivery-claim", ID: "digest"})
	apply(Command{Action: "delivery-failed", ID: "digest"})
	if err := d.Apply(Command{Action: "delivery-claim", ID: "digest", Revision: d.Revision}); !errors.Is(err, ErrConflict) {
		t.Fatal("uncertain delivery was reposted")
	}
}

func TestCapabilityRotationAndShelfSharing(t *testing.T) {
	s, _ := NewFile(filepath.Join(t.TempDir(), "desk.json"))
	d, err := s.Internal(context.Background(), Command{Action: "create-shelf", Name: "Shared"})
	if err != nil {
		t.Fatal(err)
	}
	id := d.Shelves[0].ID
	token, err := s.NewToken(context.Background(), "share", id, false)
	if err != nil {
		t.Fatal(err)
	}
	shelf, ok := s.CheckToken(context.Background(), token, "share")
	if !ok || shelf != id {
		t.Fatal("share token invalid")
	}
	if _, ok = s.CheckToken(context.Background(), token, "add"); ok {
		t.Fatal("share token authorized add")
	}
	rotated, err := s.NewToken(context.Background(), "share", id, false)
	if err != nil {
		t.Fatal(err)
	}
	if rotated == token {
		t.Fatal("rotation reused token")
	}
	if _, ok = s.CheckToken(context.Background(), token, "share"); ok {
		t.Fatal("old share still valid")
	}
	raw, _ := os.ReadFile(s.path + ".access")
	if strings.Contains(string(raw), rotated) || strings.Contains(string(raw), token) {
		t.Fatal("raw capability retained")
	}
}

func TestEnrichmentKeepsManualTitleAndConcurrentCoverage(t *testing.T) {
	s, _ := NewFile("")
	d, err := s.Internal(context.Background(), Command{Action: "submit", URL: "https://example.org/a", Title: "Manual title"})
	if err != nil {
		t.Fatal(err)
	}
	id := d.Items[0].ID
	seg, err := s.Internal(context.Background(), Command{Action: "create-segment", Name: "Headlines"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Internal(context.Background(), Command{Action: "segment", ID: id, SegmentID: seg.Segments[0].ID}); err != nil {
		t.Fatal(err)
	}
	_, err = s.Internal(context.Background(), Command{Action: "coverage", ID: id, Enrichment: &Enrichment{Coverage: []Coverage{{Title: "Related", URL: "https://example.org/b"}}}})
	if err != nil {
		t.Fatal(err)
	}
	d, err = s.Internal(context.Background(), Command{Action: "enriched", ID: id, Enrichment: &Enrichment{Title: "Extracted title", Summary: "New summary"}})
	if err != nil {
		t.Fatal(err)
	}
	if d.Items[0].Title != "Manual title" || len(d.Items[0].Enrichment.Coverage) != 1 || d.Items[0].Enrichment.Summary != "New summary" {
		t.Fatal("enrichment clobbered manual/concurrent state")
	}
}
