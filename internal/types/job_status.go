// SPDX-License-Identifier: MIT

// Package types provides type-safe enumerations and constants for xg2g.
//
// This package centralizes all typed constants, enums, and state types
// to prevent string-based bugs and improve code maintainability.
package types

import (
	"encoding/json"
	"fmt"
)

// JobStatus represents the current state of a background job.
//
// JobStatus provides type safety for job state management, preventing
// string-based typos and enabling exhaustive switch statements.
type JobStatus string

// Job status constants define all possible states of a background job.
const (
	// JobStatusPending indicates the job is queued but not yet started.
	JobStatusPending JobStatus = "pending"

	// JobStatusRunning indicates the job is currently executing.
	JobStatusRunning JobStatus = "running"

	// JobStatusCompleted indicates the job finished successfully.
	JobStatusCompleted JobStatus = "completed"

	// JobStatusFailed indicates the job encountered an error and terminated.
	JobStatusFailed JobStatus = "failed"

	// JobStatusCancelled indicates the job was manually cancelled.
	JobStatusCancelled JobStatus = "cancelled"
)

// String returns the string representation of the job status.
// Implements the fmt.Stringer interface for better logging and debugging.
func (s JobStatus) String() string {
	return string(s)
}

// IsValid checks whether the job status is one of the defined constants.
//
// Returns true if the status is valid, false otherwise.
//
// Example:
//
//	status := JobStatus("running")
//	if status.IsValid() {
//	    // Safe to use
//	}
func (s JobStatus) IsValid() bool {
	switch s {
	case JobStatusPending, JobStatusRunning, JobStatusCompleted, JobStatusFailed, JobStatusCancelled:
		return true
	default:
		return false
	}
}

// IsTerminal checks whether the job status represents a final state.
//
// Terminal states include: Completed, Failed, Cancelled.
// A job in a terminal state will not transition to another state.
//
// Example:
//
//	if status.IsTerminal() {
//	    // Job is done, cleanup resources
//	}
func (s JobStatus) IsTerminal() bool {
	switch s {
	case JobStatusCompleted, JobStatusFailed, JobStatusCancelled:
		return true
	default:
		return false
	}
}

// CanTransitionTo checks whether this status can transition to the target status.
//
// Valid transitions:
//   - Pending → Running, Cancelled
//   - Running → Completed, Failed, Cancelled
//   - Terminal states cannot transition
//
// Example:
//
//	current := JobStatusPending
//	if current.CanTransitionTo(JobStatusRunning) {
//	    // Safe to transition
//	}
func (s JobStatus) CanTransitionTo(target JobStatus) bool {
	// Terminal states cannot transition
	if s.IsTerminal() {
		return false
	}

	switch s {
	case JobStatusPending:
		return target == JobStatusRunning || target == JobStatusCancelled
	case JobStatusRunning:
		return target == JobStatusCompleted || target == JobStatusFailed || target == JobStatusCancelled
	default:
		return false
	}
}

// MarshalJSON implements json.Marshaler for JobStatus.
func (s JobStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(s))
}

// UnmarshalJSON implements json.Unmarshaler for JobStatus.
func (s *JobStatus) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}

	status := JobStatus(str)
	if !status.IsValid() {
		return fmt.Errorf("invalid job status: %q", str)
	}

	*s = status
	return nil
}

// ParseJobStatus parses a string into a JobStatus, returning an error if invalid.
//
// Example:
//
//	status, err := ParseJobStatus("running")
//	if err != nil {
//	    // Handle invalid status
//	}
func ParseJobStatus(s string) (JobStatus, error) {
	status := JobStatus(s)
	if !status.IsValid() {
		return "", fmt.Errorf("invalid job status: %q (valid: pending, running, completed, failed, cancelled)", s)
	}
	return status, nil
}

// AllJobStatuses returns all defined job statuses.
//
// Useful for validation, documentation, and UI enumeration.
func AllJobStatuses() []JobStatus {
	return []JobStatus{
		JobStatusPending,
		JobStatusRunning,
		JobStatusCompleted,
		JobStatusFailed,
		JobStatusCancelled,
	}
}
