// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/ManuGH/xg2g/internal/library"
	"github.com/ManuGH/xg2g/internal/log"
)

// GetLibraryRoots implements ServerInterface.
func (s *Server) GetLibraryRoots(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	librarySvc := s.libraryService
	s.mu.RUnlock()

	if librarySvc == nil {
		writeProblem(w, r, http.StatusServiceUnavailable, "system/unavailable", "Subsystem Unavailable", "UNAVAILABLE", "Library not enabled", nil)
		return
	}

	roots, err := librarySvc.GetRoots(r.Context())
	if err != nil {
		log.L().Error().Err(err).Msg("failed to get library roots")
		RespondError(w, r, http.StatusInternalServerError, ErrInternalServer, nil)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(roots)
}

// GetLibraryRootItems implements ServerInterface.
// Per P0+ Gate #2: Returns 503 with Retry-After if scan running.
// Phase 0 MVP: Direct param parsing + JSON encoding (type mapping refinement in Phase 1).
func (s *Server) GetLibraryRootItems(w http.ResponseWriter, r *http.Request, rootId string, params interface{}) {
	s.mu.RLock()
	librarySvc := s.libraryService
	s.mu.RUnlock()

	if librarySvc == nil {
		writeProblem(w, r, http.StatusServiceUnavailable, "system/unavailable", "Subsystem Unavailable", "UNAVAILABLE", "Library not enabled", nil)
		return
	}

	// Parse pagination params from query
	limit := 100
	offset := 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 500 {
			limit = v
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v >= 0 {
			offset = v
		}
	}

	// Get items
	items, total, err := librarySvc.GetRootItems(r.Context(), rootId, limit, offset)
	if err != nil {
		// Check for scan running error
		if errors.Is(err, library.ErrScanRunning) {
			// Per P0+ Gate #2: Deterministic 503 with Retry-After
			w.Header().Set("Retry-After", "10")
			RespondError(w, r, http.StatusServiceUnavailable, ErrLibraryScanRunning, nil)
			return
		}

		// Check for root not found
		if errors.Is(err, library.ErrRootNotFound) {
			RespondError(w, r, http.StatusNotFound, ErrLibraryRootNotFound, nil)
			return
		}

		log.L().Error().Err(err).Str("root_id", rootId).Msg("failed to get library items")
		RespondError(w, r, http.StatusInternalServerError, ErrInternalServer, nil)
		return
	}

	// Build response (Phase 0 MVP: direct JSON encoding)
	resp := map[string]interface{}{
		"items":  items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
