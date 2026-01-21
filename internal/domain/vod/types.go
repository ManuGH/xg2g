// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package vod

import (
	"errors"
)

// StreamInfo defines the properties needed for decision making.
type StreamInfo struct {
	Container string // e.g. "mov,mp4,m4a,3gp,3g2,mj2" or "mpegts"
	Video     VideoStreamInfo
	Audio     AudioStreamInfo
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
	FPS        float64
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
