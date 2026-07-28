// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lifecycle

import (
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

// ApplyFallbackRestart records the explicit recovery edge used when a running
// session must restart with a fallback profile.
func ApplyFallbackRestart(rec *model.SessionRecord, now time.Time) {
	ApplyTransition(rec, Transition{
		From:  rec.State,
		To:    model.SessionStarting,
		Event: EvRecoveryReset,
	}, now)
}

// ApplyRepeatedStopRequest enriches an already-stopping session without
// creating a second lifecycle transition.
func ApplyRepeatedStopRequest(rec *model.SessionRecord, reason model.ReasonCode, stopReason string, now time.Time) {
	if rec.Reason == "" && reason != "" {
		rec.Reason = reason
	}
	if rec.StopReason == "" && stopReason != "" {
		rec.StopReason = stopReason
	}
	rec.PipelineState = model.PipeStopRequested
	rec.UpdatedAtUnix = now.Unix()
}

// ResetForFallbackRestart reopens a terminal fallback session at the canonical
// new-session baseline while preserving its original creation timestamp.
func ResetForFallbackRestart(rec *model.SessionRecord, now time.Time) {
	baseline := NewSessionRecord(now)
	createdAtUnix := rec.CreatedAtUnix

	rec.State = baseline.State
	rec.PipelineState = baseline.PipelineState
	rec.Reason = baseline.Reason
	rec.ReasonDetailCode = baseline.ReasonDetailCode
	rec.ReasonDetailDebug = ""
	rec.UpdatedAtUnix = baseline.UpdatedAtUnix
	if createdAtUnix > 0 {
		rec.CreatedAtUnix = createdAtUnix
	} else {
		rec.CreatedAtUnix = baseline.CreatedAtUnix
	}
	rec.LastAccessUnix = 0
	rec.LastHeartbeatUnix = 0
	rec.StopReason = ""
	rec.LatestSegmentAt = time.Time{}
	rec.LastPlaylistAccessAt = time.Time{}
	rec.PlaylistPublishedAt = time.Time{}
}

// NewReadySessionRecord constructs a record through the canonical lifecycle
// edges used when adopting an already-serving orphan process.
func NewReadySessionRecord(now time.Time) (*model.SessionRecord, error) {
	rec := NewSessionRecord(now)
	for _, event := range []EventKind{EvStartRequested, EvPrimingStarted, EvReady} {
		if _, err := Dispatch(rec, PhaseFromState(rec.State), Event{Kind: event}, nil, false, now); err != nil {
			return nil, err
		}
	}
	return rec, nil
}
