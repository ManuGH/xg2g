package diagnostics

import (
	"context"
)

// HealthChecker defines the interface for subsystem health checks.
type HealthChecker interface {
	Check(ctx context.Context) SubsystemHealth
}

// ComputeOverallStatus calculates overall system health per ADR-SRE-002 P0-A.
//
// Logic:
//   - Playback unavailable → unavailable (critical subsystem)
//   - Receiver AND Library both unavailable → unavailable (no media source)
//   - Any subsystem degraded/unavailable → degraded
//   - All subsystems ok → ok
func ComputeOverallStatus(subsystems map[Subsystem]SubsystemHealth) HealthStatus {
	playback, hasPlayback := subsystems[SubsystemPlayback]
	if hasPlayback && playback.Status == Unavailable {
		return Unavailable
	}

	receiver, hasReceiver := subsystems[SubsystemReceiver]
	library, hasLibrary := subsystems[SubsystemLibrary]

	// If both receiver and library are unavailable, no media source available
	if hasReceiver && hasLibrary {
		if receiver.Status == Unavailable && library.Status == Unavailable {
			return Unavailable
		}
	}

	// If any subsystem is degraded or unavailable, overall is degraded
	for _, health := range subsystems {
		if health.Status == Degraded || health.Status == Unavailable {
			return Degraded
		}
	}

	return OK
}

// BuildDegradationSummary creates actionable degradation items for failed subsystems.
func BuildDegradationSummary(subsystems map[Subsystem]SubsystemHealth) []DegradationItem {
	var items []DegradationItem

	for _, health := range subsystems {
		if health.Status == Degraded || health.Status == Unavailable {
			item := DegradationItem{
				Subsystem: health.Subsystem,
				Status:    health.Status,
				ErrorCode: health.ErrorCode,
			}

			if health.LastOK != nil {
				item.Since = *health.LastOK
			} else {
				item.Since = health.MeasuredAt
			}

			// Add suggested actions if available
			if actions, ok := SuggestedActions[health.ErrorCode]; ok {
				item.SuggestedActions = actions
			}

			items = append(items, item)
		}
	}

	return items
}
