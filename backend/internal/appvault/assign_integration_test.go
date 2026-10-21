package appvault

import (
	"context"
	"encoding/json"
	"errors"
	"git.subcult.tv/PatrickFanella/over-laid/internal/conduitassignment"
	"git.subcult.tv/PatrickFanella/over-laid/internal/conduitownership"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/testpostgres"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitchconduit"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type shardFixture struct {
	f               testpostgres.TenantFixture
	admin           *Store
	custody         *ExecutionStore
	binding         Binding
	creation        creationLedger
	owner           conduitownership.Intent
	ledger          *conduitassignment.Store
	ledgerDSN, role string
}

func newShardFixture(t *testing.T) shardFixture {
	t.Helper()
	f, admin, custody, b, c := creationFixture(t)
	owner := prepareCreation(t, c, b)
	_, a, err := c.Start(t.Context(), owner.ID, owner.Binding, owner.ShardCount)
	if err != nil || a == nil {
		t.Fatal(err)
	}
	now := time.Now()
	owner, err = c.Complete(t.Context(), *a, twitchconduit.Creation{Outcome: twitchconduit.CreationCreated, Conduit: &twitchconduit.CreatedConduit{ClientID: b.ClientID, ID: "owned", ShardCount: 2, StartedAt: now, CompletedAt: now}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	name := "assignment_admin_" + strings.TrimPrefix(f.Schema, "tenant_test_")
	_, source, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(source), "..", "..", "..", "migrations", "0038_conduit_assignment.sql"))
	if err != nil {
		t.Fatal(err)
	}
	execSQL(t, f.Admin, strings.ReplaceAll(string(raw), "overlaid_conduit_assignment_admin", name))
	u, _ := url.Parse(f.RuntimeURL)
	password, _ := u.User.Password()
	u.User = url.UserPassword(name, password)
	execSQL(t, f.Admin, `ALTER ROLE `+name+` LOGIN PASSWORD '`+password+`'`)
	t.Cleanup(func() {
		if _, err := f.Admin.Exec(`DROP TABLE IF EXISTS service_conduit_assignments;DROP OWNED BY ` + name + `;DROP ROLE ` + name); err != nil {
			t.Error(err)
		}
	})
	ledger, err := conduitassignment.Open(t.Context(), u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := ledger.Close(); err != nil {
			t.Error(err)
		}
	})
	if err = admin.Validate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = custody.Validate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = c.Validate(t.Context()); err != nil {
		t.Fatal(err)
	}
	return shardFixture{f, admin, custody, b, c, owner, ledger, u.String(), name}
}
func shardPrivate() twitchconduit.ShardBinding {
	return twitchconduit.ShardBinding{ConduitID: "owned", ShardCount: 2, ShardID: "0", SessionID: "PRIVATE-SESSION"}
}
func (f shardFixture) prepare(t *testing.T) conduitassignment.Intent {
	t.Helper()
	b := conduitassignment.Binding{AppID: f.binding.AppID, ClientID: f.binding.ClientID, Revision: f.binding.Revision, CreationID: f.owner.ID}
	r, err := f.ledger.Prepare(t.Context(), b, shardPrivate())
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func shardLease(t *testing.T) (AssignmentSession, *atomic.Int32, *atomic.Int32, *atomic.Bool) {
	t.Helper()
	acquired, released, held := new(atomic.Int32), new(atomic.Int32), new(atomic.Bool)
	deadline := time.Now().Add(9 * time.Second)
	return AssignmentSession{Deadline: deadline, Acquire: func(ctx context.Context, p twitchconduit.ShardBinding) (func(), error) {
		acquired.Add(1)
		if p != shardPrivate() {
			t.Error("lease binding")
		}
		if d, ok := ctx.Deadline(); !ok || d.After(deadline) {
			t.Error("lease deadline")
		}
		held.Store(true)
		return func() { held.Store(false); released.Add(1) }, nil
	}}, acquired, released, held
}
func shardClient(t *testing.T, f shardFixture, r conduitassignment.Intent, held *atomic.Bool, modify func(string, *http.Request) (*http.Response, error)) (*http.Client, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	patches, reads := new(atomic.Int32), new(atomic.Int32)
	return &http.Client{Transport: transport(func(req *http.Request) (*http.Response, error) {
		if held != nil && !held.Load() {
			t.Error("provider without live lease")
		}
		var state string
		if err := f.f.Admin.QueryRow(`SELECT state FROM service_conduit_assignments WHERE id=$1`, r.ID).Scan(&state); err != nil || state != "started" {
			t.Error("provider before committed claim", err)
		}
		if req.Header.Get("Authorization") != "Bearer fixture-access" || req.URL.RawQuery != "" {
			t.Error("private header binding")
		}
		stage, body, code := "before", `{"client_id":"client","expires_in":3600}`, 200
		switch req.Method + " " + req.URL.String() {
		case "GET https://id.twitch.tv/oauth2/validate":
			if reads.Add(1) > 1 {
				stage = "after"
			}
		case "PATCH https://api.twitch.tv/helix/eventsub/conduits/shards":
			stage = "assign"
			patches.Add(1)
			raw, err := io.ReadAll(req.Body)
			if err != nil || string(raw) != `{"conduit_id":"owned","shards":[{"id":"0","transport":{"method":"websocket","session_id":"PRIVATE-SESSION"}}]}` || req.GetBody != nil || req.Header.Get("Client-Id") != "client" {
				t.Error("assignment request")
			}
			req.Body = io.NopCloser(strings.NewReader(string(raw)))
			body = `{"data":[{"id":"0","status":"enabled","transport":{"method":"websocket","session_id":"PRIVATE-SESSION"}}],"errors":[]}`
			code = 202
		default:
			t.Error("unexpected provider operation")
		}
		if modify != nil {
			if response, err := modify(stage, req); response != nil || err != nil {
				return response, err
			}
		}
		return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}, patches, reads
}
func (f shardFixture) run(ctx context.Context, k tenancy.Keyring, r conduitassignment.Intent, g AssignmentSession, c *http.Client) (conduitassignment.Intent, error) {
	return f.custody.AssignConduitShard(ctx, k, f.binding, r.ID, shardPrivate(), f.creation.Store, f.ledger, g, c)
}
func (f shardFixture) state(t *testing.T, id string) string {
	t.Helper()
	var state string
	if err := f.f.Admin.QueryRow(`SELECT state FROM service_conduit_assignments WHERE id=$1`, id).Scan(&state); err != nil {
		t.Fatal(err)
	}
	return state
}
func shardSnapshot(t *testing.T, f shardFixture) string {
	t.Helper()
	var s string
	if err := f.f.Admin.QueryRow(`SELECT json_build_object('credentials',(SELECT json_agg(t) FROM service_app_credentials t),'audit',(SELECT json_agg(t) FROM service_app_credential_audit t),'creation',(SELECT json_agg(t) FROM service_conduit_creations t),'issuance',(SELECT json_agg(t) FROM service_app_issuance t))::text`).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}
func TestShardAssignmentCompositionNoReplayAndPrivateStores(t *testing.T) {
	f := newShardFixture(t)
	r := f.prepare(t)
	before := shardSnapshot(t, f)
	g, acquired, released, held := shardLease(t)
	client, patches, reads := shardClient(t, f, r, held, nil)
	done, err := f.run(t.Context(), keys(), r, g, client)
	if err != nil || done.State != "assigned" || done.ID != r.ID || acquired.Load() != 1 || released.Load() != 1 || held.Load() || patches.Load() != 1 || reads.Load() != 2 {
		t.Fatal("composition", done.State, err)
	}
	if shardSnapshot(t, f) != before {
		t.Fatal("private stores changed")
	}
	raw, _ := json.Marshal(done)
	if strings.Contains(string(raw), "PRIVATE-SESSION") || strings.Contains(string(raw), "fixture-access") {
		t.Fatal("private projection")
	}
	execSQL(t, f.f.Admin, `UPDATE service_app_credentials SET revoked_at=clock_timestamp(),secret_ciphertext=NULL,token_ciphertext=NULL,key_version=NULL,validated_at=NULL,expires_at=NULL WHERE id=$1`, f.binding.AppID)
	execution, err := OpenExecution(t.Context(), f.f.AppExecutionURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = execution.Close() }()
	ledger, err := conduitassignment.Open(t.Context(), f.ledgerDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ledger.Close() }()
	got, err := execution.AssignConduitShard(t.Context(), tenancy.Keyring{}, f.binding, r.ID, shardPrivate(), f.creation.Store, ledger, AssignmentSession{}, client)
	if err != nil || got != done || patches.Load() != 1 || reads.Load() != 2 || acquired.Load() != 1 {
		t.Fatal("terminal/restart replay", err)
	}
}
func TestShardAssignmentHistoricalCreationCurrentCustodyRevision(t *testing.T) {
	f := newShardFixture(t)
	m := install(t, f.admin, "client", f.binding.Revision)
	f.binding.Revision = m.Revision
	if f.binding.Revision == f.owner.Binding.Revision {
		t.Fatal("fixture did not refresh")
	}
	r := f.prepare(t)
	g, _, _, held := shardLease(t)
	client, patches, _ := shardClient(t, f, r, held, nil)
	done, err := f.run(t.Context(), keys(), r, g, client)
	if err != nil || done.State != "assigned" || patches.Load() != 1 {
		t.Fatal("historical revision coupled to credential", err)
	}
}
func TestShardAssignmentPreclaimRefusal(t *testing.T) {
	for _, kind := range []string{"key", "revoked", "expiry", "creation_state", "creation_client", "creation_count", "guard_nil", "guard_expired", "guard_future", "guard_error", "guard_panic", "guard_no_release", "guard_cleanup_error", "guard_canceled", "guard_revision", "guard_creation", "expired_intent"} {
		t.Run(kind, func(t *testing.T) {
			f := newShardFixture(t)
			r := f.prepare(t)
			g, acquired, released, held := shardLease(t)
			client, patches, reads := shardClient(t, f, r, held, nil)
			k := keys()
			wantState := "prepared"
			switch kind {
			case "key":
				k = tenancy.Keyring{}
			case "revoked":
				execSQL(t, f.f.Admin, `UPDATE service_app_credentials SET revoked_at=clock_timestamp(),secret_ciphertext=NULL,token_ciphertext=NULL,key_version=NULL,validated_at=NULL,expires_at=NULL WHERE id=$1`, f.binding.AppID)
			case "expiry":
				execSQL(t, f.f.Admin, `UPDATE service_app_credentials SET expires_at=clock_timestamp() WHERE id=$1`, f.binding.AppID)
			case "creation_state":
				execSQL(t, f.f.Admin, `UPDATE service_conduit_creations SET state='uncertain',failure_code='uncertain',conduit_id=NULL WHERE id=$1`, f.owner.ID)
			case "creation_client":
				execSQL(t, f.f.Admin, `UPDATE service_conduit_creations SET client_id='other' WHERE id=$1`, f.owner.ID)
			case "creation_count":
				execSQL(t, f.f.Admin, `UPDATE service_conduit_creations SET shard_count=3 WHERE id=$1`, f.owner.ID)
			case "guard_nil":
				g.Acquire = nil
			case "guard_expired":
				g.Deadline = time.Now().Add(-time.Second)
			case "guard_future":
				g.Deadline = time.Now().Add(time.Minute)
			case "guard_error":
				g.Acquire = func(context.Context, twitchconduit.ShardBinding) (func(), error) {
					return func() { released.Add(1) }, errors.New("PRIVATE-GATE")
				}
			case "guard_panic":
				g.Acquire = func(context.Context, twitchconduit.ShardBinding) (func(), error) { panic("PRIVATE-GATE") }
			case "guard_no_release":
				g.Acquire = func(context.Context, twitchconduit.ShardBinding) (func(), error) { return nil, nil }
			case "guard_cleanup_error":
				g.Acquire = func(context.Context, twitchconduit.ShardBinding) (func(), error) {
					return func() { released.Add(1); panic("PRIVATE-CLEANUP") }, errors.New("PRIVATE-GATE")
				}
			case "guard_canceled":
				g.Acquire = func(context.Context, twitchconduit.ShardBinding) (func(), error) { return nil, context.Canceled }
			case "guard_revision":
				old := g.Acquire
				g.Acquire = func(c context.Context, p twitchconduit.ShardBinding) (func(), error) {
					release, err := old(c, p)
					execSQL(t, f.f.Admin, `UPDATE service_app_credentials SET revision=revision+1 WHERE id=$1`, f.binding.AppID)
					return release, err
				}
			case "guard_creation":
				old := g.Acquire
				g.Acquire = func(c context.Context, p twitchconduit.ShardBinding) (func(), error) {
					release, err := old(c, p)
					execSQL(t, f.f.Admin, `UPDATE service_conduit_creations SET app_revision=app_revision+1 WHERE id=$1`, f.owner.ID)
					return release, err
				}
			case "expired_intent":
				execSQL(t, f.f.Admin, `WITH stamp AS(SELECT clock_timestamp()-interval '11 seconds' AS t) UPDATE service_conduit_assignments SET created_at=stamp.t,deadline_at=stamp.t+interval '10 seconds' FROM stamp WHERE id=$1`, r.ID)
				wantState = "refused"
			}
			_, err := f.run(t.Context(), k, r, g, client)
			if err == nil || patches.Load() != 0 || reads.Load() != 0 || f.state(t, r.ID) != wantState || held.Load() {
				t.Fatal("preclaim refusal", kind, err)
			}
			if strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal("private gate escaped")
			}
			if kind == "guard_canceled" && !errors.Is(err, context.Canceled) {
				t.Fatal("context identity")
			}
			if (kind == "guard_error" || kind == "guard_cleanup_error") && released.Load() != 1 {
				t.Fatal("failed acquire cleanup")
			}
			if strings.HasPrefix(kind, "guard_") && (kind == "guard_nil" || kind == "guard_expired" || kind == "guard_future") && acquired.Load() != 0 {
				t.Fatal("invalid gate acquired")
			}
		})
	}
}
func TestShardAssignmentFinalDriftAndUncertainty(t *testing.T) {
	for _, kind := range []string{"custody_revision", "ciphertext", "creation", "role", "provider", "malformed", "release", "release_mutation", "canceled"} {
		t.Run(kind, func(t *testing.T) {
			f := newShardFixture(t)
			r := f.prepare(t)
			g, _, released, held := shardLease(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if kind == "release" {
				old := g.Acquire
				g.Acquire = func(c context.Context, p twitchconduit.ShardBinding) (func(), error) {
					release, err := old(c, p)
					return func() { release(); panic("PRIVATE-CLEANUP") }, err
				}
			}
			if kind == "release_mutation" {
				old := g.Acquire
				g.Acquire = func(c context.Context, p twitchconduit.ShardBinding) (func(), error) {
					release, err := old(c, p)
					return func() {
						release()
						execSQL(t, f.f.Admin, `UPDATE service_conduit_creations SET app_revision=app_revision+1 WHERE id=$1`, f.owner.ID)
					}, err
				}
			}
			client, patches, _ := shardClient(t, f, r, held, func(stage string, _ *http.Request) (*http.Response, error) {
				if stage != "assign" {
					return nil, nil
				}
				switch kind {
				case "custody_revision":
					execSQL(t, f.f.Admin, `UPDATE service_app_credentials SET revision=revision+1 WHERE id=$1`, f.binding.AppID)
				case "ciphertext":
					execSQL(t, f.f.Admin, `UPDATE service_app_credentials SET token_ciphertext=set_byte(token_ciphertext,0,255-get_byte(token_ciphertext,0)) WHERE id=$1`, f.binding.AppID)
				case "creation":
					execSQL(t, f.f.Admin, `UPDATE service_conduit_creations SET app_revision=app_revision+1 WHERE id=$1`, f.owner.ID)
				case "role":
					u, _ := url.Parse(f.f.AppExecutionURL)
					execSQL(t, f.f.Admin, `GRANT UPDATE(client_id) ON service_app_credentials TO `+u.User.Username())
				case "provider":
					return nil, errors.New("PRIVATE-PROVIDER")
				case "malformed":
					return &http.Response{StatusCode: 202, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":[],"errors":[]}`))}, nil
				case "canceled":
					cancel()
					return nil, ctx.Err()
				}
				return nil, nil
			})
			done, err := f.run(ctx, keys(), r, g, client)
			if !errors.Is(err, conduitassignment.ErrUncertain) || done.State != "uncertain" || patches.Load() != 1 || released.Load() != 1 || held.Load() {
				t.Fatal("uncertainty", kind, done.State, err)
			}
			if kind == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatal("context identity")
			}
			if strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal("private details")
			}
			if _, a, _ := f.ledger.Start(t.Context(), r.ID, r.Binding, shardPrivate()); a != nil {
				t.Fatal("uncertain replay")
			}
		})
	}
}
func TestShardAssignmentPoolAndPrivateScopeRefusal(t *testing.T) {
	f := newShardFixture(t)
	r := f.prepare(t)
	other := newShardFixture(t)
	g, _, _, held := shardLease(t)
	client, patches, _ := shardClient(t, f, r, held, nil)
	if _, err := f.custody.AssignConduitShard(t.Context(), keys(), f.binding, r.ID, shardPrivate(), other.creation.Store, f.ledger, g, client); !errors.Is(err, ErrBoundary) {
		t.Fatal("creation pool identity", err)
	}
	if _, err := f.custody.AssignConduitShard(t.Context(), keys(), f.binding, r.ID, shardPrivate(), f.creation.Store, other.ledger, g, client); !errors.Is(err, ErrBoundary) {
		t.Fatal("assignment pool identity", err)
	}
	p := shardPrivate()
	p.SessionID = "other"
	if _, err := f.custody.AssignConduitShard(t.Context(), keys(), f.binding, r.ID, p, f.creation.Store, f.ledger, g, client); !errors.Is(err, ErrRevision) {
		t.Fatal("private conflict", err)
	}
	if patches.Load() != 0 || f.state(t, r.ID) != "prepared" {
		t.Fatal("refusal claimed")
	}
}
func TestShardAssignmentActualCancellation(t *testing.T) {
	f := newShardFixture(t)
	r := f.prepare(t)
	g, _, released, held := shardLease(t)
	entered, stopped := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		_, _ = io.Copy(io.Discard, req.Body)
		close(entered)
		<-req.Context().Done()
		close(stopped)
	}))
	defer server.Close()
	destination, _ := url.Parse(server.URL)
	tr := http.DefaultTransport.(*http.Transport).Clone()
	defer tr.CloseIdleConnections()
	client, patches, _ := shardClient(t, f, r, held, func(stage string, req *http.Request) (*http.Response, error) {
		if stage != "assign" {
			return nil, nil
		}
		clone := req.Clone(req.Context())
		clone.URL.Scheme = destination.Scheme
		clone.URL.Host = destination.Host
		return tr.RoundTrip(clone)
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := f.run(ctx, keys(), r, g, client); done <- err }()
	select {
	case <-entered:
		cancel()
	case <-time.After(5 * time.Second):
		t.Fatal("no request")
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || !errors.Is(err, conduitassignment.ErrUncertain) {
			t.Fatal(err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("cancellation hung")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("server remained active")
	}
	if patches.Load() != 1 || released.Load() != 1 || held.Load() || f.state(t, r.ID) != "uncertain" {
		t.Fatal("cleanup/replay")
	}
}
