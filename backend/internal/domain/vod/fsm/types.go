// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package fsm

import (
	"errors"
	"fmt"
	"time"
)

// State represents the lifecycle state of a VOD artifact.
type State string

const (
	StatePreparing State = "PREPARING"
	StateReady     State = "READY"
	StateFailed    State = "FAILED"
	StateDeleted   State = "DELETED"
)

// Event represents an action that triggers a state transition.
type Event string

const (
	EventCreateArtifact Event = "CreateArtifact"
	EventCompleteBuild  Event = "CompleteBuild"
	EventFailBuild      Event = "FailBuild"
	EventRetryBuild     Event = "RetryBuild"
	EventEvictArtifact  Event = "EvictArtifact"
)

var (
	ErrInvalidStateTransition = errors.New("invalid artifact state transition")
	ErrArtifactNotFound       = errors.New("artifact not found")
)

// InvalidTransitionError represents a detailed state transition failure.
type InvalidTransitionError struct {
	FromState State
	Event     Event
	Reason    string
}

func (e *InvalidTransitionError) Error() string {
	return fmt.Sprintf("cannot trigger event %s from state %s: %s", e.Event, e.FromState, e.Reason)
}

func (e *InvalidTransitionError) Is(target error) bool {
	return target == ErrInvalidStateTransition
}

// Artifact represents the core domain artifact entity.
type Artifact struct {
	ID             string    `json:"id"`
	RecordingRef   string    `json:"recordingRef"`
	VariantHash    string    `json:"variantHash"`
	State          State     `json:"state"`
	FailureReason  string    `json:"failureReason,omitempty"`
	ManifestPath   string    `json:"manifestPath"`
	SegmentPattern string    `json:"segmentPattern"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}
