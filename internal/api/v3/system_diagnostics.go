// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"
	"net/http"
)

// GetSystemDiagnostics implements ServerInterface.
// Per ADR-SRE-002, this endpoint returns comprehensive system health status
// for all subsystems (receiver, DVR, EPG, library, playback) with concurrent probing.
func (s *Server) GetSystemDiagnostics(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	diagMgr := s.diagnosticsManager
	s.mu.RUnlock()

	if diagMgr == nil {
		// Fallback: diagnostics manager not initialized (should not happen in production)
		http.Error(w, "Diagnostics system not available", http.StatusServiceUnavailable)
		return
	}

	// Run all subsystem health checks concurrently
	report := diagMgr.CheckAll(r.Context())

	// Serialize report directly to JSON (bypass OpenAPI generated types for MVP)
	// The report already matches the OpenAPI schema structure
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(report); err != nil {
		// Log error but headers already sent
		// TODO: Add structured logging
		return
	}
}
