package recordings

import (
	"errors"
	"fmt"
	"time"
)

// RecordingState is the canonical artifact lifecycle state for a recording.
type RecordingState string

const (
	StateUnknown      RecordingState = "UNKNOWN"
	StateProbing      RecordingState = "PROBING"
	StatePreparing    RecordingState = "PREPARING"
	StateReadyPartial RecordingState = "READY_PARTIAL"
	StateReadyFinal   RecordingState = "READY_FINAL"
	StateFailed       RecordingState = "FAILED"
)

// RecordingFacts are immutable-ish facts about a recording that feed decisions.
// Keep this struct stable; add fields deliberately (Config-as-Product-Surface discipline).
type RecordingFacts struct {
	Ref string // canonical serviceRef (validated)

	// Artifact pointers (may be empty depending on state).
	PlaylistPath string

	// Duration model:
	// - DurationSeconds=nil => unknown
	// - DurationFinal=true => must be READY_FINAL and duration must not change thereafter
	DurationSeconds *int64
	DurationFinal   bool

	// Observability / debugging
	LastError      string
	LastUpdatedUTC time.Time
	LastTransition time.Time
}

// RecordingMeta is the mutable state container persisted by vodManager (or your metadata store).
// This is the single canonical mutable record. DO NOT let handlers mutate it directly.
type RecordingMeta struct {
	State RecordingState
	Facts RecordingFacts

	// Internal bookkeeping (optional):
	// - BuildAttempt counter
	// - FailureReason typed enum
	// - Provenance of duration (container, sidecar, ffprobe, etc.)
	Attempt uint32
}

// TransitionEvent is an explicit state transition request.
// The state machine will validate allowed transitions and invariants.
type TransitionEvent struct {
	From RecordingState
	To   RecordingState

	// Optional: reason + timestamp for observability
	Reason string
	AtUTC  time.Time
}

// Validate enforces invariants that must hold for a meta record in its current state.
func (m *RecordingMeta) Validate() error {
	if m.Facts.Ref == "" {
		return errors.New("recordings: missing ref")
	}

	// Finality lock
	if m.State == StateReadyFinal {
		if m.Facts.DurationSeconds == nil {
			return errors.New("recordings: READY_FINAL requires DurationSeconds")
		}
		if !m.Facts.DurationFinal {
			return errors.New("recordings: READY_FINAL requires DurationFinal=true")
		}
	}

	// DurationFinal must not be true in non-final states
	if m.Facts.DurationFinal && m.State != StateReadyFinal {
		return fmt.Errorf("recordings: DurationFinal=true invalid in state=%s", m.State)
	}

	return nil
}

// CanTransition enforces the allowed transition graph.
// Keep this table tiny and explicit; do not add “catch-all” logic.
func CanTransition(from, to RecordingState) bool {
	switch from {
	case StateUnknown:
		return to == StateProbing || to == StatePreparing
	case StateProbing:
		return to == StatePreparing || to == StateFailed
	case StatePreparing:
		return to == StateReadyPartial || to == StateReadyFinal || to == StateFailed
	case StateReadyPartial:
		return to == StateReadyFinal || to == StateFailed
	case StateReadyFinal:
		return to == StateFailed
	case StateFailed:
		return to == StatePreparing || to == StateReadyFinal || to == StateReadyPartial
	default:
		return false
	}
}

// ApplyTransition applies a validated transition and updates timestamps.
// All state transitions should flow through here.
func (m *RecordingMeta) ApplyTransition(ev TransitionEvent) error {
	if m.State != ev.From {
		return fmt.Errorf("recordings: transition mismatch have=%s wantFrom=%s", m.State, ev.From)
	}
	if !CanTransition(ev.From, ev.To) {
		return fmt.Errorf("recordings: illegal transition %s -> %s", ev.From, ev.To)
	}

	// Guard: once final, duration cannot change (enforce via duration rules elsewhere too)
	if m.State == StateReadyFinal && ev.To != StateFailed {
		return errors.New("recordings: READY_FINAL may only transition to FAILED")
	}

	m.State = ev.To
	m.Facts.LastTransition = ev.AtUTC
	m.Facts.LastUpdatedUTC = ev.AtUTC
	if ev.Reason != "" {
		m.Facts.LastError = ev.Reason
	}
	return m.Validate()
}
