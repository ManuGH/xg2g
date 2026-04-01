// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/control/read"
	"github.com/ManuGH/xg2g/internal/household"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/m3u"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/ManuGH/xg2g/internal/platform/paths"
	"github.com/ManuGH/xg2g/internal/problemcode"
)

// Responsibility: Handles Timer and DVR management including conflicts and scheduling.
// Non-goals: General system status.

// GetTimers implements ServerInterface
func (s *Server) GetTimers(w http.ResponseWriter, r *http.Request, params GetTimersParams) {
	profile, ok := s.requireHouseholdDVRManageAccess(w, r)
	if !ok {
		return
	}
	profile = household.NormalizeProfile(profile)

	s.mu.RLock()
	src := s.timersSource
	s.mu.RUnlock()

	if s.timersSource == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "dvr/unavailable", "DVR Unavailable", problemcode.CodeUnavailable, "Timers source not initialized", nil)
		return
	}

	q := read.TimersQuery{}
	if params.State != nil {
		q.State = read.TimerState(*params.State)
	}
	if params.From != nil {
		q.From = int64(*params.From)
	}

	timers, err := read.GetTimers(r.Context(), src, q, read.RealClock{})
	if err != nil {
		log.L().Error().Err(err).Msg("failed to communicate with receiver")
		writeRegisteredProblem(w, r, http.StatusBadGateway, "dvr/receiver_unreachable", "Receiver Unreachable", problemcode.CodeReceiverUnreachable, "Failed to communicate with receiver", nil)
		return
	}

	mapped := make([]Timer, 0, len(timers))
	for _, t := range timers {
		if !household.IsServiceAllowedNormalized(profile, t.ServiceRef, "") {
			continue
		}
		mapped = append(mapped, Timer{
			TimerId:     t.TimerID,
			ServiceRef:  t.ServiceRef,
			ServiceName: &t.ServiceName,
			Name:        t.Name,
			Description: &t.Description,
			Begin:       t.Begin,
			End:         t.End,
			State:       TimerState(t.State),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(TimerList{Items: mapped})
}

// AddTimer implements ServerInterface
func (s *Server) AddTimer(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireHouseholdDVRManageAccess(w, r); !ok {
		return
	}

	var req TimerCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "dvr/invalid_input", "Invalid Request Body", problemcode.CodeInvalidInput, "The request body is malformed or empty", nil)
		return
	}

	if req.Begin >= req.End {
		writeRegisteredProblem(w, r, http.StatusUnprocessableEntity, "dvr/invalid_time", "Invalid Timer Time", problemcode.CodeInvalidTime, "Begin time must be before end time", nil)
		return
	}

	s.mu.RLock()
	cfg := s.cfg
	snap := s.snap
	s.mu.RUnlock()

	client := s.owi(cfg, snap)
	ctx := r.Context()

	// 0. Resolve ServiceRef if it's an M3U Channel ID
	realSRef := req.ServiceRef
	if !strings.Contains(realSRef, ":") {
		// Doesn't look like Enigma2 Ref (1:0:1...), try to resolve from Playlist
		playlistName := strings.TrimSpace(snap.Runtime.PlaylistFilename)
		if playlistName != "" {
			playlistPath, err := paths.ValidatePlaylistPath(cfg.DataDir, playlistName)
			if err != nil {
				if !os.IsNotExist(err) {
					writeRegisteredProblem(w, r, http.StatusInternalServerError, "system/invalid_playlist_path", "Invalid Playlist Path", problemcode.CodeInvalidPlaylistPath, err.Error(), nil)
					return
				}
			} else {
				log.L().Info().Str("path", playlistPath).Str("search_id", req.ServiceRef).Msg("attempting to resolve service ref from playlist")

				if data, err := os.ReadFile(filepath.Clean(playlistPath)); err == nil { // #nosec G304
					channels := m3u.Parse(string(data))
					log.L().Info().Int("channels", len(channels)).Msg("parsed playlist for resolution")

					for _, ch := range channels {
						if ch.TvgID == req.ServiceRef {
							// Found it! Extract Ref from URL
							u, err := url.Parse(ch.URL)
							if err == nil {
								// OpenWebIF stream URL analysis
								ref := u.Query().Get("ref")
								if ref != "" {
									if strings.Contains(ref, ":") {
										realSRef = strings.TrimSuffix(ref, ":")
										log.L().Info().Str("id", req.ServiceRef).Str("resolved", realSRef).Msg("resolved channel id to service ref (via query)")
									}
								} else {
									// Check path (OpenWebIF 8001 port style)
									parts := strings.Split(u.Path, "/")
									if len(parts) > 0 {
										candidate := parts[len(parts)-1]
										if strings.Contains(candidate, ":") {
											realSRef = strings.TrimSuffix(candidate, ":")
											log.L().Info().Str("id", req.ServiceRef).Str("resolved", realSRef).Msg("resolved channel id to service ref (via path)")
										}
									}
								}
							} else {
								log.L().Warn().Err(err).Str("url", ch.URL).Msg("failed to parse channel url during resolution")
							}
							break
						}
					}
				} else {
					log.L().Warn().Err(err).Str("path", playlistPath).Msg("failed to read playlist for resolution")
				}
			}
		}
	}

	// Calculate Canonical Times ONCE (with padding)
	realBegin := req.Begin
	if req.PaddingBeforeSec != nil {
		realBegin -= int64(*req.PaddingBeforeSec)
	}
	realEnd := req.End
	if req.PaddingAfterSec != nil {
		realEnd += int64(*req.PaddingAfterSec)
	}

	if _, ok := s.requireHouseholdTimerServiceAccess(w, r, realSRef); !ok {
		return
	}

	// 1. Duplicate/Conflict Check
	existingTimers, err := client.GetTimers(ctx)
	if err != nil {
		log.L().Error().Err(err).Msg("failed to fetch timers for duplicate check")
		writeRegisteredProblem(w, r, http.StatusBadGateway, "dvr/receiver_unreachable", "Receiver Unreachable", problemcode.CodeReceiverUnreachable, "Failed to verify existing timers", nil)
		return
	}

	// Normalize ServiceRef for comparisons
	realSRefNorm := strings.TrimSuffix(realSRef, ":")

	for _, t := range existingTimers {
		if strings.TrimSuffix(t.ServiceRef, ":") == realSRefNorm && t.Begin == realBegin && t.End == realEnd {
			writeRegisteredProblem(w, r, http.StatusConflict, "dvr/duplicate", "Timer Conflict", problemcode.CodeConflict, "A timer with the same parameters already exists", nil)
			return
		}
	}

	desc := ""
	if req.Description != nil {
		desc = *req.Description
	}

	// 2. Add Timer
	err = client.AddTimer(ctx, realSRef, realBegin, realEnd, req.Name, desc)
	if err != nil {
		log.L().Error().Err(err).Str("sref", req.ServiceRef).Msg("failed to add timer")
		status, problemType, title, code := classifyTimerAddError(err)
		writeRegisteredProblem(w, r, status, problemType, title, code, err.Error(), nil)
		return
	}

	// 3. Read-back Verification
	verified := false
	var createdTimer *openwebif.Timer

verifyAddLoop:
	for i := 0; i < 5; i++ {
		checkTimers, err := client.GetTimers(r.Context())
		if err == nil {
			targetNormalized := strings.TrimSuffix(realSRef, ":")
			for _, t := range checkTimers {
				candidateRef := strings.TrimSuffix(t.ServiceRef, ":")
				if candidateRef == targetNormalized &&
					(t.Begin >= realBegin-5 && t.Begin <= realBegin+5) {
					verified = true
					createdTimer = &t
					break verifyAddLoop
				}
			}
		}
		if verified {
			break verifyAddLoop
		}

		retryTimer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-r.Context().Done():
			retryTimer.Stop()
			break verifyAddLoop
		case <-retryTimer.C:
			// continue
		}
	}

	if !verified {
		log.L().Warn().Str("sref", req.ServiceRef).Msg("timer add verified failed")
		writeRegisteredProblem(w, r, http.StatusBadGateway, "dvr/receiver_inconsistent", "Receiver Inconsistent", problemcode.CodeReceiverInconsistent, "Timer added but failed verification", nil)
		return
	}

	dto := read.MapOpenWebIFTimerToDTO(*createdTimer, read.RealClock{}.Now())

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(Timer{
		TimerId:     dto.TimerID,
		ServiceRef:  dto.ServiceRef,
		ServiceName: &dto.ServiceName,
		Name:        dto.Name,
		Description: &dto.Description,
		Begin:       dto.Begin,
		End:         dto.End,
		State:       TimerState(dto.State),
	})
}

// DeleteTimer implements ServerInterface
func (s *Server) DeleteTimer(w http.ResponseWriter, r *http.Request, timerId string) {
	sRef, begin, end, err := read.ParseTimerID(timerId)
	if err != nil {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "dvr/invalid_id", "Invalid Timer ID", problemcode.CodeInvalidID, "The provided timer ID is invalid", nil)
		return
	}
	if _, ok := s.requireHouseholdTimerServiceAccess(w, r, sRef); !ok {
		return
	}

	s.mu.RLock()
	cfg := s.cfg
	snap := s.snap
	s.mu.RUnlock()

	client := s.owi(cfg, snap)
	err = client.DeleteTimer(r.Context(), sRef, begin, end)
	if err != nil {
		log.L().Error().Err(err).Str("timerId", timerId).Msg("failed to delete timer")
		status, problemType, title, code := classifyTimerDeleteError(err)
		writeRegisteredProblem(w, r, status, problemType, title, code, err.Error(), nil)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// UpdateTimer implements ServerInterface (Edit)
func (s *Server) UpdateTimer(w http.ResponseWriter, r *http.Request, timerId string) {
	if _, ok := s.requireHouseholdDVRManageAccess(w, r); !ok {
		return
	}

	var req TimerPatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "dvr/invalid_input", "Invalid Request Body", problemcode.CodeInvalidInput, "The request body is malformed or empty", nil)
		return
	}

	oldSRef, oldBegin, oldEnd, err := read.ParseTimerID(timerId)
	if err != nil {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "dvr/invalid_id", "Invalid Timer ID", problemcode.CodeInvalidID, "The provided timer ID is invalid", nil)
		return
	}
	if _, ok := s.requireHouseholdTimerServiceAccess(w, r, oldSRef); !ok {
		return
	}

	s.mu.RLock()
	cfg := s.cfg
	snap := s.snap
	s.mu.RUnlock()
	client := s.owi(cfg, snap)
	ctx := r.Context()

	// Resolve Old Timer
	timers, err := client.GetTimers(ctx)
	if err != nil {
		log.L().Error().Err(err).Msg("failed to fetch timers during update")
		writeRegisteredProblem(w, r, http.StatusBadGateway, "dvr/receiver_unreachable", "Receiver Unreachable", problemcode.CodeReceiverUnreachable, "Failed to verify existing timer", nil)
		return
	}
	var existing *openwebif.Timer
	for _, t := range timers {
		if strings.TrimSuffix(t.ServiceRef, ":") == strings.TrimSuffix(oldSRef, ":") && t.Begin == oldBegin && t.End == oldEnd {
			existing = &t
			break
		}
	}
	if existing == nil {
		writeRegisteredProblem(w, r, http.StatusNotFound, "dvr/not_found", "Timer Not Found", problemcode.CodeNotFound, "The requested timer does not exist on the receiver", nil)
		return
	}

	// Compute New Values
	newSRef := oldSRef
	newBegin := oldBegin
	newEnd := oldEnd
	newName := existing.Name
	newDesc := existing.Description

	if req.Begin != nil {
		newBegin = *req.Begin
	}
	if req.End != nil {
		newEnd = *req.End
	}
	if req.Name != nil {
		newName = *req.Name
	}
	if req.Description != nil {
		newDesc = *req.Description
	}

	// Hardening: Reject padding in PATCH to prevent timer drift
	if req.PaddingBeforeSec != nil || req.PaddingAfterSec != nil {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "dvr/unsupported_field", "Padding Update Not Supported", problemcode.CodeInvalidInput, "Updating padding via PATCH is not supported to avoid drift. Please update 'begin' and 'end' directly with absolute values.", nil)
		return
	}

	if newBegin >= newEnd {
		writeRegisteredProblem(w, r, http.StatusUnprocessableEntity, "dvr/invalid_time", "Invalid Timer Order", problemcode.CodeInvalidTime, "Begin time must be before end time", nil)
		return
	}

	supportsNative := false
	cap, err := client.DetectTimerChange(ctx)
	if err == nil {
		supportsNative = cap.Supported
	}
	log.L().Info().Str("timerId", timerId).Bool("native", supportsNative).Msg("Updating timer")

	newEnabled := existing.Disabled == 0
	if req.Enabled != nil {
		newEnabled = *req.Enabled
	}

	// Execute Update (Client encapsulates Detection, Flavor Logic, Promotion, and Fail-Closed Fallback)
	err = client.UpdateTimer(ctx, oldSRef, oldBegin, oldEnd, newSRef, newBegin, newEnd, newName, newDesc, newEnabled)
	if err != nil {
		// Strict Error Mapping (no string matching!)
		if errors.Is(err, openwebif.ErrTimerUpdatePartial) {
			// Critical Partial Failure: Add Succeeded, Delete Failed.
			// Return 502 Bad Gateway to indicate upstream inconsistency (Receiver State vs Request).
			// Problem Type: dvr/receiver_inconsistent
			log.L().Error().Str("timerId", timerId).Err(err).Msg("partial failure in timer update")
			writeRegisteredProblem(w, r, http.StatusBadGateway, "dvr/receiver_inconsistent", "Partial failure: Timer added but old timer could not be deleted. Please check for duplicates.", problemcode.CodeReceiverInconsistent, err.Error(), nil)
			return
		}

		log.L().Error().Err(err).Str("timerId", timerId).Msg("update failed")
		status, problemType, title, code := classifyTimerUpdateError(err)
		writeRegisteredProblem(w, r, status, problemType, title, code, err.Error(), nil)
		return
	}

	// Verify Existence of NEW timer
	verified := false
	var updatedTimer *openwebif.Timer
verifyUpdateLoop:
	for i := 0; i < 5; i++ {
		checkTimers, err := client.GetTimers(ctx)
		if err == nil {
			targetNormalized := strings.TrimSuffix(newSRef, ":")
			for _, t := range checkTimers {
				candidateRef := strings.TrimSuffix(t.ServiceRef, ":")
				if candidateRef == targetNormalized && (t.Begin >= newBegin-5 && t.Begin <= newBegin+5) {
					verified = true
					updatedTimer = &t
					break verifyUpdateLoop
				}
			}
		}
		if verified {
			break verifyUpdateLoop
		}

		retryTimer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			retryTimer.Stop()
			break verifyUpdateLoop
		case <-retryTimer.C:
			// continue
		}
	}

	if !verified || updatedTimer == nil {
		log.L().Warn().Str("timerId", timerId).Msg("timer update verification failed")
		writeRegisteredProblem(w, r, http.StatusBadGateway, "dvr/receiver_inconsistent", "Inconsistent Receiver State", problemcode.CodeReceiverInconsistent, "Timer updated but failed verification", nil)
		return
	}

	dto := read.MapOpenWebIFTimerToDTO(*updatedTimer, read.RealClock{}.Now())

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(Timer{
		TimerId:     dto.TimerID,
		ServiceRef:  dto.ServiceRef,
		ServiceName: &dto.ServiceName,
		Name:        dto.Name,
		Description: &dto.Description,
		Begin:       dto.Begin,
		End:         dto.End,
		State:       TimerState(dto.State),
	})
}

// PreviewConflicts implements ServerInterface
func (s *Server) PreviewConflicts(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireHouseholdDVRManageAccess(w, r); !ok {
		return
	}

	var req TimerConflictPreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "dvr/invalid_input", "Invalid Request Body", problemcode.CodeInvalidInput, "The request body is malformed or empty", nil)
		return
	}

	if req.Proposed.Begin >= req.Proposed.End {
		writeRegisteredProblem(w, r, http.StatusUnprocessableEntity, "dvr/validation", "Invalid Timer Order", problemcode.CodeInvalidTime, "Begin time must be before end time", nil)
		return
	}
	if _, ok := s.requireHouseholdTimerServiceAccess(w, r, req.Proposed.ServiceRef); !ok {
		return
	}

	s.mu.RLock()
	cfg := s.cfg
	snap := s.snap
	s.mu.RUnlock()

	client := s.owi(cfg, snap)

	timers, err := client.GetTimers(r.Context())
	if err != nil {
		log.L().Error().Err(err).Msg("failed to fetch timers for conflict preview")
		writeRegisteredProblem(w, r, http.StatusBadGateway, "dvr/receiver_unreachable", "Receiver Unreachable", problemcode.CodeReceiverUnreachable, "Could not fetch existing timers for conflict check", nil)
		return
	}

	conflicts := DetectConflicts(req.Proposed, timers)
	var suggestions *[]struct {
		Kind          *TimerConflictPreviewResponseSuggestionsKind `json:"kind,omitempty"`
		Note          *string                                      `json:"note,omitempty"`
		ProposedBegin *int64                                       `json:"proposedBegin,omitempty"`
		ProposedEnd   *int64                                       `json:"proposedEnd,omitempty"`
	}

	if len(conflicts) > 0 {
		sugs := GenerateSuggestions(req.Proposed, conflicts)
		if len(sugs) > 0 {
			suggestions = &sugs
		}
	}

	resp := TimerConflictPreviewResponse{
		CanSchedule: len(conflicts) == 0,
		Conflicts:   conflicts,
		Suggestions: suggestions,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// GetDvrCapabilities implements ServerInterface
func (s *Server) GetDvrCapabilities(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireHouseholdDVRManageAccess(w, r); !ok {
		return
	}

	s.mu.RLock()
	ds := s.dvrSource
	s.mu.RUnlock()

	caps, err := read.GetDvrCapabilities(r.Context(), ds)
	if err != nil {
		log.L().Error().Err(err).Msg("failed to fetch dvr capabilities")
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "dvr/provider_error", "Failed to Fetch Capabilities", problemcode.CodeProviderError, err.Error(), nil)
		return
	}

	ptr := func(b bool) *bool { return &b }
	str := func(s string) *string { return &s }

	resp := DvrCapabilities{
		Timers: struct {
			Delete         *bool `json:"delete,omitempty"`
			Edit           *bool `json:"edit,omitempty"`
			ReadBackVerify *bool `json:"readBackVerify,omitempty"`
		}{
			Delete:         ptr(caps.CanDelete),
			Edit:           ptr(caps.CanEdit),
			ReadBackVerify: ptr(caps.ReadBackVerify),
		},
		Conflicts: struct {
			Preview       *bool `json:"preview,omitempty"`
			ReceiverAware *bool `json:"receiverAware,omitempty"`
		}{
			Preview:       ptr(caps.ConflictsPreview),
			ReceiverAware: ptr(caps.ReceiverAware),
		},
		Series: struct {
			DelegatedProvider *string                    `json:"delegatedProvider,omitempty"`
			Mode              *DvrCapabilitiesSeriesMode `json:"mode,omitempty"`
			Supported         *bool                      `json:"supported,omitempty"`
		}{
			Supported: ptr(caps.SeriesSupported),
			Mode:      (*DvrCapabilitiesSeriesMode)(str(caps.SeriesMode)),
		},
	}
	if caps.DelegatedProvider != "" {
		resp.Series.DelegatedProvider = &caps.DelegatedProvider
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func classifyTimerAddError(err error) (status int, problemType, title, code string) {
	if openwebif.IsTimerConflict(err) {
		return http.StatusConflict, "dvr/add_failed", "Add Timer Failed", problemcode.CodeAddFailed
	}
	if errors.Is(err, openwebif.ErrForbidden) {
		return http.StatusForbidden, "dvr/forbidden", "Access Forbidden", problemcode.CodeForbidden
	}
	if isReceiverUnavailable(err) {
		return http.StatusBadGateway, "dvr/receiver_unreachable", "Receiver Unreachable", problemcode.CodeReceiverUnreachable
	}
	return http.StatusInternalServerError, "dvr/add_failed", "Add Timer Failed", problemcode.CodeAddFailed
}

func classifyTimerDeleteError(err error) (status int, problemType, title, code string) {
	if openwebif.IsTimerNotFound(err) {
		return http.StatusNotFound, "dvr/not_found", "Timer Not Found", problemcode.CodeNotFound
	}
	if errors.Is(err, openwebif.ErrForbidden) {
		return http.StatusForbidden, "dvr/forbidden", "Access Forbidden", problemcode.CodeForbidden
	}
	if isReceiverUnavailable(err) {
		return http.StatusBadGateway, "dvr/receiver_unreachable", "Receiver Unreachable", problemcode.CodeReceiverUnreachable
	}
	return http.StatusBadGateway, "dvr/delete_failed", "Delete Failed", problemcode.CodeDeleteFailed
}

func classifyTimerUpdateError(err error) (status int, problemType, title, code string) {
	if openwebif.IsTimerConflict(err) {
		return http.StatusConflict, "dvr/update_failed", "Update Failed", problemcode.CodeUpdateFailed
	}
	if errors.Is(err, openwebif.ErrForbidden) {
		return http.StatusForbidden, "dvr/forbidden", "Access Forbidden", problemcode.CodeForbidden
	}
	if openwebif.IsTimerNotFound(err) {
		return http.StatusNotFound, "dvr/not_found", "Timer Not Found", problemcode.CodeNotFound
	}
	if isReceiverUnavailable(err) {
		return http.StatusBadGateway, "dvr/receiver_unreachable", "Receiver Unreachable", problemcode.CodeReceiverUnreachable
	}
	return http.StatusBadGateway, "dvr/update_failed", "Update Failed", problemcode.CodeUpdateFailed
}

func isReceiverUnavailable(err error) bool {
	return errors.Is(err, openwebif.ErrTimeout) ||
		errors.Is(err, openwebif.ErrUpstreamUnavailable) ||
		errors.Is(err, openwebif.ErrUpstreamError) ||
		errors.Is(err, openwebif.ErrUpstreamBadResponse)
}

// GetTimer implements ServerInterface
func (s *Server) GetTimer(w http.ResponseWriter, r *http.Request, timerId string) {
	sRef, begin, end, err := read.ParseTimerID(timerId)
	if err != nil {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "dvr/invalid_id", "Invalid Timer ID", problemcode.CodeInvalidID, "The provided timer ID is invalid", nil)
		return
	}
	if _, ok := s.requireHouseholdTimerServiceAccess(w, r, sRef); !ok {
		return
	}

	s.mu.RLock()
	cfg := s.cfg
	snap := s.snap
	s.mu.RUnlock()
	client := s.owi(cfg, snap)
	ctx := r.Context()

	timers, err := client.GetTimers(ctx)
	if err != nil {
		log.L().Error().Err(err).Msg("failed to fetch timers for individual get")
		writeRegisteredProblem(w, r, http.StatusBadGateway, "dvr/receiver_unreachable", "Receiver Unreachable", problemcode.CodeReceiverUnreachable, "Failed to fetch timers from receiver", nil)
		return
	}

	sRefNorm := strings.TrimSuffix(sRef, ":")

	for _, t := range timers {
		candRef := strings.TrimSuffix(t.ServiceRef, ":")
		if candRef == sRefNorm && t.Begin == begin && t.End == end {
			dto := read.MapOpenWebIFTimerToDTO(t, read.RealClock{}.Now())

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(Timer{
				TimerId:     dto.TimerID,
				ServiceRef:  dto.ServiceRef,
				ServiceName: &dto.ServiceName,
				Name:        dto.Name,
				Description: &dto.Description,
				Begin:       dto.Begin,
				End:         dto.End,
				State:       TimerState(dto.State),
			})
			return
		}
	}
	writeRegisteredProblem(w, r, http.StatusNotFound, "dvr/not_found", "Timer Not Found", problemcode.CodeNotFound, "The requested timer does not exist on the receiver", nil)
}

// GetDvrStatus implements ServerInterface
func (s *Server) GetDvrStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireHouseholdDVRManageAccess(w, r); !ok {
		return
	}

	s.mu.RLock()
	ds := s.dvrSource
	s.mu.RUnlock()

	st, err := read.GetDvrStatus(r.Context(), ds)
	if err != nil {
		log.L().Error().Err(err).Msg("failed to get DVR status")
		writeRegisteredProblem(w, r, http.StatusBadGateway, "system/receiver_error", "Status Failed", problemcode.CodeReceiverError, "Failed to get receiver status", nil)
		return
	}

	resp := RecordingStatus{
		IsRecording: st.IsRecording,
		ServiceName: &st.ServiceName,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
