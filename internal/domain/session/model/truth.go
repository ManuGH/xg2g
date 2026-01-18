// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package model

import "time"

const (
	StalledThreshold = 12 * time.Second
	IdleThreshold    = 30 * time.Second
)

// DeriveLifecycleState determines the semantic lifecycle state of a session
// based on absolute truths (process, files, access).
//
// Logic Protocol (Strict Priority):
// 1. error    - Terminal failure (FAILED, CANCELLED, STOPPED)
// 2. ending   - In-progress shutdown (DRAINING, STOPPING)
// 3. starting - Early initialization (NEW, STARTING)
// 4. buffering- Process running but not yet playable (PRIMING or Missing Segments)
// 5. stalled  - Segments were being produced but stopped unexpectedly (> 12s)
// 6. idle     - Stream is healthy but has no active manifest access (> 30s)
// 7. active   - Healthy stream with active manifest access
func DeriveLifecycleState(r *SessionRecord, now time.Time) LifecycleState {
	// 1. Error (Terminal)
	if r.State == SessionFailed || r.State == SessionCancelled || r.State == SessionStopped {
		return LifecycleError
	}

	// 2. Ending
	if r.State == SessionDraining || r.State == SessionStopping {
		return LifecycleEnding
	}

	// 3. Starting
	if r.State == SessionNew || r.State == SessionStarting {
		return LifecycleStarting
	}

	// Constants for threshold checks
	segmentsFresh := !r.LatestSegmentAt.IsZero() && now.Sub(r.LatestSegmentAt) <= StalledThreshold
	accessFresh := !r.LastPlaylistAccessAt.IsZero() && now.Sub(r.LastPlaylistAccessAt) <= IdleThreshold

	// Handle case where session hasn't been accessed yet (treat as fresh for a grace period)
	if r.LastPlaylistAccessAt.IsZero() {
		// If it's a fresh session (just started), it's not "idle" yet.
		// We use PlaylistPublishedAt as a reference for grace period.
		if !r.PlaylistPublishedAt.IsZero() && now.Sub(r.PlaylistPublishedAt) <= IdleThreshold {
			accessFresh = true
		} else if r.PlaylistPublishedAt.IsZero() {
			// Not published yet -> not active/idle anyway
			accessFresh = true
		}
	}

	// 4. Buffering
	// Rule: Priming state OR (FFmpeg running + Playlist up BUT no segments yet)
	isFFmpegRunning := r.PipelineState == PipeFFmpegRunning || r.PipelineState == PipePackagerReady || r.PipelineState == PipeServing
	playlistPublished := !r.PlaylistPublishedAt.IsZero()

	if r.State == SessionPriming || (isFFmpegRunning && playlistPublished && r.LatestSegmentAt.IsZero()) {
		return LifecycleBuffering
	}

	// 5. Stalled
	// Rule: Stalled if we ever saw segments, but they are now too old.
	if !r.LatestSegmentAt.IsZero() && now.Sub(r.LatestSegmentAt) > StalledThreshold {
		return LifecycleStalled
	}

	// 6. Idle
	// Rule: Segments are fresh, but playlist hasn't been polled in > 30s AND we aren't in grace period.
	if segmentsFresh && !accessFresh {
		return LifecycleIdle
	}

	// 7. Active
	// Fallback: If segments are fresh and access is fresh (or in grace period).
	if segmentsFresh && accessFresh {
		return LifecycleActive
	}

	// Catch-all: If none of the above match, default to buffering (safest)
	return LifecycleBuffering
}
