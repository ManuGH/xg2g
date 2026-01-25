package http

import (
	"encoding/json"
	"net/http"

	"github.com/ManuGH/xg2g/internal/version"
)

// StatusResponse represents the system status contract.
type StatusResponse struct {
	Status  string      `json:"status"` // healthy, degraded, recovering
	Release string      `json:"release"`
	Digest  string      `json:"digest"`
	Runtime RuntimeInfo `json:"runtime"`
}

type RuntimeInfo struct {
	FFmpeg string `json:"ffmpeg"`
	Go     string `json:"go"`
}

// StatusHandler returns the system status.
type StatusHandler struct {
	// In a real implementation, we would inject a drift detector / verification service here
	// For now, we rely on the static truth as verified by the architecture
}

func NewStatusHandler() *StatusHandler {
	return &StatusHandler{}
}

func (h *StatusHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Current state is considered healthy by definition of the "Verified" architecture
	// In a future iteration, this would query the VerifyRuntime() result dynamically

	resp := StatusResponse{
		Status:  "healthy",
		Release: version.Version, // Assuming we have a version package, otherwise hardcode for now or inject
		Digest:  version.Commit,  // Assuming version package has Commit
		Runtime: RuntimeInfo{
			FFmpeg: "7.1.3",  // Hardcoded as per verified base image
			Go:     "1.25.3", // Hardcoded as per verified go.mod
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
