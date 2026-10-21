package appvault

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func issuanceClient(t *testing.T, client string, hook func(*http.Request) error) (*http.Client, *atomic.Int32) {
	t.Helper()
	posts := new(atomic.Int32)
	return &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		body := ""
		switch r.Method + " " + r.URL.String() {
		case "POST https://id.twitch.tv/oauth2/token":
			posts.Add(1)
			raw, err := io.ReadAll(r.Body)
			if err != nil {
				return nil, err
			}
			if !strings.Contains(string(raw), "client_secret=fixture-secret") || !strings.Contains(string(raw), "client_id="+client) || r.GetBody != nil {
				return nil, errors.New("wrong non-replayable issuance binding")
			}
			body = `{"access_token":"fixture-access","token_type":"bearer","expires_in":3600}`
		case "GET https://id.twitch.tv/oauth2/validate":
			body = fmt.Sprintf(`{"client_id":%q,"expires_in":3600}`, client)
		default:
			return nil, errors.New("unexpected issuance request")
		}
		if hook != nil {
			if err := hook(r); err != nil {
				return nil, err
			}
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}, posts
}
func prepare(t *testing.T, s *Store, client string, revision int64) Issuance {
	t.Helper()
	r, err := s.PrepareIssuance(t.Context(), client, fixtureSecret, revision)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func issuanceCount(t *testing.T, db *sql.DB, id string) (int, int) {
	t.Helper()
	var credentials, receipts int
	if err := db.QueryRow(`SELECT (SELECT count(*) FROM service_app_credentials),(SELECT count(*) FROM service_app_credential_audit)`).Scan(&credentials, &receipts); err != nil {
		t.Fatal(err)
	}
	return credentials, receipts
}

func TestIssuancePrepareIssueAtomicInstallationAndPrivateMetadata(t *testing.T) {
	f, s := fixture(t)
	r := prepare(t, s, "client", 0)
	if r.State != "prepared" || r.DeadlineAt.Sub(r.CreatedAt) != 5*time.Minute || r.Principal != role(f.AppAdminURL) {
		t.Fatal(r)
	}
	duplicate := prepare(t, s, "client", 0)
	if duplicate != r {
		t.Fatal("duplicate changed original identity/deadline", r, duplicate)
	}
	for _, tc := range []struct {
		client, secret string
		revision       int64
	}{{"client", "other-secret", 0}, {"client", fixtureSecret, 1}, {"client", fixtureSecret, -1}, {"invalid client", fixtureSecret, 0}, {"client", "", 0}} {
		if _, err := s.PrepareIssuance(t.Context(), tc.client, tc.secret, tc.revision); err == nil {
			t.Fatal("invalid/conflicting preparation accepted")
		}
	}
	client, posts := issuanceClient(t, "client", func(r *http.Request) error {
		if r.Method == "POST" {
			tx, err := f.Admin.BeginTx(t.Context(), nil)
			if err != nil {
				return err
			}
			defer func() { _ = tx.Rollback() }()
			var free bool
			if err = tx.QueryRow(`SELECT pg_try_advisory_xact_lock(hashtextextended('service_app_custody:'||current_database()||':'||current_schema(),0))`).Scan(&free); err != nil {
				return err
			}
			if !free {
				return errors.New("provider request held custody lock")
			}
			current, err := s.LookupIssuance(t.Context(), rID(t, s, "client"))
			if err != nil || current.State != "started" {
				return errors.New("provider request preceded durable claim")
			}
		}
		return nil
	})
	issued, err := s.Issue(t.Context(), keys(), r.ID, r.ClientID, fixtureSecret, 0, client)
	if err != nil || issued.State != "issued" || issued.InstalledAppID == "" || issued.InstalledRevision != 1 || issued.CreatedAt != r.CreatedAt || issued.DeadlineAt != r.DeadlineAt || posts.Load() != 1 {
		t.Fatal(issued, err, posts.Load())
	}
	for range 2 {
		again, err := s.Issue(t.Context(), keys(), r.ID, r.ClientID, fixtureSecret, 0, client)
		if err != nil || again != issued || posts.Load() != 1 {
			t.Fatal("issued replay", again, err)
		}
	}
	accounts, receipts := issuanceCount(t, f.Admin, r.ID)
	if accounts != 1 || receipts != 1 {
		t.Fatal(accounts, receipts)
	}
	lookup, err := s.LookupIssuance(t.Context(), r.ID)
	if err != nil || lookup != issued {
		t.Fatal(lookup, err)
	}
	raw, _ := json.Marshal(lookup)
	for _, forbidden := range []string{fixtureSecret, "fixture-access", "secret_fingerprint", "claim_hash", "ciphertext", "key_version"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatal("private issuance metadata leaked")
		}
	}
	next := prepare(t, s, "client", 1)
	if next.ID == r.ID {
		t.Fatal("new explicit revision reused old source")
	}
	updated, err := s.Issue(t.Context(), keys(), next.ID, next.ClientID, fixtureSecret, 1, client)
	if err != nil || updated.State != "issued" || updated.InstalledRevision != 2 || updated.InstalledAppID != issued.InstalledAppID || posts.Load() != 2 {
		t.Fatal(updated, err, posts.Load())
	}
}

func rID(t *testing.T, s *Store, client string) string {
	t.Helper()
	var id string
	if err := s.db.QueryRowContext(t.Context(), `SELECT id FROM service_app_issuance WHERE client_id=$1 AND state='started'`, client).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestIssuanceConcurrentRestartNeverRepeatsClaim(t *testing.T) {
	f, s := fixture(t)
	r := prepare(t, s, "client", 0)
	entered, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	client, posts := issuanceClient(t, "client", func(req *http.Request) error {
		if req.Method == "POST" {
			close(entered)
			<-release
		}
		return nil
	})
	done := make(chan error, 1)
	go func() { _, err := s.Issue(t.Context(), keys(), r.ID, "client", fixtureSecret, 0, client); done <- err }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("issuance never started")
	}
	for range 3 {
		got, err := s.Issue(t.Context(), keys(), r.ID, "client", fixtureSecret, 0, client)
		if !errors.Is(err, ErrIssuanceUncertain) || got.State != "started" {
			t.Fatal(got, err)
		}
	}
	// Closing the first owner's pool simulates loss after provider dispatch and
	// before installation/completion. The durable started row must remain burned.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	close(release)
	select {
	case err := <-done:
		if !errors.Is(err, ErrIssuanceUncertain) {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("old attempt did not finish")
	}
	restarted, err := Open(t.Context(), f.AppAdminURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = restarted.Close() }()
	lookup, err := restarted.LookupIssuance(t.Context(), r.ID)
	if err != nil || lookup.State != "started" {
		t.Fatal(lookup, err)
	}
	if _, err = restarted.Issue(t.Context(), keys(), r.ID, "client", fixtureSecret, 0, client); !errors.Is(err, ErrIssuanceUncertain) || posts.Load() != 1 {
		t.Fatal(err, posts.Load())
	}
	if blocked, err := restarted.PrepareIssuance(t.Context(), "client", fixtureSecret, 0); !errors.Is(err, ErrIssuanceUncertain) || blocked.ID != r.ID {
		t.Fatal("started request replaced", blocked, err)
	}
	execSQL(t, f.Admin, `UPDATE service_app_issuance SET created_at=statement_timestamp()-interval '25 hours',deadline_at=statement_timestamp()-interval '25 hours'+interval '5 minutes' WHERE id=$1`, r.ID)
	if blocked, err := restarted.PrepareIssuance(t.Context(), "client", fixtureSecret, 0); !errors.Is(err, ErrIssuanceUncertain) || blocked.ID != r.ID || blocked.State != "uncertain" {
		t.Fatal("uncertain ownership aged out", blocked, err)
	}
	if accounts, receipts := issuanceCount(t, f.Admin, r.ID); accounts != 0 || receipts != 0 {
		t.Fatal(accounts, receipts)
	}
}

func TestIssuanceRefusalUncertaintyCancellationAndAtomicRollback(t *testing.T) {
	for _, name := range []string{"refused", "transport", "cancel", "revision-drift", "expired-after-provider"} {
		t.Run(name, func(t *testing.T) {
			f, s := fixture(t)
			r := prepare(t, s, "client", 0)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			client, posts := issuanceClient(t, "client", func(req *http.Request) error {
				if req.Method != "POST" {
					return nil
				}
				switch name {
				case "transport":
					return errors.New("PRIVATE-PROVIDER-ERROR")
				case "cancel":
					cancel()
					return ctx.Err()
				case "revision-drift":
					install(t, s, "client", 0)
				case "expired-after-provider":
					execSQL(t, f.Admin, `UPDATE service_app_issuance SET created_at=statement_timestamp()-interval '6 minutes',deadline_at=statement_timestamp()-interval '1 minute' WHERE id=$1`, r.ID)
				}
				return nil
			})
			if name == "refused" {
				client.Transport = transport(func(req *http.Request) (*http.Response, error) {
					posts.Add(1)
					return &http.Response{StatusCode: 401, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("PRIVATE-REFUSAL"))}, nil
				})
			}
			got, err := s.Issue(ctx, keys(), r.ID, "client", fixtureSecret, 0, client)
			if err == nil || strings.Contains(err.Error(), "PRIVATE") || posts.Load() != 1 {
				t.Fatal(got, err, posts.Load())
			}
			if name == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal("context identity lost", err)
			}
			if name != "refused" && !errors.Is(err, ErrIssuanceUncertain) {
				t.Fatal("claimed uncertainty lost", err)
			}

			lookup, lookupErr := s.LookupIssuance(t.Context(), r.ID)
			if lookupErr != nil {
				t.Fatal(lookupErr)
			}
			want := "uncertain"
			if name == "refused" {
				want = "refused"
			}
			if lookup.State != want {
				t.Fatal(lookup)
			}
			_, _ = s.Issue(t.Context(), keys(), r.ID, "client", fixtureSecret, 0, client)
			if posts.Load() != 1 {
				t.Fatal("repeated provider effect")
			}
			wantRows := 0
			if name == "revision-drift" {
				wantRows = 1
			}
			if accounts, receipts := issuanceCount(t, f.Admin, r.ID); accounts != wantRows || receipts != wantRows {
				t.Fatal("non-atomic installation", accounts, receipts)
			}
		})
	}
}

func TestIssuanceOriginalExpiryLookupPurgingAndBounds(t *testing.T) {
	f, s := fixture(t)
	r := prepare(t, s, "client", 0)
	client, posts := issuanceClient(t, "client", nil)
	for _, selection := range []struct {
		id, client, secret string
		expected           int64
	}{{r.ID, "other", fixtureSecret, 0}, {r.ID, "client", "other", 0}, {r.ID, "client", fixtureSecret, 1}, {"00000000-0000-4000-8000-000000000001", "client", fixtureSecret, 0}} {
		if _, err := s.Issue(t.Context(), keys(), selection.id, selection.client, selection.secret, selection.expected, client); !errors.Is(err, ErrRevision) {
			t.Fatal(err)
		}
	}
	execSQL(t, f.Admin, `UPDATE service_app_issuance SET created_at=statement_timestamp()-interval '6 minutes',deadline_at=statement_timestamp()-interval '1 minute' WHERE id=$1`, r.ID)
	before, err := s.LookupIssuance(t.Context(), r.ID)
	if err != nil || before.State != "prepared" {
		t.Fatal(before, err)
	}
	after, err := s.LookupIssuance(t.Context(), r.ID)
	if err != nil || before != after {
		t.Fatal("lookup mutated expired source")
	}
	expired, _ := s.Issue(t.Context(), keys(), r.ID, "client", fixtureSecret, 0, client)
	if expired.State != "refused" || expired.Failure != "expired" || !expired.DeadlineAt.Equal(before.DeadlineAt) || posts.Load() != 0 {
		t.Fatal(expired, posts.Load())
	}
	execSQL(t, f.Admin, `UPDATE service_app_issuance SET created_at=statement_timestamp()-interval '25 hours',deadline_at=statement_timestamp()-interval '25 hours'+interval '5 minutes' WHERE id=$1`, r.ID)
	fresh := prepare(t, s, "client", 0)
	if fresh.ID == r.ID {
		t.Fatal("purged identity recreated")
	}
	if _, err = s.LookupIssuance(t.Context(), r.ID); !errors.Is(err, ErrRevision) {
		t.Fatal(err)
	}
	if _, err = s.Issue(t.Context(), keys(), r.ID, "client", fixtureSecret, 0, client); !errors.Is(err, ErrRevision) || posts.Load() != 0 {
		t.Fatal(err)
	}
	for i := 1; i < 8; i++ {
		prepare(t, s, fmt.Sprintf("reserved%d", i), 0)
	}
	if _, err = s.PrepareIssuance(t.Context(), "ninth", fixtureSecret, 0); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if _, err = s.Install(t.Context(), keys(), fixtureSecret, credential(t, "ninth", 3600), 0); !errors.Is(err, ErrCapacity) {
		t.Fatal("direct install bypassed active client reservations", err)
	}
	if duplicate := prepare(t, s, "client", 0); duplicate != fresh {
		t.Fatal("reservation refreshed original")
	}
	execSQL(t, f.Admin, `INSERT INTO service_app_issuance(client_id,expected_revision,secret_fingerprint,state,completed_at,failure_code) SELECT 'filler',0,repeat('a',64),'refused',clock_timestamp(),'expired' FROM generate_series(1,992)`)
	if _, err = s.PrepareIssuance(t.Context(), "new", fixtureSecret, 0); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
}

func TestIssuanceClaimSeesCommittedRevisionWithRepeatableReadLogin(t *testing.T) {
	f, s := fixture(t)
	m := install(t, s, "client", 0)
	r := prepare(t, s, "client", 1)
	execSQL(t, f.Admin, `ALTER ROLE `+role(f.AppAdminURL)+` SET default_transaction_isolation='repeatable read'`)
	_ = s.Close()
	s, err := Open(t.Context(), f.AppAdminURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	tx, err := f.Admin.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended('service_app_custody:'||current_database()||':'||current_schema(),0))`); err != nil {
		t.Fatal(err)
	}
	client, posts := issuanceClient(t, "client", nil)
	done := make(chan error, 1)
	go func() { _, err := s.Issue(t.Context(), keys(), r.ID, "client", fixtureSecret, 1, client); done <- err }()
	waitAppLock(t, f, f.AppAdminURL)
	if _, err = tx.Exec(`UPDATE service_app_credentials SET revision=2 WHERE id=$1`, m.ID); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, ErrRevision) || posts.Load() != 0 {
			t.Fatal("stale preclaim revision authorized issuance", err, posts.Load())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("claim did not finish")
	}
}

func TestIssuanceCompletionRollbackOnLostClaim(t *testing.T) {
	f, s := fixture(t)
	r := prepare(t, s, "client", 0)
	owners := make(chan *sql.Tx, 1)
	client, posts := issuanceClient(t, "client", func(req *http.Request) error {
		if req.Method != "POST" {
			return nil
		}
		owner, err := f.Admin.BeginTx(t.Context(), nil)
		if err != nil {
			return err
		}
		var id string
		if err := owner.QueryRow(`SELECT id FROM service_app_issuance WHERE id=$1 FOR UPDATE`, r.ID).Scan(&id); err != nil {
			_ = owner.Rollback()
			return err
		}
		owners <- owner
		return nil
	})
	done := make(chan error, 1)
	go func() { _, err := s.Issue(t.Context(), keys(), r.ID, "client", fixtureSecret, 0, client); done <- err }()
	var owner *sql.Tx
	select {
	case owner = <-owners:
	case <-time.After(3 * time.Second):
		t.Fatal("no locked issuance row")
	}
	defer func() { _ = owner.Rollback() }()
	waiting := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := f.Admin.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE usename=$1 AND wait_event_type='Lock' AND query LIKE 'UPDATE service_app_issuance SET state=''issued''%')`, role(f.AppAdminURL)).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("no actual final issuance update wait")
	}
	// The query reaching that UPDATE proves credential/audit writes happened in
	// the transaction. Another connection must still observe no installed rows.
	if a, b := issuanceCount(t, f.Admin, r.ID); a != 0 || b != 0 {
		t.Fatal("partial install visible", a, b)
	}
	if _, err := owner.Exec(`UPDATE service_app_issuance SET claim_hash=repeat('0',64) WHERE id=$1`, r.ID); err != nil {
		t.Fatal(err)
	}
	if err := owner.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, ErrIssuanceUncertain) {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("completion did not stop")
	}
	if a, b := issuanceCount(t, f.Admin, r.ID); a != 0 || b != 0 {
		t.Fatal("failed completion committed partial custody", a, b)
	}
	_, _ = s.Issue(t.Context(), keys(), r.ID, "client", fixtureSecret, 0, client)
	if posts.Load() != 1 {
		t.Fatal("lost claim replayed")
	}
}

func TestIssuanceOriginalExpiryAfterClaimLockWait(t *testing.T) {
	f, s := fixture(t)
	r := prepare(t, s, "client", 0)
	tx, err := f.Admin.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended('service_app_custody:'||current_database()||':'||current_schema(),0))`); err != nil {
		t.Fatal(err)
	}
	client, posts := issuanceClient(t, "client", nil)
	done := make(chan Issuance, 1)
	go func() { got, _ := s.Issue(t.Context(), keys(), r.ID, "client", fixtureSecret, 0, client); done <- got }()
	waitAppLock(t, f, f.AppAdminURL)
	if _, err = tx.Exec(`UPDATE service_app_issuance SET created_at=statement_timestamp()-interval '6 minutes',deadline_at=statement_timestamp()-interval '1 minute' WHERE id=$1`, r.ID); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-done:
		if got.State != "refused" || got.Failure != "expired" || posts.Load() != 0 {
			t.Fatal(got, posts.Load())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("expired claim did not stop")
	}
}

func TestIssuanceSchemaRoleColumnAndNullFences(t *testing.T) {
	f, s := fixture(t)
	for _, dsn := range []string{f.RuntimeURL, f.AppExecutionURL, f.BotAdminURL, f.BotExecutorURL, f.WorkerURL, f.OutboundWorkerURL, f.ModerationWorkerURL, f.PacerURL, f.ModerationPacerURL, f.StreamWorkerURL} {
		db, err := sql.Open("pgx", dsn)
		if err != nil {
			t.Fatal(err)
		}
		var count int
		err = db.QueryRow(`SELECT count(*) FROM service_app_issuance`).Scan(&count)
		_ = db.Close()
		if err == nil {
			t.Fatal("issuance readable outside admin custody")
		}
	}
	for _, sql := range []string{
		`INSERT INTO service_app_issuance(client_id,expected_revision,secret_fingerprint,principal) VALUES('x',0,repeat('a',64),'spoof')`,
		`INSERT INTO service_app_issuance(client_id,expected_revision,secret_fingerprint,deadline_at) VALUES('x',0,repeat('a',64),clock_timestamp())`,
	} {
		if _, err := s.db.ExecContext(t.Context(), sql); err == nil {
			t.Fatal("database-stamped field writable")
		}
	}
	for _, state := range []string{"started", "issued", "refused", "uncertain"} {
		if _, err := f.Admin.Exec(`INSERT INTO service_app_issuance(client_id,expected_revision,secret_fingerprint,state) VALUES('null-fence',0,repeat('a',64),$1)`, state); err == nil {
			t.Fatal("incomplete state accepted", state)
		}
	}
	if _, err := f.Admin.Exec(`INSERT INTO service_app_issuance(client_id,expected_revision,secret_fingerprint,state,started_at,completed_at,failure_code) VALUES('null-claim',0,repeat('a',64),'uncertain',clock_timestamp(),clock_timestamp(),'uncertain')`); err == nil {
		t.Fatal("NULL claim with non-NULL start bypassed state constraint")
	}
	for _, tc := range []struct{ change, undo string }{
		{`GRANT UPDATE(deadline_at) ON service_app_issuance TO ` + role(f.AppAdminURL), `REVOKE UPDATE(deadline_at) ON service_app_issuance FROM ` + role(f.AppAdminURL)},
		{`GRANT SELECT ON service_app_issuance TO ` + role(f.AppExecutionURL), `REVOKE SELECT ON service_app_issuance FROM ` + role(f.AppExecutionURL)},
		{`ALTER TABLE service_app_issuance NO FORCE ROW LEVEL SECURITY`, `ALTER TABLE service_app_issuance FORCE ROW LEVEL SECURITY`},
		{`DROP INDEX service_app_issuance_active`, `CREATE UNIQUE INDEX service_app_issuance_active ON service_app_issuance(client_id) WHERE state IN('prepared','started','uncertain')`},
	} {
		execSQL(t, f.Admin, tc.change)
		if strings.HasPrefix(tc.change, "GRANT SELECT") {
			if ex, err := OpenExecution(t.Context(), f.AppExecutionURL); err == nil {
				_ = ex.Close()
				t.Fatal("execution can read issuance")
			}
		} else if err := s.Validate(t.Context()); !errors.Is(err, ErrBoundary) {
			t.Fatal("issuance drift accepted", tc.change, err)
		}
		execSQL(t, f.Admin, tc.undo)
		if err := s.Validate(t.Context()); err != nil {
			t.Fatal("restored issuance layout invalid", err)
		}
	}
}

func TestIssuanceConcurrentPreparationAndMigrationPreservesCustody(t *testing.T) {
	f, s := fixture(t)
	m := install(t, s, "client", 0)
	var before string
	if err := f.Admin.QueryRow(`SELECT row_to_json(c)::text || (SELECT json_agg(a)::text FROM service_app_credential_audit a WHERE app_id=$1) FROM service_app_credentials c WHERE id=$1`, m.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	// Recreate the additive pre-0036 database shape, retaining the existing
	// credential/receipt rows, then apply the actual migration without data edits.
	execSQL(t, f.Admin, `DROP TABLE service_app_issuance; DELETE FROM overlaid_migrations WHERE name='0036_service_app_issuance.sql'`)
	_, source, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(source), "..", "..", "..", "migrations", "0036_service_app_issuance.sql"))
	if err != nil {
		t.Fatal(err)
	}
	execSQL(t, f.Admin, strings.ReplaceAll(string(raw), "overlaid_app_admin", role(f.AppAdminURL)))
	var after string
	if err := f.Admin.QueryRow(`SELECT row_to_json(c)::text || (SELECT json_agg(a)::text FROM service_app_credential_audit a WHERE app_id=$1) FROM service_app_credentials c WHERE id=$1`, m.ID).Scan(&after); err != nil || before != after {
		t.Fatal("migration rewrote custody", err)
	}
	if err := s.Validate(t.Context()); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	type answer struct {
		result Issuance
		err    error
	}
	results := make(chan answer, 8)
	for range 8 {
		go func() {
			<-start
			r, err := s.PrepareIssuance(t.Context(), "client", fixtureSecret, 1)
			results <- answer{r, err}
		}()
	}
	close(start)
	var first Issuance
	for range 8 {
		got := <-results
		if got.err != nil {
			t.Fatal(got.err)
		}
		if first.ID == "" {
			first = got.result
		}
		if first != got.result {
			t.Fatal("concurrent preparations diverged")
		}
	}
	var count int
	if err := f.Admin.QueryRow(`SELECT count(*) FROM service_app_issuance`).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}
