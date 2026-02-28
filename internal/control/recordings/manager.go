// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package recordings

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/control/vod"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/singleflight"
)

// ProbeState represents the lifecycle of a background media analysis (Gate R3).
type ProbeState string

const (
	ProbeStateQueued   ProbeState = "queued"
	ProbeStateInFlight ProbeState = "in_flight"
	ProbeStateBlocked  ProbeState = "blocked"
)

type probeEntry struct {
	state      ProbeState
	until      time.Time
	retryAfter int
	failures   int
}

// ProbeManager orchestrates idempotent media probing with TTL-backed cooling (Gate R2).
// It prevents thundering herds across client retries during slow probes.
type ProbeManager struct {
	vodManager MetadataManager
	probeFn    func(ctx context.Context, serviceRef, sourceURL string) error
	sf         singleflight.Group

	mu       sync.RWMutex
	progress map[string]*probeEntry // key: serviceRef
}

// NewProbeManager initializes a ProbeManager with 60s TTL logic.
func NewProbeManager(mgr MetadataManager, probeFn func(ctx context.Context, serviceRef, sourceURL string) error) *ProbeManager {
	return &ProbeManager{
		vodManager: mgr,
		probeFn:    probeFn,
		progress:   make(map[string]*probeEntry),
	}
}

// EnsureProbed checks if a probe is already active or in cooling.
// If needed, it triggers a new probe asynchronously (Gate R2).
// Returns the current ProbeState and recommended retry interval in seconds.
func (pm *ProbeManager) EnsureProbed(ctx context.Context, serviceRef string, sourceURL string, localPath string) (ProbeState, int) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	entry, exists := pm.progress[serviceRef]
	now := time.Now()

	// 1. Check TTL / Cooling (Gate R2)
	if exists && now.Before(entry.until) {
		return entry.state, entry.retryAfter
	}

	// 2. State Transition (Initial: Queued)
	if !exists {
		entry = &probeEntry{
			state:      ProbeStateQueued,
			until:      now.Add(60 * time.Second),
			retryAfter: 5,
		}
		pm.progress[serviceRef] = entry
	} else if entry.state == ProbeStateBlocked {
		// Strictly respect backoff
		return entry.state, entry.retryAfter
	} else {
		// Refresh TTL for existing entry
		entry.until = now.Add(60 * time.Second)
	}

	// 3. Trigger Asynchronous Probe via SingleFlight (Gate R2)
	go pm.triggerProbe(serviceRef, sourceURL, localPath)

	return entry.state, entry.retryAfter
}

func (pm *ProbeManager) triggerProbe(serviceRef, sourceURL, localPath string) {
	// De-duplicate concurrent triggers across goroutines
	_, _, _ = pm.sf.Do(serviceRef, func() (interface{}, error) {
		// Promote to InFlight now that work has actually started
		pm.mu.Lock()
		if entry, ok := pm.progress[serviceRef]; ok {
			entry.state = ProbeStateInFlight
			entry.until = time.Now().Add(60 * time.Second)
			entry.retryAfter = 15
		}
		pm.mu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()

		var info *vod.StreamInfo
		var probeErr error

		log.Info().Str("sref", serviceRef).Str("local", localPath).Msg("ProbeManager: triggering probe")

		// Explicit split between Local and Remote probe paths (Gate R6)
		if localPath != "" {
			// Local filesystem probe
			info, probeErr = pm.vodManager.Probe(ctx, localPath)
		} else if pm.probeFn != nil {
			// Remote/URL-based probe via configured provider
			probeErr = pm.probeFn(ctx, serviceRef, sourceURL)
		} else {
			probeErr = fmt.Errorf("no probe mechanism available for non-local recording")
		}

		pm.mu.Lock()
		defer pm.mu.Unlock()
		entry := pm.progress[serviceRef]

		if probeErr != nil {
			log.Warn().Err(probeErr).Str("sref", serviceRef).Msg("ProbeManager: probe failed")
			if entry != nil {
				entry.failures++
				entry.state = ProbeStateBlocked

				// Exponential backoff: min(2^failures, 3600) (Gate R3)
				backoff := int(math.Min(math.Pow(2, float64(entry.failures+2)), 3600))
				entry.retryAfter = backoff
				entry.until = time.Now().Add(time.Duration(backoff) * time.Second)
			}

			pm.vodManager.MarkFailed(serviceRef, probeErr.Error())
		} else {
			log.Info().Str("sref", serviceRef).Msg("ProbeManager: probe succeeded")
			if entry != nil {
				entry.failures = 0
				// Clear entry so next Truth lookup finds Ready state from VODManager
				delete(pm.progress, serviceRef)
			}

			if localPath != "" && info != nil {
				pm.vodManager.MarkProbed(serviceRef, localPath, info, nil)
			}
		}

		return nil, nil
	})
}

// GetProbeState returns the current in-memory state of a probe for a given recording.
func (pm *ProbeManager) GetProbeState(serviceRef string) (ProbeState, int) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if entry, ok := pm.progress[serviceRef]; ok {
		return entry.state, entry.retryAfter
	}
	return "", 0
}
