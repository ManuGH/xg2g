package vod

import (
	"errors"

	vodtypes "github.com/ManuGH/xg2g/internal/domain/vod"
)

// StreamInfo is re-exported from domain for backward compatibility.
// New code should import github.com/ManuGH/xg2g/internal/domain/vod directly.
type StreamInfo = vodtypes.StreamInfo
type VideoStreamInfo = vodtypes.VideoStreamInfo
type AudioStreamInfo = vodtypes.AudioStreamInfo

// JobStatus is a stable DTO for API consumption.
// It abstracts internal state.
type JobStatus struct {
	State     JobState // Enum: Idle, Building, Finalizing, Succeeded, Failed
	Reason    string
	UpdatedAt int64 // Unix timestamp
}

type JobState string

const (
	JobStateIdle       JobState = "IDLE"
	JobStateBuilding   JobState = "BUILDING"
	JobStateFinalizing JobState = "FINALIZING"
	JobStateSucceeded  JobState = "SUCCEEDED"
	JobStateFailed     JobState = "FAILED"
)

// ArtifactState represents the readiness of recording metadata for playback.
// Availability of specific artifacts is determined by PlaylistPath/ArtifactPath.
type ArtifactState string

const (
	ArtifactStateUnknown   ArtifactState = "UNKNOWN"   // No probe attempted yet
	ArtifactStatePreparing ArtifactState = "PREPARING" // Probing or building in progress
	ArtifactStateReady     ArtifactState = "READY"     // Artifact ready for direct serving
	ArtifactStateFailed    ArtifactState = "FAILED"    // Terminal error during probe/build
	ArtifactStateMissing   ArtifactState = "MISSING"   // Source file truly absent
)

// Metadata represents the enriched properties of a recording.
type Metadata struct {
	State        ArtifactState
	ResolvedPath string
	ArtifactPath string // Authoritative path for the MP4 artifact
	PlaylistPath string // Authoritative path for the HLS playlist
	Duration     int64  // Duration in seconds (authoritative)
	// Codec/Container Cache (Deliverable #4)
	Container  string
	VideoCodec string
	AudioCodec string
	Width      int
	Height     int
	FPS        float64
	Interlaced bool

	Fingerprint Fingerprint
	Error       string
	UpdatedAt   int64
	StateGen    uint64 // Monotonic generation counter for race-safe updates
	FailureKind FailureKind
	FailedAt    int64 // Unix timestamp of last failure
}

type FailureKind string

const (
	FailureNone      FailureKind = ""
	FailureNotFound  FailureKind = "NOT_FOUND"
	FailureCorrupt   FailureKind = "CORRUPT"
	FailureTransient FailureKind = "TRANSIENT"
)

var (
	ErrProbeNotFound = errors.New("probe: source not found")
	ErrProbeCorrupt  = errors.New("probe: media corrupt or invalid")
)

// HasPlaylist returns true when a final playlist path is known.
func (m Metadata) HasPlaylist() bool {
	return m.PlaylistPath != ""
}

// HasArtifact returns true when a finalized artifact path is known.
func (m Metadata) HasArtifact() bool {
	return m.ArtifactPath != ""
}

// Fingerprint is used for cache invalidation.
type Fingerprint struct {
	Size  int64
	MTime int64
}
