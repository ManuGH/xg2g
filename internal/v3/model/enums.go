//go:build v3
// +build v3

// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package model

// SessionState is the client-visible lifecycle for a session ticket.
// It is intentionally coarse-grained and stable across profiles.
type SessionState string

const (
	SessionNew       SessionState = "NEW"
	SessionStarting  SessionState = "STARTING"
	SessionReady     SessionState = "READY"
	SessionDraining  SessionState = "DRAINING"
	SessionStopping  SessionState = "STOPPING"
	SessionFailed    SessionState = "FAILED"
	SessionCancelled SessionState = "CANCELLED"
)

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
	RNone               ReasonCode = "R_NONE"
	RUnknown            ReasonCode = "R_UNKNOWN"
	RBadRequest         ReasonCode = "R_BAD_REQUEST"
	RNotFound           ReasonCode = "R_NOT_FOUND"
	RLeaseBusy          ReasonCode = "R_LEASE_BUSY"
	RTuneTimeout        ReasonCode = "R_TUNE_TIMEOUT"
	RTuneFailed         ReasonCode = "R_TUNE_FAILED"
	RInvariantViolation ReasonCode = "R_INVARIANT_VIOLATION"
	RFFmpegStartFailed  ReasonCode = "R_FFMPEG_START_FAILED"
	RFFmpegExited       ReasonCode = "R_FFMPEG_EXIT"
	RPackagerFailed     ReasonCode = "R_PACKAGER_FAILED"
	RCancelled          ReasonCode = "R_CANCELLED"
	RIdleTimeout        ReasonCode = "R_IDLE_TIMEOUT"
)

// ProfileSpec is data-driven and future-proof (VisionOS, embedded clients, etc.).
type ProfileSpec struct {
	Name         string `json:"name"`
	LLHLS        bool   `json:"llhls"`
	DVRWindowSec int    `json:"dvrWindowSec"`
	// Future: Codec preference, audio-only, subtitles, etc.
}

// SessionRecord is the state-store source of truth for client-visible state.
type SessionRecord struct {
	SessionID     string        `json:"sessionId"`
	ServiceRef    string        `json:"serviceRef"`
	Profile       ProfileSpec   `json:"profile"`
	State         SessionState  `json:"state"`
	PipelineState PipelineState `json:"pipelineState"`
	Reason        ReasonCode    `json:"reason"`
	ReasonDetail  string        `json:"reasonDetail,omitempty"`
	CorrelationID string        `json:"correlationId"`
	CreatedAtUnix int64         `json:"createdAtUnix"`
	UpdatedAtUnix int64         `json:"updatedAtUnix"`
	ExpiresAtUnix int64         `json:"expiresAtUnix"` // TTL for garbage collection.
}

// PipelineRecord tracks the internal worker state.
type PipelineRecord struct {
	PipelineID     string        `json:"pipelineId"`
	ServiceKey     string        `json:"serviceKey"` // e.g. ref + profile
	State          PipelineState `json:"state"`
	LeaseOwner     string        `json:"leaseOwner"`
	LeaseExpiresAt int64         `json:"leaseExpiresAt"`
	Reason         ReasonCode    `json:"reason"`
	CreatedAtUnix  int64         `json:"createdAtUnix"`
	UpdatedAtUnix  int64         `json:"updatedAtUnix"`
}
