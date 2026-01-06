package v3

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/ManuGH/xg2g/internal/auth"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/pipeline/resume"
	"github.com/go-chi/chi/v5"
)

// ResumeRequest defines the payload for saving a resume point
type ResumeRequest struct {
	Position float64 `json:"position"`
	Total    float64 `json:"total,omitempty"`
	Finished bool    `json:"finished,omitempty"`
}

// HandleRecordingResumeOptions handles OPTIONS /api/v3/recordings/{recordingId}/resume
func (s *Server) HandleRecordingResumeOptions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "PUT, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Request-ID, X-API-Token, Authorization")
	w.Header().Set("Access-Control-Max-Age", "86400")
	w.WriteHeader(http.StatusNoContent)
}

// HandleRecordingResume handles PUT /api/v3/recordings/{recordingId}/resume
func (s *Server) HandleRecordingResume(w http.ResponseWriter, r *http.Request) {
	// Enable CORS for valid request
	w.Header().Set("Access-Control-Allow-Origin", "*")

	recordingID := chi.URLParam(r, "recordingId")
	serviceRef := s.DecodeRecordingID(recordingID)
	if serviceRef == "" {
		http.Error(w, "Invalid recording ID", http.StatusBadRequest)
		return
	}

	principal := auth.PrincipalFromContext(r.Context())
	if principal == nil {
		// Should be caught by auth middleware, but defensive check
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if s.resumeStore == nil {
		http.Error(w, "Resume store not available", http.StatusServiceUnavailable)
		return
	}

	var req ResumeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Basic Validation
	if req.Position < 0 {
		req.Position = 0
	}

	// Fingerprint generation
	// Ideally we check the file stats if local for a strong fingerprint.
	// For MVP, we use the ServiceRef which is the unique ID of the recording in E2.
	fingerprint := "sref:" + serviceRef

	state := &resume.State{
		PosSeconds:      int64(req.Position),
		DurationSeconds: int64(req.Total),
		Finished:        req.Finished,
		UpdatedAt:       time.Now(),
		Fingerprint:     fingerprint,
	}

	// Log for debugging
	logger := log.WithComponentFromContext(r.Context(), "resume")
	logger.Debug().
		Str("principal", principal.ID).
		Str("recording", recordingID).
		Float64("pos", req.Position).
		Msg("saving resume point")

	if err := s.resumeStore.Put(r.Context(), principal.ID, recordingID, state); err != nil {
		log.L().Error().Err(err).Msg("failed to save resume point")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
