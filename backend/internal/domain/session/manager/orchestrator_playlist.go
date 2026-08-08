package manager

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	"github.com/ManuGH/xg2g/internal/telemetry"
	"github.com/rs/zerolog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (o *Orchestrator) waitForReady(
	ctx context.Context,
	hbCtx context.Context,
	e model.StartSessionEvent,
	currentProfileSpec model.ProfileSpec,
	handle ports.RunHandle,
	playlistPath string,
	vodMode bool,
	startTime time.Time,
	attempt startupAttempt,
	logger zerolog.Logger,
	ttfpRecorded *bool,
) (ready bool, reason model.ReasonCode, detail string) {
	playlistReadyTimeout := o.playlistReadyTimeout(currentProfileSpec, vodMode, attempt)
	if playlistReadyTimeout <= 0 {
		return false, model.RPackagerFailed, "startup budget exhausted before playlist wait"
	}
	playlistPollInterval := 200 * time.Millisecond
	playlistDeadline := time.Now().Add(playlistReadyTimeout)
	ticker := time.NewTicker(playlistPollInterval)
	defer ticker.Stop()

	logger.Info().
		Str("session_id", e.SessionID).
		Str("service_ref", e.ServiceRef).
		Str("profile", currentProfileSpec.Name).
		Int("attempt", attempt.Index).
		Bool("recovery_mode", o.isRecoveryAttempt(currentProfileSpec, attempt)).
		Dur("timeout", playlistReadyTimeout).
		Dur("profile_timeout", o.profilePlaylistReadyTimeout(currentProfileSpec, vodMode, attempt)).
		Msg("waiting for playlist to become ready")

	var lastNotReadyReason string
	for {
		// Check process health first
		status := o.Pipeline.Health(ctx, handle)
		o.updatePlaybackRuntimeDiagnosticsBestEffort(hbCtx, e.SessionID, status)
		if !status.Healthy {
			return false, model.RProcessEnded, "process died during startup: " + status.Message
		}

		ready, notReadyReason, err := o.checkPlaylistReady(playlistPath, vodMode, ttfpRecorded, e.ProfileID, startTime)
		if err == nil && ready {
			telemetry.GetStartupTracer(e.SessionID).MarkOnce(telemetry.MilestoneT7, "secure_playlist_available")
			return true, "", ""
		}
		if notReadyReason != "" && notReadyReason != lastNotReadyReason {
			logger.Debug().
				Str("session_id", e.SessionID).
				Str("not_ready_reason", notReadyReason).
				Msg("playlist not ready yet")
			lastNotReadyReason = notReadyReason
		}

		if time.Now().After(playlistDeadline) {
			detail := "playlist not ready timeout"
			if lastNotReadyReason != "" {
				detail = fmt.Sprintf("playlist not ready timeout (last reason: %s)", lastNotReadyReason)
			}
			return false, model.RPackagerFailed, detail
		}

		select {
		case <-hbCtx.Done():
			return false, model.RClientStop, ""
		case <-ticker.C:
			// continue
		}
	}
}

// isRecoveryAttempt reports whether this attempt is a recovery: either the loop
// restarted it after a profile fallback, or the profile is itself one of the
// recovery profiles (which a client may also request directly).
func (o *Orchestrator) isRecoveryAttempt(currentProfileSpec model.ProfileSpec, attempt startupAttempt) bool {
	return attempt.Recovery || isStartupRecoveryProfile(currentProfileSpec.Name)
}

// playlistReadyTimeout is the profile's ceiling clamped to what THIS attempt may
// still spend, so per-attempt ceilings can no longer stack past the point where
// the player has already given up.
//
// The clamp is against the attempt's usable slice, not against the whole
// remaining budget. Clamping against the budget is what made the first attempt
// always end exactly at the deadline and left nothing for the retry that the
// profile-hardening ladder depends on.
func (o *Orchestrator) playlistReadyTimeout(currentProfileSpec model.ProfileSpec, vodMode bool, attempt startupAttempt) time.Duration {
	timeout := o.profilePlaylistReadyTimeout(currentProfileSpec, vodMode, attempt)
	usable, bounded := attempt.usable(time.Now())
	if bounded && usable < timeout {
		return usable
	}
	return timeout
}

// profilePlaylistReadyTimeout is the per-attempt ceiling for this profile alone.
func (o *Orchestrator) profilePlaylistReadyTimeout(currentProfileSpec model.ProfileSpec, vodMode bool, attempt startupAttempt) time.Duration {
	if vodMode {
		return defaultVODPlaylistReadyTimeout
	}
	normalizedProfile := profiles.NormalizeRequestedProfileID(currentProfileSpec.Name)
	if o.isRecoveryAttempt(currentProfileSpec, attempt) {
		return defaultIfZero(o.RecoveryPlaylistReadyTimeout, defaultRecoveryPlaylistReadyTimeout)
	}
	if currentProfileSpec.EffectiveRuntimeMode == ports.RuntimeModeHQ50 {
		timeout := defaultIfZero(o.SafariPlaylistReadyTimeout, defaultSafariPlaylistReadyTimeout)
		if timeout < defaultSafariHQ50PlaylistReadyTimeout {
			return defaultSafariHQ50PlaylistReadyTimeout
		}
		return timeout
	}
	if normalizedProfile == profiles.ProfileSafari || normalizedProfile == profiles.ProfileSafariRuntimeHQ {
		timeout := defaultIfZero(o.SafariPlaylistReadyTimeout, defaultSafariPlaylistReadyTimeout)
		if currentProfileSpec.TranscodeVideo && strings.TrimSpace(currentProfileSpec.HWAccel) == "" {
			if timeout < defaultSafariCPUPlaylistReadyTimeout {
				return defaultSafariCPUPlaylistReadyTimeout
			}
		}
		return timeout
	}
	return defaultIfZero(o.PlaylistReadyTimeout, defaultPlaylistReadyTimeout)
}

func defaultIfZero(v, fallback time.Duration) time.Duration {
	if v > 0 {
		return v
	}
	return fallback
}

func (o *Orchestrator) checkPlaylistReady(
	playlistPath string,
	vodMode bool,
	ttfpRecorded *bool,
	profileID string,
	startTime time.Time,
) (bool, string, error) {
	ready, reason, err := o.checkPlaylistReadyAt(playlistPath, vodMode, ttfpRecorded, profileID, startTime)
	if ready {
		return true, "", nil
	}

	legacyPlaylistPath := ""
	if filepath.Base(playlistPath) == "index.m3u8" {
		sessionDir := filepath.Dir(playlistPath)
		sessionsDir := filepath.Dir(sessionDir)
		if filepath.Base(sessionsDir) == "sessions" {
			legacyPlaylistPath = filepath.Join(filepath.Dir(sessionsDir), filepath.Base(sessionDir), "stream.m3u8")
		}
	}
	if legacyPlaylistPath == "" {
		return false, reason, err
	}

	legacyReady, legacyReason, legacyErr := o.checkPlaylistReadyAt(legacyPlaylistPath, vodMode, ttfpRecorded, profileID, startTime)
	if legacyReady {
		return true, "", nil
	}
	if err == nil {
		err = legacyErr
	}
	if legacyReason != "" && (reason == "" || reason == "playlist file missing or empty") {
		reason = legacyReason
	}
	return false, reason, err
}

func (o *Orchestrator) checkPlaylistReadyAt(
	playlistPath string,
	vodMode bool,
	ttfpRecorded *bool,
	profileID string,
	startTime time.Time,
) (bool, string, error) {
	info, err := os.Stat(playlistPath)
	if err != nil || info.Size() == 0 {
		return false, "playlist file missing or empty", err
	}
	// #nosec G304
	content, err := os.ReadFile(filepath.Clean(playlistPath))
	if err != nil {
		return false, "playlist read error", err
	}
	contentText := string(content)
	if !strings.Contains(contentText, "#EXTM3U") {
		return false, "missing #EXTM3U tag", nil
	}
	if strings.Contains(contentText, "#EXT-X-STREAM-INF") {
		segmentURIs := playlistSegments(content)
		if len(segmentURIs) == 0 {
			return false, "master playlist has no variants", nil
		}
		baseDir := filepath.Dir(playlistPath)
		variantPath := filepath.Join(baseDir, segmentURIs[0])
		if rel, relErr := filepath.Rel(baseDir, variantPath); relErr != nil || strings.HasPrefix(rel, "..") {
			return false, "invalid variant playlist path", nil
		}
		// #nosec G304 — variantPath is validated above via filepath.Rel path traversal check
		if resolvedVariantContent, readErr := os.ReadFile(variantPath); readErr == nil {
			if strings.Contains(string(resolvedVariantContent), "#EXT-X-STREAM-INF") {
				return false, "nested master playlists are not supported", nil
			}
		}
		return o.checkPlaylistReadyAt(variantPath, vodMode, ttfpRecorded, profileID, startTime)
	}
	if vodMode && !strings.Contains(contentText, "#EXT-X-ENDLIST") {
		return false, "vod playlist missing #EXT-X-ENDLIST", nil
	}
	if initURI := playlistInitSegment(content); initURI != "" {
		initPath := filepath.Join(filepath.Dir(playlistPath), initURI)
		//nolint:gosec // G703: initURI is sanitized by playlistInitSegment against traversals
		initInfo, initErr := os.Stat(initPath)
		if initErr != nil || initInfo.Size() == 0 {
			return false, "init segment missing or empty: " + initURI, nil
		}
	}
	segmentURIs := playlistSegments(content)
	if vodMode {
		if len(segmentURIs) == 0 {
			return false, "vod playlist has no segments", nil
		}
		lastSegment := segmentURIs[len(segmentURIs)-1]
		segmentPath := filepath.Join(filepath.Dir(playlistPath), lastSegment)
		segInfo, segErr := os.Stat(segmentPath)
		if segErr == nil && segInfo.Size() > 0 {
			if !*ttfpRecorded {
				observeTTFP(profileID, startTime)
				*ttfpRecorded = true
			}
			return true, "", nil
		}
		return false, "vod last segment missing or empty: " + lastSegment, nil
	}

	requiredSegments := o.liveReadySegments()
	if len(segmentURIs) < requiredSegments {
		return false, fmt.Sprintf("not enough segments: %d < %d required", len(segmentURIs), requiredSegments), nil
	}
	for _, segmentURI := range segmentURIs[:requiredSegments] {
		segmentPath := filepath.Join(filepath.Dir(playlistPath), segmentURI)
		segInfo, segErr := os.Stat(segmentPath)
		if segErr != nil || segInfo.Size() == 0 {
			return false, "segment file missing or empty: " + segmentURI, nil
		}
	}
	markerPath := filepath.Join(filepath.Dir(playlistPath), model.SessionFirstFrameMarkerFilename)
	markerInfo, markerErr := os.Stat(markerPath)
	if markerErr != nil || markerInfo.Size() == 0 {
		return false, "first-frame marker file missing: " + markerPath, nil
	}
	if !*ttfpRecorded {
		observeTTFP(profileID, startTime)
		*ttfpRecorded = true
	}
	return true, "", nil
}

func (o *Orchestrator) liveReadySegments() int {
	if o.LiveReadySegments > 0 {
		return o.LiveReadySegments
	}
	return 3
}

func playlistSegments(content []byte) []string {
	scanner := bufio.NewScanner(bytes.NewReader(content))
	segments := make([]string, 0, 8)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(line, "..") || filepath.IsAbs(line) {
			continue
		}
		segments = append(segments, line)
	}
	return segments
}

func playlistInitSegment(content []byte) string {
	scanner := bufio.NewScanner(bytes.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "#EXT-X-MAP:") {
			continue
		}
		_, after, ok := strings.Cut(line, `URI="`)
		if !ok {
			continue
		}
		rest := after
		before0, _, ok0 := strings.Cut(rest, "\"")
		if !ok0 {
			continue
		}
		uri := strings.TrimSpace(before0)
		if uri == "" || strings.Contains(uri, "..") || filepath.IsAbs(uri) {
			return ""
		}
		return uri
	}
	return ""
}
