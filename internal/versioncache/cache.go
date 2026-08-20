// Package versioncache stores latest-upstream version strings keyed by ID
// and compares them against installed versions. Services, helpers, PHP, and
// self-update all share this so "update available" is computed the same way.
package versioncache

import (
	"strings"
	"sync"
)

// Cache is a concurrency-safe map of id → latest version string.
type Cache struct {
	mu     sync.RWMutex
	latest map[string]string
}

// New returns an empty Cache.
func New() *Cache {
	return &Cache{latest: make(map[string]string)}
}

// Set stores the latest version for id. An empty version is ignored.
func (c *Cache) Set(id, version string) {
	if id == "" || version == "" {
		return
	}
	c.mu.Lock()
	c.latest[id] = version
	c.mu.Unlock()
}

// Get returns the cached latest version for id, or "".
func (c *Cache) Get(id string) string {
	c.mu.RLock()
	v := c.latest[id]
	c.mu.RUnlock()
	return v
}

// Delete removes the cached latest version for id.
func (c *Cache) Delete(id string) {
	c.mu.Lock()
	delete(c.latest, id)
	c.mu.Unlock()
}

// Snapshot returns a copy of the cache. Callers may read the map without
// holding the lock.
func (c *Cache) Snapshot() map[string]string {
	c.mu.RLock()
	out := make(map[string]string, len(c.latest))
	for k, v := range c.latest {
		out[k] = v
	}
	c.mu.RUnlock()
	return out
}

// Normalize strips a leading "v" so "v2.10.0" and "2.10.0" compare equal.
func Normalize(v string) string {
	return strings.TrimPrefix(v, "v")
}

// Available reports whether installed is behind latest. Empty either side
// means "unknown", not an update.
func Available(installed, latest string) bool {
	cur := Normalize(installed)
	lat := Normalize(latest)
	return cur != "" && lat != "" && cur != lat
}
