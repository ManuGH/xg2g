package scan

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/vod"
	infra "github.com/ManuGH/xg2g/internal/infra/ffmpeg"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/m3u"
	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/ManuGH/xg2g/internal/normalize"
)

func (m *Manager) ProbeCapability(ctx context.Context, serviceRef string) (Capability, bool, error) {
	if m == nil {
		return Capability{}, false, fmt.Errorf("scan: manager unavailable")
	}

	serviceRef = normalize.ServiceRef(serviceRef)
	if serviceRef == "" {
		return Capability{}, false, fmt.Errorf("scan: service ref required")
	}

	channel, found, err := m.lookupChannel(serviceRef)
	if err != nil {
		return Capability{}, false, err
	}
	if !found {
		return Capability{}, false, nil
	}

	// Serialize this serviceRef's Get→probe→Update against the background scan loop and any
	// other concurrent probe, so they cannot lose each other's update.
	unlock := m.lockServiceRefCap(serviceRef)
	defer unlock()

	// If context was canceled/timed out while waiting for the lock, bail out early instead
	// of performing unnecessary I/O on an already-canceled operation.
	if err := ctx.Err(); err != nil {
		return Capability{}, false, err
	}

	existingCap, existingFound := m.store.Get(serviceRef)
	probeURL := channel.URL
	resolved := false

	if m.e2Client != nil {
		resCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		freshURL, err := m.e2Client.ResolveStreamURL(resCtx, serviceRef)
		cancel()

		if err == nil && freshURL != "" {
			probeURL = freshURL
			resolved = true
			log.L().Debug().Str("sref", serviceRef).Str("fresh_url", freshURL).Msg("scan: resolved fresh stream url for targeted probe")
		} else {
			log.L().Warn().Err(err).Str("sref", serviceRef).Msg("scan: failed to resolve fresh url for targeted probe, falling back")
		}
	}

	if !resolved {
		resCtx, cancel := context.WithTimeout(ctx, resolveM3UTimeout)
		resolvedURL, err := resolveStreamURL(resCtx, channel.URL)
		cancel()
		if err == nil && resolvedURL != "" {
			probeURL = resolvedURL
		}
	}

	log.L().Info().Str("sref", serviceRef).Msg("scan: probing targeted channel capability")
	res, successfulProbeURL, err := m.probeWithFallbacks(ctx, serviceRef, channel.URL, probeURL, infra.ProbeOptions{}, defaultProbeTimeout)
	if shouldAttemptExtendedRetry(existingCap, existingFound, res, err) {
		retryInitialURL := successfulProbeURL
		if strings.TrimSpace(retryInitialURL) == "" {
			retryInitialURL = probeURL
		}
		log.L().Info().
			Str("sref", serviceRef).
			Dur("timeout", extendedProbeTimeout).
			Dur("analyzeduration", extendedProbeAnalyzeDuration).
			Int64("probesize_bytes", extendedProbeSizeBytes).
			Msg("scan: targeted probe retrying with extended ffprobe budget")

		retryRes, _, retryErr := m.probeWithFallbacks(
			ctx,
			serviceRef,
			channel.URL,
			retryInitialURL,
			infra.ProbeOptions{
				AnalyzeDuration: extendedProbeAnalyzeDuration,
				ProbeSizeBytes:  extendedProbeSizeBytes,
			},
			extendedProbeTimeout,
		)
		retryBase := res
		if retryBase == nil && existingFound {
			retryBase = streamInfoFromCapability(existingCap)
		}
		switch {
		case retryErr != nil:
			log.L().Warn().Err(retryErr).Str("sref", serviceRef).Msg("scan: targeted extended probe retry failed")
		case isRicherMediaTruth(retryBase, retryRes):
			res = retryRes
			err = nil
			log.L().Info().Str("sref", serviceRef).Msg("scan: targeted extended probe retry enriched media truth")
		default:
			log.L().Warn().Str("sref", serviceRef).Msg("scan: targeted extended probe returned conflicting or non-additive media truth; keeping original result")
		}
	}

	now := time.Now()
	if err != nil {
		// If the CALLER's context is canceled/expired (e.g. client disconnect), the
		// probe was aborted, not failed. Persisting a failure record here would set a
		// 24h retry lockout for a transient cancellation. The probe's own timeout uses a
		// child context, so ctx.Err() stays nil for a genuine probe timeout — only a
		// caller-side cancellation trips this guard.
		if ctx.Err() != nil {
			return existingCap, existingFound, err
		}
		cap := m.mergeFailedAttempt(existingCap, existingFound, serviceRef, channel.Name, now, err)
		m.store.Update(cap)
		return cap, true, err
	}

	cap := m.capabilityFromProbe(existingCap, existingFound, serviceRef, channel.Name, now, res)
	m.store.Update(cap)
	return cap, true, nil
}

// resolveProbeURL resolves the best stream URL to probe for a channel: a fresh
// Enigma2-resolved URL when available, else the (optionally resolved) M3U URL.
func (m *Manager) resolveProbeURL(ctx context.Context, ch m3u.Channel, sRef string) string {
	probeURL := ch.URL
	resolved := false
	if m.e2Client != nil && sRef != "" {
		resCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		freshURL, err := m.e2Client.ResolveStreamURL(resCtx, sRef)
		cancel()
		if err == nil && freshURL != "" {
			probeURL = freshURL
			resolved = true
			log.L().Debug().Str("sref", sRef).Str("fresh_url", freshURL).Msg("scan: resolved fresh stream url")
		} else if ctx.Err() == nil {
			log.L().Warn().Err(err).Str("sref", sRef).Msg("scan: failed to resolve fresh url, falling back to m3u")
		}
	}
	if !resolved {
		resCtx, resCancel := context.WithTimeout(ctx, resolveM3UTimeout)
		res, err := resolveStreamURL(resCtx, ch.URL)
		resCancel()
		if err == nil && res != "" {
			probeURL = res
		}
	}
	return probeURL
}

// probeChannelMediaTruth probes a channel and, when the initial result is
// incomplete, retries once with an extended ffprobe budget, keeping whichever
// result carries richer media truth.
func (m *Manager) probeChannelMediaTruth(ctx context.Context, ch m3u.Channel, sRef, probeURL string, existingCap Capability, found bool) (*vod.StreamInfo, error) {
	res, successfulProbeURL, err := m.probeWithFallbacks(ctx, sRef, ch.URL, probeURL, infra.ProbeOptions{}, defaultProbeTimeout)
	if ctx.Err() == nil && shouldAttemptExtendedRetry(existingCap, found, res, err) {
		retryInitialURL := successfulProbeURL
		if strings.TrimSpace(retryInitialURL) == "" {
			retryInitialURL = probeURL
		}
		log.L().Info().
			Str("sref", sRef).
			Dur("timeout", extendedProbeTimeout).
			Dur("analyzeduration", extendedProbeAnalyzeDuration).
			Int64("probesize_bytes", extendedProbeSizeBytes).
			Msg("scan: media truth incomplete, retrying with extended ffprobe budget")

		retryRes, _, retryErr := m.probeWithFallbacks(
			ctx,
			sRef,
			ch.URL,
			retryInitialURL,
			infra.ProbeOptions{
				AnalyzeDuration: extendedProbeAnalyzeDuration,
				ProbeSizeBytes:  extendedProbeSizeBytes,
			},
			extendedProbeTimeout,
		)
		retryBase := res
		if retryBase == nil && found {
			retryBase = streamInfoFromCapability(existingCap)
		}
		switch {
		case retryErr != nil:
			if ctx.Err() == nil {
				log.L().Warn().Err(retryErr).Str("sref", sRef).Msg("scan: extended probe retry failed")
			}
		case isRicherMediaTruth(retryBase, retryRes):
			res = retryRes
			err = nil
			log.L().Info().Str("sref", sRef).Msg("scan: extended probe retry enriched media truth")
		default:
			log.L().Warn().Str("sref", sRef).Msg("scan: extended probe retry returned conflicting or non-additive media truth; keeping original result")
		}
	}
	return res, err
}

func shouldAttemptExtendedRetry(existing Capability, found bool, initial *vod.StreamInfo, probeErr error) bool {
	if needsExtendedMediaTruthRetry(initial) {
		return true
	}
	if probeErr == nil || !found {
		return false
	}
	normalized := existing.Normalized()
	return normalized.Usable() && !normalized.HasMediaTruth()
}

func needsExtendedMediaTruthRetry(info *vod.StreamInfo) bool {
	if info == nil {
		return false
	}
	return strings.TrimSpace(info.Video.CodecName) != "" &&
		(strings.TrimSpace(info.Container) == "" ||
			strings.TrimSpace(info.Audio.CodecName) == "" ||
			info.BitrateKbps == 0)
}

func streamInfoFromCapability(cap Capability) *vod.StreamInfo {
	normalized := cap.Normalized()
	if normalized.Container == "" &&
		normalized.VideoCodec == "" &&
		normalized.AudioCodec == "" &&
		normalized.BitrateKbps == 0 &&
		normalized.Width == 0 &&
		normalized.Height == 0 &&
		normalized.FPS == 0 &&
		normalized.SignalFPS == 0 &&
		normalized.FieldOrder == "" &&
		normalized.AudioChannels == 0 &&
		normalized.AudioBitrateKbps == 0 &&
		normalized.AudioSampleRate == 0 &&
		normalized.AudioChannelLayout == "" {
		return nil
	}
	return &vod.StreamInfo{
		Container:   normalized.Container,
		BitrateKbps: normalized.BitrateKbps,
		Video: vod.VideoStreamInfo{
			CodecName:  normalized.VideoCodec,
			Width:      normalized.Width,
			Height:     normalized.Height,
			FPS:        normalized.FPS,
			SignalFPS:  normalized.SignalFPS,
			Interlaced: normalized.Interlaced,
			FieldOrder: normalized.FieldOrder,
		},
		Audio: vod.AudioStreamInfo{
			CodecName:     normalized.AudioCodec,
			SampleRate:    normalized.AudioSampleRate,
			Channels:      normalized.AudioChannels,
			BitrateKbps:   normalized.AudioBitrateKbps,
			ChannelLayout: normalized.AudioChannelLayout,
		},
	}
}

func isRicherMediaTruth(base *vod.StreamInfo, candidate *vod.StreamInfo) bool {
	if candidate == nil {
		return false
	}
	if base == nil {
		return strings.TrimSpace(candidate.Container) != "" ||
			strings.TrimSpace(candidate.Video.CodecName) != "" ||
			strings.TrimSpace(candidate.Audio.CodecName) != "" ||
			candidate.BitrateKbps > 0 ||
			candidate.Video.Width > 0 ||
			candidate.Video.Height > 0 ||
			candidate.Video.FPS > 0 ||
			candidate.Video.SignalFPS > 0 ||
			strings.TrimSpace(candidate.Video.FieldOrder) != "" ||
			candidate.Audio.Channels > 0 ||
			candidate.Audio.BitrateKbps > 0 ||
			candidate.Audio.SampleRate > 0 ||
			strings.TrimSpace(candidate.Audio.ChannelLayout) != ""
	}

	added := false
	if !compareStrictAdditiveString(base.Container, candidate.Container, &added) {
		return false
	}
	if !compareStrictAdditiveString(base.Video.CodecName, candidate.Video.CodecName, &added) {
		return false
	}
	if !compareStrictAdditiveString(base.Audio.CodecName, candidate.Audio.CodecName, &added) {
		return false
	}
	if !compareStrictAdditiveInt(base.BitrateKbps, candidate.BitrateKbps, &added) {
		return false
	}
	if !compareStrictAdditiveInt(base.Video.Width, candidate.Video.Width, &added) {
		return false
	}
	if !compareStrictAdditiveInt(base.Video.Height, candidate.Video.Height, &added) {
		return false
	}
	if !compareStrictAdditiveFloat(base.Video.FPS, candidate.Video.FPS, &added) {
		return false
	}
	if !compareStrictAdditiveFloat(base.Video.SignalFPS, candidate.Video.SignalFPS, &added) {
		return false
	}
	if !compareStrictAdditiveString(base.Video.FieldOrder, candidate.Video.FieldOrder, &added) {
		return false
	}
	if !compareStrictAdditiveInt(base.Audio.Channels, candidate.Audio.Channels, &added) {
		return false
	}
	if !compareStrictAdditiveInt(base.Audio.BitrateKbps, candidate.Audio.BitrateKbps, &added) {
		return false
	}
	if !compareStrictAdditiveInt(base.Audio.SampleRate, candidate.Audio.SampleRate, &added) {
		return false
	}
	if !compareStrictAdditiveString(base.Audio.ChannelLayout, candidate.Audio.ChannelLayout, &added) {
		return false
	}
	return added
}

func compareStrictAdditiveString(base, candidate string, added *bool) bool {
	base = strings.TrimSpace(base)
	candidate = strings.TrimSpace(candidate)
	switch {
	case base == "" && candidate == "":
		return true
	case base == "":
		*added = true
		return true
	case candidate == "":
		return false
	default:
		return strings.EqualFold(base, candidate)
	}
}

func compareStrictAdditiveInt(base, candidate int, added *bool) bool {
	switch {
	case base == 0 && candidate == 0:
		return true
	case base == 0:
		*added = true
		return true
	case candidate == 0:
		return false
	default:
		return base == candidate
	}
}

func compareStrictAdditiveFloat(base, candidate float64, added *bool) bool {
	switch {
	case base == 0 && candidate == 0:
		return true
	case base == 0:
		*added = true
		return true
	case candidate == 0:
		return false
	default:
		return fmt.Sprintf("%.3f", base) == fmt.Sprintf("%.3f", candidate)
	}
}

func (m *Manager) probeWithFallbacks(ctx context.Context, serviceRef, originalURL, initialProbeURL string, opts infra.ProbeOptions, timeout time.Duration) (*vod.StreamInfo, string, error) {
	initialProbeURL = strings.TrimSpace(initialProbeURL)
	if initialProbeURL == "" {
		initialProbeURL = strings.TrimSpace(originalURL)
	}
	attemptedProbeURLs := map[string]struct{}{}
	if normalized := normalizeProbeURL(initialProbeURL); normalized != "" {
		attemptedProbeURLs[normalized] = struct{}{}
	}

	res, err := m.runProbeAttempt(ctx, initialProbeURL, opts, timeout)
	if err == nil {
		return res, initialProbeURL, nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		metrics.IncScanProbeTimeout()
	}
	log.L().Warn().Err(err).Str("sref", serviceRef).Msg("scan: initial probe failed, attempting port 8001 fallback")

	fallbackURL, buildErr := buildFallbackURL(initialProbeURL, serviceRef)
	if buildErr == nil && !hasAttemptedProbeURL(attemptedProbeURLs, fallbackURL) {
		attemptedProbeURLs[normalizeProbeURL(fallbackURL)] = struct{}{}
		resFallback, errFallback := m.runProbeAttempt(ctx, fallbackURL, opts, timeout)
		if errFallback == nil {
			log.L().Info().Str("sref", serviceRef).Msg("scan: fallback to 8001 succeeded")
			return resFallback, fallbackURL, nil
		}
		log.L().Warn().Err(errFallback).Str("sref", serviceRef).Msg("scan: fallback 8001 probe failed")
	}

	origCtx, origCancel := context.WithTimeout(ctx, resolveM3UTimeout)
	originalProbeURL, resolvedPlaylist, resolveErr := resolveOriginalProbeURL(origCtx, originalURL)
	origCancel()

	switch {
	case resolveErr != nil:
		log.L().Debug().
			Err(resolveErr).
			Str("sref", serviceRef).
			Msg("scan: original URL fallback unavailable")
	case originalProbeURL == "":
		log.L().Debug().
			Str("sref", serviceRef).
			Msg("scan: original URL fallback resolved to empty target")
	case hasAttemptedProbeURL(attemptedProbeURLs, originalProbeURL):
		log.L().Debug().
			Str("sref", serviceRef).
			Str("probe_url", originalProbeURL).
			Bool("resolved_playlist", resolvedPlaylist).
			Msg("scan: skipping original URL fallback; target already attempted")
	default:
		log.L().Warn().Str("sref", serviceRef).Msg("scan: attempting fallback to original URL (web)")
		resOrig, errOrig := m.runProbeAttempt(ctx, originalProbeURL, opts, timeout)
		if errOrig == nil {
			log.L().Info().Str("sref", serviceRef).Msg("scan: fallback to original URL succeeded")
			return resOrig, originalProbeURL, nil
		}
		log.L().Warn().Err(errOrig).Str("sref", serviceRef).Msg("scan: final fallback failed")
	}

	return nil, "", err
}

func (m *Manager) runProbeAttempt(ctx context.Context, probeURL string, opts infra.ProbeOptions, timeout time.Duration) (*vod.StreamInfo, error) {
	if strings.TrimSpace(probeURL) == "" {
		return nil, fmt.Errorf("probe url empty")
	}
	probeCtx, probeCancel := context.WithTimeout(ctx, timeout)
	defer probeCancel()

	metrics.SetScanInflightProbes(1)
	defer metrics.SetScanInflightProbes(0)

	probeFn := m.probeFn
	if probeFn == nil {
		probeFn = infra.ProbeWithOptions
	}
	return probeFn(probeCtx, probeURL, opts)
}

func resolveStreamURL(ctx context.Context, urlStr string) (string, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return urlStr, nil // fallback
	}
	if !strings.HasSuffix(strings.ToLower(u.Path), ".m3u") {
		return urlStr, nil
	}

	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Connection", "close")
	req.Close = true
	resp, err := resolveM3UHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}

	limitedBody := io.LimitReader(resp.Body, 64*1024)
	scanner := bufio.NewScanner(limitedBody)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			return line, nil
		}
	}
	return "", fmt.Errorf("empty playlist")
}

func resolveOriginalProbeURL(ctx context.Context, urlStr string) (string, bool, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return "", false, err
	}
	if !strings.HasSuffix(strings.ToLower(u.Path), ".m3u") {
		return urlStr, false, nil
	}

	resolved, err := resolveStreamURL(ctx, urlStr)
	if err != nil {
		return "", true, err
	}
	return resolved, true, nil
}

func hasAttemptedProbeURL(attempted map[string]struct{}, probeURL string) bool {
	normalized := normalizeProbeURL(probeURL)
	if len(attempted) == 0 || normalized == "" {
		return false
	}
	_, ok := attempted[normalized]
	return ok
}

func normalizeProbeURL(probeURL string) string {
	probeURL = strings.TrimSpace(probeURL)
	if probeURL == "" {
		return ""
	}
	u, err := url.Parse(probeURL)
	if err != nil {
		return probeURL
	}
	u.User = nil
	u.Fragment = ""
	return u.String()
}

func buildFallbackURL(resolvedURL, serviceRef string) (string, error) {
	u, err := url.Parse(resolvedURL)
	if err != nil {
		return "", err
	}
	host := u.Hostname()
	if host == "" {
		return "", fmt.Errorf("missing host in resolved url")
	}
	u.Scheme = "http"
	u.Host = fmt.Sprintf("%s:%d", host, 8001)
	u.Path = "/" + serviceRef
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}
