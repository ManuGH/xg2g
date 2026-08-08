// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package model

import (
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
)

// SessionState is the client-visible lifecycle for a session ticket.
// It is intentionally coarse-grained and stable across profiles.
type SessionState string
type ProfileID string

const (
	SessionNew     SessionState = "NEW"
	SessionUnknown SessionState = "UNKNOWN"

	// Context Keys
	CtxKeyTunerSlot             = "tuner_slot"
	CtxKeyMode                  = "mode"
	CtxKeyDurationSeconds       = "duration_seconds"
	CtxKeyRecordingID           = "recording_id"
	CtxKeySourceType            = "source_type"
	CtxKeySource                = "source"
	CtxKeyClientPath            = "client_path"
	CtxKeyPrincipalID           = "principal_id"
	CtxKeyClientFamily          = "client_family"
	CtxKeyPreferredEngine       = "preferred_hls_engine"
	CtxKeyDeviceType            = "device_type"
	CtxKeyDecisionRequest       = "decision_request_id"
	CtxKeyRuntimeTargetStep     = "runtime_target_step"
	CtxKeyRuntimePolicyState    = "runtime_policy_state"
	CtxKeyRuntimePolicyAction   = "runtime_policy_action"
	CtxKeyRuntimeCurrentStep    = "runtime_current_step"
	CtxKeyRuntimeProbeStep      = "runtime_probe_step"
	CtxKeyRuntimeProbeState     = "runtime_probe_state"
	CtxKeyRuntimePolicyTimeline = "runtime_policy_timeline"
	CtxKeyRuntimePolicyReplay   = "runtime_policy_replay"
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

	RProcessEnded      ReasonCode = "R_PROCESS_ENDED"
	RPackagerFailed    ReasonCode = "R_PACKAGER_FAILED"
	RCancelled         ReasonCode = "R_CANCELLED"
	RDeadlineExceeded  ReasonCode = "R_DEADLINE_EXCEEDED"
	RIdleTimeout       ReasonCode = "R_IDLE_TIMEOUT"
	RClientStop        ReasonCode = "R_CLIENT_STOP"
	RUpstreamCorrupt   ReasonCode = "R_UPSTREAM_CORRUPT"   // Upstream source is corrupt or missing keyframes
	RUpstreamScrambled ReasonCode = "R_UPSTREAM_SCRAMBLED" // Upstream stream is scrambled (encrypted, receiver could not descramble)
	// RDescramblerDown separates a receiver that has stopped descrambling
	// altogether from a single service that is simply not entitled. Same packets
	// on the wire, opposite actions for whoever has to fix it.
	RDescramblerDown         ReasonCode = "R_DESCRAMBLER_DOWN"
	RInternalInvariantBreach ReasonCode = "R_INTERNAL_INVARIANT_BREACH"

	RReceiverUsageLiveLimitExceeded             ReasonCode = "RECEIVER_USAGE_LIVE_LIMIT_EXCEEDED"
	RReceiverUsageRecordingLimitExceeded        ReasonCode = "RECEIVER_USAGE_RECORDING_LIMIT_EXCEEDED"
	RReceiverUsageRestrictedAccessLimitExceeded ReasonCode = "RECEIVER_USAGE_RESTRICTED_ACCESS_LIMIT_EXCEEDED"
	RReceiverUsageLiveWithRecordingForbidden    ReasonCode = "RECEIVER_USAGE_LIVE_WITH_RECORDING_FORBIDDEN"
	RReceiverUsageIntentNotAllowed              ReasonCode = "RECEIVER_USAGE_INTENT_NOT_ALLOWED"
	RReceiverUsageAccessClassificationUnknown   ReasonCode = "RECEIVER_USAGE_ACCESS_CLASSIFICATION_UNKNOWN"
	RReceiverUsageChannelChangeRateLimited      ReasonCode = "RECEIVER_USAGE_CHANNEL_CHANGE_RATE_LIMITED"
)

// ReasonDetailCode is a canonical, public-safe detail code.
// Free-text details must never be exposed via the API.
type ReasonDetailCode string

const (
	DNone                      ReasonDetailCode = "D_NONE"
	DContextCanceled           ReasonDetailCode = "D_CONTEXT_CANCELED"
	DDeadlineExceeded          ReasonDetailCode = "D_DEADLINE_EXCEEDED"
	DRecordingComplete         ReasonDetailCode = "D_RECORDING_COMPLETE"
	DSweeperForcedStopStuck    ReasonDetailCode = "D_SWEEPER_FORCED_STOP_STUCK"
	DInternalInvariantBreach   ReasonDetailCode = "D_INTERNAL_INVARIANT_BREACH"
	DProcessEndedStartup       ReasonDetailCode = "D_PROCESS_ENDED_STARTUP"
	DProcessExitedUnexpectedly ReasonDetailCode = "D_PROCESS_EXITED_UNEXPECTEDLY"
	DTranscodeStalled          ReasonDetailCode = "D_TRANSCODE_STALLED"
	DUpstreamEndedPrematurely  ReasonDetailCode = "D_UPSTREAM_ENDED_PREMATURELY"
	DUpstreamInputOpenFailed   ReasonDetailCode = "D_UPSTREAM_INPUT_OPEN_FAILED"
	DInvalidUpstreamInput      ReasonDetailCode = "D_INVALID_UPSTREAM_INPUT"
	DUpstreamScrambled         ReasonDetailCode = "D_UPSTREAM_SCRAMBLED"
	DCopyOutputMissingCodec    ReasonDetailCode = "D_COPY_OUTPUT_MISSING_CODEC"
	// DEncoderCrashed and DBlackOutput carry failures that previously rendered as
	// D_NONE, i.e. reached the client with an empty reason_detail. The code itself
	// is never serialised — only its text — so these are internal additions.
	DEncoderCrashed ReasonDetailCode = "D_ENCODER_CRASHED"
	DBlackOutput    ReasonDetailCode = "D_BLACK_OUTPUT"
	// DDescramblerDown separates a receiver that has stopped descrambling
	// entirely from a single service that is not entitled.
	DDescramblerDown ReasonDetailCode = "D_DESCRAMBLER_DOWN"
)

// ProfileSpec is data-driven and future-proof (VisionOS, embedded clients, etc.).
type ProfileSpec = ports.ProfileSpec

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
	GenerationID      string           `json:"generationId,omitempty"`
	CreatedAtUnix     int64            `json:"createdAtUnix"`
	UpdatedAtUnix     int64            `json:"updatedAtUnix"`
	LastAccessUnix    int64            `json:"lastAccessUnix,omitempty"`
	ExpiresAtUnix     int64            `json:"expiresAtUnix"` // TTL for garbage collection.
	// ADR-009: Session Lease Semantics
	LeaseExpiresAtUnix    int64  `json:"leaseExpiresAtUnix"`
	HeartbeatInterval     int    `json:"heartbeatInterval"`
	LastHeartbeatUnix     int64  `json:"lastHeartbeatUnix,omitempty"`
	StopReason            string `json:"stopReason,omitempty"` // USER_STOPPED, LEASE_EXPIRED, FAILED, etc.
	StopRequestedAtUnixMs int64  `json:"stopRequestedAtUnixMs,omitempty"`

	// PR-P3-2: Deterministic Lifecycle Fields
	LatestSegmentAt      time.Time `json:"latestSegmentAt,omitempty"`
	LastPlaylistAccessAt time.Time `json:"lastPlaylistAccessAt,omitempty"`
	PlaylistPublishedAt  time.Time `json:"playlistPublishedAt,omitempty"`

	ContextData   map[string]string `json:"contextData,omitempty"`
	PlaybackTrace *PlaybackTrace    `json:"playbackTrace,omitempty"`
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

// Text renders a detail code as the public, API-visible description.
//
// This is the ONE place a detail code becomes prose. It used to be copied into
// each HTTP surface, and the copies had already drifted: the table serving
// GET /sessions/{id} was missing DUpstreamScrambled, so that endpoint returned an
// empty reason_detail for a scrambled upstream while every other surface carried
// the text. Process-failure wording comes from ports so the adapters that produce
// those failures and this renderer quote the same string.
func (c ReasonDetailCode) Text() string {
	switch c {
	case DContextCanceled:
		return "context canceled"
	case DDeadlineExceeded:
		return "deadline exceeded"
	case DRecordingComplete:
		return "recording completed"
	case DSweeperForcedStopStuck:
		return "sweeper_forced_stop_stuck"
	case DInternalInvariantBreach:
		return "internal invariant breach"
	case DProcessEndedStartup:
		return "process ended during startup"
	case DProcessExitedUnexpectedly:
		return ports.DetailProcessExitedUnexpectedly
	case DTranscodeStalled:
		return ports.DetailTranscodeStalled
	case DUpstreamEndedPrematurely:
		return ports.DetailUpstreamEndedPrematurely
	case DUpstreamInputOpenFailed:
		return ports.DetailUpstreamOpenFailed
	case DInvalidUpstreamInput:
		return ports.DetailInvalidUpstreamInput
	case DUpstreamScrambled:
		return ports.DetailUpstreamScrambled
	case DCopyOutputMissingCodec:
		return ports.DetailCopyOutputMissingCodec
	case DEncoderCrashed:
		return ports.DetailEncoderCrashed
	case DBlackOutput:
		return ports.DetailBlackOutput
	case DDescramblerDown:
		return ports.DetailDescramblerDown
	default:
		return ""
	}
}
