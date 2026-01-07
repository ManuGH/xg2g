package diagnostics

import (
	"context"
	"time"
)

// EPGChecker implements HealthChecker for the EPG subsystem.
type EPGChecker struct {
	receiverURL string
	cache       *LKGCache
}

// NewEPGChecker creates a new EPG health checker.
func NewEPGChecker(receiverURL string, cache *LKGCache) *EPGChecker {
	return &EPGChecker{
		receiverURL: receiverURL,
		cache:       cache,
	}
}

// Check probes the receiver's EPG endpoint.
// Per ADR-SRE-002:
//   - ok: EPG endpoint returns events for at least one service
//   - degraded: Partial data (some channels have EPG)
//   - unavailable: No EPG data or receiver unavailable
//
// TODO: Implement actual EPG probing in Phase 1.1
// For MVP, we return Unknown until EPG integration is complete.
func (e *EPGChecker) Check(ctx context.Context) SubsystemHealth {
	health := SubsystemHealth{
		Subsystem:   SubsystemEPG,
		Status:      Unknown,
		MeasuredAt:  time.Now(),
		Source:      SourceInferred,
		Criticality: Optional,
	}

	// TODO: Probe EPG endpoint and parse results
	// For now, mark as unknown (not yet implemented)

	return health
}
