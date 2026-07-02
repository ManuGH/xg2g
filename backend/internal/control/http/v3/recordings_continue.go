package v3

import (
	"net/http"
	"strconv"
	"time"

	"github.com/ManuGH/xg2g/internal/control/auth"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/problemcode"
)

const (
	continueWatchingDefaultLimit = 12
	continueWatchingMaxLimit     = 50
)

// ContinueWatchingItem is one resumable recording for the dashboard rail.
// Title/Channel are display snapshots persisted at resume-save time; the
// canonical metadata still lives in the recordings listing.
type ContinueWatchingItem struct {
	RecordingID     string     `json:"recordingId"`
	Title           string     `json:"title,omitempty"`
	Channel         string     `json:"channel,omitempty"`
	PosSeconds      int64      `json:"posSeconds"`
	DurationSeconds int64      `json:"durationSeconds,omitempty"`
	UpdatedAt       *time.Time `json:"updatedAt,omitempty"`
}

// ContinueWatchingResponse is the payload for GET /recordings/continue.
type ContinueWatchingResponse struct {
	Items []ContinueWatchingItem `json:"items"`
}

// HandleRecordingsContinue handles GET /api/v3/recordings/continue.
// It returns the principal's most recently updated, unfinished resume
// entries so the UI can render a continue-watching rail without walking
// the recordings directory tree.
func (s *Server) HandleRecordingsContinue(w http.ResponseWriter, r *http.Request) {
	s.setCORSIfAllowed(w, r.Header.Get("Origin"))

	principal := auth.PrincipalFromContext(r.Context())
	if principal == nil {
		writeRegisteredProblem(w, r, http.StatusUnauthorized, "auth/unauthorized", "Unauthorized", problemcode.CodeUnauthorized, "Authentication required", nil)
		return
	}

	resumeStore := s.recordingsModuleDeps().resumeStore
	if resumeStore == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "system/unavailable", "Subsystem Unavailable", problemcode.CodeUnavailable, "Resume store not available", nil)
		return
	}

	limit := continueWatchingDefaultLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writeRegisteredProblem(w, r, http.StatusBadRequest, "system/invalid_input", "Invalid Request", problemcode.CodeInvalidInput, "limit must be a positive integer", nil)
			return
		}
		limit = min(parsed, continueWatchingMaxLimit)
	}

	entries, err := resumeStore.ListRecent(r.Context(), principal.ID, limit)
	if err != nil {
		logger := log.WithComponentFromContext(r.Context(), "resume")
		logger.Error().Err(err).Msg("failed to list continue-watching entries")
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "system/internal", "Internal Error", problemcode.CodeInternalError, "Failed to list resume entries", nil)
		return
	}

	items := make([]ContinueWatchingItem, 0, len(entries))
	for _, e := range entries {
		updatedAt := e.State.UpdatedAt
		items = append(items, ContinueWatchingItem{
			// The canonical resume key IS the public recording ID.
			RecordingID:     e.RecordingKey,
			Title:           e.State.Title,
			Channel:         e.State.Channel,
			PosSeconds:      e.State.PosSeconds,
			DurationSeconds: e.State.DurationSeconds,
			UpdatedAt:       &updatedAt,
		})
	}

	writeJSON(w, http.StatusOK, ContinueWatchingResponse{Items: items})
}
