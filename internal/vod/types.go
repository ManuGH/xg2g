package vod

import (
	"context"
	"sync"
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

// JobSpec encapsulates observability metadata for a VOD job (Phase C)
type JobSpec struct {
	ID         string
	ServiceRef string            // For Circuit Breaker / Reporting
	Kind       string            // "hls", "mp4", etc.
	Tags       map[string]string // Structured logging metadata
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
type Run struct {
	ID        string
	StartedAt time.Time

	// Done is closed when the build completes (success or failure)
	Done chan struct{}

	// mu protects mutable state (Phase C)
	mu  sync.RWMutex
	err error

	Result string // e.g. OutputPath or success message

	// Cancel function to stop the specific FFmpeg process
	Cancel context.CancelFunc
}

// Error returns the run's error in a thread-safe way (Phase C)
func (r *Run) Error() error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.err
}

// setError updates the run error under lock (Phase C)
func (r *Run) setError(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.err = err
}

// Wait blocks until the run is done or the context is canceled (Phase C)
func (r *Run) Wait(ctx context.Context) error {
	select {
	case <-r.Done:
		return r.Error()
	case <-ctx.Done():
		return ctx.Err()
	}
}

// WorkFunc is the unit of execution for the manager (Stable API)
type WorkFunc func(ctx context.Context) error

// WorkFuncSpec is the structured unit of execution (Phase C)
type WorkFuncSpec func(ctx context.Context, spec JobSpec) error

// ManagerAPI defines the public interface for the VOD Manager
type ManagerAPI interface {
	// Ensure keeps the legacy API stable
	Ensure(ctx context.Context, id string, work WorkFunc) (*Run, bool)
	// EnsureSpec provides structured job orchestration (Phase C)
	EnsureSpec(ctx context.Context, spec JobSpec, work WorkFuncSpec) (*Run, bool)

	Get(id string) *Run
	Cancel(id string)
	CancelAll()
}
