package bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

func validateAPIVariableURL(raw string) error {
	if raw == "" {
		return errors.New("api variable url is required")
	}
	if len(raw) > 2048 {
		return errors.New("api variable url must be 2048 characters or less")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
		return errors.New("api variable url must be https")
	}
	if parsed.User != nil || parsed.Fragment != "" {
		return errors.New("api variable url must not include user info or fragment")
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") || host == "host.docker.internal" {
		return errors.New("api variable host is not allowed")
	}
	if ip := net.ParseIP(host); ip != nil && !isPublicIP(ip) {
		return errors.New("api variable host must be public")
	}
	return nil
}

// Transition addresses can embed a private IPv4 destination even when the
// IPv6 address itself is classified as global unicast.
var apiVariableTransitionPrefixes = []netip.Prefix{
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("2001::/32"),
}

func isPublicIP(ip net.IP) bool {
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return false
	}
	if ipv4 := ip.To4(); ipv4 != nil {
		// Shared address space can route into private overlay networks. Benchmark
		// and reserved ranges are also unsuitable for operator-defined public APIs.
		if ipv4[0] == 0 || (ipv4[0] == 100 && ipv4[1] >= 64 && ipv4[1] <= 127) || (ipv4[0] == 198 && (ipv4[1] == 18 || ipv4[1] == 19)) || ipv4[0] >= 240 {
			return false
		}
	}
	if address, ok := netip.AddrFromSlice(ip); ok {
		for _, prefix := range apiVariableTransitionPrefixes {
			if prefix.Contains(address.Unmap()) {
				return false
			}
		}
	}
	return true
}

func fetchAPIVariable(parent context.Context, variable APIVariable) (string, string, int, error) {
	client := newAPIVariableHTTPClient(variable.TimeoutMs)
	defer client.CloseIdleConnections()
	return fetchAPIVariableWithClient(parent, variable, client)
}

func newAPIVariableHTTPClient(timeoutMs int) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Environment proxies would bypass this transport's destination checks.
	transport.Proxy = nil
	transport.DialContext = publicAPIVariableDial(net.DefaultResolver.LookupIPAddr, (&net.Dialer{}).DialContext)
	return &http.Client{Transport: transport, Timeout: time.Duration(timeoutMs) * time.Millisecond, CheckRedirect: checkAPIVariableRedirect}
}

func checkAPIVariableRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 2 {
		return errors.New("too many redirects")
	}
	return validateAPIVariableURL(req.URL.String())
}

// Resolve once, validate every answer, then dial a numeric address. This keeps
// DNS changes between validation and connection from selecting a private host.
func publicAPIVariableDial(
	lookup func(context.Context, string) ([]net.IPAddr, error),
	dial func(context.Context, string, string) (net.Conn, error),
) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, errors.New("invalid api variable destination")
		}
		addresses, err := lookup(ctx, host)
		if err != nil {
			return nil, errors.New("api variable DNS lookup failed")
		}
		if len(addresses) == 0 {
			return nil, errors.New("api variable host has no addresses")
		}
		for _, candidate := range addresses {
			if candidate.Zone != "" || !isPublicIP(candidate.IP) {
				return nil, errors.New("api variable host must resolve only to public addresses")
			}
		}
		for _, candidate := range addresses {
			connection, dialErr := dial(ctx, network, net.JoinHostPort(candidate.IP.String(), port))
			if dialErr == nil {
				return connection, nil
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
		}
		return nil, errors.New("api variable connection failed")
	}
}

func fetchAPIVariableWithClient(parent context.Context, variable APIVariable, client *http.Client) (string, string, int, error) {
	if err := validateAPIVariableURL(variable.URL); err != nil {
		return "", "request_error", 0, err
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(variable.TimeoutMs)*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, variable.URL, nil)
	if err != nil {
		return "", "request_error", 0, err
	}
	req.Header.Set("Accept", "text/plain, application/json")
	resp, err := client.Do(req)
	if err != nil {
		// url.Error includes the complete request URL, including query values.
		// Diagnostics must not copy operator-supplied API credentials.
		return "", "request_error", 0, errors.New("api variable request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "http_error", resp.StatusCode, fmt.Errorf("upstream status %d", resp.StatusCode)
	}
	const maxResponseBytes = 32 * 1024
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return "", "response_error", resp.StatusCode, errors.New("unable to read api variable response")
	}
	if len(data) > maxResponseBytes {
		return "", "response_error", resp.StatusCode, errors.New("api variable response exceeds 32 KiB")
	}
	value, err := extractAPIResponseValue(resp.Header.Get("Content-Type"), data, variable.ResponseSelector)
	if err != nil {
		return "", "parse_error", resp.StatusCode, err
	}
	return capRunes(value, 200), "fetched", resp.StatusCode, nil
}

func extractAPIResponseValue(contentType string, data []byte, selector string) (string, error) {
	contentType = strings.ToLower(contentType)
	if strings.Contains(contentType, "json") {
		var value any
		if err := json.Unmarshal(data, &value); err != nil {
			return "", err
		}
		if selector != "" {
			for _, part := range strings.Split(selector, ".") {
				object, ok := value.(map[string]any)
				if !ok {
					return "", errors.New("selector did not match object")
				}
				value = object[part]
			}
		}
		switch typed := value.(type) {
		case string:
			return strings.TrimSpace(typed), nil
		case float64, bool:
			return fmt.Sprintf("%v", typed), nil
		default:
			return "", errors.New("json response is not scalar")
		}
	}
	if strings.Contains(contentType, "text/plain") || contentType == "" {
		return strings.TrimSpace(string(data)), nil
	}
	return "", errors.New("unsupported content type")
}
