// Package services implements the use cases of the platform on top of the
// ports; it holds the business rules that span more than one entity.
package services

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
)

// cache keeps results that no longer change (finished sessions). It is small
// and cleared on every write that could affect them.
type cache struct {
	mu      sync.Mutex
	entries map[string]any
	limit   int
}

func newCache(limit int) *cache { return &cache{entries: map[string]any{}, limit: limit} }

func (c *cache) get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.entries[key]
	return v, ok
}

func (c *cache) put(key string, v any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= c.limit {
		c.entries = map[string]any{}
	}
	c.entries[key] = v
}

func (c *cache) clear() {
	c.mu.Lock()
	c.entries = map[string]any{}
	c.mu.Unlock()
}

// newID returns 24 random hex characters.
func newID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
