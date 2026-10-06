package bot

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

type apiRoundTripFunc func(*http.Request) (*http.Response, error)

func (f apiRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestAPIVariableDialRejectsNonPublicDNSBeforeConnecting(t *testing.T) {
	for _, addresses := range [][]string{
		{"127.0.0.1"}, {"10.1.2.3"}, {"169.254.169.254"}, {"100.64.0.1"}, {"198.18.0.1"}, {"240.0.0.1"}, {"::1"}, {"fc00::1"}, {"::ffff:192.168.0.1"}, {"64:ff9b::c0a8:1"}, {"64:ff9b:1::a00:1"}, {"2002:c0a8:1::"}, {"8.8.8.8", "192.168.0.1"}, {},
	} {
		t.Run(strings.Join(addresses, ","), func(t *testing.T) {
			lookups, connections := 0, 0
			dial := publicAPIVariableDial(func(_ context.Context, host string) ([]net.IPAddr, error) {
				lookups++
				if host != "fixture.example.test" {
					t.Fatalf("lookup host=%q", host)
				}
				out := []net.IPAddr{}
				for _, address := range addresses {
					out = append(out, net.IPAddr{IP: net.ParseIP(address)})
				}
				return out, nil
			}, func(context.Context, string, string) (net.Conn, error) { connections++; return nil, nil })
			if _, err := dial(context.Background(), "tcp", "fixture.example.test:443"); err == nil {
				t.Fatal("unsafe DNS answers accepted")
			}
			if lookups != 1 || connections != 0 {
				t.Fatalf("lookups=%d, connections=%d", lookups, connections)
			}
		})
	}
}

func TestAPIVariableDialPinsResolvedAddressesAndRetriesPublicAddresses(t *testing.T) {
	lookups := 0
	destinations := []string{}
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	dial := publicAPIVariableDial(func(context.Context, string) ([]net.IPAddr, error) {
		lookups++
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}, {IP: net.ParseIP("2001:4860:4860::8888")}}, nil
	}, func(_ context.Context, network, destination string) (net.Conn, error) {
		if network != "tcp" {
			t.Fatalf("network=%q", network)
		}
		destinations = append(destinations, destination)
		if len(destinations) == 1 {
			return nil, errors.New("first connection failed")
		}
		return client, nil
	})
	if conn, err := dial(context.Background(), "tcp", "fixture.example.test:443"); err != nil || conn != client {
		t.Fatalf("dial=%v, %v", conn, err)
	}
	if lookups != 1 || len(destinations) != 2 || destinations[0] != "8.8.8.8:443" || destinations[1] != "[2001:4860:4860::8888]:443" {
		t.Fatalf("lookups=%d, destinations=%v", lookups, destinations)
	}
}

func TestAPIVariableDialStopsOnDNSFailureAndCancellation(t *testing.T) {
	connections := 0
	dial := publicAPIVariableDial(func(context.Context, string) ([]net.IPAddr, error) { return nil, errors.New("DNS failed") }, func(context.Context, string, string) (net.Conn, error) { connections++; return nil, nil })
	if _, err := dial(context.Background(), "tcp", "fixture.example.test:443"); err == nil || connections != 0 {
		t.Fatalf("DNS failure: %v, connections=%d", err, connections)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dial = publicAPIVariableDial(func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}, {IP: net.ParseIP("1.1.1.1")}}, nil
	}, func(ctx context.Context, _, _ string) (net.Conn, error) { connections++; return nil, ctx.Err() })
	if _, err := dial(ctx, "tcp", "fixture.example.test:443"); !errors.Is(err, context.Canceled) || connections != 1 {
		t.Fatalf("cancellation: %v, connections=%d", err, connections)
	}
}

func TestAPIVariableHTTPRejectsUnsafeRedirectsAndCapsRedirectCount(t *testing.T) {
	for _, destination := range []string{"https://localhost/private", "https://192.168.1.1/private", "http://api.example.test/value", "https://user:pass@api.example.test/value"} {
		calls := 0
		client := &http.Client{CheckRedirect: checkAPIVariableRedirect, Transport: apiRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{destination}}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
		})}
		_, status, _, err := fetchAPIVariableWithClient(context.Background(), APIVariable{URL: "https://api.example.test/start", TimeoutMs: 1500}, client)
		if err == nil || status != "request_error" || calls != 1 {
			t.Fatalf("redirect %q: status=%q calls=%d err=%v", destination, status, calls, err)
		}
	}
	calls := 0
	client := &http.Client{CheckRedirect: checkAPIVariableRedirect, Transport: apiRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://api.example.test/again"}}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	})}
	_, status, _, err := fetchAPIVariableWithClient(context.Background(), APIVariable{URL: "https://api.example.test/start", TimeoutMs: 1500}, client)
	if err == nil || status != "request_error" || calls != 2 {
		t.Fatalf("redirect cap: status=%q calls=%d err=%v", status, calls, err)
	}
}

type apiBrokenBody struct{}

func (apiBrokenBody) Read([]byte) (int, error) { return 0, errors.New("SECRET_RESPONSE_DETAIL") }
func (apiBrokenBody) Close() error             { return nil }

func TestAPIVariableHTTPResponseLimitsAndSafeRequestErrors(t *testing.T) {
	for _, test := range []struct {
		name, body, contentType, selector, value, status string
		code                                             int
		broken                                           bool
	}{
		{name: "scalar JSON", body: `{"data":{"value":72}}`, contentType: "application/json", selector: "data.value", value: "72", status: "fetched", code: 200},
		{name: "text cap", body: strings.Repeat("é", 250), contentType: "text/plain", value: strings.Repeat("é", 200), status: "fetched", code: 200},
		{name: "boundary", body: strings.Repeat("a", 32*1024), contentType: "text/plain", value: strings.Repeat("a", 200), status: "fetched", code: 200},
		{name: "oversize", body: strings.Repeat("a", 32*1024+1), contentType: "text/plain", status: "response_error", code: 200},
		{name: "broken read", status: "response_error", code: 200, broken: true},
		{name: "upstream failure", body: "SECRET_RESPONSE_DETAIL", status: "http_error", code: 503},
		{name: "non-scalar JSON", body: `{"data":{}}`, contentType: "application/json", selector: "data", status: "parse_error", code: 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &http.Client{Transport: apiRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.Method != "GET" || req.Header.Get("Accept") != "text/plain, application/json" {
					t.Fatalf("request=%#v", req)
				}
				var body io.ReadCloser = io.NopCloser(strings.NewReader(test.body))
				if test.broken {
					body = apiBrokenBody{}
				}
				return &http.Response{StatusCode: test.code, Header: http.Header{"Content-Type": []string{test.contentType}}, Body: body, Request: req}, nil
			})}
			value, status, code, err := fetchAPIVariableWithClient(context.Background(), APIVariable{URL: "https://api.example.test/value", TimeoutMs: 1500, ResponseSelector: test.selector}, client)
			if value != test.value || status != test.status || code != test.code || (err == nil) != (test.status == "fetched") {
				t.Fatalf("value=%q status=%q code=%d err=%v", value, status, code, err)
			}
			if err != nil && strings.Contains(err.Error(), "SECRET") {
				t.Fatalf("private response error leaked: %v", err)
			}
		})
	}
	client := &http.Client{Transport: apiRoundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("SECRET_QUERY_TOKEN") })}
	_, status, _, err := fetchAPIVariableWithClient(context.Background(), APIVariable{URL: "https://api.example.test/value?token=SECRET_QUERY_TOKEN", TimeoutMs: 1500}, client)
	if err == nil || status != "request_error" || strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "token=") {
		t.Fatalf("unsafe request error: status=%q err=%v", status, err)
	}
}

func TestAPIVariableHTTPUsesPinnedTLSConnectionWithoutEnvironmentProxy(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		hits.Add(1)
		if req.Method != "GET" || req.URL.Path != "/current" {
			t.Errorf("unexpected request: %s %s", req.Method, req.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"weather":{"temp":72}}`)
	}))
	defer server.Close()
	var proxyHits atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { proxyHits.Add(1); w.WriteHeader(http.StatusBadGateway) }))
	defer proxy.Close()
	t.Setenv("HTTPS_PROXY", proxy.URL)
	t.Setenv("NO_PROXY", "")
	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	client := newAPIVariableHTTPClient(1500)
	defer client.CloseIdleConnections()
	transport := client.Transport.(*http.Transport)
	if transport.Proxy != nil {
		t.Fatal("API-variable transport permits environment proxy bypass")
	}
	transport.TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	transport.TLSClientConfig.ServerName = "127.0.0.1"
	lookups, connections := 0, 0
	transport.DialContext = publicAPIVariableDial(func(_ context.Context, host string) ([]net.IPAddr, error) {
		lookups++
		if host != "fixture.example.test" {
			t.Errorf("lookup host=%q", host)
		}
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
	}, func(ctx context.Context, network, destination string) (net.Conn, error) {
		connections++
		if destination != net.JoinHostPort("8.8.8.8", port) {
			t.Errorf("unpinned destination=%q", destination)
		}
		// The test's approved numeric destination is routed to its TLS fixture.
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	})
	value, status, code, err := fetchAPIVariableWithClient(context.Background(), APIVariable{URL: "https://fixture.example.test:" + port + "/current", ResponseSelector: "weather.temp", TimeoutMs: 1500}, client)
	if err != nil || value != "72" || status != "fetched" || code != 200 {
		t.Fatalf("HTTP result=%q, %q, %d, %v", value, status, code, err)
	}
	if hits.Load() != 1 || proxyHits.Load() != 0 || lookups != 1 || connections != 1 {
		t.Fatalf("server=%d proxy=%d lookups=%d connections=%d", hits.Load(), proxyHits.Load(), lookups, connections)
	}
}
