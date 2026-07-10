package ffmpeg

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/domain/vod"
	infraffmpeg "github.com/ManuGH/xg2g/internal/infra/ffmpeg"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	"net/url"
	"strings"
	"time"
)

func (a *LocalAdapter) planInput(spec ports.StreamSpec, inputURL string) (inputPlan, error) {
	// Early initialisation: if inputURL is empty for a URL source, use
	// spec.Source.ID directly so the credential-stripping logic below
	// always acts on the actual source URL.  Without this, url.Parse("")
	// yields a nil User and the stripping block is skipped entirely,
	// allowing raw credentials to flow through to ffmpeg's -i argument.
	if inputURL == "" && spec.Source.Type == ports.SourceURL {
		inputURL = spec.Source.ID
	}

	// Preserve the original URL before credential stripping so that probe
	// functions (ffprobe, warmup HTTP, safari runtime probe) can still
	// authenticate against protected sources even after the main ffmpeg -i
	// argument has been sanitised.
	authURL := inputURL

	fflags := strings.TrimSpace(a.IngestFFlags)
	if fflags == "" {
		fflags = "+genpts+discardcorrupt+flush_packets"
	}
	analyzeDuration := strings.TrimSpace(a.AnalyzeDuration)
	probeSize := strings.TrimSpace(a.ProbeSize)
	baseInputArgs := make([]string, 0, 20)
	strictIngest := spec.Source.Type != ports.SourceFile &&
		spec.Profile.TranscodeVideo &&
		strictLiveIngestCodec(spec.Profile.VideoCodec)
	if v := strings.TrimSpace(a.IngestErrDetect); v != "" && !strictIngest {
		baseInputArgs = append(baseInputArgs, "-err_detect", v)
	}
	if v := strings.TrimSpace(a.IngestMaxErrorRate); v != "" && !strictIngest {
		baseInputArgs = append(baseInputArgs, "-max_error_rate", v)
	}
	baseInputArgs = append(baseInputArgs, "-ignore_unknown")
	if spec.Source.Type != ports.SourceFile {
		if liveAnalyze := strings.TrimSpace(a.LiveAnalyzeDuration); liveAnalyze != "" {
			analyzeDuration = liveAnalyze
		}
		if liveProbe := strings.TrimSpace(a.LiveProbeSize); liveProbe != "" {
			probeSize = liveProbe
		}
		// Stream relay MPEG-TS often needs a much deeper initial probe before
		// FFmpeg can resolve video dimensions and audio layout reliably.
		// Keep that only for video-transcode paths; passthrough/remux sessions
		// already know enough about the stream and pay a visible startup penalty.
		if isStreamRelayURL(inputURL) {
			if spec.Profile.TranscodeVideo {
				// Relay MPEG-TS needs a deeper probe than the general live
				// default to resolve dimensions/audio reliably. Default ≈10s,
				// but overridable (XG2G_STREAMRELAY_ANALYZE_DURATION /
				// XG2G_STREAMRELAY_PROBE_SIZE) so a fleet that has verified a
				// faster probe can cut the visible relay-transcode startup
				// penalty without patching code.
				if v := strings.TrimSpace(a.StreamRelayAnalyzeDuration); v != "" {
					analyzeDuration = v
				} else {
					analyzeDuration = "10000000"
				}
				if v := strings.TrimSpace(a.StreamRelayProbeSize); v != "" {
					probeSize = v
				} else {
					probeSize = "20M"
				}
			} else {
				// Fast path for stream copy (passthrough). We just copy whatever
				// streams we find, so we don't need a deep probe to configure
				// scalers or hardware decoders. 
				// We use 2.5s to ensure we capture at least one IDR/Keyframe (SPS/PPS),
				// as shorter times (e.g. 0.5s) break H264/HLS completely.
				analyzeDuration = "5000000" // 5s
				probeSize = "20M"
			}
		}
		// igndts discards healthy container DTS and forces a PTS-based
		// reconstruction that breaks on B-pyramid GOPs (non-monotonic DTS,
		// visible judder on copy paths — issue #581). Only opt in for
		// sources whose DTS are genuinely broken.
		if a.ForceIgnDTS && !strings.Contains(fflags, "igndts") {
			fflags += "+igndts"
		}
		if a.LiveNoBuffer && !strings.Contains(fflags, "nobuffer") {
			fflags += "+nobuffer"
		}

		headers := "Icy-MetaData: 1\r\n"
		if u, err := url.Parse(inputURL); err == nil && u.User != nil {
			if pwd, ok := u.User.Password(); ok {
				auth := u.User.Username() + ":" + pwd
				headers += "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(auth)) + "\r\n"
			} else {
				auth := u.User.Username() + ":"
				headers += "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(auth)) + "\r\n"
			}
			// Credentials now travel in the Authorization header; strip them from
			// the URL so they cannot leak into ffmpeg's argv (/proc/<pid>/cmdline)
			// or any logged command line.
			u.User = nil
			inputURL = u.String()
		}

		baseInputArgs = append(baseInputArgs, "-avoid_negative_ts", "make_zero")
		// Only override the user-agent when explicitly configured. The previous
		// hard-coded "VLC/..." UA breaks the OSCam stream-relay source-host
		// resolution (it falls back to the client IP and the scrambled-source
		// fetch fails), so the default is now FFmpeg's built-in UA. See
		// XG2G_LIVE_USER_AGENT / NewLocalAdapter for the rationale.
		if ua := strings.TrimSpace(a.LiveUserAgent); ua != "" {
			baseInputArgs = append(baseInputArgs, "-user_agent", ua)
		}
		baseInputArgs = append(baseInputArgs,
			"-headers", headers,
		)
		if v := strings.TrimSpace(a.IngestFlags2); v != "" && !strictIngest {
			baseInputArgs = append(baseInputArgs, "-flags2", v)
		}
	}
	baseInputArgs = append([]string{"-fflags", fflags}, baseInputArgs...)
	if analyzeDuration != "" {
		baseInputArgs = append(baseInputArgs, "-analyzeduration", analyzeDuration)
	}
	if probeSize != "" {
		baseInputArgs = append(baseInputArgs, "-probesize", probeSize)
	}

	netInputArgs := append([]string{}, baseInputArgs...)
	if whitelist, ok := infraffmpeg.InputProtocolWhitelist(inputURL); ok {
		netInputArgs = append(netInputArgs, "-protocol_whitelist", whitelist)
	}
	netInputArgs = append(netInputArgs,
		"-reconnect", "1",
		"-reconnect_at_eof", "1",
		"-reconnect_streamed", "1",
		// Cold-start FBC tuning race: xg2g must never zap an in-use receiver, so it
		// relies on background tuner allocation and the first connection can race the
		// enigma2/FBC lock and get a premature EOF. A tighter backoff cap lets ffmpeg
		// re-pick the source within ~2s of the lock completing (and recover faster from
		// mid-stream blips) instead of waiting up to 5s.
		"-reconnect_delay_max", "2",
		"-reconnect_on_network_error", "1",
		"-reconnect_on_http_error", "4xx,5xx",
	)

	phase := inputPlan{inputURL: inputURL, authURL: authURL}

	switch spec.Source.Type {
	case ports.SourceTuner:
		if phase.inputURL == "" {
			return inputPlan{}, fmt.Errorf("missing stream url for tuner source")
		}
		phase.args = append(phase.args, netInputArgs...)
		phase.args = append(phase.args, "-i", phase.inputURL)
	case ports.SourceURL:
		phase.args = append(phase.args, netInputArgs...)
		phase.args = append(phase.args, "-i", phase.inputURL)
	case ports.SourceFile:
		phase.args = append(phase.args, baseInputArgs...)
		phase.args = append(phase.args, "-re", "-i", spec.Source.ID)
	default:
		return inputPlan{}, fmt.Errorf("unsupported source type: %s", spec.Source.Type)
	}

	return phase, nil
}

func (a *LocalAdapter) defaultLiveFPS(spec ports.StreamSpec, inputURL string) int {
	fps := a.FPSFallback
	if fps <= 0 {
		fps = 25
	}
	if (spec.Source.Type != ports.SourceTuner && !isStreamRelayURL(inputURL)) && !spec.Profile.Deinterlace {
		fps = 30
	}
	if spec.Profile.Deinterlace || strings.EqualFold(strings.TrimSpace(spec.Profile.Name), "safari_dirty") {
		fps = a.FPSFallbackInter
	}
	if fps <= 0 {
		fps = 25
	}
	return fps
}

func (a *LocalAdapter) resolveLiveFPS(ctx context.Context, spec ports.StreamSpec, inputURL string) int {
	fps := a.defaultLiveFPS(spec, inputURL)
	sourceKey := fpsCacheKey(spec.Source, inputURL)
	if resolved, skipProbe := a.resolveSkippedLiveFPS(ctx, spec, inputURL, sourceKey, fps); skipProbe {
		return resolved
	}
	return a.resolveProbedLiveFPS(ctx, spec, inputURL, sourceKey, fps)
}

func (a *LocalAdapter) resolveSkippedLiveFPS(ctx context.Context, spec ports.StreamSpec, inputURL, sourceKey string, fallback int) (int, bool) {
	if isStreamRelayURL(inputURL) {
		logEvt := a.Logger.Info().
			Str("session_id", spec.SessionID).
			Str("startup_phase", "fps_probe_skipped_streamrelay").
			Str("source_key", sourceKey).
			Str("input_url", sanitizeURLForLog(inputURL))
		if cachedFPS, ok := a.cachedFPS(sourceKey); ok {
			logEvt.Int("cached_fps", cachedFPS).
				Msg("skipping fps probe for stream relay input; using cached fps")
			return cachedFPS, true
		}
		logEvt.Int("fallback_fps", fallback).
			Msg("skipping fps probe for stream relay input; using fallback fps")
		return fallback, true
	}
	if sourceKey == "" {
		return fallback, false
	}
	cachedFPS, ok := a.cachedFPS(sourceKey)
	if !ok {
		return fallback, false
	}
	a.Logger.Info().
		Str("session_id", spec.SessionID).
		Str("startup_phase", "fps_cache_available").
		Str("source_key", sourceKey).
		Int("cached_fps", cachedFPS).
		Msg("fps cache available before probe")
	if !a.SkipFPSProbeOnCache {
		return fallback, false
	}
	if a.shouldAbortCachedFPSSkip(ctx, spec, inputURL, sourceKey, cachedFPS) {
		return fallback, false
	}
	a.Logger.Info().
		Str("session_id", spec.SessionID).
		Str("startup_phase", "fps_probe_skipped").
		Str("source_key", sourceKey).
		Int("cached_fps", cachedFPS).
		Msg("skipping fps probe because cached fps is available")
	return cachedFPS, true
}

func (a *LocalAdapter) shouldAbortCachedFPSSkip(ctx context.Context, spec ports.StreamSpec, inputURL, sourceKey string, cachedFPS int) bool {
	if a.SkipFPSProbeWarmup <= 0 || !isHTTPInputURL(inputURL) {
		return false
	}
	a.Logger.Info().
		Str("session_id", spec.SessionID).
		Str("startup_phase", "fps_probe_warmup_started").
		Str("source_key", sourceKey).
		Str("input_url", sanitizeURLForLog(inputURL)).
		Dur("warmup_duration", a.SkipFPSProbeWarmup).
		Msg("starting cache-hit stream warmup")
	warmupResult, warmupErr := a.warmupInputStream(ctx, inputURL, a.SkipFPSProbeWarmup)
	warmupEvt := a.Logger.Info().
		Str("session_id", spec.SessionID).
		Str("startup_phase", "fps_probe_warmup_finished").
		Str("source_key", sourceKey).
		Str("input_url", sanitizeURLForLog(inputURL)).
		Int("warmup_bytes", warmupResult.bytes).
		Int("http_status", warmupResult.httpStatus).
		Int64("latency_ms", warmupResult.latencyMs)
	if warmupErr != nil {
		warmupEvt = warmupEvt.Err(warmupErr)
	}
	warmupEvt.Msg("cache-hit stream warmup finished")
	if warmupErr == nil {
		return false
	}
	a.Logger.Warn().
		Str("session_id", spec.SessionID).
		Str("startup_phase", "fps_probe_skip_aborted").
		Str("source_key", sourceKey).
		Int("cached_fps", cachedFPS).
		Msg("cache-hit stream warmup failed, falling back to fps probe")
	return true
}

func (a *LocalAdapter) resolveProbedLiveFPS(ctx context.Context, spec ports.StreamSpec, inputURL, sourceKey string, fallback int) int {
	a.Logger.Info().
		Str("session_id", spec.SessionID).
		Str("startup_phase", "fps_probe_started").
		Str("source_key", sourceKey).
		Str("input_url", sanitizeURLForLog(inputURL)).
		Msg("fps probe started")
	detected, basis, err := a.probeFPS(ctx, inputURL)
	probeEvt := a.Logger.Info().
		Str("session_id", spec.SessionID).
		Str("startup_phase", "fps_probe_finished").
		Str("source_key", sourceKey).
		Str("input_url", sanitizeURLForLog(inputURL))
	if err != nil {
		probeEvt = probeEvt.Err(err)
	} else {
		probeEvt = probeEvt.Int("detected_fps", detected).Str("fps_basis", basis)
	}
	probeEvt.Msg("fps probe finished")
	if err == nil && a.isValidFPS(detected) {
		if sourceKey != "" {
			a.setLastKnownFPS(sourceKey, detected)
		}
		a.Logger.Debug().
			Str("sessionId", spec.SessionID).
			Int("fps", detected).
			Str("fps_basis", basis).
			Str("url", sanitizeURLForLog(inputURL)).
			Msg("detected input fps")
		return detected
	}
	if err == nil {
		err = fmt.Errorf("detected fps out of range: %d", detected)
	}
	if cachedFPS, ok := a.cachedFPS(sourceKey); ok {
		a.Logger.Warn().
			Str("sessionId", spec.SessionID).
			Err(err).
			Int("cached_fps", cachedFPS).
			Str("fps_basis", "last_known_source").
			Str("source_key", sourceKey).
			Int("fps_min", a.FPSMin).
			Int("fps_max", a.FPSMax).
			Str("url", sanitizeURLForLog(inputURL)).
			Msg("fps detection failed, using last known source fps")
		return cachedFPS
	}
	a.Logger.Warn().
		Str("sessionId", spec.SessionID).
		Err(err).
		Int("fallback_fps", fallback).
		Int("fps_min", a.FPSMin).
		Int("fps_max", a.FPSMax).
		Str("url", sanitizeURLForLog(inputURL)).
		Msg("fps detection failed, using fallback")
	return fallback
}

func (a *LocalAdapter) cachedFPS(sourceKey string) (int, bool) {
	if sourceKey == "" {
		return 0, false
	}
	cachedFPS, ok := a.getLastKnownFPS(sourceKey)
	if !ok || !a.isValidFPS(cachedFPS) {
		return 0, false
	}
	return cachedFPS, true
}

func (a *LocalAdapter) isValidFPS(fps int) bool {
	return fps >= a.FPSMin && fps <= a.FPSMax
}

func (a *LocalAdapter) adjustLiveFPSForRuntimeServiceOverride(spec ports.StreamSpec, inputURL string, fps int) int {
	if !shouldForce25FPSForLiveProfile(spec, inputURL) {
		return fps
	}

	targetFPS := 25
	if fps == targetFPS {
		return fps
	}

	logEvt := a.Logger.Info().
		Str("sessionId", spec.SessionID).
		Int("cached_or_detected_fps", fps).
		Int("override_fps", targetFPS).
		Str("runtime_mode", string(effectiveLiveRuntimeMode(spec.Profile)))
	if serviceRef := safariRuntimeServiceRef(spec, inputURL); serviceRef != "" {
		logEvt = logEvt.Str("service_ref", serviceRef)
	}
	logEvt.Msg("forcing 25fps for live runtime profile")
	return targetFPS
}

func shouldForce25FPSForLiveProfile(spec ports.StreamSpec, inputURL string) bool {
	if shouldForceSafariHQ25ForServiceRef(spec, inputURL) {
		return true
	}
	if shouldForceSafariHQ50ForServiceRef(spec, inputURL) {
		return false
	}
	if shouldForceSafariHQForServiceRef(spec, inputURL) {
		return shouldForce25FPSForSafariHQ(spec.Profile)
	}
	if effectiveLiveRuntimeMode(spec.Profile) != ports.RuntimeModeHQ25 {
		return false
	}
	return normalizeRequestedCodec(spec.Profile.VideoCodec) == "hevc"
}

func effectiveLiveRuntimeMode(profile ports.ProfileSpec) ports.RuntimeMode {
	if profile.EffectiveRuntimeMode != "" && profile.EffectiveRuntimeMode != ports.RuntimeModeUnknown {
		return profile.EffectiveRuntimeMode
	}
	if profile.PolicyModeHint != "" && profile.PolicyModeHint != ports.RuntimeModeUnknown {
		return profile.PolicyModeHint
	}
	return profiles.RuntimeModeHintFromProfile(profile)
}

func targetLiveOutputFPS(spec ports.StreamSpec) int {
	if spec.Mode != ports.ModeLive {
		return 0
	}
	if !spec.Profile.TranscodeVideo {
		return 0
	}
	// HQ50 must NOT clamp the framerate: the send_field bob deinterlace already
	// emits native 50fps and -force_key_frames keeps segment boundaries aligned
	// (time-based). Only HQ25 pins -r 25.
	if effectiveLiveRuntimeMode(spec.Profile) != ports.RuntimeModeHQ25 {
		return 0
	}
	return 25
}

func (a *LocalAdapter) runSafariRuntimeProbeWithRetry(ctx context.Context, sessionID, inputURL string, probeTimeout time.Duration) (*vod.StreamInfo, error) {
	const maxAttempts = 2

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, probeTimeout)
		info, err := a.runSafariRuntimeProbeOnce(attemptCtx, inputURL)
		cancel()
		if err == nil {
			if attempt > 1 {
				a.Logger.Info().
					Str("sessionId", sessionID).
					Str("input_url", sanitizeURLForLog(inputURL)).
					Int("attempt", attempt).
					Msg("safari runtime probe recovered after transient startup error")
			}
			return info, nil
		}

		lastErr = err
		if attempt == maxAttempts || !shouldRetrySafariRuntimeProbe(err, inputURL) {
			break
		}

		a.Logger.Info().
			Err(err).
			Str("sessionId", sessionID).
			Str("input_url", sanitizeURLForLog(inputURL)).
			Int("attempt", attempt).
			Msg("safari runtime probe transient startup error; retrying")

		select {
		case <-time.After(250 * time.Millisecond):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return nil, lastErr
}

func (a *LocalAdapter) runSafariRuntimeProbeOnce(ctx context.Context, inputURL string) (*vod.StreamInfo, error) {
	if a.streamProbeFn != nil {
		return a.streamProbeFn(ctx, inputURL)
	}
	return infraffmpeg.ProbeWithBin(ctx, a.FFprobeBin, inputURL)
}

func shouldRetrySafariRuntimeProbe(err error, inputURL string) bool {
	if err == nil || !isStreamRelayURL(inputURL) {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	msg := strings.ToLower(err.Error())
	transientMarkers := []string{
		"stream ends prematurely",
		"input/output error",
		"non-existing pps",
		"decode_slice_header error",
		"no frame!",
		"mmco: unref short failure",
	}
	for _, marker := range transientMarkers {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

func fpsCacheKey(source ports.StreamSource, inputURL string) string {
	if source.Type == ports.SourceTuner {
		if ref := normalizeServiceRef(source.ID); ref != "" {
			return "service_ref:" + ref
		}
	}
	if ref := extractServiceRefFromURL(inputURL); ref != "" {
		return "service_ref:" + ref
	}
	return ""
}
