// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package recording

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrInvalidStateTransition = errors.New("invalid recording state transition")
	ErrJobAlreadyFinished     = errors.New("recording job is already in terminal state")
)

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
	StateFailed        RecordingState = "FAILED"
)

// IsTerminal returns true if the recording state is a final unchangeable state.
func (s RecordingState) IsTerminal() bool {
	return s == StateCompleted || s == StateFailed
}

// RecordingJob represents an active or historic recording within the Unified Recording Storage Architecture.
type RecordingJob struct {
	mu              sync.RWMutex     `json:"-"`
	ID              string           `json:"id"`
	ServiceRef      string           `json:"service_ref"`
	EventID         string           `json:"event_id,omitempty"`
	Title           string           `json:"title"`
	SourceType      RecordingSource  `json:"source_type"`
	RequestedStart  time.Time        `json:"requested_start"`
	RequestedEnd    time.Time        `json:"requested_end"`
	ActualStart     time.Time        `json:"actual_start,omitempty"`
	ActualEnd       time.Time        `json:"actual_end,omitempty"`
	TargetBackendID string           `json:"target_backend_id"`
	LocalFallbackID string           `json:"local_fallback_id"`
	State           RecordingState   `json:"state"`
	LocalStagingPath string          `json:"local_staging_path,omitempty"`
	FinalizedPath   string           `json:"finalized_path,omitempty"`
	ErrorDetail     string           `json:"error_detail,omitempty"`
}

// NewRecordingJob creates a new RecordingJob initialized in PENDING state.
func NewRecordingJob(id, serviceRef, title string, source RecordingSource, reqStart, reqEnd time.Time, targetBackendID string) *RecordingJob {
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
	}
}

// TransitionTo updates the job state if the requested transition is valid.
func (j *RecordingJob) TransitionTo(newState RecordingState, errDetail string) error {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.State.IsTerminal() {
		return fmt.Errorf("%w: current state is %s", ErrJobAlreadyFinished, j.State)
	}

	valid := false
	switch j.State {
	case StatePending:
		valid = (newState == StatePreparing || newState == StateFailed)
	case StatePreparing:
		valid = (newState == StateRecording || newState == StateStaging || newState == StateFailed)
	case StateRecording:
		valid = (newState == StateStaging || newState == StateFailed)
	case StateStaging:
		valid = (newState == StateFinalizing || newState == StateFailed)
	case StateFinalizing:
		valid = (newState == StateTransferring || newState == StateCompleted || newState == StateWaitingTarget || newState == StateFailed)
	case StateTransferring:
		valid = (newState == StateCompleted || newState == StateWaitingTarget || newState == StateFailed)
	case StateWaitingTarget:
		valid = (newState == StateTransferring || newState == StateCompleted || newState == StateFailed)
	}

	if !valid {
		return fmt.Errorf("%w: cannot transition from %s to %s", ErrInvalidStateTransition, j.State, newState)
	}

	j.State = newState
	if errDetail != "" {
		j.ErrorDetail = errDetail
	}
	if newState == StateRecording && j.ActualStart.IsZero() {
		j.ActualStart = time.Now()
	}
	if (newState == StateCompleted || newState == StateFailed) && j.ActualEnd.IsZero() {
		j.ActualEnd = time.Now()
	}

	return nil
}

// CurrentState returns the current state under read lock.
func (j *RecordingJob) CurrentState() RecordingState {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.State
}
