// Package services implements the use cases of the platform on top of the
// ports; it holds the business rules that span more than one entity.
package services

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
)

// cache keeps results that no longer change (finished sessions) and is cleared
// on every write that could affect them. When full it drops only the oldest
// entry: emptying it all at once made every session compute again as soon as
// the site had more sessions than the limit.
type cache struct {
	mu      sync.Mutex
	entries map[string]any
	order   []string // keys in insertion order, the oldest first
	limit   int
	gen     uint64 // goes up on every clear
}

func newCache(limit int) *cache { return &cache{entries: map[string]any{}, limit: limit} }

func (c *cache) get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.entries[key]
	return v, ok
}

// generation identifies the cache contents: take it before computing a value
// and store it with putIf, so a value computed before a clear is not kept.
func (c *cache) generation() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gen
}

func (c *cache) put(key string, v any) { c.putIf(c.generation(), key, v) }

// putIf stores v unless the cache was cleared after gen was taken.
func (c *cache) putIf(gen uint64, key string, v any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if gen != c.gen {
		return
	}
	if _, ok := c.entries[key]; !ok {
		for len(c.order) >= c.limit && len(c.order) > 0 {
			delete(c.entries, c.order[0])
			c.order = c.order[1:]
		}
		c.order = append(c.order, key)
	}
	c.entries[key] = v
}

func (c *cache) clear() {
	c.mu.Lock()
	c.entries = map[string]any{}
	c.order = nil
	c.gen++
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
