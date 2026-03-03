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
