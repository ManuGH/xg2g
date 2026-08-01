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
	ErrInvalidJobID                  = errors.New("invalid recording job ID")
	ErrInvalidJobStateTransition     = errors.New("invalid recording job state transition")
	ErrJobAlreadyTerminal            = errors.New("recording job is in terminal state")
)

var jobIDRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// RecordingSource indicates how the recording was initiated.
type RecordingSource string

const (
	SourceLive      RecordingSource = "LIVE"
	SourceRetro     RecordingSource = "RETRO"
	SourceScheduled RecordingSource = "SCHEDULED"
)

// RecordingState defines the authoritative lifecycle of a recording job.
type RecordingState string

const (
	StatePreparing     RecordingState = "PREPARING"
	StateRecording     RecordingState = "RECORDING"
	StateStaging       RecordingState = "STAGING"
	StateFinalizing    RecordingState = "FINALIZING"
	StateWaitingTarget RecordingState = "WAITING_FOR_TARGET"
	StateTransferring  RecordingState = "TRANSFERRING"
	StateCompleted     RecordingState = "COMPLETED"
	StatePartial       RecordingState = "PARTIAL"
	StateFailed        RecordingState = "FAILED"
)

// IsTerminal returns true if the job has reached a final state.
func (s RecordingState) IsTerminal() bool {
	return s == StateCompleted || s == StatePartial || s == StateFailed
}

// ValidateJobID ensures job ID meets naming standards and prevents path traversal.
func ValidateJobID(id string) error {
	if !jobIDRegex.MatchString(id) {
		return fmt.Errorf("%w: '%s' must match [a-zA-Z0-9_-]{1,64}", ErrInvalidJobID, id)
	}
	return nil
}

// RecordingJob represents an active or completed recording workflow manifest.
type RecordingJob struct {
	ID               string          `json:"id"`
	ServiceRef       string          `json:"service_ref"`
	Title            string          `json:"title"`
	Source           RecordingSource `json:"source"`
	State            RecordingState  `json:"state"`
	StartTime        time.Time       `json:"start_time"`
	EndTime          time.Time       `json:"end_time"`
	RequestedStart   time.Time       `json:"requested_start,omitempty"`
	RequestedEnd     time.Time       `json:"requested_end,omitempty"`
	TargetBackendID  string          `json:"target_backend_id"`
	LocalStagingPath string          `json:"local_staging_path,omitempty"`
	FinalizedPath    string          `json:"finalized_path,omitempty"`
	ErrorDetail      string          `json:"error_detail,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
	FinishedAt       *time.Time      `json:"finished_at,omitempty"`
	FailedAt         *time.Time      `json:"failed_at,omitempty"`
}

// NewRecordingJob constructs a new RecordingJob.
func NewRecordingJob(id, serviceRef, title string, source RecordingSource, start, end time.Time, backendID string) (*RecordingJob, error) {
	if err := ValidateJobID(id); err != nil {
		return nil, err
	}
	if serviceRef == "" || title == "" || backendID == "" {
		return nil, fmt.Errorf("job serviceRef, title, and targetBackendID cannot be empty")
	}
	now := time.Now()
	return &RecordingJob{
		ID:              id,
		ServiceRef:      serviceRef,
		Title:           title,
		Source:          source,
		State:           StatePreparing,
		StartTime:       start,
		EndTime:         end,
		TargetBackendID: backendID,
		CreatedAt:       now,
		UpdatedAt:       now,
	}, nil
}

// CanTransitionTo validates if a job state transition is legal.
func (j *RecordingJob) CanTransitionTo(newState RecordingState) error {
	if j.State.IsTerminal() {
		return fmt.Errorf("%w: job %s is in terminal state %s", ErrJobAlreadyTerminal, j.ID, j.State)
	}

	valid := false
	switch j.State {
	case StatePreparing:
		valid = (newState == StateRecording || newState == StateStaging || newState == StateFinalizing || newState == StateWaitingTarget || newState == StateFailed)
	case StateRecording:
		valid = (newState == StateStaging || newState == StateFinalizing || newState == StateWaitingTarget || newState == StateFailed)
	case StateStaging:
		valid = (newState == StateFinalizing || newState == StateWaitingTarget || newState == StateTransferring || newState == StateCompleted || newState == StatePartial || newState == StateFailed)
	case StateFinalizing:
		valid = (newState == StateWaitingTarget || newState == StateTransferring || newState == StateCompleted || newState == StatePartial || newState == StateFailed)
	case StateWaitingTarget:
		valid = (newState == StateTransferring || newState == StateCompleted || newState == StatePartial || newState == StateFailed)
	case StateTransferring:
		valid = (newState == StateCompleted || newState == StatePartial || newState == StateFailed)
	}

	if !valid {
		return fmt.Errorf("%w: cannot transition job from %s to %s", ErrInvalidJobStateTransition, j.State, newState)
	}
	return nil
}

// TransitionState returns a deep copy of RecordingJob with the updated state and timestamps.
func (j *RecordingJob) TransitionState(newState RecordingState, errDetail string) (*RecordingJob, error) {
	if err := j.CanTransitionTo(newState); err != nil {
		return nil, err
	}
	cp := *j
	cp.State = newState
	now := time.Now()
	cp.UpdatedAt = now

	if newState == StateCompleted || newState == StatePartial {
		cp.FinishedAt = &now
	} else if newState == StateFailed {
		cp.FailedAt = &now
		if errDetail != "" {
			cp.ErrorDetail = errDetail
		}
	}

	return &cp, nil
}
