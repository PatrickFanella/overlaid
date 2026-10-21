package variables

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

const cacheTenantA = "aaaaaaaa-1111-2222-3333-444444444444"
const cacheTenantB = "bbbbbbbb-1111-2222-3333-444444444444"

func cached(t *testing.T, c *Cache, tenant, source string, wantHit bool) Template {
	t.Helper()
	template, hit, err := c.Parse(tenant, source, SyntaxVersion)
	if err != nil || hit != wantHit || template.Source() != source {
		t.Fatalf("parse: hit=%v want=%v err=%v", hit, wantHit, err)
	}
	return template
}

func TestCacheIsolationAndImmutableValues(t *testing.T) {
	c, err := NewCache(8, 4)
	if err != nil {
		t.Fatal(err)
	}
	source := `$(user "$(query)")`
	template := cached(t, c, cacheTenantA, source, false)
	cached(t, c, cacheTenantB, source, false)
	parts := template.Parts()
	parts[0].Call.Name = "changed"
	parts[0].Call.Arguments[0].Parts[0].Call.Name = "changed"
	got := cached(t, c, cacheTenantA, source, true)
	if got.Parts()[0].Call.Name != "user" || got.Parts()[0].Call.Arguments[0].Parts[0].Call.Name != "query" {
		t.Fatal("cached AST mutated")
	}
	cached(t, c, cacheTenantA, "different", false)
	if err := c.PurgeTenant(cacheTenantA); err != nil {
		t.Fatal(err)
	}
	cached(t, c, cacheTenantB, source, true)
	cached(t, c, cacheTenantA, source, false)
	if template.Source() != source {
		t.Fatal("purge changed returned template")
	}
}

func TestCacheLRUBounds(t *testing.T) {
	c, _ := NewCache(3, 2)
	cached(t, c, cacheTenantA, "one", false)
	cached(t, c, cacheTenantA, "two", false)
	cached(t, c, cacheTenantB, "other", false)
	cached(t, c, cacheTenantA, "one", true)
	cached(t, c, cacheTenantA, "three", false) // evicts A/two only
	cached(t, c, cacheTenantB, "other", true)
	cached(t, c, cacheTenantA, "one", true)
	cached(t, c, cacheTenantB, "four", false) // global oldest is A/three
	cached(t, c, cacheTenantA, "one", true)
	cached(t, c, cacheTenantB, "other", true)
	cached(t, c, cacheTenantA, "three", false)
	if len(c.entries) != 3 || c.lru.Len() != 3 {
		t.Fatal("capacity mismatch")
	}
}

func TestCacheRejectsWithoutRetention(t *testing.T) {
	for _, bounds := range [][2]int{{0, 1}, {257, 1}, {1, 0}, {33, 33}, {1, 2}, {-1, 1}} {
		if c, err := NewCache(bounds[0], bounds[1]); c != nil || !errors.Is(err, ErrCacheConfig) {
			t.Fatal("accepted bounds", bounds)
		}
	}
	c, _ := NewCache(2, 1)
	cached(t, c, cacheTenantA, "valid", false)
	for _, tenant := range []string{"", strings.ToUpper(cacheTenantA), "00000000-0000-0000-0000-000000000000", cacheTenantA + "x", "aaaaaaaa_1111-2222-3333-444444444444"} {
		if _, _, err := c.Parse(tenant, "x", 2); !errors.Is(err, ErrCacheTenant) {
			t.Fatal("accepted tenant")
		}
		if err := c.PurgeTenant(tenant); !errors.Is(err, ErrCacheTenant) {
			t.Fatal("accepted purge tenant")
		}
	}
	for _, input := range []struct {
		source  string
		version int
	}{{"x", 1}, {strings.Repeat("x", 4097), 2}, {"$(", 2}, {"\x00", 2}, {"\xff", 2}} {
		v, hit, err := c.Parse(cacheTenantA, input.source, input.version)
		if err == nil || hit || v.SyntaxVersion() != 0 || len(c.entries) != 1 {
			t.Fatal("invalid parse retained")
		}
	}
	cached(t, c, cacheTenantA, "valid", true)
	for _, invalid := range []*Cache{nil, {}} {
		if _, _, err := invalid.Parse(cacheTenantA, "x", 2); !errors.Is(err, ErrCacheConfig) {
			t.Fatal("invalid cache accepted")
		}
		if err := invalid.PurgeTenant(cacheTenantA); !errors.Is(err, ErrCacheConfig) {
			t.Fatal("invalid cache purge")
		}
	}
}

func TestCacheCollisionDoesNotSubstitute(t *testing.T) {
	c, _ := NewCache(2, 2)
	cached(t, c, cacheTenantA, "old", false)
	oldKey := cacheKey{tenant: cacheTenantA, version: 2, digest: sha256.Sum256([]byte("old"))}
	e := c.entries[oldKey]
	delete(c.entries, oldKey)
	key := cacheKey{tenant: cacheTenantA, version: 2, digest: sha256.Sum256([]byte("new"))}
	entry := e.Value.(cacheEntry)
	entry.key = key
	e.Value = entry
	c.entries[key] = e
	cached(t, c, cacheTenantA, "new", false)
	cached(t, c, cacheTenantA, "new", true)
	if len(c.entries) != 1 || c.lru.Len() != 1 {
		t.Fatal("collision orphan")
	}
}

func TestCacheConcurrentParseAndPurge(t *testing.T) {
	c, _ := NewCache(16, 8)
	var wg sync.WaitGroup
	for worker := range 12 {
		wg.Go(func() {
			tenant := cacheTenantA
			if worker%2 != 0 {
				tenant = cacheTenantB
			}
			for i := range 200 {
				source := fmt.Sprintf("$(user) %d", i%20)
				template, _, err := c.Parse(tenant, source, 2)
				if err != nil || template.Source() != source {
					t.Error("concurrent parse failed")
					return
				}
				if i%7 == 0 {
					if err := c.PurgeTenant(tenant); err != nil {
						t.Error(err)
						return
					}
				}
			}
		})
	}
	wg.Wait()
	for _, tenant := range []string{cacheTenantA, cacheTenantB} {
		if err := c.PurgeTenant(tenant); err != nil {
			t.Fatal(err)
		}
	}
	if len(c.entries) != 0 || c.lru.Len() != 0 {
		t.Fatal("purge left entries")
	}
	cached(t, c, cacheTenantA, "$(user)", false)
}

func FuzzCacheBounds(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 255, 4})
	f.Fuzz(func(t *testing.T, operations []byte) {
		if len(operations) > 128 {
			return
		}
		c, _ := NewCache(4, 2)
		for _, op := range operations {
			tenant := cacheTenantA
			if op%2 != 0 {
				tenant = cacheTenantB
			}
			if op == 255 {
				if err := c.PurgeTenant(tenant); err != nil {
					t.Fatal(err)
				}
			} else {
				source := fmt.Sprintf("$(user) %d", op%7)
				cached(t, c, tenant, source, c.entries[cacheKey{tenant: tenant, version: 2, digest: sha256.Sum256([]byte(source))}] != nil)
			}
			counts := map[string]int{}
			for e := c.lru.Front(); e != nil; e = e.Next() {
				key := e.Value.(cacheEntry).key
				if c.entries[key] != e {
					t.Fatal("index/list mismatch")
				}
				counts[key.tenant]++
			}
			if c.lru.Len() != len(c.entries) || len(c.entries) > 4 || counts[cacheTenantA] > 2 || counts[cacheTenantB] > 2 {
				t.Fatal("cache bounds")
			}
		}
	})
}
