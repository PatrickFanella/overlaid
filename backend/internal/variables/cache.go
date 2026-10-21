package variables

import (
	"container/list"
	"crypto/sha256"
	"errors"
	"strings"
	"sync"
)

const (
	MaxCacheEntries       = 256
	MaxTenantCacheEntries = 32
)

var (
	ErrCacheConfig = errors.New("variable_cache_config")
	ErrCacheTenant = errors.New("variable_cache_tenant")
)

type cacheKey struct {
	tenant  string
	version int
	digest  [sha256.Size]byte
}
type cacheEntry struct {
	key      cacheKey
	template Template
}

// Cache retains parsed source only. Construct with NewCache; do not copy after
// first use. Entries are private immutable templates, never rendered values.
// Parsing and purge share a lock: purge cannot race a delayed insertion.
type Cache struct {
	mu                  sync.Mutex
	capacity, perTenant int
	entries             map[cacheKey]*list.Element
	lru                 list.List
}

func NewCache(capacity, perTenant int) (*Cache, error) {
	if capacity < 1 || capacity > MaxCacheEntries || perTenant < 1 || perTenant > MaxTenantCacheEntries || perTenant > capacity {
		return nil, ErrCacheConfig
	}
	return &Cache{capacity: capacity, perTenant: perTenant, entries: make(map[cacheKey]*list.Element)}, nil
}

// Parse returns a template and whether it reused an entry. Tenant identity is
// canonical but not authenticated here; callers must establish their scope.
// Failed parses do not change entries or eviction order. Work under the lock
// is bounded by the parser's 4 KiB source and 256-node limits, without callbacks.
func (c *Cache) Parse(tenant, source string, version int) (Template, bool, error) {
	if !cacheTenant(tenant) {
		return Template{}, false, ErrCacheTenant
	}
	// Reject oversized/unsupported input before hashing or waiting for the lock.
	if version != SyntaxVersion || len(source) > MaxSourceBytes {
		t, err := Parse(source, version)
		return t, false, err
	}
	if c == nil {
		return Template{}, false, ErrCacheConfig
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		return Template{}, false, ErrCacheConfig
	}
	key := cacheKey{tenant: tenant, version: version, digest: sha256.Sum256([]byte(source))}
	if e := c.entries[key]; e != nil {
		entry := e.Value.(cacheEntry)
		// A hash collision cannot substitute another source.
		if entry.template.Source() == source {
			c.lru.MoveToFront(e)
			return entry.template, true, nil
		}
	}
	t, err := Parse(source, version)
	if err != nil {
		return Template{}, false, err
	}
	if old := c.entries[key]; old != nil {
		c.remove(old)
	}
	count := 0
	var oldestTenant *list.Element
	for e := c.lru.Front(); e != nil; e = e.Next() {
		if e.Value.(cacheEntry).key.tenant == tenant {
			count++
			oldestTenant = e
		}
	}
	if count >= c.perTenant {
		c.remove(oldestTenant)
	}
	if len(c.entries) >= c.capacity {
		c.remove(c.lru.Back())
	}
	key.tenant = strings.Clone(tenant)
	c.entries[key] = c.lru.PushFront(cacheEntry{key: key, template: t})
	return t, false, nil
}

// PurgeTenant releases this cache's references for a tenant. Previously returned
// templates remain caller-owned values. A later Parse may explicitly repopulate.
func (c *Cache) PurgeTenant(tenant string) error {
	if !cacheTenant(tenant) {
		return ErrCacheTenant
	}
	if c == nil {
		return ErrCacheConfig
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		return ErrCacheConfig
	}
	for e := c.lru.Front(); e != nil; {
		next := e.Next()
		if e.Value.(cacheEntry).key.tenant == tenant {
			c.remove(e)
		}
		e = next
	}
	return nil
}

func (c *Cache) remove(e *list.Element) {
	delete(c.entries, e.Value.(cacheEntry).key)
	c.lru.Remove(e)
}

func cacheTenant(s string) bool {
	if len(s) != 36 || s == "00000000-0000-0000-0000-000000000000" {
		return false
	}
	for i := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if s[i] != '-' {
				return false
			}
		} else if !digit(s[i]) && (s[i] < 'a' || s[i] > 'f') {
			return false
		}
	}
	return true
}
