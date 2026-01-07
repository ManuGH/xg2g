package v3

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/ManuGH/xg2g/internal/diagnostics"
)

// DiagnosticsManager coordinates all subsystem health checks.
type DiagnosticsManager struct {
	cache           *diagnostics.LKGCache
	receiverChecker diagnostics.HealthChecker
	dvrChecker      diagnostics.HealthChecker
	epgChecker      diagnostics.HealthChecker
	libraryChecker  diagnostics.HealthChecker
	playbackChecker diagnostics.HealthChecker
}

// NewDiagnosticsManager creates a new diagnostics manager.
func NewDiagnosticsManager(receiverURL string) *DiagnosticsManager {
	cache := diagnostics.NewLKGCache()

	return &DiagnosticsManager{
		cache:           cache,
		receiverChecker: diagnostics.NewReceiverChecker(receiverURL),
		dvrChecker:      diagnostics.NewDVRChecker(receiverURL, cache),
		epgChecker:      diagnostics.NewEPGChecker(receiverURL, cache),
		libraryChecker:  diagnostics.NewLibraryChecker(),
		playbackChecker: diagnostics.NewPlaybackChecker(),
	}
}

// CheckAll runs all subsystem health checks concurrently.
func (dm *DiagnosticsManager) CheckAll(ctx context.Context) diagnostics.DiagnosticsReport {
	// Create timeout context per ADR-SRE-002 P0-D
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Run all checks concurrently
	type result struct {
		subsystem diagnostics.Subsystem
		health    diagnostics.SubsystemHealth
	}

	results := make(chan result, 5)
	checkers := map[diagnostics.Subsystem]diagnostics.HealthChecker{
		diagnostics.SubsystemReceiver: dm.receiverChecker,
		diagnostics.SubsystemDVR:      dm.dvrChecker,
		diagnostics.SubsystemEPG:      dm.epgChecker,
		diagnostics.SubsystemLibrary:  dm.libraryChecker,
		diagnostics.SubsystemPlayback: dm.playbackChecker,
	}

	for subsystem, checker := range checkers {
		go func(s diagnostics.Subsystem, c diagnostics.HealthChecker) {
			health := c.Check(checkCtx)
			results <- result{subsystem: s, health: health}
		}(subsystem, checker)
	}

	// Collect results
	subsystems := make(map[diagnostics.Subsystem]diagnostics.SubsystemHealth)
	measuredAt := time.Now()

	for i := 0; i < len(checkers); i++ {
		r := <-results
		subsystems[r.subsystem] = r.health
		// Track earliest measured_at for overall report
		if r.health.MeasuredAt.Before(measuredAt) {
			measuredAt = r.health.MeasuredAt
		}
	}

	// Compute overall status per ADR-SRE-002 P0-A
	overallStatus := diagnostics.ComputeOverallStatus(subsystems)

	// Build degradation summary
	degradationSummary := diagnostics.BuildDegradationSummary(subsystems)

	return diagnostics.DiagnosticsReport{
		MeasuredAt:         measuredAt,
		DerivedAt:          time.Now(),
		OverallStatus:      overallStatus,
		Subsystems:         subsystems,
		DegradationSummary: degradationSummary,
	}
}

// HandleDiagnostics handles GET /api/v3/system/diagnostics.
func (s *Server) HandleDiagnostics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	report := s.diagnosticsManager.CheckAll(r.Context())

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(report); err != nil {
		// Log error but don't send response (headers already sent)
		// TODO: Add structured logging
		return
	}
}
