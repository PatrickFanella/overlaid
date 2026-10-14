package newsroom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"git.subcult.tv/PatrickFanella/over-laid/internal/testpostgres"
)

func TestTenantDeskAndCredentialIsolation(t *testing.T) {
	f := testpostgres.NewTenantFixture(t, "")
	other := "00000000-0000-4000-8000-000000000099"
	if _, err := f.Admin.Exec(`INSERT INTO tenants(id,slug,name,owner_account_id,plan_id) VALUES($1,'newsroom-other','Other','00000000-0000-4000-8000-000000000002','unlimited')`, other); err != nil {
		t.Fatal(err)
	}
	db, err := tenantdb.Open(f.RuntimeURL, tenantdb.BootstrapID)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	otherDB, _ := db.Scoped(other)
	a, err := NewTenant(db)
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewTenant(otherDB)
	if err != nil {
		t.Fatal(err)
	}
	d, err := a.Apply(context.Background(), Command{Action: "submit", URL: "https://example.org/private"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = b.Apply(context.Background(), Command{Action: "approve", ID: d.Items[0].ID}); err != ErrNotFound {
		t.Fatal("cross-tenant item accepted", err)
	}
	keys := tenancy.Keyring{Active: "v1", Keys: map[string][]byte{"v1": []byte(strings.Repeat("a", 32))}}
	if err = a.SaveKeys(context.Background(), keys, ProviderKeys{Anthropic: "private-fixture-key"}); err != nil {
		t.Fatal(err)
	}
	otherKeys, err := b.Keys(context.Background(), keys)
	if err != nil || otherKeys.Anthropic != "" {
		t.Fatal("credential crossed tenants", err)
	}
	var count int
	if err = otherDB.QueryRowContext(context.Background(), `SELECT count(*) FROM newsroom_documents`).Scan(&count); err != nil || count != 0 {
		t.Fatal("RLS allowed predicate-free access", count, err)
	}
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(context.Background(), `INSERT INTO newsroom_documents(tenant_id,id,document) VALUES($1,'forged',$2)`, other, raw); err == nil {
		t.Fatal("forged tenant insert accepted")
	}
	loaded, err := NewTenant(db)
	if err != nil {
		t.Fatal(err)
	}
	read, err := loaded.Get(context.Background())
	if err != nil || len(read.Items) != 1 {
		t.Fatal("restart lost persisted desk", err)
	}
	add, err := a.NewToken(context.Background(), "add", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.CheckToken(context.Background(), add, "add"); !ok {
		t.Fatal("add token rejected")
	}
	if _, ok := b.CheckToken(context.Background(), add, "add"); ok {
		t.Fatal("add capability crossed tenants")
	}
	var resolved string
	if err = otherDB.QueryRowContext(context.Background(), `SELECT resolve_newsroom_capability($1,'add')`, HashToken(add)).Scan(&resolved); err != nil || resolved != tenantdb.BootstrapID {
		t.Fatal("narrow capability lookup failed", resolved, err)
	}
	if _, err = a.NewToken(context.Background(), "add", "", true); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.CheckToken(context.Background(), add, "add"); ok {
		t.Fatal("revoked add capability remained valid")
	}
	control, err := tenancy.Open(f.RuntimeURL)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	exported, err := control.Export(context.Background(), "00000000-0000-4000-8000-000000000002", tenantdb.BootstrapID)
	if err != nil {
		t.Fatal(err)
	}
	rawExport, _ := json.Marshal(exported["newsroom_documents"])
	if strings.Contains(string(rawExport), "ciphertext") || strings.Contains(string(rawExport), "token_hash") || strings.Contains(string(rawExport), "private-fixture-key") {
		t.Fatal("credentials entered workspace export")
	}
	personal, err := a.Internal(context.Background(), Command{Action: "submit", URL: "https://example.org/viewer", SubmittedBy: "viewer", ViewerID: "viewer-42"})
	if err != nil {
		t.Fatal(err)
	}
	personalID := personal.Items[len(personal.Items)-1].ID
	if _, err = a.Internal(context.Background(), Command{Action: "approve", ID: personalID}); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Internal(context.Background(), Command{Action: "focus", ID: personalID}); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Internal(context.Background(), Command{Action: "question-submit", Text: "Personal question", SubmittedBy: "viewer", ViewerID: "viewer-42"}); err != nil {
		t.Fatal(err)
	}
	applyWorkflowTest(t, a, Command{Action: "bundle-create", ID: personalID, Curate: true})
	applyWorkflowTest(t, a, Command{Action: "broadcast-start", Title: "Viewer lifecycle show", Owner: true})
	applyWorkflowTest(t, a, Command{Action: "play", ID: personalID})
	d, _ = a.Get(context.Background())
	personalShow := d.Broadcasts[len(d.Broadcasts)-1].ID
	d = applyWorkflowTest(t, a, Command{Action: "broadcast-end", ID: personalShow, Owner: true})
	preview, _ := d.EpisodePreview(personalShow, "Viewer episode", true, true)
	applyWorkflowTest(t, a, Command{Action: "episode-publish", ID: personalShow, Owner: true, PublicTitle: "Viewer episode", IncludeSummary: true, IncludeNotes: true, Fingerprint: preview.Fingerprint, PublicTokenHash: HashToken(strings.Repeat("v", 43))})
	applyWorkflowTest(t, a, Command{Action: "delete", ID: personalID})
	if err = control.DeleteViewer(context.Background(), "00000000-0000-4000-8000-000000000002", tenantdb.BootstrapID, "viewer-42", "viewer"); err != nil {
		t.Fatal(err)
	}
	after, err := a.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Items) != 1 || len(after.Questions) != 0 || after.CurrentID != "" || len(after.Order) != 0 || len(after.History) != 0 || len(after.Bundles) != 0 || len(after.Publications) != 0 || len(after.ShowExports) != 0 || len(after.Activity) != 0 || len(after.Broadcasts) != 1 || len(after.Broadcasts[0].Rundown) != 0 {
		t.Fatal("viewer deletion left personal/derived state", after)
	}
}

func TestTenantEnrichmentAndDeliveryClaimsSerialize(t *testing.T) {
	f := testpostgres.NewTenantFixture(t, "")
	db, err := tenantdb.Open(f.RuntimeURL, tenantdb.BootstrapID)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a, err := NewTenant(db)
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewTenant(db)
	if err != nil {
		t.Fatal(err)
	}
	d := applyWorkflowTest(t, a, Command{Action: "submit", URL: "https://example.org/claim"})
	id := d.Items[0].ID
	applyWorkflowTest(t, a, Command{Action: "job-queue", ID: id})
	outcomes := make(chan error, 2)
	go func() {
		_, err := a.Internal(context.Background(), Command{Action: "job-claim", ID: id, Token: "first"})
		outcomes <- err
	}()
	go func() {
		_, err := b.Internal(context.Background(), Command{Action: "job-claim", ID: id, Token: "second"})
		outcomes <- err
	}()
	accepted := 0
	for i := 0; i < 2; i++ {
		if <-outcomes == nil {
			accepted++
		}
	}
	if accepted != 1 {
		t.Fatal("concurrent PG enrichment claims", accepted)
	}
	go func() {
		_, err := a.Internal(context.Background(), Command{Action: "delivery-lock", Token: "first"})
		outcomes <- err
	}()
	go func() {
		_, err := b.Internal(context.Background(), Command{Action: "delivery-lock", Token: "second"})
		outcomes <- err
	}()
	accepted = 0
	for i := 0; i < 2; i++ {
		if <-outcomes == nil {
			accepted++
		}
	}
	if accepted != 1 {
		t.Fatal("concurrent PG delivery claims", accepted)
	}
}

func TestTenantLegacyHistoryBackfill(t *testing.T) {
	f := testpostgres.NewTenantFixture(t, "")
	db, err := tenantdb.Open(f.RuntimeURL, tenantdb.BootstrapID)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s, err := NewTenant(db)
	if err != nil {
		t.Fatal(err)
	}
	d := playedFixture(t, s, "https://example.org/legacy-pg", "Legacy PG takeaway")
	if _, err = db.ExecContext(context.Background(), `UPDATE newsroom_documents SET document=document-'history' WHERE /*tenant:newsroom_documents*/ AND id='default'`); err != nil {
		t.Fatal(err)
	}
	backfilled, err := s.Get(context.Background())
	if err != nil || len(backfilled.History) != 1 {
		t.Fatal("PG legacy backfill missing", err)
	}
	d = applyWorkflowTest(t, s, Command{Action: "delete", ID: d.Items[0].ID})
	if !strings.Contains(d.Markdown(""), "Legacy PG takeaway") {
		t.Fatal("PG write did not preserve legacy note")
	}
}

func TestTenantReviewBroadcastAndSearchContracts(t *testing.T) {
	f := testpostgres.NewTenantFixture(t, "")
	db, err := tenantdb.Open(f.RuntimeURL, tenantdb.BootstrapID)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a, err := NewTenant(db)
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewTenant(db)
	if err != nil {
		t.Fatal(err)
	}
	d := applyWorkflowTest(t, a, Command{Action: "submit", URL: "https://example.org/coordination", Title: "Workspace story"})
	id := d.Items[0].ID
	results := make(chan error, 2)
	for index, store := range []*Store{a, b} {
		go func(index int, s *Store) {
			_, err := s.Internal(context.Background(), Command{Action: "review-start", ID: id, ActorID: fmt.Sprintf("mod-%d", index)})
			results <- err
		}(index, store)
	}
	winners := 0
	for index := 0; index < 2; index++ {
		if <-results == nil {
			winners++
		}
	}
	if winners != 1 {
		t.Fatal("PG claims did not serialize", winners)
	}
	d, _ = a.Get(context.Background())
	claim := d.Items[0].ReviewClaim
	applyWorkflowTest(t, a, Command{Action: "review-release", ID: id, ActorID: claim.ActorID, ReviewToken: claim.Token})
	for index, store := range []*Store{a, b} {
		go func(index int, s *Store) {
			old := ""
			_, err := s.Internal(context.Background(), Command{Action: "edit", ID: id, ActorID: fmt.Sprintf("mod-%d", index), ExpectedEditorialRevision: revisionPointer(0), ExpectedNotes: &old, Notes: "Winner"})
			results <- err
		}(index, store)
	}
	winners = 0
	for index := 0; index < 2; index++ {
		if <-results == nil {
			winners++
		}
	}
	if winners != 1 {
		t.Fatal("PG stale edits both won", winners)
	}
	for _, store := range []*Store{a, b} {
		go func(s *Store) {
			_, err := s.Internal(context.Background(), Command{Action: "bundle-create", ID: id, Curate: true})
			results <- err
		}(store)
	}
	for n := 0; n < 2; n++ {
		if err := <-results; err != nil {
			t.Fatal("idempotent concurrent bundle creation", err)
		}
	}
	d, _ = a.Get(context.Background())
	if len(d.Bundles) != 1 {
		t.Fatal("concurrent creation duplicated bundles")
	}
	bundleID := d.Bundles[0].ID
	for _, store := range []*Store{a, b} {
		go func(s *Store) {
			_, err := s.Internal(context.Background(), Command{Action: "bundle-edit", ID: bundleID, Title: "Concurrent bundle", Notes: "Winning prep", BundleRevision: revisionPointer(0), Curate: true})
			results <- err
		}(store)
	}
	winners = 0
	for n := 0; n < 2; n++ {
		if err := <-results; err == nil {
			winners++
		} else if !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	}
	if winners != 1 {
		t.Fatal("concurrent bundle edits both won", winners)
	}
	applyWorkflowTest(t, a, Command{Action: "approve", ID: id})
	for _, store := range []*Store{a, b} {
		go func(s *Store) {
			_, err := s.Internal(context.Background(), Command{Action: "broadcast-start", Title: "Concurrent show", Owner: true})
			results <- err
		}(store)
	}
	winners = 0
	for index := 0; index < 2; index++ {
		if <-results == nil {
			winners++
		}
	}
	if winners != 1 {
		t.Fatal("PG duplicate shows admitted", winners)
	}
	d, _ = b.Get(context.Background())
	show := d.ActiveBroadcast()
	applyWorkflowTest(t, b, Command{Action: "focus", ID: id})
	d = applyWorkflowTest(t, b, Command{Action: "play", ID: id})
	if d.History[0].BroadcastID != show.ID {
		t.Fatal("PG play/show stamp separated")
	}
	otherDB, _ := db.Scoped("00000000-0000-4000-8000-000000000002")
	other, err := NewTenant(otherDB)
	if err != nil {
		t.Fatal(err)
	}
	otherDoc, _ := other.Get(context.Background())
	page, err := otherDoc.SearchQueue(QueueSearch{Query: "Workspace story"})
	if err != nil || page.Total != 0 || len(otherDoc.Broadcasts) != 0 || len(otherDoc.Bundles) != 0 {
		t.Fatal("tenant search/show leaked", err)
	}
	if _, err = otherDoc.BroadcastExport(show.ID, "markdown"); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross-tenant show export succeeded", err)
	}
}

func TestTenantCatchupUsagePublicationAndCopies(t *testing.T) {
	f := testpostgres.NewTenantFixture(t, "")
	db, err := tenantdb.Open(f.RuntimeURL, tenantdb.BootstrapID)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a, _ := NewTenant(db)
	b, _ := NewTenant(db)
	ctx := context.Background()
	limit := int64(5000)
	applyWorkflowTest(t, a, Command{Action: "usage-settings", Owner: true, UsagePolicy: &UsagePolicy{AllowanceMicros: &limit, WarningPercent: 80}})
	results := make(chan error, 2)
	for _, s := range []*Store{a, b} {
		go func(s *Store) { results <- s.AdmitPaid(ctx, "search", 1) }(s)
	}
	winners := 0
	for n := 0; n < 2; n++ {
		if <-results == nil {
			winners++
		}
	}
	if winners != 1 {
		t.Fatal("PG allowance overspent", winners)
	}
	d := applyWorkflowTest(t, a, Command{Action: "shelf-import", Name: "Template", ImportSegments: []Segment{{ID: "source", Name: "Empty", TargetMinutes: intPointer(30)}}, ImportItems: []ShelfLink{{URL: "https://example.org/template"}}})
	shelf := d.Shelves[0].ID
	applyWorkflowTest(t, a, Command{Action: "shelf-preparation", ID: shelf, Curate: true, Template: true, PrepPrompts: "Private prompts"})
	d = applyWorkflowTest(t, b, Command{Action: "shelf-copy", ID: shelf, Curate: true, Name: "Independent"})
	if d.Shelves[1].Segments[0].ID == d.Shelves[0].Segments[0].ID {
		t.Fatal("template copy reused IDs")
	}
	d = applyWorkflowTest(t, a, Command{Action: "submit", URL: "https://example.org/public", Title: "Public source"})
	id := d.Items[len(d.Items)-1].ID
	applyWorkflowTest(t, a, Command{Action: "approve", ID: id})
	d = applyWorkflowTest(t, a, Command{Action: "broadcast-start", Owner: true, Title: "Public show"})
	show := d.Broadcasts[0].ID
	applyWorkflowTest(t, a, Command{Action: "focus", ID: id})
	applyWorkflowTest(t, a, Command{Action: "play", ID: id})
	d = applyWorkflowTest(t, a, Command{Action: "broadcast-end", Owner: true, ID: show})
	preview, _ := d.EpisodePreview(show, "Public title", false, false)
	hash := HashToken(strings.Repeat("p", 43))
	applyWorkflowTest(t, a, Command{Action: "episode-publish", Owner: true, ID: show, PublicTitle: "Public title", Fingerprint: preview.Fingerprint, PublicTokenHash: hash})
	var tenant string
	if err = db.QueryRowContext(ctx, `SELECT resolve_newsroom_capability($1,'episode')`, hash).Scan(&tenant); err != nil || tenant != tenantdb.BootstrapID {
		t.Fatal("narrow episode lookup failed", tenant, err)
	}
	other, _ := db.Scoped("00000000-0000-4000-8000-000000000002")
	var count int
	if err = other.QueryRowContext(ctx, `SELECT count(*) FROM newsroom_documents WHERE token_hash=$1`, hash).Scan(&count); err != nil || count != 0 {
		t.Fatal("episode token bypassed RLS", err, count)
	}
	applyWorkflowTest(t, b, Command{Action: "episode-revoke", ID: show, Owner: true})
	if err = db.QueryRowContext(ctx, `SELECT resolve_newsroom_capability($1,'episode')`, hash).Scan(&tenant); err == nil {
		t.Fatal("revoked token resolves")
	}
}
