package resume

import (
	"context"
	"errors"
	"time"
)

var ErrNilState = errors.New("resume state must not be nil")

// ErrStoreClosed is returned by a closed store instead of panicking on a nil map.
var ErrStoreClosed = errors.New("resume store is closed")

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

	// Title is a display-metadata snapshot taken at save time so surfaces
	// like "continue watching" can render without a receiver round-trip.
	// It may go stale if the recording is renamed; the canonical title
	// still lives in the recordings listing.
	Title string `json:"title,omitempty"`

	// Channel is the channel-name snapshot taken at save time (see Title).
	Channel string `json:"channel,omitempty"`
}

// RecentEntry pairs a canonical recording key with its saved state, as
// returned by Store.ListRecent.
type RecentEntry struct {
	// RecordingKey is the canonical resume key (the public recording ID).
	RecordingKey string

	// State is the persisted resume state for the key.
	State State
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

	// ListRecent returns the principal's unfinished resume entries with a
	// position > 0, most recently updated first, capped at limit.
	ListRecent(ctx context.Context, principalID string, limit int) ([]RecentEntry, error)

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
