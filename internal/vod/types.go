package vod

import (
	"context"
	"time"

	"github.com/rs/zerolog"
)

// Exec abstracts the command execution for testing
type Exec interface {
	Run(ctx context.Context, name string, args []string) error
}

// Logger alias for zerolog (or just use zerolog directly in structs)
type Logger = zerolog.Logger

// NopLogger helper for tests
func NopLogger() Logger {
	return zerolog.Nop()
}

// RemuxRequest contains all inputs needed to start a VOD build
type RemuxRequest struct {
	ID         string // Unique identifier for the build (usually recordingID)
	InputPath  string
	OutputPath string // Cache path
	StartTime  string // "1" or precision calculated

	// Stream Properties (passed to builder)
	StreamInfo StreamInfo

	// CleanupPaths are temporary files to remove after the build completes
	CleanupPaths []string
}

// StreamInfo mirrors the input needed for builders
type StreamInfo struct {
	VideoCodec    string
	VideoPixFmt   string
	VideoBitDepth int
	AudioCodec    string
	AudioTracks   int
	AudioDelayMs  int
}

// Run represents an active or completed VOD build state
// Phase B: State Extraction
type Run struct {
	ID        string
	StartedAt time.Time

	// Done is closed when the build completes (success or failure)
	Done chan struct{}

	// Err and Result are immutable after Done is closed
	Err    error
	Result string // e.g. OutputPath or success message

	// Cancel function to stop the specific FFmpeg process
	Cancel context.CancelFunc
}

// WorkFunc is the unit of execution for the manager
type WorkFunc func(ctx context.Context) error

// ManagerAPI defines the public interface for the VOD Manager
type ManagerAPI interface {
	Ensure(ctx context.Context, id string, work WorkFunc) (*Run, bool)
	Get(id string) *Run
	Cancel(id string)
}
