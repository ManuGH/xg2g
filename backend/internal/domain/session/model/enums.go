// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package model

import "time"

// SessionState is the client-visible lifecycle for a session ticket.
// It is intentionally coarse-grained and stable across profiles.
type SessionState string
type ProfileID string

const (
	SessionNew     SessionState = "NEW"
	SessionUnknown SessionState = "UNKNOWN"

	// Context Keys
	CtxKeyTunerSlot       = "tuner_slot"
	CtxKeyMode            = "mode"
	CtxKeyDurationSeconds = "duration_seconds"
	CtxKeyRecordingID     = "recording_id"
	CtxKeySourceType      = "source_type"
	CtxKeySource          = "source"
)

const (
	ModeLive      = "LIVE"
	ModeRecording = "RECORDING"
)

// ExitStatus describes how a Transcoder process ended.
// Moved here from exec package to avoid import cycles.
type ExitStatus struct {
	Code      int
	Reason    string
	StartedAt time.Time
	EndedAt   time.Time
}

const (
	SessionStarting  SessionState = "STARTING"
	SessionPriming   SessionState = "PRIMING"
	SessionReady     SessionState = "READY"
	SessionDraining  SessionState = "DRAINING"
	SessionStopping  SessionState = "STOPPING"
	SessionFailed    SessionState = "FAILED"
	SessionCancelled SessionState = "CANCELLED"
	SessionStopped   SessionState = "STOPPED"
)

// IsTerminal returns true if the state is a final state.
func (s SessionState) IsTerminal() bool {
	switch s {
	case SessionFailed, SessionCancelled, SessionStopped:
		return true
	}
	return false
}

// IsResourceOccupying returns true if the session in this state is expected
// to occupy system resources (tuner slots, transcoding slots, etc.).
func IsResourceOccupying(s SessionState) bool {
	switch s {
	case SessionNew, SessionStarting, SessionPriming, SessionReady, SessionDraining, SessionStopping:
		return true
	default:
		return false
	}
}

// PipelineState is the internal worker lifecycle.
// This is where “real truth” lives for tuning, FFmpeg, packaging, etc.
type PipelineState string

const (
	PipeInit           PipelineState = "INIT"
	PipeLeaseAcquired  PipelineState = "LEASE_ACQUIRED"
	PipeTuneRequested  PipelineState = "TUNE_REQUESTED"
	PipeTuned          PipelineState = "TUNED"
	PipeFFmpegStarting PipelineState = "FFMPEG_STARTING"
	PipeFFmpegRunning  PipelineState = "FFMPEG_RUNNING"
	PipePackagerReady  PipelineState = "PACKAGER_READY"
	PipeServing        PipelineState = "SERVING"
	PipeFail           PipelineState = "FAIL"
	PipeStopRequested  PipelineState = "STOP_REQUESTED"
	PipeStopped        PipelineState = "STOPPED"
)

// ReasonCode is a compact, typed failure/decision signal.
// Keep these stable: metrics + client UX depend on them.
type ReasonCode string

const (
	RNone                ReasonCode = "R_NONE"
	RUnknown             ReasonCode = "R_UNKNOWN"
	RBadRequest          ReasonCode = "R_BAD_REQUEST"
	RNotFound            ReasonCode = "R_NOT_FOUND"
	RLeaseBusy           ReasonCode = "R_LEASE_BUSY" // Capacity rejection (no tuner available), retry later.
	RTuneTimeout         ReasonCode = "R_TUNE_TIMEOUT"
	RLeaseExpired        ReasonCode = "R_LEASE_EXPIRED" // Lease lost or expired
	RTuneFailed          ReasonCode = "R_TUNE_FAILED"
	RInvariantViolation  ReasonCode = "R_INVARIANT_VIOLATION"
	RPipelineStartFailed ReasonCode = "R_PIPELINE_START_FAILED"

	RProcessEnded            ReasonCode = "R_PROCESS_ENDED"
	RPackagerFailed          ReasonCode = "R_PACKAGER_FAILED"
	RCancelled               ReasonCode = "R_CANCELLED"
	RDeadlineExceeded        ReasonCode = "R_DEADLINE_EXCEEDED"
	RIdleTimeout             ReasonCode = "R_IDLE_TIMEOUT"
	RClientStop              ReasonCode = "R_CLIENT_STOP"
	RUpstreamCorrupt         ReasonCode = "R_UPSTREAM_CORRUPT" // Upstream source is corrupt or missing keyframes
	RInternalInvariantBreach ReasonCode = "R_INTERNAL_INVARIANT_BREACH"
)

// ReasonDetailCode is a canonical, public-safe detail code.
// Free-text details must never be exposed via the API.
type ReasonDetailCode string

const (
	DNone                    ReasonDetailCode = "D_NONE"
	DContextCanceled         ReasonDetailCode = "D_CONTEXT_CANCELED"
	DDeadlineExceeded        ReasonDetailCode = "D_DEADLINE_EXCEEDED"
	DRecordingComplete       ReasonDetailCode = "D_RECORDING_COMPLETE"
	DSweeperForcedStopStuck  ReasonDetailCode = "D_SWEEPER_FORCED_STOP_STUCK"
	DInternalInvariantBreach ReasonDetailCode = "D_INTERNAL_INVARIANT_BREACH"
)

// ProfileSpec is data-driven and future-proof (VisionOS, embedded clients, etc.).
type ProfileSpec struct {
	Name           string `json:"name"`
	LLHLS          bool   `json:"llhls"`
	DVRWindowSec   int    `json:"dvrWindowSec"`
	VOD            bool   `json:"vod,omitempty"`
	TranscodeVideo bool   `json:"transcodeVideo"`
	VideoCodec     string `json:"videoCodec,omitempty"` // "h264" (default) or "hevc"
	HWAccel        string `json:"hwAccel,omitempty"`    // "vaapi", "qsv", "nvenc", etc.
	Deinterlace    bool   `json:"deinterlace,omitempty"`
	VideoCRF       int    `json:"videoCrf,omitempty"`
	VideoMaxWidth  int    `json:"videoMaxWidth,omitempty"`
	VideoMaxRateK  int    `json:"videoMaxRateK,omitempty"`
	VideoBufSizeK  int    `json:"videoBufSizeK,omitempty"`
	BFrames        int    `json:"bframes,omitempty"`
	AudioBitrateK  int    `json:"audioBitrateK,omitempty"`
	Preset         string `json:"preset,omitempty"`
	Container      string `json:"container,omitempty"` // "ts" (default) or "fmp4"
}

// SessionRecord is the state-store source of truth for client-visible state.
type SessionRecord struct {
	SessionID         string           `json:"sessionId"`
	ServiceRef        string           `json:"serviceRef"`
	Profile           ProfileSpec      `json:"profile"`
	State             SessionState     `json:"state"`
	PipelineState     PipelineState    `json:"pipelineState"`
	Reason            ReasonCode       `json:"reason"`
	ReasonDetailCode  ReasonDetailCode `json:"reasonDetailCode,omitempty"`
	ReasonDetailDebug string           `json:"reasonDetailDebug,omitempty"`
	FallbackReason    string           `json:"fallbackReason,omitempty"`
	FallbackAtUnix    int64            `json:"fallbackAtUnix,omitempty"`
	CorrelationID     string           `json:"correlationId"`
	CreatedAtUnix     int64            `json:"createdAtUnix"`
	UpdatedAtUnix     int64            `json:"updatedAtUnix"`
	LastAccessUnix    int64            `json:"lastAccessUnix,omitempty"`
	ExpiresAtUnix     int64            `json:"expiresAtUnix"` // TTL for garbage collection.
	// ADR-009: Session Lease Semantics
	LeaseExpiresAtUnix int64  `json:"leaseExpiresAtUnix"`
	HeartbeatInterval  int    `json:"heartbeatInterval"`
	LastHeartbeatUnix  int64  `json:"lastHeartbeatUnix,omitempty"`
	StopReason         string `json:"stopReason,omitempty"` // USER_STOPPED, LEASE_EXPIRED, FAILED, etc.

	// PR-P3-2: Deterministic Lifecycle Fields
	LatestSegmentAt      time.Time `json:"latestSegmentAt,omitempty"`
	LastPlaylistAccessAt time.Time `json:"lastPlaylistAccessAt,omitempty"`
	PlaylistPublishedAt  time.Time `json:"playlistPublishedAt,omitempty"`

	ContextData map[string]string `json:"contextData,omitempty"`
}

// IntentType defines the type of intent (command).
type IntentType string

const (
	IntentTypeStreamStart IntentType = "stream.start"
	IntentTypeStreamStop  IntentType = "stream.stop"
)

// Intent represents a user desire to change state (e.g., start a stream).
type Intent struct {
	Type       IntentType        `json:"type"`
	SessionID  string            `json:"sessionId,omitempty"`
	ServiceRef string            `json:"serviceRef"`
	Profile    string            `json:"profile"`
	Priority   int               `json:"priority"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}
