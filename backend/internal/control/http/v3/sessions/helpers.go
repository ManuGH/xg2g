// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package sessions

import (
	"context"
	"os"
	"runtime/debug"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/log"
)

const (
	fallbackRestartPollInterval = 50 * time.Millisecond
	fallbackRestartTimeout      = 5 * time.Second
)

// EnsureSessionPlaybackTrace returns the existing playback trace on a session,
// or initializes and returns a new one if nil.
func EnsureSessionPlaybackTrace(session *model.SessionRecord) *model.PlaybackTrace {
	if session.PlaybackTrace == nil {
		session.PlaybackTrace = &model.PlaybackTrace{}
	}
	return session.PlaybackTrace
}

// SessionHasFirstFrameArtifact checks whether the first frame marker exists in the HLS root directory.
func SessionHasFirstFrameArtifact(hlsRoot, sessionID string) bool {
	if !model.IsSafeSessionID(sessionID) {
		return false
	}
	markerPath := model.SessionFirstFrameMarkerPath(hlsRoot, sessionID)
	if markerPath == "" {
		return false
	}
	info, err := os.Stat(markerPath)
	return err == nil && !info.IsDir()
}

// SessionFirstFrameUnix returns the modification timestamp of the first frame marker.
func SessionFirstFrameUnix(hlsRoot, sessionID string) int64 {
	if !model.IsSafeSessionID(sessionID) {
		return 0
	}
	markerPath := model.SessionFirstFrameMarkerPath(hlsRoot, sessionID)
	if markerPath == "" {
		return 0
	}
	info, err := os.Stat(markerPath)
	if err != nil || info.IsDir() {
		return 0
	}
	return info.ModTime().Unix()
}

// ScheduleFallbackRestart schedules a detached goroutine to restart a session following a fallback stop.
func (s *Service) ScheduleFallbackRestart(sess *model.SessionRecord) {
	s.ScheduleSessionRestart(sess)
}

// ScheduleSessionRestart schedules a detached goroutine that waits for a session to reach a terminal state
// before publishing a start event with its new profile.
func (s *Service) ScheduleSessionRestart(sess *model.SessionRecord) {
	if s == nil || s.deps == nil || sess == nil {
		return
	}
	eventBus := s.deps.Bus()
	store := s.deps.SessionStore()
	if eventBus == nil || store == nil {
		return
	}

	sessionID := sess.SessionID
	serviceRef := sess.ServiceRef
	correlationID := sess.CorrelationID
	restartCtx := s.deps.RuntimeContext()
	if restartCtx == nil {
		restartCtx = context.Background()
	}

	go func() {
		// Detached, long-lived goroutine on the live path: a panic here would
		// otherwise take down the whole daemon. The bus is now panic-safe; this
		// is defence-in-depth for any future fault in the restart path.
		defer func() {
			if r := recover(); r != nil {
				log.L().Error().
					Interface("panic", r).
					Str("sessionId", sessionID).
					Str("stack", string(debug.Stack())).
					Msg("session restart goroutine recovered from panic")
			}
		}()

		ctx, cancel := context.WithTimeout(restartCtx, fallbackRestartTimeout)
		defer cancel()

		if err := WaitForTerminalSession(ctx, store, sessionID); err != nil {
			log.L().Error().Err(err).Str("sessionId", sessionID).Msg("failed to observe terminal state before session restart")
			return
		}

		startEvt := model.StartSessionEvent{
			Type:          model.EventStartSession,
			SessionID:     sessionID,
			ServiceRef:    serviceRef,
			ProfileID:     sess.Profile.Name,
			CorrelationID: correlationID,
			RequestedAtUN: time.Now().Unix(),
		}

		if err := eventBus.Publish(ctx, string(model.EventStartSession), startEvt); err != nil {
			log.L().Error().Err(err).Str("sessionId", sessionID).Msg("failed to publish session restart event")
		}
	}()
}

// WaitForTerminalSession polls until the specified session reaches a terminal state or context expires.
func WaitForTerminalSession(ctx context.Context, store SessionStore, sessionID string) error {
	ticker := time.NewTicker(fallbackRestartPollInterval)
	defer ticker.Stop()

	for {
		sess, err := store.GetSession(ctx, sessionID)
		if err != nil {
			return err
		}
		if sess == nil || sess.State.IsTerminal() {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
