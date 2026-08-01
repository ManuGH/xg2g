// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package recording

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrInvalidAssetStateTransition = errors.New("invalid recording asset state transition")
	ErrAssetAlreadyFinished        = errors.New("recording asset is in terminal state")
)

// AssetState defines the authoritative lifecycle of permanent media library assets.
type AssetState string

const (
	AssetInProgress      AssetState = "IN_PROGRESS"
	AssetTransferPending AssetState = "TRANSFER_PENDING"
	AssetAvailable       AssetState = "AVAILABLE"
	AssetOffline         AssetState = "OFFLINE"
	AssetMissing         AssetState = "MISSING"
	AssetCorrupt         AssetState = "CORRUPT"
)

// AssetCompleteness documents the media completeness status of a recording asset.
type AssetCompleteness string

const (
	AssetComplete       AssetCompleteness = "COMPLETE"
	AssetPartialAtStart AssetCompleteness = "PARTIAL_AT_START"
	AssetPartialAtEnd   AssetCompleteness = "PARTIAL_AT_END"
	AssetPartialAtBoth  AssetCompleteness = "PARTIAL_AT_BOTH"
	AssetGapped         AssetCompleteness = "GAPPED"
	AssetInterrupted    AssetCompleteness = "INTERRUPTED"
	AssetUnknown        AssetCompleteness = "UNKNOWN"
)

// RecordingAsset is the permanent, versioned domain model for media library entries.
type RecordingAsset struct {
	ID                  string            `json:"id"`
	JobID               string            `json:"job_id"`
	ProfileID           string            `json:"profile_id,omitempty"`
	Title               string            `json:"title"`
	ServiceRef          string            `json:"service_ref"`
	EventID             string            `json:"event_id,omitempty"`
	State               AssetState        `json:"state"`
	BackendID           string            `json:"backend_id"`
	ObjectKey           string            `json:"object_key"` // Relative path inside storage backend
	Container           ContainerFormat   `json:"container"`  // "ts", "mp4"
	VideoCodec          string            `json:"video_codec"`
	AudioCodecs         []string          `json:"audio_codecs"`
	DurationSeconds     int               `json:"duration_seconds"`
	SizeBytes           int64             `json:"size_bytes"`
	RecordedStart       time.Time         `json:"recorded_start"`
	RecordedEnd         time.Time         `json:"recorded_end"`
	FinalizedAt         *time.Time        `json:"finalized_at,omitempty"`
	Completeness        AssetCompleteness `json:"completeness"`
	GapCount            int               `json:"gap_count"`
	MissingStartSeconds int               `json:"missing_start_seconds"`
	MissingEndSeconds   int               `json:"missing_end_seconds"`
	Version             uint64            `json:"version"`
	CreatedAt           time.Time         `json:"created_at"`
	UpdatedAt           time.Time         `json:"updated_at"`
}

// NewRecordingAsset initializes a new RecordingAsset in IN_PROGRESS state.
func NewRecordingAsset(id, jobID, title, serviceRef, backendID, objectKey string, format ContainerFormat) (*RecordingAsset, error) {
	if id == "" {
		return nil, fmt.Errorf("asset ID cannot be empty")
	}
	now := time.Now()
	return &RecordingAsset{
		ID:           id,
		JobID:        jobID,
		Title:        title,
		ServiceRef:   serviceRef,
		State:        AssetInProgress,
		BackendID:    backendID,
		ObjectKey:    objectKey,
		Container:    format,
		Completeness: AssetUnknown,
		Version:      1,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

// CanTransitionTo validates if an asset state transition is legal.
func (a *RecordingAsset) CanTransitionTo(newState AssetState) error {
	valid := false
	switch a.State {
	case AssetInProgress:
		valid = (newState == AssetTransferPending || newState == AssetAvailable || newState == AssetCorrupt)
	case AssetTransferPending:
		valid = (newState == AssetAvailable || newState == AssetOffline || newState == AssetMissing || newState == AssetCorrupt)
	case AssetAvailable:
		valid = (newState == AssetOffline || newState == AssetMissing || newState == AssetCorrupt)
	case AssetOffline:
		valid = (newState == AssetAvailable || newState == AssetMissing || newState == AssetCorrupt)
	case AssetMissing:
		valid = (newState == AssetAvailable || newState == AssetOffline || newState == AssetCorrupt)
	case AssetCorrupt:
		valid = false
	}

	if !valid {
		return fmt.Errorf("%w: cannot transition asset from %s to %s", ErrInvalidAssetStateTransition, a.State, newState)
	}

	return nil
}

// TransitionState creates an immutable clone of the asset with the updated state without mutating the receiver directly.
func (a *RecordingAsset) TransitionState(newState AssetState) (*RecordingAsset, error) {
	if err := a.CanTransitionTo(newState); err != nil {
		return nil, err
	}
	cp := a.Clone()
	cp.State = newState
	cp.UpdatedAt = time.Now()
	return cp, nil
}

// Clone creates a deep copy of RecordingAsset.
func (a *RecordingAsset) Clone() *RecordingAsset {
	if a == nil {
		return nil
	}
	cp := *a
	if a.AudioCodecs != nil {
		cp.AudioCodecs = make([]string, len(a.AudioCodecs))
		copy(cp.AudioCodecs, a.AudioCodecs)
	}
	if a.FinalizedAt != nil {
		t := *a.FinalizedAt
		cp.FinalizedAt = &t
	}
	return &cp
}
