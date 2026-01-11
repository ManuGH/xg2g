package resolver

import (
	"context"
	"time"

	"github.com/ManuGH/xg2g/internal/control/playback"
)

// ResolveOK contains ONLY the facts and decision needed for success.
// It DOES NOT contain URL mapping or error states.
type ResolveOK struct {
	Decision  playback.Decision
	MediaInfo playback.MediaInfo
	Reason    string // Resolution path: "resolved_via_store", "probed_and_persisted", etc.
}

// ResolveCode defines stable categories for failures.
type ResolveCode string

const (
	CodePreparing ResolveCode = "PREPARING"            // HTTP 503 + Retry-After
	CodeInvalid   ResolveCode = "INVALID_ID"           // HTTP 400
	CodeNotFound  ResolveCode = "VOD_NOT_FOUND"        // HTTP 404
	CodeUpstream  ResolveCode = "UPSTREAM_UNAVAILABLE" // HTTP 502
	CodeFailed    ResolveCode = "VOD_PLAYBACK_ERROR"   // HTTP 500
	CodeInternal  ResolveCode = "INTERNAL_ERROR"       // HTTP 500
)

// ResolveError is the EXCLUSIVE representation of failure.
type ResolveError struct {
	Code       ResolveCode
	RetryAfter time.Duration // Only for PREPARING
	Err        error         // Wrapped cause
	Detail     string        // Stable detail string
}

func (e *ResolveError) Error() string {
	if e.Detail != "" {
		return string(e.Code) + ": " + e.Detail
	}
	return string(e.Code)
}

func (e *ResolveError) Unwrap() error { return e.Err }

// DurationStore abstracts duration persistence from the resolver.
// This allows testing without full library store and keeps the resolver focused.
type DurationStore interface {
	// GetDuration retrieves duration for a recording by library coordinates.
	// Returns (seconds, true, nil) if found, (0, false, nil) if not found, (0, false, err) on error.
	GetDuration(ctx context.Context, rootID, relPath string) (seconds int64, ok bool, err error)
	// SetDuration persists duration for a recording by library coordinates.
	SetDuration(ctx context.Context, rootID, relPath string, seconds int64) error
}

// PathResolver maps recording references to local paths and library coordinates.
type PathResolver interface {
	// ResolveRecordingPath maps a serviceRef to (localPath, rootID, relPath).
	// Returns error if mapping fails or recording not accessible.
	ResolveRecordingPath(serviceRef string) (localPath, rootID, relPath string, err error)
}
