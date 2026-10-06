package bot

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

var errAPIVariableMissing = errors.New("api variable not found")

// The repository preserves configuration revisions when writing diagnostics.
type apiVariableRepository interface {
	lookupAPIVariable(context.Context, string) (APIVariable, error)
	updateAPIVariableStatus(context.Context, APIVariable, string, string) error
}

type apiVariableFetch func(context.Context, APIVariable) (string, string, int, error)

type apiVariableCacheEntry struct {
	id        string
	updatedAt time.Time
	value     string
	expiresAt time.Time
}

// APIVariableResolver owns bounded resolution, fallbacks, and revision-aware caching.
// SQL storage and the guarded HTTP fetcher are its adapters.
type APIVariableResolver struct {
	repository apiVariableRepository
	fetch      apiVariableFetch
	now        func() time.Time
	cacheMu    sync.Mutex
	cache      map[string]apiVariableCacheEntry
}

func newAPIVariableResolver(repository apiVariableRepository, fetch apiVariableFetch, now func() time.Time) *APIVariableResolver {
	return &APIVariableResolver{repository: repository, fetch: fetch, now: now, cache: make(map[string]apiVariableCacheEntry)}
}

// Reuse the store's resolver so every render shares the same revision-aware cache.
func NewAPIVariableResolver(store *Store) *APIVariableResolver {
	if store == nil {
		return newAPIVariableResolver(nil, fetchAPIVariable, time.Now)
	}
	return store.apiVariables
}

func (r *APIVariableResolver) ResolveAPIVariables(names []string) map[string]APIVariableResult {
	out := map[string]APIVariableResult{}
	if r == nil {
		return out
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for _, name := range normalizeAPIVariableNames(names) {
		out[name] = r.resolveAPIVariable(ctx, name)
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

func (r *APIVariableResolver) resolveAPIVariable(ctx context.Context, name string) APIVariableResult {
	if r == nil || r.repository == nil {
		return APIVariableResult{Name: name, Status: "unavailable"}
	}
	variable, err := r.repository.lookupAPIVariable(ctx, name)
	if errors.Is(err, errAPIVariableMissing) {
		return APIVariableResult{Name: name, Status: "missing"}
	}
	if err != nil {
		return APIVariableResult{Name: name, Status: "unavailable"}
	}
	if !variable.Enabled {
		return APIVariableResult{Name: name, Value: variable.Fallback, Status: "disabled"}
	}
	if cached, ok := r.cachedAPIVariable(variable); ok {
		return APIVariableResult{Name: name, Value: cached, Status: "cache_hit"}
	}
	value, status, code, err := r.fetch(ctx, variable)
	if err != nil {
		_ = r.repository.updateAPIVariableStatus(context.Background(), variable, status, sanitizeAPIError(err))
		return APIVariableResult{Name: name, Value: variable.Fallback, Status: status, HTTPStatus: code, Error: sanitizeAPIError(err)}
	}
	r.cacheAPIVariable(variable, value, time.Duration(variable.CacheTTLSeconds)*time.Second)
	_ = r.repository.updateAPIVariableStatus(context.Background(), variable, status, "")
	return APIVariableResult{Name: name, Value: value, Status: status, HTTPStatus: code}
}

func (r *APIVariableResolver) cachedAPIVariable(variable APIVariable) (string, bool) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	entry, ok := r.cache[variable.Name]
	if !ok || entry.id != variable.ID || !entry.updatedAt.Equal(variable.UpdatedAt) || r.now().After(entry.expiresAt) {
		return "", false
	}
	return entry.value, true
}
func (r *APIVariableResolver) cacheAPIVariable(variable APIVariable, value string, ttl time.Duration) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	if r.cache == nil {
		r.cache = make(map[string]apiVariableCacheEntry)
	}
	r.cache[variable.Name] = apiVariableCacheEntry{id: variable.ID, updatedAt: variable.UpdatedAt, value: value, expiresAt: r.now().Add(ttl)}
}

func sanitizeAPIError(err error) string {
	if err == nil {
		return ""
	}
	return capRunes(strings.TrimSpace(err.Error()), 180)
}
