package twitch

import (
	"strings"
	"sync"
	"time"
)

const sentMessageLimit = 1024
const sentMessageTTL = 2 * time.Minute

// SentMessages remembers IDs, not text or senders, so manual commands still run.
// Entries survive duplicate deliveries until expiry; both size and age are bounded.
type SentMessages struct {
	mu      sync.Mutex
	entries map[string]time.Time
}

func (c *SentMessages) Remember(id string)      { c.rememberAt(id, time.Now()) }
func (c *SentMessages) Contains(id string) bool { return c.containsAt(id, time.Now()) }
func (c *SentMessages) prune(now time.Time) {
	for id, expiry := range c.entries {
		if !expiry.After(now) {
			delete(c.entries, id)
		}
	}
}
func (c *SentMessages) rememberAt(id string, now time.Time) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]time.Time)
	}
	c.prune(now)
	if _, exists := c.entries[id]; !exists && len(c.entries) >= sentMessageLimit {
		var oldest string
		var expiry time.Time
		for key, value := range c.entries {
			if oldest == "" || value.Before(expiry) {
				oldest, expiry = key, value
			}
		}
		delete(c.entries, oldest)
	}
	c.entries[id] = now.Add(sentMessageTTL)
}
func (c *SentMessages) containsAt(id string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.prune(now)
	expiry, ok := c.entries[id]
	return ok && expiry.After(now)
}
