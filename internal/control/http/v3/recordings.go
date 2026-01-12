// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/control/auth"
	v3recordings "github.com/ManuGH/xg2g/internal/control/http/v3/recordings"
	"github.com/ManuGH/xg2g/internal/control/http/v3/types"
	"github.com/ManuGH/xg2g/internal/control/playback"
	recservice "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/log"
)

// Types are now generated in server_gen.go

var (
	errRecordingInvalid = v3recordings.ErrInvalidRecordingRef
)

// GetRecordings handles GET /api/v3/recordings
// Query: ?root=<id>&path=<rel_path>
func (s *Server) GetRecordings(w http.ResponseWriter, r *http.Request, params GetRecordingsParams) {
	if s.recordingsService == nil {
		writeProblem(w, r, http.StatusInternalServerError, "system/internal", "Internal Error", "INTERNAL_ERROR", "Recordings service not available", nil)
		return
	}

	// 1. Parse Query
	var qRootID, qPath string
	if params.Root != nil {
		qRootID = *params.Root
	}
	if params.Path != nil {
		qPath = *params.Path
	}

	// 2. Call Service
	input := recservice.ListInput{
		RootID:      qRootID,
		Path:        qPath,
		PrincipalID: "",
	}
	// Enrich with Principal ID if available (for resume)
	if p := auth.PrincipalFromContext(r.Context()); p != nil {
		input.PrincipalID = p.ID
	}

	listing, err := s.recordingsService.List(r.Context(), input)
	if err != nil {
		s.writeRecordingError(w, r, err)
		return
	}

	// 3. Map to DTO
	recordingsList := make([]RecordingItem, 0, len(listing.Recordings))
	for _, m := range listing.Recordings {
		item := RecordingItem{
			ServiceRef:       strPtr(m.ServiceRef),
			RecordingId:      strPtr(m.RecordingID),
			Title:            strPtr(m.Title),
			Description:      strPtr(m.Description),
			BeginUnixSeconds: int64Ptr(m.BeginUnixSeconds),
			Length:           strPtr(m.Length),
			Filename:         strPtr(m.Filename),
		}
		if m.DurationSeconds != nil {
			item.DurationSeconds = m.DurationSeconds
		}
		if m.Resume != nil {
			item.Resume = &ResumeSummary{
				PosSeconds:      int64Ptr(m.Resume.PosSeconds),
				DurationSeconds: int64Ptr(m.Resume.DurationSeconds), // DTO expects *int64
				Finished:        boolPtr(m.Resume.Finished),
				UpdatedAt:       m.Resume.UpdatedAt, // Domain has *time.Time
			}
		}
		recordingsList = append(recordingsList, item)
	}

	directoriesList := make([]DirectoryItem, 0, len(listing.Directories))
	for _, d := range listing.Directories {
		directoriesList = append(directoriesList, DirectoryItem{
			Name: strPtr(d.Name),
			Path: strPtr(d.Path),
		})
	}

	rootNodes := make([]RecordingRoot, 0, len(listing.Roots))
	for _, rt := range listing.Roots {
		rootNodes = append(rootNodes, RecordingRoot{
			Id:   strPtr(rt.ID),
			Name: strPtr(rt.Name),
		})
	}

	resp := RecordingResponse{
		Recordings:  &recordingsList,
		Directories: &directoriesList,
		Roots:       &rootNodes,
		Breadcrumbs: &[]Breadcrumb{},
	}

	crumbs := make([]Breadcrumb, 0, len(listing.Breadcrumbs))
	for _, c := range listing.Breadcrumbs {
		crumbs = append(crumbs, Breadcrumb{
			Name: strPtr(c.Name),
			Path: strPtr(c.Path),
		})
	}
	resp.Breadcrumbs = &crumbs
	resp.CurrentRoot = strPtr(listing.CurrentRoot)
	resp.CurrentPath = strPtr(listing.CurrentPath)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.L().Error().Err(err).Msg("failed to encode recordings response")
	}
}

// GetRecordingsRecordingIdStatus handles GET /api/v3/recordings/{recordingId}/status.
// GetRecordingsRecordingIdStatus handles GET /status
func (s *Server) GetRecordingsRecordingIdStatus(w http.ResponseWriter, r *http.Request, recordingId string) {
	if s.recordingsService == nil {
		writeProblem(w, r, http.StatusInternalServerError, "system/internal", "Internal Error", "INTERNAL_ERROR", "Recordings service not available", nil)
		return
	}

	status, err := s.recordingsService.GetStatus(r.Context(), recservice.StatusInput{
		RecordingID: recordingId,
	})
	if err != nil {
		s.writeRecordingError(w, r, err)
		return
	}

	resp := RecordingBuildStatus{State: RecordingBuildStatusStateIDLE}
	switch status.State {
	case "RUNNING":
		resp.State = RecordingBuildStatusStateRUNNING
	case "READY":
		resp.State = RecordingBuildStatusStateREADY
	case "FAILED":
		resp.State = RecordingBuildStatusStateFAILED
		resp.Error = status.Error
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.L().Error().Err(err).Msg("failed to encode recordings status response")
	}
}

// GetRecordingPlaybackInfo determines the best playback strategy
func (s *Server) GetRecordingPlaybackInfo(w http.ResponseWriter, r *http.Request, recordingId string) {
	if s.recordingsService == nil {
		writeProblem(w, r, http.StatusInternalServerError, "system/internal", "Internal Error", "INTERNAL_ERROR", "Recordings service not available", nil)
		return
	}

	start := time.Now()
	// 1. Adapter: Inputs
	intent := recservice.IntentMetadata
	if r.URL.Query().Get("intent") == "stream" {
		intent = recservice.IntentStream
	}

	clientProfile := s.mapProfile(r)
	var profile recservice.PlaybackProfile = recservice.ProfileGeneric
	if strings.Contains(clientProfile.Name, "Safari") && !strings.Contains(clientProfile.Name, "Chrome") {
		profile = recservice.ProfileSafari
	}

	// 2. Adapter: Call Service
	info, err := s.recordingsService.GetPlaybackInfo(r.Context(), recservice.PlaybackInfoInput{
		RecordingID: recordingId,
		Intent:      string(intent),
		Profile:     profile,
	})
	if err != nil {
		s.writeRecordingError(w, r, err)
		return
	}

	// 3. Adapter: Success Mapping
	var mode string
	var url string

	// Use playback.Mode constants implicitly via decision
	switch info.Decision.Mode {
	case playback.ModeDirectPlay, playback.ModeDirectStream:
		mode = "direct_mp4"
		url = fmt.Sprintf("/api/v3/recordings/%s/stream.mp4", recordingId) // Using recordingID in URL
	case playback.ModeTranscode:
		mode = "hls"
		url = fmt.Sprintf("/api/v3/recordings/%s/index.m3u8", recordingId)
	default:
		s.writeRecordingError(w, r, errors.New("unknown playback mode"))
		return
	}

	resp := types.VODPlaybackResponse{
		Mode:            mode,
		URL:             url,
		DurationSeconds: int64(info.MediaInfo.Duration),
		Reason:          info.Reason,
	}

	_ = start // Suppress unused

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.L().Error().Err(err).Msg("failed to encode playback info")
	}
}

// DeleteRecording handles DELETE /api/v3/recordings/{recordingId}
func (s *Server) DeleteRecording(w http.ResponseWriter, r *http.Request, recordingId string) {
	if s.recordingsService == nil {
		writeProblem(w, r, http.StatusInternalServerError, "system/internal", "Internal Error", "INTERNAL_ERROR", "Recordings service not available", nil)
		return
	}

	_, err := s.recordingsService.Delete(r.Context(), recservice.DeleteInput{
		RecordingID: recordingId,
	})
	if err != nil {
		s.writeRecordingError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) StreamRecordingDirect(w http.ResponseWriter, r *http.Request, recordingId string) {
	if s.recordingsService == nil {
		writeProblem(w, r, http.StatusInternalServerError, "system/internal", "Internal Error", "INTERNAL_ERROR", "Recordings service not available", nil)
		return
	}

	// Delegate to Service.Stream (Thin Adapter)
	// Service handles orchestration, probing, and file readiness checks.
	res, err := s.recordingsService.Stream(r.Context(), recservice.StreamInput{
		RecordingID: recordingId,
	})
	if err != nil {
		s.writeRecordingError(w, r, err)
		return
	}

	if !res.Ready {
		// Not Ready: Return 503 with Retry-After (RFC 7807)
		w.Header().Set("Retry-After", fmt.Sprintf("%d", res.RetryAfter))
		writeProblem(w, r, http.StatusServiceUnavailable, "recordings/preparing", "Preparing", "PREPARING", "Recording is being prepared for playback", map[string]interface{}{
			"recording_id": recordingId,
			"state":        res.State,
		})
		return
	}

	// Ready: Serve Content
	// We use http.ServeContent which handles Range requests efficiently.
	// Since we are the "adapter", we are allowed to open the file for writing to the response,
	// provided the service has guaranteed its readiness and path.
	f, err := os.Open(res.LocalPath)
	if err != nil {
		// Race condition or file deletion? Service said ready.
		// Fallback to error
		log.L().Error().Err(err).Str("path", res.LocalPath).Msg("failed to open ready artifact")
		s.writeRecordingError(w, r, err)
		return
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		s.writeRecordingError(w, r, err)
		return
	}

	http.ServeContent(w, r, "stream.mp4", info.ModTime(), f)
}

func (s *Server) writeRecordingError(w http.ResponseWriter, r *http.Request, err error) {
	// Map domain errors to HTTP problems using Classification
	class := recservice.Classify(err)
	msg := err.Error()

	switch class {
	case recservice.ClassInvalidArgument:
		writeProblem(w, r, http.StatusBadRequest, "recordings/invalid", "Invalid Request", "INVALID_INPUT", msg, nil)
	case recservice.ClassNotFound:
		writeProblem(w, r, http.StatusNotFound, "recordings/not-found", "Not Found", "NOT_FOUND", msg, nil)
	case recservice.ClassForbidden:
		writeProblem(w, r, http.StatusForbidden, "recordings/forbidden", "Access Denied", "FORBIDDEN", msg, nil)
	case recservice.ClassPreparing:
		w.Header().Set("Retry-After", "5")
		writeProblem(w, r, http.StatusServiceUnavailable, "recordings/preparing", "Preparing", "PREPARING", msg, nil)
	case recservice.ClassUnsupported:
		writeProblem(w, r, http.StatusUnprocessableEntity, "recordings/remote-probe-unsupported", "Remote Probe Unsupported", "REMOTE_PROBE_UNSUPPORTED", msg, nil)
	case recservice.ClassUpstream:
		// 502 Bad Gateway is appropriate for upstream/backend errors
		writeProblem(w, r, http.StatusBadGateway, "recordings/upstream", "Upstream Error", "UPSTREAM_ERROR", msg, nil)
	default:
		log.L().Error().Err(err).Msg("recordings service error")
		writeProblem(w, r, http.StatusInternalServerError, "recordings/internal", "Internal Error", "INTERNAL_ERROR", "An unexpected error occurred", nil)
	}
}

// Helpers
func strPtr(s string) *string { return &s }
func int64Ptr(i int64) *int64 { return &i }
func boolPtr(b bool) *bool    { return &b }

// IsAllowedVideoSegment validates segment filenames (reused in other places)
func IsAllowedVideoSegment(name string) bool {
	return v3recordings.IsAllowedVideoSegment(name)
}

// mapProfile scans User-Agent for Safari/Chrome to assist playback decisions
func (s *Server) mapProfile(r *http.Request) types.ClientProfile {
	ua := r.Header.Get("User-Agent")
	p := types.ClientProfile{
		Name:        "Unknown",
		VideoCodecs: []string{"h264"},
		AudioCodecs: []string{"aac", "mp3"},
		Containers:  []string{"mp4", "ts"},
		SupportsHLS: false,
	}

	if strings.Contains(ua, "Safari") && !strings.Contains(ua, "Chrome") {
		p.Name = "Safari"
		p.VideoCodecs = append(p.VideoCodecs, "hevc")
		p.SupportsHLS = true
	} else if strings.Contains(ua, "VLC") {
		p.Name = "VLC"
		p.VideoCodecs = append(p.VideoCodecs, "hevc", "mpeg2")
		p.SupportsHLS = true
	} else if strings.Contains(ua, "Chrome") {
		p.Name = "Chrome"
		p.SupportsHLS = true
	}

	return p
}

func (s *Server) writePreparingResponse(w http.ResponseWriter, r *http.Request, recordingId, state string, retryAfter int) {
	w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
	writeProblem(w, r, http.StatusServiceUnavailable, "recordings/preparing", "Preparing", "PREPARING", "Recording is being prepared for playback", map[string]interface{}{
		"recording_id": recordingId,
		"state":        state,
	})
}
