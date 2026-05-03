package v3

import (
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

const (
	playbackStartupSoftWarningWarmup = 15 * time.Second
	sessionRuntimeStartupWarmup      = 20 * time.Second
)

func sessionPlaybackStartupAnchor(session *model.SessionRecord, hlsRoot string) time.Time {
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
		firstFrameAtUnix = sessionFirstFrameUnix(hlsRoot, session.SessionID)
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

func sessionPlaybackStartupWarmupUntil(session *model.SessionRecord, hlsRoot string, duration time.Duration) time.Time {
	if duration <= 0 {
		return time.Time{}
	}
	anchor := sessionPlaybackStartupAnchor(session, hlsRoot)
	if anchor.IsZero() {
		return time.Time{}
	}
	return anchor.Add(duration)
}

func sessionInPlaybackStartupWarmup(session *model.SessionRecord, hlsRoot string, duration time.Duration, now time.Time) bool {
	until := sessionPlaybackStartupWarmupUntil(session, hlsRoot, duration)
	return !until.IsZero() && now.UTC().Before(until)
}

func isSoftStartupPlaybackWarning(req PlaybackFeedbackRequest) bool {
	if req.Event != PlaybackFeedbackRequestEventWarning {
		return false
	}
	switch derefInt(req.Code) {
	case 101, 102, 104:
		return true
	default:
		return false
	}
}
