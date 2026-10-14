package newsroom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEvidenceRecordsObservedInputWithoutProviderPayloads(t *testing.T) {
	for _, tc := range []struct {
		kind, body, source string
		observed           bool
	}{{"article", "real article", "page_text", true}, {"youtube", "Video transcript:\nspoken words", "transcript", true}, {"youtube", "Transcript unavailable. Description only", "description", true}, {"social", "public post", "post_text", false}, {"article", "", "unavailable", false}} {
		evidence := extractionEvidence(Item{Kind: tc.kind}, Enrichment{Body: tc.body}, tc.observed)
		if evidence.Source != tc.source || evidence.InputCharacters != len([]rune(tc.body)) || evidence.CheckedAt.IsZero() {
			t.Fatal(tc, evidence)
		}
	}
	long := extractionEvidence(Item{}, Enrichment{Body: strings.Repeat("界", 16001)}, true)
	if !long.Truncated || long.InputCharacters != 16000 {
		t.Fatal(long)
	}
	for _, tc := range []struct{ reply, outcome string }{{`{"stop_reason":"refusal","content":[]}`, "refused"}, {`{"content":[{"type":"text","text":"private invalid response"}]}`, "invalid_response"}, {`{"content":[{"type":"text","text":"{\"summary\":\"\",\"credibility\":\"unknown\",\"dmca_risk\":\"low\"}"}]}`, "invalid_response"}} {
		client := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Host == "example.org" {
				return response(`<article>Observed text</article>`), nil
			}
			return response(tc.reply), nil
		})}
		out := (Enricher{Client: client, Keys: ProviderKeys{Anthropic: "fixture"}}).Enrich(context.Background(), Item{URL: "https://example.org/story", Kind: "article"}, true)
		if out.AIStatus != "failed" || out.AIInputEvidence.Outcome != tc.outcome || out.AIInputEvidence.Retrieval != "supplied_text" || out.ExtractionEvidence.Retrieval != "backend_observed" {
			t.Fatal(out)
		}
		raw, _ := json.Marshal(out.AIInputEvidence)
		if strings.Contains(string(raw), "private invalid") || strings.Contains(string(raw), "Observed text") {
			t.Fatal("evidence retained provider payload")
		}
	}
	client := &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("private transport detail") })}
	out := (Enricher{Client: client}).Enrich(context.Background(), Item{URL: "https://example.org/story", Kind: "article"}, true)
	if out.ExtractionEvidence.Source != "unavailable" || out.ExtractionEvidence.Retrieval != "failed" || out.AIInputEvidence.Outcome != "skipped" {
		t.Fatal(out)
	}
}
func TestEvidenceStaysWithSummaryAcrossMetadataRefresh(t *testing.T) {
	s, _ := NewFile("")
	d := applyWorkflowTest(t, s, Command{Action: "submit", URL: "https://example.org/evidence"})
	id := d.Items[0].ID
	evidence := &InputEvidence{Version: 1, Source: "transcript", Outcome: "succeeded", Model: "fixture", InputCharacters: 42}
	applyWorkflowTest(t, s, Command{Action: "enriched", ID: id, Enrichment: &Enrichment{Summary: "Retained AI notes", AIInputEvidence: evidence, AIStatus: "complete"}})
	d = applyWorkflowTest(t, s, Command{Action: "enriched", ID: id, Enrichment: &Enrichment{Title: "New metadata", ExtractionEvidence: &InputEvidence{Source: "page_metadata"}}})
	if d.Items[0].Enrichment.Summary != "Retained AI notes" || d.Items[0].Enrichment.AIInputEvidence.Source != "transcript" || d.Items[0].Enrichment.ExtractionEvidence.Source != "page_metadata" {
		t.Fatal(d.Items[0])
	}
	d = applyWorkflowTest(t, s, Command{Action: "enriched", ID: id, Enrichment: &Enrichment{Summary: "Imported legacy notes"}})
	if d.Items[0].Enrichment.AIInputEvidence != nil {
		t.Fatal("guessed evidence for legacy notes")
	}
}
func TestStoryBundleSnapshotsConflictAndDetachedHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bundle.json")
	s, _ := NewFile(path)
	d := applyWorkflowTest(t, s, Command{Action: "submit", URL: "https://example.org/lead", Title: "Lead headline"})
	id := d.Items[0].ID
	if _, err := s.Internal(context.Background(), Command{Action: "bundle-create", ID: id}); !errors.Is(err, ErrInvalid) {
		t.Fatal("untrusted curation admitted")
	}
	d = applyWorkflowTest(t, s, Command{Action: "bundle-create", ID: id, Curate: true})
	bundleID := d.Bundles[0].ID
	d = applyWorkflowTest(t, s, Command{Action: "bundle-create", ID: id, Curate: true})
	if len(d.Bundles) != 1 {
		t.Fatal("duplicate bundle")
	}
	sources := []BundleSource{{Title: "Budget", URL: "https://example.org/budget", Role: "primary_document"}}
	d = applyWorkflowTest(t, s, Command{Action: "bundle-edit", ID: bundleID, Title: "Budget story", Notes: "Starting preparation", Sources: sources, BundleRevision: revisionPointer(0), Curate: true})
	applyWorkflowTest(t, s, Command{Action: "approve", ID: id})
	d = applyWorkflowTest(t, s, Command{Action: "broadcast-start", Title: "Bundle show", Owner: true})
	showID := d.Broadcasts[0].ID
	applyWorkflowTest(t, s, Command{Action: "bundle-edit", ID: bundleID, Title: "Played budget story", Notes: "Prepared for play", Sources: sources, BundleRevision: revisionPointer(1), Curate: true})
	applyWorkflowTest(t, s, Command{Action: "focus", ID: id})
	d = applyWorkflowTest(t, s, Command{Action: "play", ID: id})
	applyWorkflowTest(t, s, Command{Action: "bundle-edit", ID: bundleID, Title: "Later title", Notes: "Later edit", BundleRevision: revisionPointer(2), Curate: true})
	if _, err := s.Internal(context.Background(), Command{Action: "bundle-edit", ID: bundleID, Title: "Stale", BundleRevision: revisionPointer(2), Curate: true}); !errors.Is(err, ErrConflict) {
		t.Fatal("stale bundle edit admitted", err)
	}
	d = applyWorkflowTest(t, s, Command{Action: "delete", ID: id})
	if d.Bundles[0].LeadID != "" {
		t.Fatal("deleted lead not detached")
	}
	reopened, err := NewFile(path)
	if err != nil {
		t.Fatal(err)
	}
	d, _ = reopened.Get(context.Background())
	if d.Broadcasts[0].Rundown[0].Bundle.PrepNotes != "Starting preparation" || d.History[0].Bundle.PrepNotes != "Prepared for play" || d.History[0].Bundle.Sources[0].Role != "primary_document" {
		t.Fatal("bundle snapshots changed")
	}
	markdown, _ := d.BroadcastExport(showID, "markdown")
	chapters, _ := d.BroadcastExport(showID, "chapters")
	if !strings.Contains(markdown, "Prepared for play") || !strings.Contains(markdown, "https://example.org/budget") || strings.Contains(markdown, "Later edit") || !strings.Contains(chapters, "Played budget story") {
		t.Fatal(markdown, chapters)
	}
}
func TestBundleValidationAndStablePagination(t *testing.T) {
	for _, sources := range [][]BundleSource{{{Title: "x", URL: "https://user:pass@example.org", Role: "coverage"}}, {{Title: "x", URL: "https://example.org", Role: "verified"}}, {{Title: "x", URL: "https://example.org/a", Role: "coverage"}, {Title: "y", URL: "https://example.org/a", Role: "clip"}}, make([]BundleSource, 13)} {
		if _, err := normalizeBundleSources(sources); err == nil {
			t.Fatal("invalid supporting source admitted", sources)
		}
	}
	d := Empty()
	at := time.Now().UTC()
	for n := 0; n < 101; n++ {
		d.Bundles = append(d.Bundles, StoryBundle{BundleSnapshot: BundleSnapshot{ID: fmt.Sprintf("%032x", n)}, CreatedAt: at})
	}
	seen := map[string]bool{}
	cursor := ""
	for {
		page, err := d.ListBundles(cursor)
		if err != nil {
			t.Fatal(err)
		}
		for _, b := range page.Bundles {
			if seen[b.ID] {
				t.Fatal("duplicate cursor bundle")
			}
			seen[b.ID] = true
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if len(seen) != 101 {
		t.Fatal(len(seen))
	}
	if _, err := d.ListBundles("bad"); err == nil {
		t.Fatal("invalid cursor")
	}
}

func TestEvidenceObservesFallbackSocialRetrieval(t *testing.T) {
	client := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "publish.twitter.com" {
			return response(`{"author_name":"Fixture","html":"<blockquote>Observed public post</blockquote>"}`), nil
		}
		return nil, errors.New("source unavailable")
	})}
	out := (Enricher{Client: client}).Enrich(context.Background(), Item{URL: "https://x.com/fixture/status/1", Kind: "social"}, false)
	if out.ExtractionEvidence.Retrieval != "backend_observed" || out.ExtractionEvidence.Source != "post_text" || out.Error != "" {
		t.Fatal(out)
	}
}
