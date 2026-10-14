package newsroom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func revisionPointer(n int64) *int64 { return &n }
func TestReviewOwnershipRevisionAndMetadataIsolation(t *testing.T) {
	s, _ := NewFile("")
	ctx := context.Background()
	d := applyWorkflowTest(t, s, Command{Action: "submit", URL: "https://example.org/review"})
	id := d.Items[0].ID
	d = applyWorkflowTest(t, s, Command{Action: "review-start", ID: id, ActorID: "mod-a"})
	token := d.Items[0].ReviewClaim.Token
	if _, err := s.Internal(ctx, Command{Action: "review-start", ID: id, ActorID: "mod-b"}); !errors.Is(err, ErrConflict) {
		t.Fatal("competing review accepted", err)
	}
	if _, err := s.Internal(ctx, Command{Action: "edit", ID: id, ActorID: "mod-b", Title: "Changed"}); !errors.Is(err, ErrConflict) {
		t.Fatal("legacy write ignored claim", err)
	}
	d = applyWorkflowTest(t, s, Command{Action: "review-extend", ID: id, ActorID: "mod-a", ReviewToken: token})
	newToken := d.Items[0].ReviewClaim.Token
	if newToken == token {
		t.Fatal("renewal failed to fence old tab")
	}
	if _, err := s.Internal(ctx, Command{Action: "review-release", ID: id, ActorID: "mod-a", ReviewToken: token}); !errors.Is(err, ErrConflict) {
		t.Fatal("old release cleared new claim")
	}
	if _, err := s.Internal(ctx, Command{Action: "review-release", ID: id, ActorID: "mod-b", ReviewToken: newToken, Force: true}); !errors.Is(err, ErrInvalid) {
		t.Fatal("non-owner force release admitted", err)
	}
	d = applyWorkflowTest(t, s, Command{Action: "review-release", ID: id, ActorID: "owner", ReviewToken: newToken, Force: true, Owner: true})
	readRevision := d.Revision
	applyWorkflowTest(t, s, Command{Action: "enriched", ID: id, Enrichment: &Enrichment{Title: "Provider headline", Publisher: "Fixture", AIStatus: "disabled"}})
	oldNotes := ""
	d, err := s.Apply(ctx, Command{Action: "edit", ID: id, ActorID: "mod-a", Revision: readRevision, ExpectedEditorialRevision: revisionPointer(0), ExpectedNotes: &oldNotes, Notes: "Editorial takeaway", Title: "Stale derived title"})
	if err != nil || d.Items[0].Title != "Provider headline" || d.Items[0].Notes != "Editorial takeaway" || d.Items[0].EditorialRevision != 1 {
		t.Fatal("metadata invalidated or overwritten by draft", err, d)
	}
	if _, err = s.Internal(ctx, Command{Action: "reject", ID: id, ActorID: "mod-b", ExpectedEditorialRevision: revisionPointer(0)}); !errors.Is(err, ErrConflict) {
		t.Fatal("stale decision accepted", err)
	}
	d = applyWorkflowTest(t, s, Command{Action: "reject", ID: id, ActorID: "mod-a", ExpectedEditorialRevision: revisionPointer(1)})
	applyWorkflowTest(t, s, Command{Action: "edit", ID: id, ActorID: "mod-a", Title: d.Items[0].Title, Notes: "Newer work"})
	if _, err = s.Internal(ctx, Command{Action: "undo-reject", ID: id, ActorID: "mod-a", ExpectedEditorialRevision: revisionPointer(2)}); !errors.Is(err, ErrConflict) {
		t.Fatal("stale Undo reversed newer edit", err)
	}
}
func TestReviewExpiryAndApprovalClear(t *testing.T) {
	d := Empty()
	if err := d.Apply(Command{Action: "submit", URL: "https://example.org/expiry"}); err != nil {
		t.Fatal(err)
	}
	id := d.Items[0].ID
	d.Items[0].ReviewClaim = &ReviewClaim{ActorID: "old", Token: "expired", Until: time.Now().Add(-time.Second)}
	if err := d.Apply(Command{Action: "review-extend", ID: id, ActorID: "old", ReviewToken: "expired", Revision: d.Revision}); !errors.Is(err, ErrConflict) {
		t.Fatal("expired renewal accepted")
	}
	if err := d.Apply(Command{Action: "review-start", ID: id, ActorID: "new", Revision: d.Revision}); err != nil {
		t.Fatal(err)
	}
	if err := d.Apply(Command{Action: "approve", ID: id, ActorID: "new", Revision: d.Revision, ExpectedEditorialRevision: revisionPointer(0)}); err != nil {
		t.Fatal(err)
	}
	if d.Items[0].ReviewClaim != nil {
		t.Fatal("approval retained review claim")
	}
}
func TestQueueCursorBacklogLiteralSearchAndHeadInsert(t *testing.T) {
	d := Empty()
	at := time.Date(2026, 10, 14, 0, 0, 0, 123456789, time.UTC)
	for n := 0; n < 256; n++ {
		d.Items = append(d.Items, Item{ID: fmt.Sprintf("%032x", n), Title: fmt.Sprintf("Story %d", n), URL: fmt.Sprintf("https://example.org/%d", n), Kind: "article", Status: "pending", CreatedAt: at})
	}
	seen := map[string]bool{}
	q := QueueSearch{Status: "pending"}
	for pageNumber := 0; ; pageNumber++ {
		page, err := d.SearchQueue(q)
		if err != nil {
			t.Fatal(err)
		}
		if pageNumber == 0 {
			if len(page.Items) != 50 || page.Total != 256 {
				t.Fatal(page)
			}
			d.Items = append(d.Items, Item{ID: "head", Title: "New head", URL: "https://example.org/head", Kind: "article", Status: "pending", CreatedAt: at.Add(time.Nanosecond)})
		}
		for _, item := range page.Items {
			if seen[item.ID] {
				t.Fatal("duplicated cursor ID", item.ID)
			}
			seen[item.ID] = true
		}
		if page.NextCursor == "" {
			break
		}
		q.Cursor = page.NextCursor
	}
	if len(seen) != 256 || seen["head"] {
		t.Fatal("cursor shifted under head insertion", len(seen))
	}
	d.Items[255].DisplayTitle = "Literal 100%_ coverage"
	d.Items[255].Enrichment = &Enrichment{Publisher: "Publisher needle", Topics: []string{"Budget needle"}}
	for _, query := range []string{"100%_", "PUBLISHER NEEDLE", "budget needle", "story 255"} {
		page, err := d.SearchQueue(QueueSearch{Query: query, Status: "pending", Kind: "article"})
		if err != nil || page.Total != 1 {
			t.Fatal(query, page.Total, err)
		}
	}
	page, _ := d.SearchQueue(QueueSearch{Status: "pending"})
	if _, err := d.SearchQueue(QueueSearch{Status: "played", Cursor: page.NextCursor}); !errors.Is(err, ErrInvalid) {
		t.Fatal("cross-filter cursor admitted")
	}
	if _, err := d.SearchQueue(QueueSearch{Cursor: "bad"}); !errors.Is(err, ErrInvalid) {
		t.Fatal("malformed cursor admitted")
	}
	page, _ = d.SearchQueue(QueueSearch{Query: "%"})
	if page.Total != 1 {
		t.Fatal("wildcard treated as pattern")
	}
}
func TestBroadcastSnapshotHistoryRecordingAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shows.json")
	s, _ := NewFile(path)
	d := applyWorkflowTest(t, s, Command{Action: "submit", URL: "https://example.org/story", Title: "Starting headline"})
	id := d.Items[0].ID
	applyWorkflowTest(t, s, Command{Action: "approve", ID: id})
	d = applyWorkflowTest(t, s, Command{Action: "broadcast-start", Title: "Evening show", Owner: true, SessionID: "native-session"})
	show := d.Broadcasts[0]
	if len(show.Rundown) != 1 || show.SessionID != "native-session" {
		t.Fatal("rundown/session not captured")
	}
	if _, err := s.Internal(context.Background(), Command{Action: "broadcast-start", Title: "Competing", Owner: true}); !errors.Is(err, ErrConflict) {
		t.Fatal("two active shows admitted")
	}
	applyWorkflowTest(t, s, Command{Action: "focus", ID: id})
	d = applyWorkflowTest(t, s, Command{Action: "play", ID: id})
	if d.History[0].BroadcastID != show.ID {
		t.Fatal("play not stamped atomically")
	}
	d = applyWorkflowTest(t, s, Command{Action: "broadcast-end", ID: show.ID, Owner: true})
	ended := *d.Broadcasts[0].EndedAt
	d = applyWorkflowTest(t, s, Command{Action: "broadcast-end", ID: show.ID, Owner: true})
	if !d.Broadcasts[0].EndedAt.Equal(ended) {
		t.Fatal("end retry changed boundary")
	}
	d = applyWorkflowTest(t, s, Command{Action: "broadcast-update", ID: show.ID, Owner: true, Title: "Evening show", VODURL: "https://youtu.be/dQw4w9WgXcQ?si=tracking", VODOffsetSeconds: 30})
	applyWorkflowTest(t, s, Command{Action: "delete", ID: id})
	reopened, err := NewFile(path)
	if err != nil {
		t.Fatal(err)
	}
	d, _ = reopened.Get(context.Background())
	markdown, err := d.BroadcastExport(show.ID, "markdown")
	if err != nil || !strings.Contains(markdown, "Starting headline") || !strings.Contains(markdown, fmt.Sprintf("t=%ds", 30+d.History[0].OffsetSeconds)) {
		t.Fatal(markdown, err)
	}
	chapters, err := d.BroadcastExport(show.ID, "chapters")
	if err != nil || !strings.Contains(chapters, "00:00 Recording begins") || !strings.Contains(chapters, ChapterTime(30+d.History[0].OffsetSeconds)+" Starting headline") {
		t.Fatal(chapters, err)
	}
	if len(d.Broadcasts[0].Rundown) != 1 {
		t.Fatal("clearing card erased rundown")
	}
	rawBefore, _ := json.Marshal(d.Deliveries)
	_, _ = d.BroadcastExport(show.ID, "markdown")
	rawAfter, _ := json.Marshal(d.Deliveries)
	if string(rawBefore) != string(rawAfter) {
		t.Fatal("show export consumed receipts")
	}
	for _, raw := range []string{"http://youtube.com/watch?v=dQw4w9WgXcQ", "https://youtube.com.evil/watch?v=dQw4w9WgXcQ", "https://user:pass@youtube.com/watch?v=dQw4w9WgXcQ", "https://www.youtube.com/playlist?list=anything"} {
		if _, err := NormalizeVOD(raw); err == nil {
			t.Fatal("invalid VOD accepted", raw)
		}
	}
}

func TestBulkTriageAdvancesEditorialRevisions(t *testing.T) {
	s, _ := NewFile("")
	d := applyWorkflowTest(t, s, Command{Action: "submit", URL: "https://example.org/bulk-review"})
	id := d.Items[0].ID
	applyWorkflowTest(t, s, Command{Action: "review-start", ID: id, ActorID: "reviewer"})
	d = applyWorkflowTest(t, s, Command{Action: "clear-pending"})
	if d.Items[0].EditorialRevision != 1 || d.Items[0].ReviewClaim != nil || d.Items[0].Status != "rejected" {
		t.Fatal("bulk triage left stale editorial state")
	}
	if _, err := s.Internal(context.Background(), Command{Action: "edit", ID: id, ExpectedEditorialRevision: revisionPointer(0)}); !errors.Is(err, ErrConflict) {
		t.Fatal("old draft survived bulk decision", err)
	}
}
