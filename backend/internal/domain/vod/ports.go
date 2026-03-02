// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package vod

import (
	"context"
	"time"
)

// Spec defines the immutable configuration for a VOD build job.
// This is a Port (interface contract) that Control defines and Infra implements.
type Spec struct {
	// Input matches the source stream or file path.
	Input string

	// WorkDir is the root of the temporary workspace owned by Control.
	// Infrastructure MUST NOT write outside this directory.
	WorkDir string

	// OutputTemp is the filename relative to WorkDir where the artifact is built.
	OutputTemp string

	// Profile defines the transcoding/remuxing profile invariant.
	Profile Profile
}

// Profile represents a strict configuration set for FFmpeg.
type Profile int

const (
	// ProfileDefault preserves input characteristics where possible (copy).
	ProfileDefault Profile = iota
	// ProfileHigh enforces high quality transcoding constraints.
	ProfileHigh
	// ProfileLow enforces bandwidth-constrained settings.
	ProfileLow
)

// ProgressEvent signals activity.
type ProgressEvent struct {
	// At is the monotonic timestamp when this event was observed.
	// It is informational; the act of receiving the event is the heartbeat.
	At time.Time
}

// Runner defines the contract for starting an external process.
// This interface MUST be implemented by the Infrastructure layer.
type Runner interface {
	// Start launches the process defined by Spec.
	// Returns immediate error if execution fails to start/fork.
	Start(ctx context.Context, cmd Spec) (Handle, error)
}

// Handle controls a running process.
type Handle interface {
	// Wait blocks until the process exits.
	// Returns nil if exit code 0, error otherwise.
	Wait() error

	// Stop attempts graceful termination (SIGTERM), then kills (SIGKILL) after killDelay.
	// grace: duration to wait for graceful exit before sending KILL.
	// kill: duration to wait for KILL to take effect before giving up (or internal timeout).
	Stop(grace, kill time.Duration) error

	// Progress returns a read-only channel of monotonic heartbeat events.
	// Invariant: Any receive on this channel constitutes a heartbeat.
	Progress() <-chan ProgressEvent

	// Diagnostics returns a bounded snapshot of recent logs/errors (ring buffer).
	Diagnostics() []string
}
