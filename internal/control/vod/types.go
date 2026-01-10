package vod

// StreamInfo defines the properties needed for decision making.
type StreamInfo struct {
	Video VideoStreamInfo
	Audio AudioStreamInfo
}

type VideoStreamInfo struct {
	CodecName  string
	PixFmt     string
	Profile    string
	Level      int
	BitDepth   int
	StartTime  float64
	Duration   float64
	Width      int
	Height     int
	Interlaced bool
}

type AudioStreamInfo struct {
	CodecName     string
	SampleRate    int
	Channels      int
	ChannelLayout string
	TrackCount    int
	StartTime     float64
}

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
	Duration     float64
	Fingerprint  Fingerprint
	Error        string
	UpdatedAt    int64
	StateGen     uint64 // Monotonic generation counter for race-safe updates
}

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
