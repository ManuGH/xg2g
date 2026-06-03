package manager

import (
	"bufio"
	"bytes"
	"context"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
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
	logger zerolog.Logger,
	ttfpRecorded *bool,
) (ready bool, reason model.ReasonCode, detail string) {
	playlistReadyTimeout := o.playlistReadyTimeout(currentProfileSpec, vodMode)
	playlistPollInterval := 200 * time.Millisecond
	playlistDeadline := time.Now().Add(playlistReadyTimeout)
	ticker := time.NewTicker(playlistPollInterval)
	defer ticker.Stop()

	logger.Info().
		Str("session_id", e.SessionID).
		Str("service_ref", e.ServiceRef).
		Str("profile", currentProfileSpec.Name).
		Bool("recovery_mode", isStartupRecoveryProfile(currentProfileSpec.Name)).
		Dur("timeout", playlistReadyTimeout).
		Msg("waiting for playlist to become ready")

	for {
		// Check process health first
		status := o.Pipeline.Health(ctx, handle)
		o.updatePlaybackRuntimeDiagnosticsBestEffort(hbCtx, e.SessionID, status)
		if !status.Healthy {
			return false, model.RProcessEnded, "process died during startup: " + status.Message
		}

		ready, err := o.checkPlaylistReady(playlistPath, vodMode, ttfpRecorded, e.ProfileID, startTime)
		if err == nil && ready {
			return true, "", ""
		}

		if time.Now().After(playlistDeadline) {
			// reason, detail := o.classifyFailure(...) // Removed for now due to complexity of mapping logs
			return false, model.RPackagerFailed, "playlist not ready timeout"
		}

		select {
		case <-hbCtx.Done():
			return false, model.RClientStop, ""
		case <-ticker.C:
			// continue
		}
	}
}

func (o *Orchestrator) playlistReadyTimeout(currentProfileSpec model.ProfileSpec, vodMode bool) time.Duration {
	if vodMode {
		return defaultVODPlaylistReadyTimeout
	}
	normalizedProfile := profiles.NormalizeRequestedProfileID(currentProfileSpec.Name)
	if isStartupRecoveryProfile(currentProfileSpec.Name) {
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
) (bool, error) {
	ready, err := o.checkPlaylistReadyAt(playlistPath, vodMode, ttfpRecorded, profileID, startTime)
	if ready {
		return true, nil
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
		return false, err
	}

	legacyReady, legacyErr := o.checkPlaylistReadyAt(legacyPlaylistPath, vodMode, ttfpRecorded, profileID, startTime)
	if legacyReady {
		return true, nil
	}
	if err == nil {
		err = legacyErr
	}
	return false, err
}

func (o *Orchestrator) checkPlaylistReadyAt(
	playlistPath string,
	vodMode bool,
	ttfpRecorded *bool,
	profileID string,
	startTime time.Time,
) (bool, error) {
	info, err := os.Stat(playlistPath)
	if err != nil || info.Size() == 0 {
		return false, err
	}
	// #nosec G304
	content, err := os.ReadFile(filepath.Clean(playlistPath))
	if err != nil {
		return false, err
	}
	contentText := string(content)
	if !strings.Contains(contentText, "#EXTM3U") {
		return false, nil
	}
	if vodMode && !strings.Contains(contentText, "#EXT-X-ENDLIST") {
		return false, nil
	}
	if initURI := playlistInitSegment(content); initURI != "" {
		initPath := filepath.Join(filepath.Dir(playlistPath), initURI)
		//nolint:gosec // G703: initURI is sanitized by playlistInitSegment against traversals
		initInfo, initErr := os.Stat(initPath)
		if initErr != nil || initInfo.Size() == 0 {
			return false, nil
		}
	}
	segmentURIs := playlistSegments(content)
	if vodMode {
		if len(segmentURIs) == 0 {
			return false, nil
		}
		lastSegment := segmentURIs[len(segmentURIs)-1]
		segmentPath := filepath.Join(filepath.Dir(playlistPath), lastSegment)
		segInfo, segErr := os.Stat(segmentPath)
		if segErr == nil && segInfo.Size() > 0 {
			if !*ttfpRecorded {
				observeTTFP(profileID, startTime)
				*ttfpRecorded = true
			}
			return true, nil
		}
		return false, nil
	}

	requiredSegments := o.liveReadySegments()
	if len(segmentURIs) < requiredSegments {
		return false, nil
	}
	for _, segmentURI := range segmentURIs[:requiredSegments] {
		segmentPath := filepath.Join(filepath.Dir(playlistPath), segmentURI)
		segInfo, segErr := os.Stat(segmentPath)
		if segErr != nil || segInfo.Size() == 0 {
			return false, nil
		}
	}
	markerPath := filepath.Join(filepath.Dir(playlistPath), model.SessionFirstFrameMarkerFilename)
	markerInfo, markerErr := os.Stat(markerPath)
	if markerErr != nil || markerInfo.Size() == 0 {
		return false, nil
	}
	if !*ttfpRecorded {
		observeTTFP(profileID, startTime)
		*ttfpRecorded = true
	}
	return true, nil
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
