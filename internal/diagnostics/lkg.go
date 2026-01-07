package diagnostics

import (
	"sync"
	"time"
)

// LKGCache implements Last-Known-Good caching per ADR-SRE-002 P0-C.
// All data is in-memory only (no persistence across restart).
type LKGCache struct {
	mu  sync.RWMutex
	dvr map[string]*DVRCacheEntry
	epg map[string]*EPGCacheEntry
}

// DVRCacheEntry caches DVR subsystem state.
type DVRCacheEntry struct {
	RecordingCount int
	TimerCount     int
	LastOK         time.Time
	TTL            time.Duration // 6h per ADR-SRE-002
}

// EPGCacheEntry caches EPG subsystem state.
type EPGCacheEntry struct {
	EventCount   int
	ServiceCount int
	OldestEvent  time.Time
	NewestEvent  time.Time
	LastOK       time.Time
	TTL          time.Duration // 24h per ADR-SRE-002
}

// NewLKGCache creates a new Last-Known-Good cache.
func NewLKGCache() *LKGCache {
	return &LKGCache{
		dvr: make(map[string]*DVRCacheEntry),
		epg: make(map[string]*EPGCacheEntry),
	}
}

// GetDVR retrieves cached DVR state if valid (not expired).
// Returns nil if not found or expired.
func (c *LKGCache) GetDVR(receiverID string) *DVRCacheEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.dvr[receiverID]
	if !ok {
		return nil
	}

	if time.Since(entry.LastOK) > entry.TTL {
		return nil // Expired
	}

	return entry
}

// SetDVR caches DVR state with 6h TTL.
func (c *LKGCache) SetDVR(receiverID string, recordingCount, timerCount int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.dvr[receiverID] = &DVRCacheEntry{
		RecordingCount: recordingCount,
		TimerCount:     timerCount,
		LastOK:         time.Now(),
		TTL:            6 * time.Hour,
	}
}

// GetEPG retrieves cached EPG state if valid (not expired).
// Returns nil if not found or expired.
func (c *LKGCache) GetEPG(receiverID string) *EPGCacheEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.epg[receiverID]
	if !ok {
		return nil
	}

	if time.Since(entry.LastOK) > entry.TTL {
		return nil // Expired
	}

	return entry
}

// SetEPG caches EPG state with 24h TTL.
// Max 10,000 events per receiver enforced at caller level.
func (c *LKGCache) SetEPG(receiverID string, eventCount, serviceCount int, oldestEvent, newestEvent time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.epg[receiverID] = &EPGCacheEntry{
		EventCount:   eventCount,
		ServiceCount: serviceCount,
		OldestEvent:  oldestEvent,
		NewestEvent:  newestEvent,
		LastOK:       time.Now(),
		TTL:          24 * time.Hour,
	}
}

// EvictExpired removes expired entries from cache (garbage collection).
func (c *LKGCache) EvictExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()

	// Evict expired DVR entries
	for id, entry := range c.dvr {
		if now.Sub(entry.LastOK) > entry.TTL {
			delete(c.dvr, id)
		}
	}

	// Evict expired EPG entries
	for id, entry := range c.epg {
		if now.Sub(entry.LastOK) > entry.TTL {
			delete(c.epg, id)
		}
	}
}
