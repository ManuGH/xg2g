package ports

import "context"

// MediaPipeline defines the contract for controlling media processes.
// It is strictly an orchestration interface: Start, Stop, Check.
// Implementations (Infrastructure) handle the "how" (FFmpeg, GStreamer, Hardware).
type MediaPipeline interface {
	// OutputDirectory removed: implementation details are derived from spec.
	Start(ctx context.Context, spec StreamSpec) (RunHandle, error)

	// Stop gracefully terminates the media process associated with the handle.
	Stop(ctx context.Context, handle RunHandle) error

	// Health checks the liveness and status of the process.
	Health(ctx context.Context, handle RunHandle) HealthStatus
}

// FinalizedProfileProvider exposes the effective profile after the pipeline has
// applied runtime overrides and finalized its launch plan.
type FinalizedProfileProvider interface {
	FinalizedProfile(handle RunHandle) (ProfileSpec, bool)
}

// ExecutedFFmpegPlan is the execution-truth view of the ffmpeg command that was
// actually spawned, derived by parsing the FINAL argv handed to the process —
// never a profile prediction. Anything that surfaces "what ffmpeg runs" must
// source from this so the displayed plan cannot drift from the real process.
type ExecutedFFmpegPlan struct {
	Container  string
	Packaging  string
	HWAccel    string
	VideoMode  string
	VideoCodec string
	AudioMode  string
	AudioCodec string
}

// ExecutedFFmpegPlanProvider exposes the execution-truth ffmpeg plan parsed from
// the real argv of the process launched for a handle.
type ExecutedFFmpegPlanProvider interface {
	ExecutedFFmpegPlan(handle RunHandle) (ExecutedFFmpegPlan, bool)
}

// DiagnosticMetadata holds essential read-only fields for diagnostic logging during session lifecycle events.
type DiagnosticMetadata struct {
	GenerationID          string
	CorrelationID         string
	Reason                string
	StopRequestedAtUnixMs int64
}

// DiagnosticLookup exposes session metadata needed for diagnostic contexts without coupling to store models.
type DiagnosticLookup interface {
	GetDiagnosticMetadata(ctx context.Context, sessionID string) (DiagnosticMetadata, bool)
}
