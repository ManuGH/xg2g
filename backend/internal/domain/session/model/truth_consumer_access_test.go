// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package model

import (
	"testing"
	"time"
)

// TestConsumerSegmentAccessKeepsSessionActive guards against the idle sweeper
// stopping active VOD/recording playback. Such players fetch the (complete)
// playlist once and then pull only segments, so the playlist-access age goes stale
// while the consumer is still actively streaming. The consumer's segment fetches
// must count as liveness.
func TestConsumerSegmentAccessKeepsSessionActive(t *testing.T) {
	now := time.Now()
	rec := &SessionRecord{
		State:                SessionReady,
		PipelineState:        PipeServing,
		LastPlaylistAccessAt: now.Add(-2 * time.Minute), // playlist fetched once, long ago
		PlaylistPublishedAt:  now.Add(-3 * time.Minute),
		LatestSegmentAt:      now.Add(-1 * time.Second), // producer still writing
		PlaybackTrace: &PlaybackTrace{
			HLS: &HLSAccessTrace{
				LastSegmentAtUnix: now.Add(-2 * time.Second).UnixMilli(), // consumer pulled a segment 2s ago
			},
		},
	}

	if PlaylistAccessExceeded(rec, now, IdleThreshold) {
		t.Errorf("session actively pulling segments was marked idle (playlist access went stale)")
	}
	if got := DeriveLifecycleState(rec, now); got != LifecycleActive {
		t.Errorf("DeriveLifecycleState = %q, want %q for an actively-segment-streaming session", got, LifecycleActive)
	}

	// Control: with neither playlist nor segment access fresh, it must still go idle.
	idleRec := &SessionRecord{
		State:                SessionReady,
		PipelineState:        PipeServing,
		LastPlaylistAccessAt: now.Add(-2 * time.Minute),
		PlaylistPublishedAt:  now.Add(-3 * time.Minute),
		PlaybackTrace: &PlaybackTrace{
			HLS: &HLSAccessTrace{LastSegmentAtUnix: now.Add(-2 * time.Minute).UnixMilli()},
		},
	}
	if !PlaylistAccessExceeded(idleRec, now, IdleThreshold) {
		t.Errorf("session with no recent consumer access should be idle")
	}
}
