// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package llhls

import (
	"context"
	"sync"
	"time"
)

// DefaultPartTargetMs is the nominal EXT-X-PART duration. It must match the
// frag_duration the FFmpeg live plan sets (see infra/media/ffmpeg
// llhlsPartTargetMs) so advertised part durations track real fragment cuts.
const DefaultPartTargetMs = 500

// Registry manages one Tracker per live session, created lazily on the
// first LL playlist request and torn down after an idle period (no playlist
// access), which covers session teardown without coupling to the session
// lifecycle machinery.
type Registry struct {
	partTargetMs int
	idleTimeout  time.Duration

	mu       sync.Mutex
	trackers map[string]*registryEntry
}

type registryEntry struct {
	tracker    *Tracker
	cancel     context.CancelFunc
	lastAccess time.Time
}

// NewRegistry creates a tracker registry. partTargetMs is the nominal
// EXT-X-PART duration advertised in playlists.
func NewRegistry(partTargetMs int) *Registry {
	r := &Registry{
		partTargetMs: partTargetMs,
		idleTimeout:  2 * time.Minute,
		trackers:     make(map[string]*registryEntry),
	}
	go r.evictLoop()
	return r
}

// PartTargetMs exposes the configured part target for consumers that size
// blocking-reload deadlines.
func (r *Registry) PartTargetMs() int {
	return r.partTargetMs
}

// Get returns the session's tracker, creating and starting it when absent.
func (r *Registry) Get(sessionDir, sessionID string) *Tracker {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.trackers[sessionID]; ok {
		e.lastAccess = time.Now()
		return e.tracker
	}
	ctx, cancel := context.WithCancel(context.Background())
	e := &registryEntry{
		tracker:    NewTracker(ctx, sessionDir, r.partTargetMs),
		cancel:     cancel,
		lastAccess: time.Now(),
	}
	r.trackers[sessionID] = e
	return e.tracker
}

func (r *Registry) evictLoop() {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for range tick.C {
		cutoff := time.Now().Add(-r.idleTimeout)
		r.mu.Lock()
		for id, e := range r.trackers {
			if e.lastAccess.Before(cutoff) {
				e.cancel()
				delete(r.trackers, id)
			}
		}
		r.mu.Unlock()
	}
}
