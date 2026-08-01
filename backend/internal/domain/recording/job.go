// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package recording

import (
	"errors"
	"fmt"
	"regexp"
	"time"
)

var (
	ErrInvalidStateTransition = errors.New("invalid recording state transition")
	ErrJobAlreadyFinished     = errors.New("recording job is already in terminal state")
	ErrInvalidJobID           = errors.New("invalid job ID format")
)

var jobIDRegex = regexp.MustCompile(`^[a-zA-Z0-9_\-]+$`)

// ValidateJobID ensures jobID is free of path traversal and illegal characters.
func ValidateJobID(jobID string) error {
	if jobID == "" || !jobIDRegex.MatchString(jobID) {
		return fmt.Errorf("%w: '%s'", ErrInvalidJobID, jobID)
	}
	return nil
}

// RecordingSource identifies how a recording was initiated.
type RecordingSource string

const (
	SourceScheduled RecordingSource = "SCHEDULED"
	SourceManual    RecordingSource = "MANUAL"
	SourceRetro     RecordingSource = "RETRO"
)

// RecordingState defines the authoritative state machine lifecycle of a recording job.
type RecordingState string

const (
	StatePending       RecordingState = "PENDING"
	StatePreparing     RecordingState = "PREPARING"
	StateRecording     RecordingState = "RECORDING"
	StateStaging       RecordingState = "STAGING"
	StateFinalizing    RecordingState = "FINALIZING"
	StateTransferring  RecordingState = "TRANSFERRING"
	StateCompleted     RecordingState = "COMPLETED"
	StateWaitingTarget RecordingState = "WAITING_FOR_TARGET"
	StateCanceled      RecordingState = "CANCELED"
	StateInterrupted   RecordingState = "INTERRUPTED"
	StatePartial       RecordingState = "PARTIAL"
	StateFailed        RecordingState = "FAILED"
)

// IsTerminal returns true if the recording state is a final unchangeable state.
func (s RecordingState) IsTerminal() bool {
	return s == StateCompleted || s == StateCanceled || s == StatePartial || s == StateFailed
}

// RecordingJob is a serializable, pure domain model without embedded locks.
type RecordingJob struct {
	ID               string          `json:"id"`
	ServiceRef       string          `json:"service_ref"`
	EventID          string          `json:"event_id,omitempty"`
	Title            string          `json:"title"`
	SourceType       RecordingSource `json:"source_type"`
	RequestedStart   time.Time       `json:"requested_start"`
	RequestedEnd     time.Time       `json:"requested_end"`
	ActualStart      time.Time       `json:"actual_start,omitempty"`
	ActualEnd        time.Time       `json:"actual_end,omitempty"`
	RecordedUntil    time.Time       `json:"recorded_until,omitempty"`
	FinishedAt       time.Time       `json:"finished_at,omitempty"`
	FailedAt         time.Time       `json:"failed_at,omitempty"`
	TargetBackendID  string          `json:"target_backend_id"`
	LocalFallbackID  string          `json:"local_fallback_id"`
	State            RecordingState  `json:"state"`
	LocalStagingPath string          `json:"local_staging_path,omitempty"`
	FinalizedPath    string          `json:"finalized_path,omitempty"`
	ErrorDetail      string          `json:"error_detail,omitempty"`
}

// NewRecordingJob creates a new pure RecordingJob model in PENDING state.
func NewRecordingJob(id, serviceRef, title string, source RecordingSource, reqStart, reqEnd time.Time, targetBackendID string) (*RecordingJob, error) {
	if err := ValidateJobID(id); err != nil {
		return nil, err
	}
	return &RecordingJob{
		ID:              id,
		ServiceRef:      serviceRef,
		Title:           title,
		SourceType:      source,
		RequestedStart:  reqStart,
		RequestedEnd:    reqEnd,
		TargetBackendID: targetBackendID,
		LocalFallbackID: "local-nvme",
		State:           StatePending,
	}, nil
}

// CanTransitionTo validates if a state transition is legal according to domain rules.
func (j *RecordingJob) CanTransitionTo(newState RecordingState) error {
	if j.State.IsTerminal() {
		return fmt.Errorf("%w: current state is %s", ErrJobAlreadyFinished, j.State)
	}

	valid := false
	switch j.State {
	case StatePending:
		valid = (newState == StatePreparing || newState == StateCanceled || newState == StateFailed)
	case StatePreparing:
		valid = (newState == StateRecording || newState == StateStaging || newState == StateCanceled || newState == StateFailed)
	case StateRecording:
		valid = (newState == StateStaging || newState == StateInterrupted || newState == StateCanceled || newState == StateFailed)
	case StateStaging:
		valid = (newState == StateFinalizing || newState == StateInterrupted || newState == StateFailed)
	case StateFinalizing:
		valid = (newState == StateTransferring || newState == StateCompleted || newState == StateWaitingTarget || newState == StatePartial || newState == StateFailed)
	case StateTransferring:
		valid = (newState == StateCompleted || newState == StateWaitingTarget || newState == StateFailed)
	case StateWaitingTarget:
		valid = (newState == StateTransferring || newState == StateCompleted || newState == StateFailed)
	case StateInterrupted, StatePartial:
		valid = (newState == StateStaging || newState == StateFinalizing || newState == StateFailed)
	}

	if !valid {
		return fmt.Errorf("%w: cannot transition from %s to %s", ErrInvalidStateTransition, j.State, newState)
	}

	return nil
}
