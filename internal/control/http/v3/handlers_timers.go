// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/control/read"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/m3u"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/ManuGH/xg2g/internal/platform/paths"
)

// Responsibility: Handles Timer and DVR management including conflicts and scheduling.
// Non-goals: General system status.

// GetTimers implements ServerInterface
func (s *Server) GetTimers(w http.ResponseWriter, r *http.Request, params GetTimersParams) {
	s.mu.RLock()
	src := s.timersSource
	s.mu.RUnlock()

	if s.timersSource == nil {
		writeProblem(w, r, http.StatusServiceUnavailable, "dvr/unavailable", "DVR Unavailable", "UNAVAILABLE", "Timers source not initialized", nil)
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
		writeProblem(w, r, http.StatusBadGateway, "dvr/receiver_unreachable", "Receiver Unreachable", "RECEIVER_UNREACHABLE", "Failed to communicate with receiver", nil)
		return
	}

	mapped := make([]Timer, 0, len(timers))
	for _, t := range timers {
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
	var req TimerCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "dvr/invalid_input", "Invalid Request Body", "INVALID_INPUT", "The request body is malformed or empty", nil)
		return
	}

	if req.Begin >= req.End {
		writeProblem(w, r, http.StatusUnprocessableEntity, "dvr/invalid_time", "Invalid Timer Time", "INVALID_TIME", "Begin time must be before end time", nil)
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
					writeProblem(w, r, http.StatusInternalServerError, "system/invalid_playlist_path", "Invalid Playlist Path", "INVALID_PLAYLIST_PATH", err.Error(), nil)
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

	// 1. Duplicate/Conflict Check
	existingTimers, err := client.GetTimers(ctx)
	if err != nil {
		log.L().Error().Err(err).Msg("failed to fetch timers for duplicate check")
		writeProblem(w, r, http.StatusBadGateway, "dvr/receiver_unreachable", "Receiver Unreachable", "RECEIVER_UNREACHABLE", "Failed to verify existing timers", nil)
		return
	}

	// Normalize ServiceRef for comparisons
	realSRefNorm := strings.TrimSuffix(realSRef, ":")

	for _, t := range existingTimers {
		if strings.TrimSuffix(t.ServiceRef, ":") == realSRefNorm && t.Begin == realBegin && t.End == realEnd {
			writeProblem(w, r, http.StatusConflict, "dvr/duplicate", "Timer Conflict", "CONFLICT", "A timer with the same parameters already exists", nil)
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

		status := http.StatusInternalServerError
		errStr := err.Error()
		if strings.Contains(errStr, "Konflikt") || strings.Contains(errStr, "Conflict") || strings.Contains(errStr, "overlap") {
			status = http.StatusConflict
		}

		writeProblem(w, r, status, "dvr/add_failed", "Add Timer Failed", "ADD_FAILED", err.Error(), nil)
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
		writeProblem(w, r, http.StatusBadGateway, "dvr/receiver_inconsistent", "Receiver Inconsistent", "RECEIVER_INCONSISTENT", "Timer added but failed verification", nil)
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
		writeProblem(w, r, http.StatusBadRequest, "dvr/invalid_id", "Invalid Timer ID", "INVALID_ID", "The provided timer ID is invalid", nil)
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

		status := http.StatusBadGateway
		errCode := "dvr/delete_failed"
		title := "Delete Failed"
		errStr := err.Error()

		if strings.Contains(errStr, "not found") || strings.Contains(errStr, "nicht gefunden") || strings.Contains(errStr, "404") {
			status = http.StatusNotFound
			errCode = "dvr/not_found"
			title = "Timer Not Found"
		} else if strings.Contains(errStr, "connection") || strings.Contains(errStr, "refused") || strings.Contains(errStr, "timeout") {
			status = http.StatusBadGateway
			errCode = "dvr/receiver_unreachable"
			title = "Receiver Unreachable"
		}

		writeProblem(w, r, status, errCode, title, "DELETE_FAILED", errStr, nil)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// UpdateTimer implements ServerInterface (Edit)
func (s *Server) UpdateTimer(w http.ResponseWriter, r *http.Request, timerId string) {
	var req TimerPatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "dvr/invalid_input", "Invalid Request Body", "INVALID_INPUT", "The request body is malformed or empty", nil)
		return
	}

	oldSRef, oldBegin, oldEnd, err := read.ParseTimerID(timerId)
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "dvr/invalid_id", "Invalid Timer ID", "INVALID_ID", "The provided timer ID is invalid", nil)
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
		writeProblem(w, r, http.StatusBadGateway, "dvr/receiver_unreachable", "Receiver Unreachable", "RECEIVER_UNREACHABLE", "Failed to verify existing timer", nil)
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
		writeProblem(w, r, http.StatusNotFound, "dvr/not_found", "Timer Not Found", "NOT_FOUND", "The requested timer does not exist on the receiver", nil)
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
		writeProblem(w, r, http.StatusBadRequest, "dvr/unsupported_field", "Padding Update Not Supported", "INVALID_INPUT", "Updating padding via PATCH is not supported to avoid drift. Please update 'begin' and 'end' directly with absolute values.", nil)
		return
	}

	if newBegin >= newEnd {
		writeProblem(w, r, http.StatusUnprocessableEntity, "dvr/invalid_time", "Invalid Timer Order", "INVALID_TIME", "Begin time must be before end time", nil)
		return
	}

	supportsNative := client.HasTimerChange(ctx)
	log.L().Info().Str("timerId", timerId).Bool("native", supportsNative).Msg("Updating timer")

	if supportsNative {
		err = client.UpdateTimer(ctx, oldSRef, oldBegin, oldEnd, newSRef, newBegin, newEnd, newName, newDesc, true)
		if err != nil {
			log.L().Error().Err(err).Str("timerId", timerId).Msg("native update failed")
			status := http.StatusBadGateway
			errStr := err.Error()
			if strings.Contains(err.Error(), "Konflikt") || strings.Contains(err.Error(), "Conflict") || strings.Contains(err.Error(), "overlap") {
				status = http.StatusConflict
			}
			writeProblem(w, r, status, "dvr/update_failed", "Update Failed", "UPDATE_FAILED", errStr, nil)
			return
		}
	} else {
		// Fallback: Delete + Add
		log.L().Info().Str("timerId", timerId).Msg("performing delete+add fallback for update")
		err = client.DeleteTimer(ctx, oldSRef, oldBegin, oldEnd)
		if err != nil {
			log.L().Error().Err(err).Str("timerId", timerId).Msg("delete failed")
			writeProblem(w, r, http.StatusInternalServerError, "dvr/receiver_error", "Delete Failed", "DELETE_FAILED", "Failed to delete timer from receiver during update", nil)
			return
		}

		err = client.AddTimer(ctx, newSRef, newBegin, newEnd, newName, newDesc)
		if err != nil {
			log.L().Error().Err(err).Str("timerId", timerId).Msg("fallback add failed, attempting rollback")
			// Rollback
			_ = client.AddTimer(ctx, oldSRef, oldBegin, oldEnd, existing.Name, existing.Description)

			status := http.StatusBadGateway
			if strings.Contains(err.Error(), "Konflikt") || strings.Contains(err.Error(), "Conflict") {
				status = http.StatusConflict
			}
			writeProblem(w, r, status, "dvr/receiver_inconsistent", "Update Failed (Add Step)", "RECEIVER_INCONSISTENT", err.Error(), nil)
			return
		}
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
		writeProblem(w, r, http.StatusBadGateway, "dvr/receiver_inconsistent", "Inconsistent Receiver State", "RECEIVER_INCONSISTENT", "Timer updated but failed verification", nil)
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
	var req TimerConflictPreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "dvr/invalid_input", "Invalid Request Body", "INVALID_INPUT", "The request body is malformed or empty", nil)
		return
	}

	if req.Proposed.Begin >= req.Proposed.End {
		writeProblem(w, r, http.StatusUnprocessableEntity, "dvr/validation", "Invalid Timer Order", "INVALID_TIME", "Begin time must be before end time", nil)
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
		writeProblem(w, r, http.StatusBadGateway, "dvr/receiver_unreachable", "Receiver Unreachable", "RECEIVER_UNREACHABLE", "Could not fetch existing timers for conflict check", nil)
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
	s.mu.RLock()
	ds := s.dvrSource
	s.mu.RUnlock()

	caps, err := read.GetDvrCapabilities(r.Context(), ds)
	if err != nil {
		log.L().Error().Err(err).Msg("failed to fetch dvr capabilities")
		writeProblem(w, r, http.StatusInternalServerError, "dvr/provider_error", "Failed to Fetch Capabilities", "PROVIDER_ERROR", err.Error(), nil)
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

// GetTimer implements ServerInterface
func (s *Server) GetTimer(w http.ResponseWriter, r *http.Request, timerId string) {
	sRef, begin, end, err := read.ParseTimerID(timerId)
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "dvr/invalid_id", "Invalid Timer ID", "INVALID_ID", "The provided timer ID is invalid", nil)
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
		writeProblem(w, r, http.StatusBadGateway, "dvr/receiver_unreachable", "Receiver Unreachable", "RECEIVER_UNREACHABLE", "Failed to fetch timers from receiver", nil)
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
	writeProblem(w, r, http.StatusNotFound, "dvr/not_found", "Timer Not Found", "NOT_FOUND", "The requested timer does not exist on the receiver", nil)
}

// GetDvrStatus implements ServerInterface
func (s *Server) GetDvrStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	ds := s.dvrSource
	s.mu.RUnlock()

	st, err := read.GetDvrStatus(r.Context(), ds)
	if err != nil {
		log.L().Error().Err(err).Msg("failed to get DVR status")
		writeProblem(w, r, http.StatusBadGateway, "system/receiver_error", "Status Failed", "RECEIVER_ERROR", "Failed to get receiver status", nil)
		return
	}

	resp := RecordingStatus{
		IsRecording: st.IsRecording,
		ServiceName: &st.ServiceName,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
