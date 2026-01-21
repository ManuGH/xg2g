// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"

	"github.com/ManuGH/xg2g/internal/control/auth"
	xg2ghttp "github.com/ManuGH/xg2g/internal/control/http"
	v3recordings "github.com/ManuGH/xg2g/internal/control/http/v3/recordings"
	recservice "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/log"
)

// Types are now generated in server_gen.go

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

		// P3-3-4: Status Mapping
		if m.Status != "" && m.Status != "unknown" {
			apiStatus := RecordingItemStatus(m.Status)
			item.Status = &apiStatus
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
		RequestId:   requestID(r.Context()),
		Recordings:  &recordingsList,
		Directories: &directoriesList,
		Roots:       &rootNodes,
		Breadcrumbs: &[]Breadcrumb{},
	}

	ensureTraceHeader(w, r.Context())

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

	resp := RecordingBuildStatus{
		RequestId: requestID(r.Context()),
		State:     RecordingBuildStatusStateIDLE,
	}
	ensureTraceHeader(w, r.Context())

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
	s.serveRecordingDirect(w, r, recordingId, false)
}

func (s *Server) ProbeRecordingMp4(w http.ResponseWriter, r *http.Request, recordingId string) {
	s.serveRecordingDirect(w, r, recordingId, true)
}

func (s *Server) serveRecordingDirect(w http.ResponseWriter, r *http.Request, recordingId string, isHead bool) {
	if s.recordingsService == nil {
		writeProblem(w, r, http.StatusInternalServerError, "system/internal", "Internal Error", "INTERNAL_ERROR", "Recordings service not available", nil)
		return
	}

	// 1. Check readiness and get artifact path
	res, err := s.recordingsService.Stream(r.Context(), recservice.StreamInput{
		RecordingID: recordingId,
	})
	if err != nil {
		s.writeRecordingError(w, r, err)
		return
	}

	if !res.Ready {
		s.writePreparingResponse(w, r, recordingId, res.State, res.RetryAfter)
		return
	}

	// 2. Open file and get status
	f, err := os.Open(res.LocalPath)
	if err != nil {
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
	size := info.Size()

	// 3. Set Base Headers
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Last-Modified", info.ModTime().UTC().Format(http.TimeFormat))

	// 4. Case A: No Range
	rangeHeader := r.Header.Get("Range")
	if rangeHeader == "" {
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
		w.WriteHeader(http.StatusOK)
		if !isHead {
			_, _ = io.Copy(w, f)
		}
		return
	}

	// 5. Case B: Range Header Present (Parse via SSOT)
	rng, err := xg2ghttp.ParseRange(rangeHeader, size)
	if err != nil {
		// Policy A: Invalid or Multi-range -> 416
		xg2ghttp.Write416(w, size)
		return
	}

	// 6. Respond with 206 Partial Content
	contentLength := rng.End - rng.Start + 1
	w.Header().Set("Content-Range", xg2ghttp.FormatContentRange(rng, size))
	w.Header().Set("Content-Length", strconv.FormatInt(contentLength, 10))
	w.WriteHeader(http.StatusPartialContent)

	if !isHead {
		if _, err := f.Seek(rng.Start, io.SeekStart); err != nil {
			log.L().Error().Err(err).Int64("start", rng.Start).Msg("failed to seek in artifact")
			// Already sent 206, but haven't written body.
			// Too late for WriteHeader, but we can't do much here.
			return
		}
		_, _ = io.CopyN(w, f, contentLength)
	}
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

func (s *Server) writePreparingResponse(w http.ResponseWriter, r *http.Request, recordingId, state string, retryAfter int) {
	xg2ghttp.WritePreparingHLS(w, r, recordingId, state, retryAfter)
}
