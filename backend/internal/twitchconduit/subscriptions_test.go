package twitchconduit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type subscriptionFixture struct {
	tokens, validates, selected, walks, pages atomic.Int32
	respond                                   func(string, *http.Request) (*http.Response, error)
}

func subscriptionItem(id, kind string, condition map[string]any) string {
	version := "1"
	if kind == "channel.update" {
		version = "2"
	}
	raw, _ := json.Marshal(map[string]any{"id": id, "type": kind, "version": version, "status": "enabled", "condition": condition, "transport": map[string]string{"method": "conduit", "conduit_id": "selected"}, "cost": 0, "created_at": "2026-10-21T00:00:00Z", "private": "PRIVATE-EXTRA"})
	return string(raw)
}
func subscriptionPage(items []string, total int, cursor string) string {
	raw, _ := json.Marshal(cursor)
	return fmt.Sprintf(`{"data":[%s],"total":%d,"pagination":{"cursor":%s},"private":"PRIVATE-ROOT"}`, strings.Join(items, ","), total, raw)
}
func subscriptionReader(t *testing.T) (*Inspector, *subscriptionFixture) {
	t.Helper()
	f := &subscriptionFixture{}
	i, e := NewInspector(Config{ClientID: "client", ConduitID: "selected", Token: func(ctx context.Context) (string, error) {
		f.tokens.Add(1)
		if d, ok := ctx.Deadline(); !ok || time.Until(d) > 10*time.Second {
			t.Error("unbounded token source")
		}
		return "PRIVATE-TOKEN", nil
	}, HTTPClient: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer PRIVATE-TOKEN" {
			t.Error("unexpected private request")
		}
		stage := ""
		switch r.URL.String() {
		case validateURL:
			stage = fmt.Sprint("validate", f.validates.Add(1))
		case conduitURL:
			stage = fmt.Sprint("selected", f.selected.Add(1))
		default:
			if r.URL.Scheme != "https" || r.URL.Host != "api.twitch.tv" || r.URL.Path != "/helix/eventsub/subscriptions" || r.URL.Query().Get("conduit_id") != "selected" {
				t.Error("untrusted endpoint/filter")
			}
			for key := range r.URL.Query() {
				if key != "conduit_id" && key != "after" {
					t.Error("extra filter")
				}
			}
			if r.URL.Query().Get("after") == "" {
				f.walks.Add(1)
			}
			f.pages.Add(1)
			stage = fmt.Sprint("walk", f.walks.Load())
		}
		if r.URL.String() != validateURL && r.Header.Get("Client-Id") != "client" {
			t.Error("wrong client")
		}
		if f.respond != nil {
			if response, e := f.respond(stage, r); response != nil || e != nil {
				return response, e
			}
		}
		switch {
		case strings.HasPrefix(stage, "validate"):
			return creationResponse(http.StatusOK, `{"client_id":"client","expires_in":3600,"user_id":null}`), nil
		case strings.HasPrefix(stage, "selected"):
			return creationResponse(http.StatusOK, `{"data":[{"id":"selected","shard_count":2}]}`), nil
		default:
			return creationResponse(http.StatusOK, subscriptionPage([]string{subscriptionItem("subscription", "stream.online", map[string]any{"broadcaster_user_id": "channel"})}, 1, "")), nil
		}
	})}})
	if e != nil {
		t.Fatal(e)
	}
	return i, f
}
func noSubscriptionReport(t *testing.T, r SubscriptionObservation, e, want error) {
	t.Helper()
	if !errors.Is(e, want) || r.ClientID != "" || r.ConduitID != "" || r.Subscriptions != nil || !r.StartedAt.IsZero() || !r.CompletedAt.IsZero() {
		t.Fatal("partial or incorrect report", r, e)
	}
	if strings.Contains(fmt.Sprintf("%+v %#v", r, e), "PRIVATE") {
		t.Fatal("private error escaped")
	}
}
func TestSubscriptionPaginationProjectionAndEmpty(t *testing.T) {
	i, f := subscriptionReader(t)
	cursor := "next+/=?&page"
	f.respond = func(stage string, r *http.Request) (*http.Response, error) {
		if !strings.HasPrefix(stage, "walk") {
			return nil, nil
		}
		if r.URL.Query().Get("after") == "" {
			return creationResponse(http.StatusOK, subscriptionPage([]string{subscriptionItem("z", "user.update", map[string]any{"user_id": "PRIVATE-USER"})}, 9, cursor)), nil
		}
		if r.URL.Query().Get("after") != cursor {
			t.Error("cursor not escaped exactly")
		}
		return creationResponse(http.StatusOK, subscriptionPage([]string{subscriptionItem("a", "channel.update", map[string]any{"broadcaster_user_id": "channel"}), subscriptionItem("b", "stream.offline", map[string]any{"broadcaster_user_id": "channel", "unreviewed": "PRIVATE-CONDITION"})}, 9, "")), nil
	}
	r, e := i.InspectSubscriptions(t.Context())
	if e != nil || !r.ValidFor("client", "selected") || r.ReportedTotal != 9 || len(r.Subscriptions) != 3 || f.tokens.Load() != 1 || f.validates.Load() != 2 || f.selected.Load() != 2 || f.pages.Load() != 4 {
		t.Fatal(r, e)
	}
	if !r.Subscriptions[0].ReviewedStream || r.Subscriptions[0].BroadcasterID != "channel" || r.Subscriptions[1].ReviewedStream || r.Subscriptions[1].BroadcasterID != "" || r.Subscriptions[2].ReviewedStream {
		t.Fatal("unknown conditions became routes", r)
	}
	raw, _ := json.Marshal(r)
	if strings.Contains(string(raw), "PRIVATE") || strings.Contains(string(raw), "condition") || strings.Contains(string(raw), "session") {
		t.Fatal("private metadata escaped", string(raw))
	}
	i, f = subscriptionReader(t)
	f.respond = func(stage string, _ *http.Request) (*http.Response, error) {
		if strings.HasPrefix(stage, "walk") {
			return creationResponse(http.StatusOK, subscriptionPage(nil, 0, "")), nil
		}
		return nil, nil
	}
	r, e = i.InspectSubscriptions(t.Context())
	if e != nil || r.Subscriptions == nil || len(r.Subscriptions) != 0 || !r.ValidFor("client", "selected") {
		t.Fatal("empty observation", r, e)
	}
}
func TestSubscriptionMalformedPartialAndDriftRefuseAll(t *testing.T) {
	for _, kind := range []string{"wrong_transport", "wrong_conduit", "extra_transport", "duplicate", "cycle", "empty_cursor_page", "missing_total", "null_total", "null_cursor", "null_data", "null_cost", "null_condition_value", "case_alias", "wrong_status", "wrong_type", "bad_created", "total_changed", "count_underflow", "private_condition_drift", "public_drift", "selected_drift", "identity_drift", "short_original", "http_failure", "oversized", "page_bound", "aggregate_bound"} {
		t.Run(kind, func(t *testing.T) {
			i, f := subscriptionReader(t)
			want := ErrInvalid
			f.respond = func(stage string, req *http.Request) (*http.Response, error) {
				if stage == "validate1" && kind == "short_original" {
					want = ErrUnauthorized
					return creationResponse(http.StatusOK, `{"client_id":"client","expires_in":20}`), nil
				}
				if stage == "validate2" && kind == "identity_drift" {
					want = ErrUnauthorized
					return creationResponse(http.StatusOK, `{"client_id":"other","expires_in":3600}`), nil
				}
				if stage == "selected2" && kind == "selected_drift" {
					want = ErrChanged
					return creationResponse(http.StatusOK, `{"data":[{"id":"selected","shard_count":3}]}`), nil
				}
				if !strings.HasPrefix(stage, "walk") {
					return nil, nil
				}
				item := subscriptionItem("subscription", "stream.online", map[string]any{"broadcaster_user_id": "channel"})
				page := subscriptionPage([]string{item}, 1, "")
				switch kind {
				case "wrong_transport":
					page = strings.Replace(page, `"method":"conduit"`, `"method":"websocket"`, 1)
				case "wrong_conduit":
					page = strings.Replace(page, `"conduit_id":"selected"`, `"conduit_id":"other"`, 1)
				case "extra_transport":
					page = strings.Replace(page, `"method":"conduit"`, `"method":"conduit","session_id":"PRIVATE-SESSION"`, 1)
				case "duplicate":
					want = ErrChanged
					page = subscriptionPage([]string{item, item}, 2, "")
				case "cycle":
					want = ErrChanged
					page = subscriptionPage([]string{subscriptionItem(fmt.Sprint("id", f.pages.Load()), "stream.offline", map[string]any{"broadcaster_user_id": "channel"})}, 10, "loop")
				case "empty_cursor_page":
					want = ErrChanged
					page = subscriptionPage(nil, 1, "more")
				case "missing_total":
					page = `{"data":[],"pagination":{}}`
				case "null_total":
					page = `{"data":[],"total":null,"pagination":{}}`
				case "null_cursor":
					page = `{"data":[],"total":0,"pagination":{"cursor":null}}`
				case "null_data":
					page = `{"data":null,"total":0,"pagination":{}}`
				case "null_cost":
					page = strings.Replace(page, `"cost":0`, `"cost":null`, 1)
				case "null_condition_value":
					page = strings.Replace(page, `"broadcaster_user_id":"channel"`, `"broadcaster_user_id":null`, 1)
				case "case_alias":
					page = strings.Replace(page, `"total":1`, `"total":1,"Total":1`, 1)
				case "wrong_status":
					page = strings.Replace(page, `"status":"enabled"`, `"status":"PRIVATE-STATUS"`, 1)
				case "wrong_type":
					page = strings.Replace(page, `"type":"stream.online"`, `"type":"PRIVATE URL/"`, 1)
				case "bad_created":
					page = strings.Replace(page, `2026-10-21T00:00:00Z`, `bad`, 1)
				case "total_changed":
					want = ErrChanged
					if stage == "walk2" {
						page = subscriptionPage([]string{item}, 2, "")
					}
				case "count_underflow":
					want = ErrChanged
					page = subscriptionPage([]string{item}, 0, "")
				case "private_condition_drift":
					want = ErrChanged
					condition := "PRIVATE-FIRST"
					if stage == "walk2" {
						condition = "PRIVATE-SECOND"
					}
					page = subscriptionPage([]string{subscriptionItem("subscription", "user.update", map[string]any{"user_id": condition})}, 1, "")
				case "public_drift":
					want = ErrChanged
					if stage == "walk2" {
						page = strings.Replace(page, `"status":"enabled"`, `"status":"authorization_revoked"`, 1)
					}
				case "http_failure":
					want = ErrRateLimited
					return creationResponse(http.StatusTooManyRequests, `PRIVATE BODY`), nil
				case "oversized":
					page = `{"data":[],"total":0,"pagination":{},"private":"` + strings.Repeat("x", maxBody) + `"}`
				case "page_bound":
					want = ErrChanged
					page = subscriptionPage([]string{subscriptionItem(fmt.Sprint("id", f.pages.Load()), "stream.offline", map[string]any{"broadcaster_user_id": "channel"})}, 1000000, fmt.Sprint("cursor", f.pages.Load()))
				case "aggregate_bound":
					page = subscriptionPage([]string{subscriptionItem(fmt.Sprint("id", req.URL.Query().Get("after")), "stream.offline", map[string]any{"broadcaster_user_id": "channel"})}, 1000000, fmt.Sprint("cursor", f.pages.Load()))
					page = strings.TrimSuffix(page, "}") + `,"padding":"` + strings.Repeat("x", maxBody/2) + `"}`
				}
				return creationResponse(http.StatusOK, page), nil
			}
			r, e := i.InspectSubscriptions(t.Context())
			noSubscriptionReport(t, r, e, want)
		})
	}
}
func TestSubscriptionCancellationPanicAndShape(t *testing.T) {
	for _, kind := range []string{"cancel", "panic", "token_failure"} {
		t.Run(kind, func(t *testing.T) {
			i, f := subscriptionReader(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			want := ErrUnavailable
			switch kind {
			case "cancel":
				want = context.Canceled
				f.respond = func(stage string, _ *http.Request) (*http.Response, error) {
					if stage == "walk1" {
						cancel()
						return nil, errors.New("PRIVATE CANCEL")
					}
					return nil, nil
				}
			case "panic":
				f.respond = func(stage string, _ *http.Request) (*http.Response, error) {
					if stage == "walk1" {
						panic("PRIVATE PANIC")
					}
					return nil, nil
				}
			case "token_failure":
				want = ErrUnauthorized
				i.token = func(context.Context) (string, error) { return "", errors.New("PRIVATE TOKEN") }
			}
			r, e := i.InspectSubscriptions(ctx)
			noSubscriptionReport(t, r, e, want)
		})
	}
	i, _ := subscriptionReader(t)
	r, e := i.InspectSubscriptions(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	for _, kind := range []string{"nil", "scope", "count", "total", "time", "duplicate", "unsorted", "reviewed_unknown", "private_projection", "bad_status"} {
		t.Run(kind, func(t *testing.T) {
			v := r
			v.Subscriptions = append([]SubscriptionMetadata(nil), r.Subscriptions...)
			switch kind {
			case "nil":
				v.Subscriptions = nil
			case "scope":
				v.ConduitID = "other"
			case "count":
				v.ShardCount = 0
			case "total":
				v.ReportedTotal = 0
			case "time":
				v.CompletedAt = v.StartedAt.Add(11 * time.Second)
			case "duplicate":
				v.Subscriptions = append(v.Subscriptions, v.Subscriptions[0])
				v.ReportedTotal = 2
			case "unsorted":
				v.Subscriptions = append(v.Subscriptions, SubscriptionMetadata{ID: "a", Type: "user.update", Version: "1", Status: "enabled"})
				v.ReportedTotal = 2
			case "reviewed_unknown":
				v.Subscriptions[0].Type = "user.update"
			case "private_projection":
				v.Subscriptions[0].ReviewedStream = false
			case "bad_status":
				v.Subscriptions[0].Status = "PRIVATE STATUS"
			}
			if v.ValidFor("client", "selected") {
				t.Fatal("malformed observation accepted")
			}
		})
	}
	var absent *Inspector
	if r, e := absent.InspectSubscriptions(t.Context()); !errors.Is(e, ErrInvalid) || r.Subscriptions != nil {
		t.Fatal(r, e)
	}
}
