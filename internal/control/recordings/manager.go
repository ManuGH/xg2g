// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package recordings

import (
	"context"
	"errors"
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

// probeManager orchestrates idempotent media probing with TTL-backed cooling (Gate R2).
// It prevents thundering herds across client retries during slow probes.
type probeManager struct {
	rootCtx    context.Context
	vodManager MetadataManager
	probeFn    func(ctx context.Context, serviceRef, sourceURL string) (*vod.StreamInfo, error)
	persistor  DurationPersistor
	sf         singleflight.Group

	mu       sync.RWMutex
	progress map[string]*probeEntry // key: serviceRef
}

func newProbeManager(rootCtx context.Context, mgr MetadataManager, probeFn func(ctx context.Context, serviceRef, sourceURL string) (*vod.StreamInfo, error), persistor DurationPersistor) *probeManager {
	if rootCtx == nil {
		rootCtx = context.Background()
	}
	return &probeManager{
		rootCtx:    rootCtx,
		vodManager: mgr,
		probeFn:    probeFn,
		persistor:  persistor,
		progress:   make(map[string]*probeEntry),
	}
}

// ensureProbed checks if a probe is already active or in cooling.
// If needed, it triggers a new probe asynchronously (Gate R2).
// Returns the current ProbeState and recommended retry interval in seconds.
func (pm *probeManager) ensureProbed(ctx context.Context, serviceRef string, sourceURL string, localPath string) (ProbeState, int) {
	if pm == nil {
		return "", 0
	}
	pm.mu.Lock()
	defer pm.mu.Unlock()

	entry, exists := pm.progress[serviceRef]
	now := time.Now()

	// 1. Check TTL / Cooling (Gate R2)
	if exists && now.Before(entry.until) {
		if entry.state == ProbeStateBlocked {
			recordingsProbeBlockedTotal.WithLabelValues(probeBlockedReasonCooldown).Inc()
		}
		return entry.state, entry.retryAfter
	}

	// 2. State Transition (Initial/Expired -> Queued)
	if !exists {
		entry = &probeEntry{}
		pm.progress[serviceRef] = entry
	}
	// NOTE: Expired blocked entries MUST be allowed to retry to avoid permanent liveness loss.
	entry.state = ProbeStateQueued
	entry.until = now.Add(60 * time.Second)
	entry.retryAfter = 5

	// 3. Trigger Asynchronous Probe via SingleFlight (Gate R2)
	// Use serviceRef for state tracking, but use hashed source for thundering herd protection.
	key := pm.ResolveKey(serviceRef, sourceURL)
	go pm.triggerProbe(key, serviceRef, sourceURL, localPath)

	return entry.state, entry.retryAfter
}

func (pm *probeManager) triggerProbe(key, serviceRef, sourceURL, localPath string) {
	if pm == nil {
		return
	}
	// De-duplicate concurrent triggers across goroutines using the ResolveKey (usually hashed source)
	executed := false
	_, _, shared := pm.sf.Do(key, func() (interface{}, error) {
		executed = true
		probeID := probeIDForKey(key)
		startedAt := time.Now()
		result := probeResultSuccess
		recordingsProbeStartedTotal.Inc()
		recordingsProbeInflight.Inc()
		defer func() {
			recordingsProbeInflight.Dec()
			recordingsProbeFinishedTotal.WithLabelValues(result).Inc()
			recordingsProbeDurationSeconds.WithLabelValues(result).Observe(time.Since(startedAt).Seconds())
		}()
		// Promote to InFlight now that work has actually started
		pm.mu.Lock()
		if entry, ok := pm.progress[serviceRef]; ok {
			entry.state = ProbeStateInFlight
			entry.until = time.Now().Add(60 * time.Second)
			entry.retryAfter = 15
		}
		pm.mu.Unlock()

		ctx, cancel := context.WithTimeout(pm.rootCtx, 3*time.Minute)
		defer cancel()

		var info *vod.StreamInfo
		var probeErr error

		log.Info().Str("probe_id", probeID).Str("sref", serviceRef).Str("local", localPath).Msg("ProbeManager: triggering probe")

		// Explicit split between Local and Remote probe paths (Gate R6)
		if localPath != "" {
			// Local filesystem probe
			info, probeErr = pm.vodManager.Probe(ctx, localPath)
		} else if pm.probeFn != nil {
			// Remote/URL-based probe via configured provider
			info, probeErr = pm.probeFn(ctx, serviceRef, sourceURL)
		} else {
			probeErr = fmt.Errorf("no probe mechanism available for non-local recording")
		}

		pm.mu.Lock()
		defer pm.mu.Unlock()
		entry := pm.progress[serviceRef]

		if probeErr != nil {
			result = classifyProbeResult(probeErr)
			logEvent := log.Warn().Err(probeErr).Str("probe_id", probeID).Str("sref", serviceRef).Int64("duration_ms", time.Since(startedAt).Milliseconds())
			if entry != nil {
				blockWithBackoff(entry)
				logEvent = logEvent.Time("blocked_until", entry.until)
			}
			logEvent.Msg("ProbeManager: probe failed")
			recordingsProbeBlockedTotal.WithLabelValues(probeBlockedReasonProbeError).Inc()

			pm.vodManager.MarkFailed(serviceRef, probeErr.Error())
		} else {
			// Gate R8.2 Option A (fail-closed): persistence must succeed before truth is marked ready.
			if pm.persistor != nil && info != nil && info.Video.Duration > 0 {
				if persistErr := pm.persistor.PersistDuration(ctx, serviceRef, int64(info.Video.Duration)); persistErr != nil {
					result = probeResultPersistError
					logEvent := log.Error().Err(persistErr).Str("probe_id", probeID).Str("sref", serviceRef).Int64("duration_ms", time.Since(startedAt).Milliseconds())
					if entry != nil {
						blockWithBackoff(entry)
						logEvent = logEvent.Time("blocked_until", entry.until)
					}
					logEvent.Msg("ProbeManager: duration persist failed; keeping truth non-ready")
					recordingsProbeBlockedTotal.WithLabelValues(probeBlockedReasonPersistError).Inc()
					return nil, nil
				}
			}

			log.Info().Str("probe_id", probeID).Str("sref", serviceRef).Int64("duration_ms", time.Since(startedAt).Milliseconds()).Msg("ProbeManager: probe succeeded")
			pm.vodManager.MarkProbed(serviceRef, localPath, info, nil)

			if entry != nil {
				entry.failures = 0
				// Clear entry so next Truth lookup finds Ready state from VODManager
				delete(pm.progress, serviceRef)
			}
		}

		return nil, nil
	})
	if shared && !executed {
		recordingsProbeDedupedTotal.Inc()
		log.Debug().Str("sref", serviceRef).Msg("ProbeManager: probe request deduped by singleflight")
	}
}

// ResolveKey generates a canonical key for singleflight de-duplication.
// It prefers hashing the source URL to protect against thundering herds on shared files.
func (pm *probeManager) ResolveKey(serviceRef, sourceURL string) string {
	if sourceURL == "" {
		return serviceRef
	}
	// Note: resolveSource already canonicalizes paths.
	return hashSingleflightKey("probe", sourceURL)
}

// GetProbeState returns the current in-memory state of a probe for a given recording.
func (pm *probeManager) GetProbeState(serviceRef string) (ProbeState, int) {
	if pm == nil {
		return "", 0
	}
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if entry, ok := pm.progress[serviceRef]; ok {
		return entry.state, entry.retryAfter
	}
	return "", 0
}

func blockWithBackoff(entry *probeEntry) {
	entry.failures++
	entry.state = ProbeStateBlocked
	// Exponential backoff: min(2^failures, 3600) (Gate R3)
	backoff := int(math.Min(math.Pow(2, float64(entry.failures+2)), 3600))
	entry.retryAfter = backoff
	entry.until = time.Now().Add(time.Duration(backoff) * time.Second)
}

func probeIDForKey(key string) string {
	prefix := key
	if len(prefix) > 12 {
		prefix = prefix[:12]
	}
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func classifyProbeResult(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return probeResultCanceled
	case errors.Is(err, context.DeadlineExceeded):
		return probeResultTimeout
	default:
		return probeResultProbeError
	}
}
