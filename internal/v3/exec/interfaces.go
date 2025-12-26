// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package exec

import (
	"context"

	"github.com/ManuGH/xg2g/internal/v3/model"
)

// Tuner manages the tuning hardware or source.
type Tuner interface {
	// Tune locks onto a specific service reference.
	Tune(ctx context.Context, serviceRef string) error

	// Healthy checks if the tuner is still locked and receiving signal.
	Healthy(ctx context.Context) error

	// Close releases the tuner resource.
	Close() error
}

// Transcoder manages the ffmpeg process.
type Transcoder interface {
	// Start launches the process with the given specification.
	Start(ctx context.Context, sessionID, serviceRef string, profileSpec model.ProfileSpec) error

	// Wait blocks until the process exits or context is cancelled.
	// It returns the final status.
	Wait(ctx context.Context) (model.ExitStatus, error)

	// Stop gracefully terminates the process.
	Stop(ctx context.Context) error
}

// Factory creates execution components.
type Factory interface {
	NewTuner(slot int) (Tuner, error)
	NewTranscoder() (Transcoder, error)
}
