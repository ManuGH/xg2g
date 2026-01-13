package v3

import (
	"bytes"
	"net/http"

	"github.com/ManuGH/xg2g/internal/control/http/v3/recordings/artifacts"
)

// PR3: HLS Handlers (Pure Adapters)

// GetRecordingHLSPlaylist handles GET /api/v3/recordings/{recordingId}/playlist.m3u8
func (s *Server) GetRecordingHLSPlaylist(w http.ResponseWriter, r *http.Request, recordingId string) {
	// 1. Detect Profile
	profile := detectClientProfile(r)

	// 2. Delegate to Resolver
	artifact, artErr := s.artifacts.ResolvePlaylist(r.Context(), recordingId, string(profile))

	// 3. Map Result
	if artErr != nil {
		s.writeArtifactError(w, r, recordingId, artErr)
		return
	}

	// 4. Serve Artifact
	s.serveArtifact(w, r, artifact)
}

// GetRecordingHLSPlaylistHead handles HEAD /api/v3/recordings/{recordingId}/playlist.m3u8
func (s *Server) GetRecordingHLSPlaylistHead(w http.ResponseWriter, r *http.Request, recordingId string) {
	s.GetRecordingHLSPlaylist(w, r, recordingId)
}

// GetRecordingHLSTimeshift handles GET /api/v3/recordings/{recordingId}/timeshift.m3u8
func (s *Server) GetRecordingHLSTimeshift(w http.ResponseWriter, r *http.Request, recordingId string) {
	// Detect Profile
	profile := detectClientProfile(r)

	artifact, artErr := s.artifacts.ResolveTimeshift(r.Context(), recordingId, string(profile))
	if artErr != nil {
		s.writeArtifactError(w, r, recordingId, artErr)
		return
	}
	s.serveArtifact(w, r, artifact)
}

// GetRecordingHLSTimeshiftHead handles HEAD /api/v3/recordings/{recordingId}/timeshift.m3u8
func (s *Server) GetRecordingHLSTimeshiftHead(w http.ResponseWriter, r *http.Request, recordingId string) {
	s.GetRecordingHLSTimeshift(w, r, recordingId)
}

// GetRecordingHLSCustomSegment handles GET /api/v3/recordings/{recordingId}/{segment}
func (s *Server) GetRecordingHLSCustomSegment(w http.ResponseWriter, r *http.Request, recordingId string, segment string) {
	artifact, artErr := s.artifacts.ResolveSegment(r.Context(), recordingId, segment)
	if artErr != nil {
		s.writeArtifactError(w, r, recordingId, artErr)
		return
	}
	s.serveArtifact(w, r, artifact)
}

// GetRecordingHLSCustomSegmentHead handles HEAD /api/v3/recordings/{recordingId}/{segment}
func (s *Server) GetRecordingHLSCustomSegmentHead(w http.ResponseWriter, r *http.Request, recordingId string, segment string) {
	s.GetRecordingHLSCustomSegment(w, r, recordingId, segment)
}

// Helpers

func (s *Server) writeArtifactError(w http.ResponseWriter, r *http.Request, recordingId string, err *artifacts.ArtifactError) {
	switch err.Code {
	case artifacts.CodePreparing:
		retrySec := 5
		if err.RetryAfter > 0 {
			retrySec = int(err.RetryAfter.Seconds())
			if retrySec < 1 {
				retrySec = 1
			}
		}
		// Pass recordingId as the 'instance' or a hint to help observability.
		// writePreparingResponse signature is likely (w, r, instance/context, status, retry)
		// We pass recordingId as context.
		s.writePreparingResponse(w, r, recordingId, "PREPARING", retrySec)
	case artifacts.CodeNotFound:
		RespondError(w, r, http.StatusNotFound, ErrRecordingNotFound, err.Detail)
	case artifacts.CodeInvalid:
		RespondError(w, r, http.StatusBadRequest, ErrInvalidInput, err.Detail)
	case artifacts.CodeInternal:
		RespondError(w, r, http.StatusInternalServerError, ErrInternalServer, err.Error())
	default:
		RespondError(w, r, http.StatusInternalServerError, ErrInternalServer, "internal error")
	}
}

func (s *Server) serveArtifact(w http.ResponseWriter, r *http.Request, a artifacts.ArtifactOK) {
	// Set Headers
	if a.ContentType != "" {
		w.Header().Set("Content-Type", a.ContentType)
	}
	if a.CacheControl != "" {
		w.Header().Set("Cache-Control", a.CacheControl)
	}

	// Serve Content
	if a.Data != nil {
		// Serve memory content
		readSeeker := bytes.NewReader(a.Data)
		http.ServeContent(w, r, "artifact", a.ModTime, readSeeker)
		return
	}

	if a.AbsPath != "" {
		// Serve local file
		http.ServeFile(w, r, a.AbsPath)
		return
	}

	// Empty? 500
	RespondError(w, r, http.StatusInternalServerError, ErrInternalServer, "empty artifact")
}
