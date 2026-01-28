package http

import (
	"encoding/json"
	"net/http"

	"github.com/ManuGH/xg2g/internal/verification"
	"github.com/ManuGH/xg2g/internal/version"
)

// StatusResponse represents the system status contract.
type StatusResponse struct {
	Status  string                   `json:"status"` // healthy, degraded, recovering
	Release string                   `json:"release"`
	Digest  string                   `json:"digest"`
	Runtime RuntimeInfo              `json:"runtime"`
	Drift   *verification.DriftState `json:"drift,omitempty"`
}

type RuntimeInfo struct {
	FFmpeg string `json:"ffmpeg"`
	Go     string `json:"go"`
}

// StatusHandler returns the system status.
type StatusHandler struct {
	store verification.Store
}

func NewStatusHandler(store verification.Store) *StatusHandler {
	return &StatusHandler{
		store: store,
	}
}

func (h *StatusHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	resp := StatusResponse{
		Status:  "healthy", // Logic could be enhanced based on drift detected
		Release: version.Version,
		Digest:  version.Commit,
		Runtime: RuntimeInfo{
			FFmpeg: "7.1.3",
			Go:     "1.25.3",
		},
	}

	if h.store != nil {
		if drift, ok := h.store.Get(r.Context()); ok {
			resp.Drift = &drift
			if drift.Detected {
				resp.Status = "degraded" // Simple logic: drift = degraded
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
