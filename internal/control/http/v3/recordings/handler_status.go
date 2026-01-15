package recordings

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/ManuGH/xg2g/internal/control/vod"
)

// StatusDeps defines the dependencies required by the Status handler.
type StatusDeps struct {
	// Read-only config accessors
	HLSRoot func() string

	VODManager interface {
		Get(ctx context.Context, cacheDir string) (*vod.JobStatus, bool)
		GetMetadata(serviceRef string) (vod.Metadata, bool)
	}

	// Cache dir function
	RecordingCacheDir func(hlsRoot, serviceRef string) (string, error)

	// Error writer
	WriteError func(w http.ResponseWriter, r *http.Request, serviceRef string, err error)
}

// recordingBuildStatus represents the JSON response for status.
type recordingBuildStatus struct {
	State string  `json:"state"`
	Error *string `json:"error,omitempty"`
}

// WriteStatus handles GET /recordings/{id}/status.
// Mandate: Handlers are thin adapters and treat IDs as opaque.
func WriteStatus(w http.ResponseWriter, r *http.Request, recordingID string, deps StatusDeps) {
	serviceRef := recordingID // Treated as opaque

	hlsRoot := deps.HLSRoot()
	cacheDir, err := RecordingCacheDir(hlsRoot, serviceRef)
	if err != nil {
		deps.WriteError(w, r, serviceRef, err)
		return
	}

	job, jobOk := deps.VODManager.Get(r.Context(), cacheDir)
	meta, metaOk := deps.VODManager.GetMetadata(serviceRef)

	resp := mapStatus(job, jobOk, &meta, metaOk)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store") // Status is volatile
	_ = json.NewEncoder(w).Encode(resp)
}

func mapStatus(job *vod.JobStatus, jobOk bool, meta *vod.Metadata, metaOk bool) recordingBuildStatus {
	// 1. If actively building, use that state
	if jobOk {
		switch job.State {
		case vod.JobStateBuilding, vod.JobStateFinalizing:
			return recordingBuildStatus{State: "RUNNING"}
		case vod.JobStateFailed:
			msg := job.Reason
			return recordingBuildStatus{State: "FAILED", Error: &msg}
		case vod.JobStateSucceeded:
			return recordingBuildStatus{State: "READY"}
		}
	}

	// 2. If nothing active, check persisted metadata
	if metaOk {
		switch meta.State {
		case vod.ArtifactStateReady:
			return recordingBuildStatus{State: "READY"}
		case vod.ArtifactStateFailed:
			return recordingBuildStatus{State: "FAILED", Error: &meta.Error}
		case vod.ArtifactStatePreparing:
			// "Preparing" means we are reconciling or starting a build. Treat as RUNNING.
			return recordingBuildStatus{State: "RUNNING"}
		case vod.ArtifactStateUnknown:
			// Fallthrough to IDLE or handle explicitly?
			// Use IDLE as default for unknown.
		}
	}

	// 3. Fallback: IDLE (Offer to start)
	return recordingBuildStatus{State: "IDLE"}
}
