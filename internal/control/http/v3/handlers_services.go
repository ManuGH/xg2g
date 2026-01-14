// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"
	"net/http"
	"os"
	"time"

	"github.com/ManuGH/xg2g/internal/control/read"
	"github.com/ManuGH/xg2g/internal/log"
)

// Responsibility: Handles Service/Bouquet lists and manual refresh scanning.
// Non-goals: Streams or configuration.

// GetServicesBouquets implements ServerInterface
func (s *Server) GetServicesBouquets(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	cfg := s.cfg
	snap := s.snap
	s.mu.RUnlock()

	// Use Truthful Counting version
	// Use Truthful Counting version
	bouquets, fellBackToConfig, err := read.GetBouquetsWithCounts(cfg, snap)

	// Metrics: Missing Playlist Fallback
	if fellBackToConfig {
		v3BouquetsMissingTotal.Inc()
	}

	if err != nil {
		// Metrics: Read Error with Cause Label
		var cause string
		if os.IsPermission(err) {
			cause = "permission"
		} else if os.IsNotExist(err) {
			// Should be handled by read layer fallback, but just in case
			cause = "stat"
		} else {
			// Infer other causes (open/read/parse not easily distinguishable without wrapped errors)
			// User allowed "unknown" or "read"
			cause = "read"
		}
		v3BouquetsReadErrorTotal.WithLabelValues(cause).Inc()

		log.L().Error().Err(err).Str("cause", cause).Msg("failed to get bouquets")
		writeProblem(w, r, http.StatusInternalServerError, "services/read_failed", "Failed to Read Bouquets", "READ_FAILED", err.Error(), nil)
		return
	}

	// Metrics: Empty Playlist Truth
	if len(bouquets) == 0 && !fellBackToConfig {
		v3BouquetsEmptyTotal.Inc()
	}

	resp := make([]Bouquet, 0, len(bouquets))
	for _, b := range bouquets {
		// Scoping
		b := b
		resp = append(resp, Bouquet{
			Name:     &b.Name,
			Services: &b.Count,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	// Contract: Always return list (empty list if nil) - checked by make()
	_ = json.NewEncoder(w).Encode(resp)
}

// GetServices implements ServerInterface
func (s *Server) GetServices(w http.ResponseWriter, r *http.Request, params GetServicesParams) {
	s.mu.RLock()
	cfg := s.cfg
	snap := s.snap
	src := s.servicesSource
	s.mu.RUnlock()

	q := read.ServicesQuery{}
	if params.Bouquet != nil {
		q.Bouquet = *params.Bouquet
	}

	res, err := read.GetServices(cfg, snap, src, q)
	if err != nil {
		log.L().Error().Err(err).Msg("failed to get services")
		writeProblem(w, r, http.StatusInternalServerError, "system/internal_error", "Receiver Read Error", "INTERNAL_ERROR", err.Error(), nil)
		return
	}

	var items []Service
	if res.Items != nil {
		items = make([]Service, 0, len(res.Items))
		for _, s := range res.Items {
			// Scoping
			s := s
			items = append(items, Service{
				Id:         &s.ID,
				Name:       &s.Name,
				Group:      &s.Group,
				LogoUrl:    &s.LogoURL,
				Number:     &s.Number,
				Enabled:    &s.Enabled,
				ServiceRef: &s.ServiceRef,
			})
		}
	} else if res.EmptyEncoding == read.EmptyEncodingArray {
		items = make([]Service, 0)
	}

	w.Header().Set("Content-Type", "application/json")
	if items == nil && res.EmptyEncoding == read.EmptyEncodingNull {
		_, _ = w.Write([]byte("null"))
		return
	}
	if items == nil {
		items = make([]Service, 0)
	}
	_ = json.NewEncoder(w).Encode(items)
}

// PostServicesIdToggle implements ServerInterface
func (s *Server) PostServicesIdToggle(w http.ResponseWriter, r *http.Request, id string) {
	var req PostServicesIdToggleJSONBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "system/invalid_input", "Invalid Request Body", "INVALID_INPUT", "The request body is malformed", nil)
		return
	}

	if s.channelManager == nil {
		writeProblem(w, r, http.StatusInternalServerError, "system/unavailable", "Subsystem Unavailable", "UNAVAILABLE", "Channel manager not initialized", nil)
		return
	}

	enabled := false
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	if err := s.channelManager.SetEnabled(id, enabled); err != nil {
		log.L().Error().Err(err).Str("channel_id", id).Msg("failed to toggle channel")
		writeProblem(w, r, http.StatusInternalServerError, "system/save_failed", "Save Failed", "SAVE_FAILED", "Failed to save channel state", nil)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// PostSystemRefresh implements ServerInterface
func (s *Server) PostSystemRefresh(w http.ResponseWriter, r *http.Request) {
	s.handleRefresh(w, r)
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	scan := s.v3Scan
	s.mu.RUnlock()

	if isNil(scan) {
		writeProblem(w, r, http.StatusServiceUnavailable, "system/unavailable", "Subsystem Unavailable", "UNAVAILABLE", "Scanner not enabled", nil)
		return
	}

	go func() {
		start := time.Now()
		scan.RunBackground()
		log.L().Info().Dur("duration", time.Since(start)).Msg("manual refresh triggered via API")
	}()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "refresh triggered"})
}
