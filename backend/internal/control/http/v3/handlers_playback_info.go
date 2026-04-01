// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"
	"net/http"

	"github.com/ManuGH/xg2g/internal/control/read"
	"github.com/ManuGH/xg2g/internal/household"
	"github.com/ManuGH/xg2g/internal/problemcode"
)

// Responsibility: Handles truthful playback capability probing.
// Non-goals: Actual serving of media (see handlers_hls.go).

// GetRecordingPlaybackInfo implements ServerInterface (Legacy GET)
func (s *Server) GetRecordingPlaybackInfo(w http.ResponseWriter, r *http.Request, recordingId string) {
	if _, ok := s.requireHouseholdRecordingAccess(w, r, recordingId); !ok {
		return
	}
	s.handlePlaybackInfo(w, r, recordingId, nil, "v3", "legacy")
}

// PostRecordingPlaybackInfo implements ServerInterface (v3.1 POST)
func (s *Server) PostRecordingPlaybackInfo(w http.ResponseWriter, r *http.Request, recordingId string) {
	if _, ok := s.requireHouseholdRecordingAccess(w, r, recordingId); !ok {
		return
	}
	caps, problem := parseRecordingPlaybackPostInput(r)
	if problem != nil {
		writePlaybackInfoInputProblem(w, r, problem)
		return
	}
	s.handlePlaybackInfo(w, r, recordingId, caps, "v3.1", "compact")
}

// PostLivePlaybackInfo implements ServerInterface (Live Stream Info)
func (s *Server) PostLivePlaybackInfo(w http.ResponseWriter, r *http.Request) {
	input, problem := parseLivePlaybackPostInput(r)
	if problem != nil {
		writePlaybackInfoInputProblem(w, r, problem)
		return
	}
	profile := household.NormalizeProfile(s.currentHouseholdProfile(r.Context()))
	if household.HasServiceRestrictionsNormalized(profile) {
		visibleRefs, err := s.householdVisibleServiceRefSet(profile, s.systemModuleDeps())
		if err != nil {
			writeRegisteredProblem(w, r, http.StatusInternalServerError, "household/service_resolution_failed", "Household Service Resolution Failed", problemcode.CodeReadFailed, "Failed to resolve visible household services", nil)
			return
		}
		if _, ok := visibleRefs[read.CanonicalServiceRef(input.serviceRef)]; !ok {
			writeHouseholdForbidden(w, r, "household/live_service_forbidden", "Live Service Forbidden", "The active household profile is not allowed to access this service")
			return
		}
	}

	// For live playback we pass the normalized serviceRef through the shared decision path.
	// The frontend uses the returned PlaybackInfo to check mode and obtain playbackDecisionToken.
	s.handlePlaybackInfo(w, r, input.serviceRef, input.capabilities, "v3.1", "live")
}

func (s *Server) handlePlaybackInfo(w http.ResponseWriter, r *http.Request, recordingId string, caps *PlaybackCapabilities, apiVersion string, schemaType string) {
	deps := s.recordingsModuleDeps()
	serviceRequest := buildPlaybackInfoServiceRequest(r, recordingId, caps, apiVersion, schemaType)
	dto, err := s.buildPlaybackInfoHTTPResponse(r.Context(), deps, recordingId, caps, schemaType, serviceRequest)
	if err != nil {
		writePlaybackInfoServiceError(w, r, recordingId, schemaType, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(dto)
}

// mapInternalCapsToDecision REMOVED (Replaced by decision.FromCapabilities)
