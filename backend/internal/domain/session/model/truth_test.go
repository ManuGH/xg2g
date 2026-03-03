// Copyright (c) 2025 ManuGH

package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDeriveLifecycleState(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		record   *SessionRecord
		expected LifecycleState
	}{
		{
			name: "Terminal FAILED -> error",
			record: &SessionRecord{
				State: SessionFailed,
			},
			expected: LifecycleError,
		},
		{
			name: "Stopping -> ending",
			record: &SessionRecord{
				State: SessionStopping,
			},
			expected: LifecycleEnding,
		},
		{
			name: "New -> starting",
			record: &SessionRecord{
				State: SessionNew,
			},
			expected: LifecycleStarting,
		},
		{
			name: "Starting -> starting",
			record: &SessionRecord{
				State: SessionStarting,
			},
			expected: LifecycleStarting,
		},
		{
			name: "Priming -> buffering",
			record: &SessionRecord{
				State: SessionPriming,
			},
			expected: LifecycleBuffering,
		},
		{
			name: "Healthy running, playlist published, but no segments yet -> buffering",
			record: &SessionRecord{
				State:               SessionReady,
				PipelineState:       PipeFFmpegRunning,
				PlaylistPublishedAt: now.Add(-1 * time.Second),
				LatestSegmentAt:     time.Time{}, // Zero
			},
			expected: LifecycleBuffering,
		},
		{
			name: "Active: segments fresh, access fresh",
			record: &SessionRecord{
				State:                SessionReady,
				PipelineState:        PipeServing,
				PlaylistPublishedAt:  now.Add(-60 * time.Second),
				LatestSegmentAt:      now.Add(-5 * time.Second),
				LastPlaylistAccessAt: now.Add(-2 * time.Second),
			},
			expected: LifecycleActive,
		},
		{
			name: "Active (Grace Period): segments fresh, access zero (newly published)",
			record: &SessionRecord{
				State:                SessionReady,
				PipelineState:        PipeServing,
				PlaylistPublishedAt:  now.Add(-5 * time.Second),
				LatestSegmentAt:      now.Add(-2 * time.Second),
				LastPlaylistAccessAt: time.Time{}, // Never accessed yet
			},
			expected: LifecycleActive,
		},
		{
			name: "Idle: segments fresh, access old (>30s)",
			record: &SessionRecord{
				State:                SessionReady,
				PipelineState:        PipeServing,
				PlaylistPublishedAt:  now.Add(-120 * time.Second),
				LatestSegmentAt:      now.Add(-5 * time.Second),
				LastPlaylistAccessAt: now.Add(-31 * time.Second),
			},
			expected: LifecycleIdle,
		},
		{
			name: "Idle (Zero Access + Post Grace): segments fresh, access zero, published old",
			record: &SessionRecord{
				State:                SessionReady,
				PipelineState:        PipeServing,
				PlaylistPublishedAt:  now.Add(-60 * time.Second),
				LatestSegmentAt:      now.Add(-5 * time.Second),
				LastPlaylistAccessAt: time.Time{},
			},
			expected: LifecycleIdle,
		},
		{
			name: "Stalled: segments seen but old (>12s)",
			record: &SessionRecord{
				State:                SessionReady,
				PipelineState:        PipeServing,
				PlaylistPublishedAt:  now.Add(-60 * time.Second),
				LatestSegmentAt:      now.Add(-13 * time.Second),
				LastPlaylistAccessAt: now.Add(-2 * time.Second),
			},
			expected: LifecycleStalled,
		},
		{
			name: "Stalled wins over Idle",
			record: &SessionRecord{
				State:                SessionReady,
				PipelineState:        PipeServing,
				PlaylistPublishedAt:  now.Add(-60 * time.Second),
				LatestSegmentAt:      now.Add(-13 * time.Second),
				LastPlaylistAccessAt: now.Add(-31 * time.Second),
			},
			expected: LifecycleStalled,
		},
		{
			name: "Ending wins over Stalled",
			record: &SessionRecord{
				State:                SessionDraining,
				PipelineState:        PipeServing,
				PlaylistPublishedAt:  now.Add(-60 * time.Second),
				LatestSegmentAt:      now.Add(-13 * time.Second),
				LastPlaylistAccessAt: now.Add(-2 * time.Second),
			},
			expected: LifecycleEnding,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := DeriveLifecycleState(tt.record, now)
			assert.Equal(t, tt.expected, actual)
		})
	}
}
