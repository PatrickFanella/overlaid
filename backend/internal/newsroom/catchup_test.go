package newsroom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func intPointer(n int) *int { return &n }
func TestPreparationCopiesTargetsAndPrivacy(t *testing.T) {
	s, _ := NewFile("")
	d := applyWorkflowTest(t, s, Command{Action: "shelf-import", Name: "Source", ImportSegments: []Segment{{ID: "filled", Name: "Opening", TargetMinutes: intPointer(15)}, {ID: "empty", Name: "Research", TargetMinutes: intPointer(20)}}, ImportItems: []ShelfLink{{Title: "Original title", URL: "https://example.org/prep", Notes: "Item preparation", SegmentID: "filled"}, {Title: "Ungrouped", URL: "https://example.org/ungrouped"}}})
	id := d.Shelves[0].ID
	segment := d.Shelves[0].Segments[0].ID
	d = applyWorkflowTest(t, s, Command{Action: "shelf-preparation", ID: id, Curate: true, Template: true, PrepPrompts: "Private checklist", UngroupedPosition: 1})
	d = applyWorkflowTest(t, s, Command{Action: "shelf-copy", ID: id, Curate: true, Name: "Working"})
	copy := d.Shelves[1]
	if copy.ID == id || copy.Template || copy.PrepPrompts != "Private checklist" || len(copy.Segments) != 2 || copy.Segments[0].ID == segment || *copy.Segments[1].TargetMinutes != 20 || copy.UngroupedPosition != 1 {
		t.Fatal(copy)
	}
	var copied Item
	for _, item := range d.Items {
		if item.ShelfID == copy.ID && item.URL == "https://example.org/prep" {
			copied = item
		}
	}
	if copied.ID == d.Items[0].ID || copied.Notes != "Item preparation" || copied.Processing.State != "complete" {
		t.Fatal(copied)
	}
	d = applyWorkflowTest(t, s, Command{Action: "shelf-send", ID: copy.ID, Curate: true})
	if len(d.Order) != 2 || len(d.Segments) != 1 || *d.Segments[0].TargetMinutes != 15 {
		t.Fatal("deck did not skip empty segments", d)
	}
	applyWorkflowTest(t, s, Command{Action: "shelf-send", ID: copy.ID, Curate: true})
	d, _ = s.Get(context.Background())
	if len(d.Order) != 2 {
		t.Fatal("duplicate approved URLs copied")
	}
	applyWorkflowTest(t, s, Command{Action: "shelf-segment", ID: copy.ID, SegmentID: copy.Segments[0].ID, Name: "Independent", TargetMinutes: intPointer(5), Curate: true})
	d, _ = s.Get(context.Background())
	if d.Shelves[0].Segments[0].Name != "Opening" || *d.Segments[0].TargetMinutes != 15 {
		t.Fatal("copy mutated source or deck")
	}
	before := d.Revision
	if _, err := s.Internal(context.Background(), Command{Action: "shelf-copy", ID: id, Name: "Denied"}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	d, _ = s.Get(context.Background())
	if d.Revision != before {
		t.Fatal("failed mutation committed")
	}
}
func TestUsageAdmissionIsAtomicAndPreservesSkippedJobs(t *testing.T) {
	s, _ := NewFile("")
	limit := int64(3000)
	applyWorkflowTest(t, s, Command{Action: "usage-settings", Owner: true, UsagePolicy: &UsagePolicy{AllowanceMicros: &limit, WarningPercent: 80}})
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for n := 0; n < 2; n++ {
		wg.Go(func() { results <- s.AdmitPaid(context.Background(), "ai", 1) })
	}
	wg.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else if !errors.Is(err, ErrUsageBlocked) {
			t.Fatal(err)
		}
	}
	if winners != 1 {
		t.Fatal("allowance overspent", winners)
	}
	d, _ := s.Get(context.Background())
	month := time.Now().UTC().Format("2006-01")
	if d.Usage[month].AI != 1 || d.Usage[month].EstimatedMicros != 3000 {
		t.Fatal(d.Usage)
	}
	calls := 0
	client := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "example.org" {
			return response(`<article>Observed content</article>`), nil
		}
		calls++
		return response("provider should not be called"), nil
	})}
	out := (Enricher{Client: client, Keys: ProviderKeys{Anthropic: "fixture", Brave: "fixture"}, Admit: s.AdmitPaid}).Enrich(context.Background(), Item{URL: "https://example.org/usage", Kind: "article"}, true)
	if out.AIStatus != "skipped" || calls != 0 {
		t.Fatal(out, calls)
	}
	if _, err := (Enricher{Client: client, Keys: ProviderKeys{Brave: "fixture"}, Admit: s.AdmitPaid}).Search(context.Background(), "topic"); !errors.Is(err, ErrUsageBlocked) || calls != 0 {
		t.Fatal("blocked search called provider", err, calls)
	}
	d = applyWorkflowTest(t, s, Command{Action: "submit", URL: "https://example.org/skipped"})
	id := d.Items[0].ID
	applyWorkflowTest(t, s, Command{Action: "approve", ID: id})
	applyWorkflowTest(t, s, Command{Action: "job-queue", ID: id, AI: true, Retry: true})
	applyWorkflowTest(t, s, Command{Action: "job-claim", ID: id, Token: "lease"})
	d = applyWorkflowTest(t, s, Command{Action: "job-complete", ID: id, Token: "lease", Enrichment: &out})
	if d.Items[0].Processing.State != "skipped" || d.Items[0].NeedsWork(time.Now()) {
		t.Fatal("skipped work automatically retried")
	}
	applyWorkflowTest(t, s, Command{Action: "job-queue", ID: id, AI: true, Retry: true})
}
func TestActivityIsTransactionalPrivateAndSearchableAfterDeletion(t *testing.T) {
	s, _ := NewFile("")
	d := applyWorkflowTest(t, s, Command{Action: "submit", Title: "Literal 100%_ title", URL: "https://example.org/activity", ViewerID: "viewer"})
	id := d.Items[0].ID
	applyWorkflowTest(t, s, Command{Action: "review-start", ID: id, ActorID: "mod"})
	applyWorkflowTest(t, s, Command{Action: "edit", ID: id, ActorID: "mod", Title: "Edited 100%_ title", Notes: "Private note", Warning: "Private warning"})
	d, _ = s.Get(context.Background())
	count := len(d.Activity)
	if _, err := s.Internal(context.Background(), Command{Action: "reject", ID: id, ActorID: "other", ExpectedEditorialRevision: revisionPointer(0)}); err == nil {
		t.Fatal("stale action accepted")
	}
	applyWorkflowTest(t, s, Command{Action: "enriched", ID: id, Enrichment: &Enrichment{Publisher: "Metadata only"}})
	d, _ = s.Get(context.Background())
	if len(d.Activity) != count {
		t.Fatal("failed/provider writes recorded activity")
	}
	applyWorkflowTest(t, s, Command{Action: "reject", ID: id, ActorID: "mod"})
	d = applyWorkflowTest(t, s, Command{Action: "delete", ID: id, ActorID: "mod"})
	raw, _ := json.Marshal(d.Activity)
	if strings.Contains(string(raw), "Private note") || strings.Contains(string(raw), "Private warning") || strings.Contains(string(raw), "token") {
		t.Fatal("private data in activity", string(raw))
	}
	page, err := d.SearchActivity("100%_", "delete", "")
	if err != nil || page.Total != 1 || page.Activity[0].ActorID != "mod" {
		t.Fatal(page, err)
	}
}
func TestCarryPublicEpisodeAndShowSearchKeepExactSnapshots(t *testing.T) {
	path := filepath.Join(t.TempDir(), "show.json")
	s, _ := NewFile(path)
	d := applyWorkflowTest(t, s, Command{Action: "create-segment", Name: "Timed"})
	seg := d.Segments[0].ID
	applyWorkflowTest(t, s, Command{Action: "segment-target", ID: seg, Curate: true, TargetMinutes: intPointer(15)})
	for n := 0; n < 2; n++ {
		d = applyWorkflowTest(t, s, Command{Action: "submit", URL: fmt.Sprintf("https://example.org/carry-%d", n), Title: fmt.Sprintf("Story %d", n), Notes: "Private saved preparation"})
		applyWorkflowTest(t, s, Command{Action: "approve", ID: d.Items[n].ID, SegmentID: seg})
	}
	d = applyWorkflowTest(t, s, Command{Action: "broadcast-start", Title: "Archived show", Owner: true})
	show := d.Broadcasts[0]
	played := d.Items[0].ID
	unused := d.Items[1].ID
	applyWorkflowTest(t, s, Command{Action: "edit", ID: played, Title: "Story 0", Notes: "On air 100%_ takeaway"})
	applyWorkflowTest(t, s, Command{Action: "focus", ID: played})
	applyWorkflowTest(t, s, Command{Action: "play", ID: played})
	applyWorkflowTest(t, s, Command{Action: "reject", ID: unused})
	applyWorkflowTest(t, s, Command{Action: "delete", ID: unused})
	applyWorkflowTest(t, s, Command{Action: "delete", ID: played})
	d = applyWorkflowTest(t, s, Command{Action: "broadcast-end", ID: show.ID, Owner: true})
	d = applyWorkflowTest(t, s, Command{Action: "broadcast-carry", ID: show.ID, Owner: true, Name: "Next show"})
	shelf := d.Shelves[0]
	if len(d.Items) != 1 || d.Items[0].Notes != "Private saved preparation" || *shelf.Segments[0].TargetMinutes != 15 {
		t.Fatal("carry lost deleted unused context", d)
	}
	d = applyWorkflowTest(t, s, Command{Action: "broadcast-carry", ID: show.ID, Owner: true, Name: "Retry ignored"})
	if len(d.Shelves) != 1 {
		t.Fatal("duplicate carry shelf")
	}
	page, err := d.SearchBroadcasts(ArchiveSearch{Query: "100%_", Status: "ended", From: show.StartedAt.UTC().Format("2006-01-02"), Through: show.StartedAt.UTC().Format("2006-01-02")})
	if err != nil || page.Total != 1 {
		t.Fatal(page, err)
	}
	preview, err := d.EpisodePreview(show.ID, "Public episode", false, false)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(preview.Episode)
	for _, private := range []string{"Private saved preparation", "On air 100%_ takeaway", played, show.ID, "rundown"} {
		if strings.Contains(string(raw), private) {
			t.Fatal("private projection leak", string(raw))
		}
	}
	hash := HashToken(strings.Repeat("a", 43))
	d = applyWorkflowTest(t, s, Command{Action: "episode-publish", ID: show.ID, Owner: true, PublicTitle: "Public episode", Fingerprint: preview.Fingerprint, PublicTokenHash: hash})
	if d.Publications[show.ID].Hash != hash {
		t.Fatal("publication not saved")
	}
	if _, err := s.Internal(context.Background(), Command{Action: "episode-publish", ID: show.ID, Owner: true, PublicTitle: "Changed after preview", Fingerprint: preview.Fingerprint, PublicTokenHash: hash}); !errors.Is(err, ErrConflict) {
		t.Fatal("stale preview published", err)
	}
	reopened, _ := NewFile(path)
	d, _ = reopened.Get(context.Background())
	if d.Publications[show.ID].Episode.Title != "Public episode" {
		t.Fatal("publication lost on restart")
	}
	d = applyWorkflowTest(t, s, Command{Action: "episode-revoke", ID: show.ID, Owner: true})
	if len(d.Publications) != 0 {
		t.Fatal("revoke failed")
	}
}
func TestBoundaryCorrectionAndTwitchFencing(t *testing.T) {
	s, _ := NewFile("")
	applyWorkflowTest(t, s, Command{Action: "automation-settings", Owner: true, Enabled: true})
	start := time.Now().UTC().Add(-time.Hour)
	event := start.Add(time.Second)
	d := applyWorkflowTest(t, s, Command{Action: "twitch-online", EventID: "online-1", EventAt: event, StartedAt: &start, TwitchStreamID: "stream-1"})
	id := d.Broadcasts[0].ID
	applyWorkflowTest(t, s, Command{Action: "twitch-online", EventID: "online-1", EventAt: event, StartedAt: &start, TwitchStreamID: "stream-1"})
	d, _ = s.Get(context.Background())
	if len(d.Broadcasts) != 1 || !d.Broadcasts[0].Automatic {
		t.Fatal(d.Broadcasts)
	}
	end := start.Add(30 * time.Minute)
	applyWorkflowTest(t, s, Command{Action: "twitch-offline", EventID: "offline-1", EventAt: end})
	d = applyWorkflowTest(t, s, Command{Action: "broadcast-boundaries", Owner: true, ID: id, ShowRevision: revisionPointer(1), StartedAt: &start, ClearEnd: true})
	if d.Broadcasts[0].Automatic || d.Broadcasts[0].EndedAt != nil {
		t.Fatal("correction failed to take manual control")
	}
	later := end.Add(time.Minute)
	d = applyWorkflowTest(t, s, Command{Action: "twitch-online", EventID: "online-2", EventAt: later, StartedAt: &later, TwitchStreamID: "stream-2"})
	if len(d.Broadcasts) != 1 {
		t.Fatal("manual active show replaced")
	}
	if _, err := s.Internal(context.Background(), Command{Action: "broadcast-boundaries", Owner: true, ID: id, ShowRevision: revisionPointer(1), StartedAt: &start}); !errors.Is(err, ErrConflict) {
		t.Fatal("stale boundary draft accepted", err)
	}
	applyWorkflowTest(t, s, Command{Action: "broadcast-end", Owner: true, ID: id})
	d = applyWorkflowTest(t, s, Command{Action: "twitch-online", EventID: "replayed-online", EventAt: time.Now(), StartedAt: &start, TwitchStreamID: "stream-1"})
	if len(d.Broadcasts) != 1 {
		t.Fatal("manually ended stream reopened")
	}
}
func TestPerShowDiscordVersionsResumeExactChunks(t *testing.T) {
	s, _ := NewFile("")
	d := applyWorkflowTest(t, s, Command{Action: "submit", URL: "https://example.org/show-export", Title: "Export story", Notes: strings.Repeat("Original notes ", 400)})
	id := d.Items[0].ID
	applyWorkflowTest(t, s, Command{Action: "approve", ID: id})
	d = applyWorkflowTest(t, s, Command{Action: "broadcast-start", Owner: true, Title: "Export show"})
	show := d.Broadcasts[0].ID
	applyWorkflowTest(t, s, Command{Action: "focus", ID: id})
	applyWorkflowTest(t, s, Command{Action: "play", ID: id})
	d = applyWorkflowTest(t, s, Command{Action: "broadcast-end", Owner: true, ID: show})
	preview, _ := d.ShowDiscordPreview(show)
	calls := 0
	failed := false
	client := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 2 && !failed {
			failed = true
			return &http.Response{StatusCode: 429, Body: response("").Body}, nil
		}
		return response(`{"id":"acknowledged"}`), nil
	})}
	e := Enricher{Client: client, Keys: ProviderKeys{Discord: "https://discord.com/api/webhooks/1/fixture"}}
	job, err := s.DeliverShow(context.Background(), e, show, preview.Fingerprint, "")
	if err == nil || job.Chunks[0].Status != "delivered" || job.Chunks[1].Status != "failed" {
		t.Fatal(job, err)
	}
	original := job.Chunks[0].Text
	d = applyWorkflowTest(t, s, Command{Action: "broadcast-update", Owner: true, ID: show, Title: "Renamed after failure"})
	job, err = s.DeliverShow(context.Background(), e, show, "", job.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, chunk := range job.Chunks {
		if chunk.Status != "delivered" {
			t.Fatal("did not resume saved version")
		}
	}
	if job.Chunks[0].Text != original {
		t.Fatal("saved text changed")
	}
	before := calls
	_, err = s.DeliverShow(context.Background(), e, show, "", job.ID)
	if err != nil || calls != before {
		t.Fatal("completed version resent", calls, err)
	}
	if _, err = s.DeliverShow(context.Background(), e, show, preview.Fingerprint, ""); !errors.Is(err, ErrConflict) {
		t.Fatal("stale preview posted", err)
	}
	d, _ = s.Get(context.Background())
	if len(d.Deliveries) != 0 {
		t.Fatal("per-show export advanced general receipts")
	}
}

func TestArchivedShowAndActivityCursorsArePreciseAndFilterBound(t *testing.T) {
	d := Empty()
	at := time.Date(2026, 10, 14, 0, 0, 0, 123456789, time.UTC)
	end := at.Add(time.Hour)
	for n := 0; n < 101; n++ {
		id := fmt.Sprintf("%032x", n)
		d.Broadcasts = append(d.Broadcasts, Broadcast{ID: id, Title: "Literal 100%_ archive", StartedAt: at, EndedAt: &end})
		d.Activity = append(d.Activity, ModeratorActivity{ID: id, At: at, Title: "Literal 100%_ action", Action: "edit", ActorID: "mod"})
	}
	q := ArchiveSearch{Query: "100%_", Status: "ended", From: "2026-10-14", Through: "2026-10-14"}
	seen := map[string]bool{}
	for {
		page, err := d.SearchBroadcasts(q)
		if err != nil {
			t.Fatal(err)
		}
		for _, show := range page.Broadcasts {
			if seen[show.ID] {
				t.Fatal("duplicate archived show")
			}
			seen[show.ID] = true
		}
		if q.Cursor == "" {
			if len(page.Broadcasts) != 50 {
				t.Fatal("unbounded page")
			}
			if _, err = d.SearchActivity("100%_", "edit", page.NextCursor); !errors.Is(err, ErrInvalid) {
				t.Fatal("cross-resource cursor admitted")
			}
			other := q
			other.Status = "active"
			other.Cursor = page.NextCursor
			if _, err = d.SearchBroadcasts(other); !errors.Is(err, ErrInvalid) {
				t.Fatal("cross-filter cursor admitted")
			}
		}
		if page.NextCursor == "" {
			break
		}
		q.Cursor = page.NextCursor
	}
	if len(seen) != 101 {
		t.Fatal(len(seen))
	}
	cursor := ""
	count := 0
	for {
		page, err := d.SearchActivity("100%_", "edit", cursor)
		if err != nil {
			t.Fatal(err)
		}
		count += len(page.Activity)
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if count != 101 {
		t.Fatal(count)
	}
	if _, err := d.SearchBroadcasts(ArchiveSearch{From: "2026-02-30"}); err == nil {
		t.Fatal("invalid UTC date admitted")
	}
}

func TestStatusFenceAndIdempotentPlayDoNotCreateActivity(t *testing.T) {
	s, _ := NewFile("")
	d := applyWorkflowTest(t, s, Command{Action: "submit", URL: "https://example.org/fence"})
	id := d.Items[0].ID
	if _, err := s.Internal(context.Background(), Command{Action: "approve", ID: id, ExpectedStatus: "played"}); !errors.Is(err, ErrConflict) {
		t.Fatal("status fence admitted wrong lead", err)
	}
	d = applyWorkflowTest(t, s, Command{Action: "approve", ID: id, ExpectedStatus: "pending", ActorID: "mod"})
	applyWorkflowTest(t, s, Command{Action: "focus", ID: id})
	d = applyWorkflowTest(t, s, Command{Action: "play", ID: id, ActorID: "owner"})
	revision := d.Items[0].EditorialRevision
	count := len(d.Activity)
	history := len(d.History)
	d = applyWorkflowTest(t, s, Command{Action: "play", ID: id, ActorID: "owner"})
	if d.Items[0].EditorialRevision != revision || len(d.Activity) != count || len(d.History) != history {
		t.Fatal("play retry advanced editorial state")
	}
}
func TestProviderAndDiscordRedirectsDoNotDispatchAgain(t *testing.T) {
	original, _ := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", nil)
	original.Header.Set("x-api-key", "fixture")
	redirect, _ := http.NewRequest("POST", "https://api.anthropic.com/another-request", nil)
	if PublicClient(nil).CheckRedirect(redirect, []*http.Request{original}) == nil {
		t.Fatal("paid provider redirect allowed")
	}
	calls := 0
	client := &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://discord.com/api/webhooks/1/other"}}, Body: response("").Body}, nil
	})}
	err := (Enricher{Client: client}).postChunk(context.Background(), "https://discord.com/api/webhooks/1/fixture", "fixture")
	if err == nil || calls != 1 {
		t.Fatal("Discord followed redirect", calls, err)
	}
}
