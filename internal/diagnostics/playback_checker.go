package diagnostics

import (
	"context"
	"time"
)

// PlaybackChecker implements HealthChecker for the Playback subsystem.
type PlaybackChecker struct {
	// TODO: Add session manager reference to query active sessions
}

// NewPlaybackChecker creates a new Playback health checker.
func NewPlaybackChecker() *PlaybackChecker {
	return &PlaybackChecker{}
}

// Check derives playback health from session manager state.
// Per ADR-SRE-002:
//   - ok: Active sessions < 80% of lease limit
//   - degraded: Active sessions >= 80% or recent FFmpeg crashes
//   - unavailable: FFmpeg missing or lease manager dead
//
// TODO: Integrate with session manager in Phase 1.1
// For MVP, we return OK (derived state).
func (p *PlaybackChecker) Check(ctx context.Context) SubsystemHealth {
	health := SubsystemHealth{
		Subsystem:   SubsystemPlayback,
		Status:      OK, // Assume ok until we have session manager integration
		MeasuredAt:  time.Now(),
		Source:      SourceDerived,
		Criticality: Critical,
	}

	// TODO: Query session manager for:
	// - Active session count
	// - Max sessions (from lease config)
	// - Recent FFmpeg failures

	health.Details = PlaybackDetails{
		ActiveSessions:   0,
		MaxSessions:      10, // Placeholder
		UtilizationPct:   0,
		RecentFailures1h: 0,
	}

	return health
}
