package twitchoutbound

import (
	"net/http"
	"strings"
	"testing"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformoutbound"
)

type planLimitTransport func(*http.Request) (*http.Response, error)

func (f planLimitTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTenantHTTPAllowanceReceiptClassification(t *testing.T) {
	for _, mode := range []string{"initial", "post-claim", "pre-transport", "provider-quota", "permission"} {
		t.Run(mode, func(t *testing.T) {
			x := newFixture(t, true)
			if _, err := x.f.Admin.Exec(`UPDATE tenants SET plan_id='free'`); err != nil {
				t.Fatal(err)
			}
			exhaust := func() {
				t.Helper()
				if _, err := x.f.Admin.Exec(`INSERT INTO tenant_usage(tenant_id,kind,window_start,used) VALUES($1,'http',date_trunc('hour',clock_timestamp()),120) ON CONFLICT(tenant_id,kind,window_start) DO UPDATE SET used=120`, x.target.TenantID); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "initial" {
				exhaust()
			}
			if mode == "permission" {
				x.p.moderator = false
			}
			x.adapter.client.Transport = planLimitTransport(func(r *http.Request) (*http.Response, error) {
				if mode == "provider-quota" && strings.HasSuffix(r.URL.Path, "/chat/messages") {
					x.p.sends++
					out := response(429, `{"message":"private quota body"}`)
					out.Header.Set("Retry-After", "42")
					return out, nil
				}
				out, err := x.p.RoundTrip(r)
				if strings.HasSuffix(r.URL.Path, "/moderators") && ((mode == "post-claim" && x.p.roles == 1) || (mode == "pre-transport" && x.p.roles == 2)) {
					exhaust()
				}
				return out, err
			})
			result, err := execute(t, x, x.adapter.Authorize)
			if err != nil || !result.Done || result.Status != platform.Rejected {
				t.Fatal("allowance rejection did not complete", result, err)
			}
			store, _ := platformoutbound.New(x.store.Product)
			receipts, err := store.Receipts(t.Context(), 10)
			if err != nil || len(receipts) != 1 {
				t.Fatal(receipts, err)
			}
			want := platform.PlanLimited
			if mode == "provider-quota" {
				want = platform.QuotaLimited
			}
			if mode == "permission" {
				want = platform.Unauthorized
			}
			if receipts[0].Failure != want {
				t.Fatal("failure classifications collapsed", mode, receipts[0])
			}
			wantSends := 0
			if mode == "provider-quota" {
				wantSends = 1
			}
			if x.p.sends != wantSends {
				t.Fatal("local rejection reached transport", mode, x.p.sends)
			}
			var charged, cooled, permits int
			if err := x.f.Admin.QueryRow(`SELECT coalesce(sum(jsonb_array_length(attempts)),0),count(*) FILTER(WHERE blocked_until>clock_timestamp()),count(*) FILTER(WHERE lease_until IS NOT NULL) FROM chat_pacing_accounts`).Scan(&charged, &cooled, &permits); err != nil {
				t.Fatal(err)
			}
			wantCharged := 1
			if mode == "initial" || mode == "permission" {
				wantCharged = 0
			}
			wantCooled := 0
			if mode == "provider-quota" {
				wantCooled = 1
			}
			if charged != wantCharged || cooled != wantCooled || permits != 0 {
				t.Fatal("local limit changed provider pacing", mode, charged, cooled, permits)
			}
			if want == platform.PlanLimited && receipts[0].RetryAfter != 0 {
				t.Fatal("local limit gained provider backoff", receipts[0])
			}
			before := x.p.sends
			if repeated, err := execute(t, x, x.adapter.Authorize); err != nil || !repeated.Done || x.p.sends != before {
				t.Fatal("terminal limit rejection replayed", repeated, err)
			}
		})
	}
}
