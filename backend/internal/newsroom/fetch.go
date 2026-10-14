package newsroom

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"time"
)

// PublicClient pins the validated address in DialContext. Each redirect gets
// the same checks; proxy environment variables cannot bypass them.
func PublicClient(reserve func(context.Context) error) *http.Client {
	transport := &http.Transport{MaxIdleConns: 10, MaxIdleConnsPerHost: 2, IdleConnTimeout: 30 * time.Second, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		if port != "80" && port != "443" {
			return nil, errors.New("only public HTTP(S) ports are allowed")
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, errors.New("public host lookup failed")
		}
		if len(ips) == 0 {
			return nil, errors.New("public host not found")
		}
		for _, ip := range ips {
			if !PublicIP(ip) {
				return nil, errors.New("private or reserved address refused")
			}
		}
		var dialer net.Dialer
		dialer.Timeout = 5 * time.Second
		return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
	}
	return &http.Client{Transport: meteredTransport{base: transport, reserve: reserve}, Timeout: 25 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("redirect limit reached")
		}
		if req.URL.Scheme != "http" && req.URL.Scheme != "https" || req.URL.User != nil {
			return errors.New("redirect refused")
		}
		if via[0].Header.Get("x-api-key") != "" || via[0].Header.Get("X-Subscription-Token") != "" || via[0].URL.Query().Get("key") != "" {
			return errors.New("provider redirect refused")
		}
		if via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
			return errors.New("HTTPS downgrade refused")
		}
		return nil
	}}
}

type meteredTransport struct {
	base    http.RoundTripper
	reserve func(context.Context) error
}

func (t meteredTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if t.reserve != nil {
		if err := t.reserve(r.Context()); err != nil {
			return nil, err
		}
	}
	return t.base.RoundTrip(r)
}

func PublicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return false
	}
	for _, cidr := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32", "64:ff9b::/96", "2001::/32"} {
		if netip.MustParsePrefix(cidr).Contains(ip) {
			return false
		}
	}
	return true
}

func fetch(ctx context.Context, client *http.Client, raw string, headers map[string]string) ([]byte, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, errors.New("invalid public URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "OverLaid-Newsroom/1.0")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, errors.New("page or provider could not be reached")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("page or provider returned HTTP %d", response.StatusCode)
	}
	rawBody, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(rawBody) > 2<<20 {
		return nil, errors.New("response exceeds 2 MiB")
	}
	return rawBody, nil
}
