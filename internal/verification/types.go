package verification

import (
	"context"
	"time"
)

// DriftState represents the persisted verification state of the system.
// It is the Single Source of Truth for the /status endpoint.
type DriftState struct {
	Version    int        `json:"version"` // Schema version (v1)
	Detected   bool       `json:"detected"`
	LastCheck  time.Time  `json:"last_check"` // RFC3339
	Mismatches []Mismatch `json:"mismatches"`
}

// Mismatch describes a specific deviation from the expected state.
type Mismatch struct {
	Kind     MismatchKind `json:"kind"`     // config, binary, runtime
	Key      string       `json:"key"`      // e.g. "runtime.ffmpeg.version"
	Expected string       `json:"expected"` // Redacted, max 256 chars
	Actual   string       `json:"actual"`   // Redacted, max 256 chars
}

type MismatchKind string

const (
	KindConfig  MismatchKind = "config"
	KindBinary  MismatchKind = "binary"
	KindRuntime MismatchKind = "runtime"
	KindEnv     MismatchKind = "env" // Optional
)

// Valid returns true if the kind is known.
func (k MismatchKind) Valid() bool {
	switch k {
	case KindConfig, KindBinary, KindRuntime, KindEnv:
		return true
	default:
		return false
	}
}

// Store defines the contract for accessing drift state.
// Implementations MUST guarantee O(1) reads (memory-first).
type Store interface {
	// Get returns the current state. Safe for concurrent use.
	Get(ctx context.Context) (DriftState, bool)

	// Set updates the state. It relies on the implementation to handle persistence.
	// Implementation should sanitize/redact values before storing.
	Set(ctx context.Context, st DriftState) error
}
