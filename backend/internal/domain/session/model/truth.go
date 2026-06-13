// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package model

import "time"

const (
	StalledThreshold = 12 * time.Second
	IdleThreshold    = 30 * time.Second
)

// PlaylistAccessAge returns the age of the last confirmed consumer access — a
// playlist GET or a media-segment GET, whichever is more recent. If no consumer
// request happened yet, PlaylistPublishedAt acts as the grace-period anchor.
func PlaylistAccessAge(r *SessionRecord, now time.Time) (time.Duration, bool) {
	if r == nil {
		return 0, false
	}
	if access, ok := lastConsumerAccess(r); ok {
		return now.Sub(access), true
	}
	if !r.PlaylistPublishedAt.IsZero() {
		return now.Sub(r.PlaylistPublishedAt), true
	}
	return 0, false
}

// lastConsumerAccess returns the most recent confirmed consumer access. It counts
// both playlist GETs (LastPlaylistAccessAt) and media-segment GETs (the consumer
// segment-fetch timestamp in the HLS trace). VOD/recording playlists are complete
// (EXT-X-ENDLIST), so a player fetches the playlist once and then pulls only
// segments — without counting segment fetches such a session looks idle after the
// idle threshold and the sweeper stops active playback. The segment timestamp is
// the consumer fetch time (written only on a client segment request, not on
// producer output), so an unwatched live stream still goes idle correctly.
func lastConsumerAccess(r *SessionRecord) (time.Time, bool) {
	latest := r.LastPlaylistAccessAt
	if r.PlaybackTrace != nil && r.PlaybackTrace.HLS != nil && r.PlaybackTrace.HLS.LastSegmentAtUnix > 0 {
		if segAt := time.UnixMilli(r.PlaybackTrace.HLS.LastSegmentAtUnix); segAt.After(latest) {
			latest = segAt
		}
	}
	if latest.IsZero() {
		return time.Time{}, false
	}
	return latest, true
}

// PlaylistAccessFresh reports whether playlist access is still fresh within the given threshold.
// Sessions that never published a playlist yet are treated as fresh so startup does not look idle.
func PlaylistAccessFresh(r *SessionRecord, now time.Time, threshold time.Duration) bool {
	if threshold <= 0 {
		return true
	}
	age, ok := PlaylistAccessAge(r, now)
	if !ok {
		return true
	}
	return age <= threshold
}

// PlaylistAccessExceeded reports whether the playlist access age exceeded the given threshold.
func PlaylistAccessExceeded(r *SessionRecord, now time.Time, threshold time.Duration) bool {
	if threshold <= 0 {
		return false
	}
	age, ok := PlaylistAccessAge(r, now)
	if !ok {
		return false
	}
	return age > threshold
}

func segmentsFresh(r *SessionRecord, now time.Time, threshold time.Duration) bool {
	return r != nil && !r.LatestSegmentAt.IsZero() && now.Sub(r.LatestSegmentAt) <= threshold
}

func segmentsStalled(r *SessionRecord, now time.Time, threshold time.Duration) bool {
	return r != nil && !r.LatestSegmentAt.IsZero() && now.Sub(r.LatestSegmentAt) > threshold
}

// DeriveLifecycleState determines the semantic lifecycle state of a session
// based on absolute truths (process, files, access).
//
// Logic Protocol (Strict Priority):
//  1. error    - Terminal failure (FAILED, CANCELLED, STOPPED)
//  2. ending   - In-progress shutdown (DRAINING, STOPPING)
//  3. starting - Early initialization (NEW, STARTING)
//  4. buffering- Process running but not yet playable (PRIMING or Missing Segments)
//  5. idle     - Stream has no active manifest access (> 30s), regardless of short segment drift
//  6. stalled  - Either an actively consumed stream stopped producing segments (> 12s), or an idle
//     stream stayed abandoned long enough to outlive the idle window plus stall grace
//  7. active   - Healthy stream with active manifest access
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

	hasFreshSegments := segmentsFresh(r, now, StalledThreshold)
	hasStalledSegments := segmentsStalled(r, now, StalledThreshold)
	accessFresh := PlaylistAccessFresh(r, now, IdleThreshold)
	accessAge, hasAccessAge := PlaylistAccessAge(r, now)

	// 4. Buffering
	// Rule: Priming state OR (FFmpeg running + Playlist up BUT no segments yet while
	// playlist access is still within the startup grace window).
	isFFmpegRunning := r.PipelineState == PipeFFmpegRunning || r.PipelineState == PipePackagerReady || r.PipelineState == PipeServing
	playlistPublished := !r.PlaylistPublishedAt.IsZero()

	if r.State == SessionPriming || (isFFmpegRunning && playlistPublished && r.LatestSegmentAt.IsZero() && accessFresh) {
		return LifecycleBuffering
	}

	// 5. Idle
	// Rule: Missing recent playlist access is the primary consumer truth.
	// If access has gone stale, hold the session in idle first. Only escalate to stalled after
	// the idle window plus a segment-stall grace has also elapsed.
	if !accessFresh {
		if hasStalledSegments && hasAccessAge && accessAge > IdleThreshold+StalledThreshold {
			return LifecycleStalled
		}
		return LifecycleIdle
	}

	// 6. Stalled
	// Rule: With an active consumer, stale segments indicate a broken pipeline.
	if hasStalledSegments {
		return LifecycleStalled
	}

	// 7. Active
	// Fallback: If segments are fresh and access is fresh (or in grace period).
	if hasFreshSegments && accessFresh {
		return LifecycleActive
	}

	// Catch-all: If none of the above match, default to buffering (safest)
	return LifecycleBuffering
}
