// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package fsm

import (
	"fmt"
	"strings"
	"time"
)

// NewArtifact initializes a new artifact in the PREPARING state.
func NewArtifact(id, recordingRef, variantHash, manifestPath, segmentPattern string, now time.Time) (*Artifact, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("artifact ID cannot be empty")
	}
	if strings.TrimSpace(recordingRef) == "" {
		return nil, fmt.Errorf("recording ref cannot be empty")
	}
	if strings.TrimSpace(variantHash) == "" {
		return nil, fmt.Errorf("variant hash cannot be empty")
	}
	if now.IsZero() {
		now = time.Now()
	}

	return &Artifact{
		ID:             id,
		RecordingRef:   recordingRef,
		VariantHash:    variantHash,
		State:          StatePreparing,
		ManifestPath:   manifestPath,
		SegmentPattern: segmentPattern,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

// CompleteBuild transitions an artifact from PREPARING to READY.
func CompleteBuild(a *Artifact, now time.Time) error {
	if a == nil {
		return ErrArtifactNotFound
	}
	if a.State != StatePreparing {
		return &InvalidTransitionError{
			FromState: a.State,
			Event:     EventCompleteBuild,
			Reason:    "only artifacts in PREPARING state can complete build",
		}
	}
	if now.IsZero() {
		now = time.Now()
	}

	a.State = StateReady
	a.FailureReason = ""
	a.UpdatedAt = now
	return nil
}

// FailBuild transitions an artifact from PREPARING to FAILED.
func FailBuild(a *Artifact, reason string, now time.Time) error {
	if a == nil {
		return ErrArtifactNotFound
	}
	if a.State != StatePreparing {
		return &InvalidTransitionError{
			FromState: a.State,
			Event:     EventFailBuild,
			Reason:    "only artifacts in PREPARING state can fail build",
		}
	}
	if now.IsZero() {
		now = time.Now()
	}

	a.State = StateFailed
	a.FailureReason = reason
	a.UpdatedAt = now
	return nil
}

// RetryBuild transitions an artifact from FAILED back to PREPARING for a retry attempt.
func RetryBuild(a *Artifact, now time.Time) error {
	if a == nil {
		return ErrArtifactNotFound
	}
	if a.State != StateFailed {
		return &InvalidTransitionError{
			FromState: a.State,
			Event:     EventRetryBuild,
			Reason:    "only artifacts in FAILED state can be retried",
		}
	}
	if now.IsZero() {
		now = time.Now()
	}

	a.State = StatePreparing
	a.FailureReason = ""
	a.UpdatedAt = now
	return nil
}

// EvictArtifact transitions an artifact from READY or FAILED to DELETED.
func EvictArtifact(a *Artifact, now time.Time) error {
	if a == nil {
		return ErrArtifactNotFound
	}
	if a.State != StateReady && a.State != StateFailed {
		return &InvalidTransitionError{
			FromState: a.State,
			Event:     EventEvictArtifact,
			Reason:    "only READY or FAILED artifacts can be evicted",
		}
	}
	if now.IsZero() {
		now = time.Now()
	}

	a.State = StateDeleted
	a.UpdatedAt = now
	return nil
}
