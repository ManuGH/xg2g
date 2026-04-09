package resume

import (
	"context"
	"errors"
	"time"
)

var ErrNilState = errors.New("resume state must not be nil")

// State represents the saved playback state for a user and recording.
type State struct {
	// PosSeconds is the last known playback position in seconds.
	PosSeconds int64 `json:"pos_seconds"`

	// DurationSeconds is the total duration at the time of saving (optional, for validation).
	DurationSeconds int64 `json:"duration_seconds,omitempty"`

	// UpdatedAt is the timestamp when this state was last saved.
	UpdatedAt time.Time `json:"updated_at"`

	// Fingerprint is a checksum/property-string to validate the asset hasn't changed.
	// If the asset changes (re-recorded, replaced), the fingerprint mismatch should invalidate resume.
	Fingerprint string `json:"fingerprint,omitempty"`

	// Finished indicates if the user has watched to the end (e.g. > 95%).
	Finished bool `json:"finished,omitempty"`
}

// Store defines the interface for persisting resume state.
type Store interface {
	// Put saves and fully replaces the persisted resume state for a principal and
	// canonical recording key. The most recent write for a key is authoritative.
	Put(ctx context.Context, principalID, recordingKey string, state *State) error

	// Get retrieves the full persisted resume state for a canonical recording
	// key. Returns nil if not found.
	Get(ctx context.Context, principalID, recordingKey string) (*State, error)

	// Delete removes the resume state.
	Delete(ctx context.Context, principalID, recordingKey string) error

	// Close cleans up resources.
	Close() error
}

func cloneState(state *State) *State {
	if state == nil {
		return nil
	}
	cloned := *state
	return &cloned
}
