package bot

import (
	"context"
	"database/sql"
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

type APIVariable struct {
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	Enabled          bool       `json:"enabled"`
	URL              string     `json:"url"`
	ResponseSelector string     `json:"responseSelector,omitempty"`
	Fallback         string     `json:"fallback,omitempty"`
	CacheTTLSeconds  int        `json:"cacheTtlSeconds"`
	TimeoutMs        int        `json:"timeoutMs"`
	LastStatus       string     `json:"lastStatus,omitempty"`
	LastError        string     `json:"lastError,omitempty"`
	LastCheckedAt    *time.Time `json:"lastCheckedAt,omitempty"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
}

type APIVariableResult struct {
	Name       string `json:"name"`
	Value      string `json:"value"`
	Status     string `json:"status"`
	HTTPStatus int    `json:"httpStatus,omitempty"`
	Error      string `json:"error,omitempty"`
}

type apiVariableCacheEntry struct {
	id        string
	updatedAt time.Time
	value     string
	expiresAt time.Time
}

type APIVariableResolver struct {
	store *Store
}

func NewAPIVariableResolver(store *Store) *APIVariableResolver {
	return &APIVariableResolver{store: store}
}

type apiVariableScanner interface{ Scan(dest ...any) error }

func (s *Store) ListAPIVariables(ctx context.Context) ([]APIVariable, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, enabled, url, response_selector, fallback, cache_ttl_seconds, timeout_ms, last_status, last_error, last_checked_at, created_at, updated_at FROM bot_api_variables ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []APIVariable{}
	for rows.Next() {
		item, err := scanAPIVariable(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) SaveAPIVariable(ctx context.Context, variable APIVariable) (APIVariable, error) {
	variable, err := NormalizeAPIVariable(variable)
	if err != nil {
		return APIVariable{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if strings.TrimSpace(variable.ID) == "" {
		return scanAPIVariable(s.db.QueryRowContext(ctx, `INSERT INTO bot_api_variables (name, enabled, url, response_selector, fallback, cache_ttl_seconds, timeout_ms) VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id, name, enabled, url, response_selector, fallback, cache_ttl_seconds, timeout_ms, last_status, last_error, last_checked_at, created_at, updated_at`, variable.Name, variable.Enabled, variable.URL, variable.ResponseSelector, variable.Fallback, variable.CacheTTLSeconds, variable.TimeoutMs))
	}
	return scanAPIVariable(s.db.QueryRowContext(ctx, `UPDATE bot_api_variables SET name=$2, enabled=$3, url=$4, response_selector=$5, fallback=$6, cache_ttl_seconds=$7, timeout_ms=$8, last_status='', last_error='', last_checked_at=NULL, updated_at=now() WHERE id=$1 RETURNING id, name, enabled, url, response_selector, fallback, cache_ttl_seconds, timeout_ms, last_status, last_error, last_checked_at, created_at, updated_at`, variable.ID, variable.Name, variable.Enabled, variable.URL, variable.ResponseSelector, variable.Fallback, variable.CacheTTLSeconds, variable.TimeoutMs))
}

func (s *Store) DeleteAPIVariable(ctx context.Context, id string) error {
	return s.deleteByID(ctx, "bot_api_variables", id, "api variable id is required")
}

func scanAPIVariable(row apiVariableScanner) (APIVariable, error) {
	var item APIVariable
	var lastChecked sql.NullTime
	if err := row.Scan(&item.ID, &item.Name, &item.Enabled, &item.URL, &item.ResponseSelector, &item.Fallback, &item.CacheTTLSeconds, &item.TimeoutMs, &item.LastStatus, &item.LastError, &lastChecked, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return APIVariable{}, err
	}
	if lastChecked.Valid {
		item.LastCheckedAt = &lastChecked.Time
	}
	return item, nil
}

func NormalizeAPIVariable(variable APIVariable) (APIVariable, error) {
	variable.ID = strings.TrimSpace(variable.ID)
	variable.Name = strings.ToLower(strings.TrimSpace(variable.Name))
	variable.URL = strings.TrimSpace(variable.URL)
	variable.ResponseSelector = strings.TrimSpace(variable.ResponseSelector)
	variable.Fallback = capRunes(strings.TrimSpace(variable.Fallback), 200)
	if variable.Name == "" {
		return APIVariable{}, errors.New("api variable name is required")
	}
	if len(variable.Name) > 32 || !isSafeBotName(variable.Name) {
		return APIVariable{}, errors.New("api variable name must use lowercase letters, numbers, dash, or underscore")
	}
	if err := validateAPIVariableURL(variable.URL); err != nil {
		return APIVariable{}, err
	}
	if variable.CacheTTLSeconds == 0 {
		variable.CacheTTLSeconds = 60
	}
	if variable.CacheTTLSeconds < 15 || variable.CacheTTLSeconds > 3600 {
		return APIVariable{}, errors.New("api variable cache ttl must be between 15 and 3600 seconds")
	}
	if variable.TimeoutMs == 0 {
		variable.TimeoutMs = 1500
	}
	if variable.TimeoutMs < 250 || variable.TimeoutMs > 3000 {
		return APIVariable{}, errors.New("api variable timeout must be between 250 and 3000 ms")
	}
	return variable, nil
}

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

func (r *APIVariableResolver) ResolveAPIVariables(names []string) map[string]APIVariableResult {
	if r == nil {
		return map[string]APIVariableResult{}
	}
	return resolveAPIVariables(r.store, names)
}

func (s *Store) ResolveAPIVariables(names []string) map[string]APIVariableResult {
	return resolveAPIVariables(s, names)
}

func resolveAPIVariables(store *Store, names []string) map[string]APIVariableResult {
	out := map[string]APIVariableResult{}
	unique := normalizeAPIVariableNames(names)
	if len(unique) == 0 {
		return out
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for _, name := range unique {
		out[name] = store.resolveAPIVariable(ctx, name)
	}
	return out
}

func normalizeAPIVariableNames(names []string) []string {
	unique := []string{}
	seen := map[string]struct{}{}
	for _, name := range names {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		unique = append(unique, name)
		if len(unique) >= 3 {
			break
		}
	}
	return unique
}

func (s *Store) resolveAPIVariable(ctx context.Context, name string) APIVariableResult {
	if s == nil {
		return APIVariableResult{Name: name, Status: "unavailable"}
	}
	var variable APIVariable
	row := s.db.QueryRowContext(ctx, `SELECT id, name, enabled, url, response_selector, fallback, cache_ttl_seconds, timeout_ms, last_status, last_error, last_checked_at, created_at, updated_at FROM bot_api_variables WHERE name=$1`, name)
	loaded, err := scanAPIVariable(row)
	if err != nil {
		return APIVariableResult{Name: name, Status: "missing"}
	}
	variable = loaded
	if !variable.Enabled {
		return APIVariableResult{Name: name, Value: variable.Fallback, Status: "disabled"}
	}
	if cached, ok := s.cachedAPIVariable(variable); ok {
		return APIVariableResult{Name: name, Value: cached, Status: "cache_hit"}
	}
	value, status, code, err := fetchAPIVariable(ctx, variable)
	if err != nil {
		_ = s.updateAPIVariableStatus(context.Background(), variable, status, sanitizeAPIError(err))
		return APIVariableResult{Name: name, Value: variable.Fallback, Status: status, HTTPStatus: code, Error: sanitizeAPIError(err)}
	}
	s.cacheAPIVariable(variable, value, time.Duration(variable.CacheTTLSeconds)*time.Second)
	_ = s.updateAPIVariableStatus(context.Background(), variable, status, "")
	return APIVariableResult{Name: name, Value: value, Status: status, HTTPStatus: code}
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

func (s *Store) cachedAPIVariable(variable APIVariable) (string, bool) {
	s.apiCacheMu.Lock()
	defer s.apiCacheMu.Unlock()
	entry, ok := s.apiCache[variable.Name]
	if !ok || entry.id != variable.ID || !entry.updatedAt.Equal(variable.UpdatedAt) || time.Now().After(entry.expiresAt) {
		return "", false
	}
	return entry.value, true
}
func (s *Store) cacheAPIVariable(variable APIVariable, value string, ttl time.Duration) {
	s.apiCacheMu.Lock()
	defer s.apiCacheMu.Unlock()
	if s.apiCache == nil {
		s.apiCache = make(map[string]apiVariableCacheEntry)
	}
	s.apiCache[variable.Name] = apiVariableCacheEntry{id: variable.ID, updatedAt: variable.UpdatedAt, value: value, expiresAt: time.Now().Add(ttl)}
}

// Diagnostics keep the configuration revision unchanged. A fetch from an older
// revision cannot overwrite diagnostics after an operator edits the variable.
func (s *Store) updateAPIVariableStatus(ctx context.Context, variable APIVariable, status, detail string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `UPDATE bot_api_variables SET last_status=$2, last_error=$3, last_checked_at=now() WHERE id=$1 AND updated_at=$4`, variable.ID, status, capRunes(detail, 180), variable.UpdatedAt)
	return err
}
func sanitizeAPIError(err error) string {
	if err == nil {
		return ""
	}
	return capRunes(strings.TrimSpace(err.Error()), 180)
}
