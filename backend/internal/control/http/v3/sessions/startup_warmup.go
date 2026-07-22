// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package sessions

import (
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

const (
	PlaybackStartupSoftWarningWarmup = 15 * time.Second
	SessionRuntimeStartupWarmup      = 20 * time.Second
)

// SessionPlaybackStartupAnchor determines the reference starting timestamp of a playback session.
func SessionPlaybackStartupAnchor(session *model.SessionRecord, hlsRoot string) time.Time {
	if session == nil {
		return time.Time{}
	}

	anchor := time.Time{}
	if !session.PlaylistPublishedAt.IsZero() {
		anchor = session.PlaylistPublishedAt.UTC()
	}

	firstFrameAtUnix := int64(0)
	if session.PlaybackTrace != nil && session.PlaybackTrace.FirstFrameAtUnix > 0 {
		firstFrameAtUnix = session.PlaybackTrace.FirstFrameAtUnix
	} else {
		firstFrameAtUnix = SessionFirstFrameUnix(hlsRoot, session.SessionID)
	}
	if firstFrameAtUnix > 0 {
		firstFrameAt := time.Unix(firstFrameAtUnix, 0).UTC()
		if firstFrameAt.After(anchor) {
			anchor = firstFrameAt
		}
	}

	if anchor.IsZero() && session.CreatedAtUnix > 0 {
		anchor = time.Unix(session.CreatedAtUnix, 0).UTC()
	}
	return anchor
}

// SessionPlaybackStartupWarmupUntil calculates the end time of the warmup period.
func SessionPlaybackStartupWarmupUntil(session *model.SessionRecord, hlsRoot string, duration time.Duration) time.Time {
	if duration <= 0 {
		return time.Time{}
	}
	anchor := SessionPlaybackStartupAnchor(session, hlsRoot)
	if anchor.IsZero() {
		return time.Time{}
	}
	return anchor.Add(duration)
}

// SessionInPlaybackStartupWarmup checks if the session is currently within its warmup window.
func SessionInPlaybackStartupWarmup(session *model.SessionRecord, hlsRoot string, duration time.Duration, now time.Time) bool {
	until := SessionPlaybackStartupWarmupUntil(session, hlsRoot, duration)
	return !until.IsZero() && now.UTC().Before(until)
}
