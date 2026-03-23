// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package manager

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/log"
	sqliteutil "github.com/ManuGH/xg2g/internal/persistence/sqlite"
	"github.com/rs/zerolog"
)

const (
	heartbeatPollInterval           = 2 * time.Second
	heartbeatStoreUpdateMaxAttempts = 3
	heartbeatStoreRetryBaseBackoff  = 75 * time.Millisecond
)

// SegmentHeartbeatSource defines the interface for monitoring stream activity.
// This allows switching from FS-polling to HLS-native event tracking in the future (P3-4).
type SegmentHeartbeatSource interface {
	// LatestSegmentAt returns the modification time of the latest segment for a session.
	// Returns (time, found, error).
	LatestSegmentAt(ctx context.Context, sessionID string) (time.Time, bool, error)
}

// FSWatcherHeartbeatSource is an interim implementation that polls the filesystem.
// It is rate-limited and bounded to avoid excessive I/O.
type FSWatcherHeartbeatSource struct {
	HLSRoot string
}

// LatestSegmentAt polls the session directory for the latest .ts or .m4s segment.
func (s *FSWatcherHeartbeatSource) LatestSegmentAt(ctx context.Context, sessionID string) (time.Time, bool, error) {
	sessionDir := filepath.Join(s.HLSRoot, "sessions", sessionID)

	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		if os.IsNotExist(err) {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, err
	}

	var latest time.Time
	found := false

	// Bounded I/O: Iterate but don't recurse. Segments are always in the top level.
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Only check segments
		if !strings.HasPrefix(name, "seg_") && !strings.HasPrefix(name, "stream") {
			continue
		}
		if !strings.HasSuffix(name, ".ts") && !strings.HasSuffix(name, ".m4s") && !strings.HasSuffix(name, ".mp4") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		// Only count finished segments (size > 0)
		if info.Size() > 0 {
			if !found || info.ModTime().After(latest) {
				latest = info.ModTime()
				found = true
			}
		}
	}

	return latest, found, nil
}

// StartHeartbeatMonitor starts a background loop to poll the heartbeat source.
// It updates the session record in the store.
func (o *Orchestrator) startHeartbeatMonitor(ctx context.Context, sessionID string) {
	if o.HeartbeatSource == nil {
		return
	}

	// Rate-limited: 2s interval (as recommended by CTO "no busy loop", enough for 1s segments)
	ticker := time.NewTicker(heartbeatPollInterval)
	defer ticker.Stop()

	logger := log.L().With().Str("sid", sessionID).Str("monitor", "heartbeat").Logger()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t, found, err := o.HeartbeatSource.LatestSegmentAt(ctx, sessionID)
			if err != nil {
				logger.Warn().Err(err).Msg("heartbeat poll failed")
				continue
			}

			if found {
				if err := o.updateLatestSegmentHeartbeat(ctx, sessionID, t, logger); err != nil {
					logger.Warn().Err(err).Msg("failed to update session heartbeat in store")
				}
			}
		}
	}
}

func (o *Orchestrator) updateLatestSegmentHeartbeat(ctx context.Context, sessionID string, latest time.Time, logger zerolog.Logger) error {
	var err error
	for attempt := 1; attempt <= heartbeatStoreUpdateMaxAttempts; attempt++ {
		_, err = o.Store.UpdateSession(ctx, sessionID, func(r *model.SessionRecord) error {
			if r == nil {
				return nil
			}
			// Only update if it's newer to avoid backwards drift
			if latest.After(r.LatestSegmentAt) {
				r.LatestSegmentAt = latest
			}
			return nil
		})
		if err == nil {
			return nil
		}
		if !sqliteutil.IsBusyRetryable(err) || attempt == heartbeatStoreUpdateMaxAttempts {
			return err
		}

		backoff := heartbeatStoreRetryBaseBackoff * time.Duration(attempt)
		logger.Debug().Err(err).Int("attempt", attempt).Dur("backoff", backoff).Msg("retrying transient sqlite heartbeat contention")
		if err := sleepWithContext(ctx, backoff); err != nil {
			return err
		}
	}
	return err
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
