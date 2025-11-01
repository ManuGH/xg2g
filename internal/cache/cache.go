// SPDX-License-Identifier: MIT

// Package cache provides a simple in-memory cache with TTL support.
package cache

import (
	"sync"
	"time"
)

// Cache provides thread-safe caching with expiration support.
type Cache interface {
	// Get retrieves a value from the cache. Returns nil if not found or expired.
	Get(key string) (any, bool)
	// Set stores a value in the cache with the specified TTL.
	Set(key string, value any, ttl time.Duration)
	// Delete removes a value from the cache.
	Delete(key string)
	// Clear removes all values from the cache.
	Clear()
	// Stats returns cache statistics.
	Stats() CacheStats
}

// CacheStats holds cache performance metrics.
type CacheStats struct {
	Hits        int64 // Number of successful Get operations
	Misses      int64 // Number of failed Get operations (not found or expired)
	Sets        int64 // Number of Set operations
	Evictions   int64 // Number of expired entries cleaned up
	CurrentSize int   // Current number of cached entries
}

// entry represents a cached value with expiration time.
type entry struct {
	value      any
	expiration time.Time
}

// isExpired checks if the entry has expired.
func (e *entry) isExpired() bool {
	return time.Now().After(e.expiration)
}

// memoryCache is an in-memory implementation of Cache.
type memoryCache struct {
	mu      sync.RWMutex
	entries map[string]*entry
	stats   CacheStats
	janitor *janitor
}

// NewMemoryCache creates a new in-memory cache with automatic cleanup.
// The cleanupInterval determines how often expired entries are removed.
func NewMemoryCache(cleanupInterval time.Duration) Cache {
	c := &memoryCache{
		entries: make(map[string]*entry),
	}

	// Start background cleanup goroutine
	if cleanupInterval > 0 {
		c.janitor = &janitor{
			interval: cleanupInterval,
			stop:     make(chan struct{}),
		}
		go c.janitor.run(c)
	}

	return c
}

// Get retrieves a value from the cache.
func (c *memoryCache) Get(key string) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	e, found := c.entries[key]
	if !found {
		c.stats.Misses++
		return nil, false
	}

	if e.isExpired() {
		c.stats.Misses++
		return nil, false
	}

	c.stats.Hits++
	return e.value, true
}

// Set stores a value in the cache.
func (c *memoryCache) Set(key string, value any, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[key] = &entry{
		value:      value,
		expiration: time.Now().Add(ttl),
	}
	c.stats.Sets++
}

// Delete removes a value from the cache.
func (c *memoryCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
}

// Clear removes all values from the cache.
func (c *memoryCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*entry)
}

// Stats returns cache statistics.
func (c *memoryCache) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	stats := c.stats
	stats.CurrentSize = len(c.entries)
	return stats
}

// deleteExpired removes all expired entries from the cache.
// Returns the number of entries deleted.
func (c *memoryCache) deleteExpired() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	count := 0
	for key, entry := range c.entries {
		if entry.isExpired() {
			delete(c.entries, key)
			count++
		}
	}

	c.stats.Evictions += int64(count)
	return count
}

// Stop stops the background cleanup goroutine.
func (c *memoryCache) Stop() {
	if c.janitor != nil {
		c.janitor.stop <- struct{}{}
	}
}

// janitor performs periodic cleanup of expired entries.
type janitor struct {
	interval time.Duration
	stop     chan struct{}
}

// run starts the cleanup loop.
func (j *janitor) run(c *memoryCache) {
	ticker := time.NewTicker(j.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.deleteExpired()
		case <-j.stop:
			return
		}
	}
}

// NoOpCache is a cache that does nothing (useful for disabling caching).
type noOpCache struct{}

// NewNoOpCache creates a cache that doesn't cache anything.
func NewNoOpCache() Cache {
	return &noOpCache{}
}

func (c *noOpCache) Get(key string) (any, bool)                  { return nil, false }
func (c *noOpCache) Set(key string, value any, ttl time.Duration) {}
func (c *noOpCache) Delete(key string)                            {}
func (c *noOpCache) Clear()                                        {}
func (c *noOpCache) Stats() CacheStats                            { return CacheStats{} }
