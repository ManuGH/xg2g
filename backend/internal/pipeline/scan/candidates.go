package scan

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/vod"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/m3u"
)

func (m *Manager) executeScan(ctx context.Context) error {
	force := m.forceScan.Swap(false)
	if err := m.waitForPlaybackIdle(ctx); err != nil {
		return err
	}
	if m.scanFn != nil {
		return m.scanFn(ctx)
	}
	return m.scanInternal(ctx, force)
}

func (m *Manager) scanInternal(ctx context.Context, force bool) error {
	logEvt := log.L().Info()
	if force {
		logEvt = logEvt.Bool("force", true)
	}
	logEvt.Msg("scan: starting channel capability scan")

	m.mu.Lock()
	m.status.State = "running"
	m.status.StartedAt = time.Now().Unix()
	m.status.FinishedAt = 0
	m.status.TotalChannels = 0
	m.status.ScannedChannels = 0
	m.status.UpdatedCount = 0
	m.status.LastError = ""
	m.mu.Unlock()

	// 1. Read and Parse Playlist
	content, err := os.ReadFile(m.m3uPath)
	if err != nil {
		log.L().Error().Err(err).Msg("scan: failed to read playlist")
		m.mu.Lock()
		m.status.State = "failed"
		m.status.FinishedAt = time.Now().Unix()
		m.status.LastError = err.Error()
		m.mu.Unlock()
		return err
	}

	channels := m3u.Parse(string(content))
	log.L().Info().Int("count", len(channels)).Msg("scan: playlist loaded")

	// Shuffle channel order so a single dead service-ref cannot starve the rest
	// across consecutive scan runs.
	rand.Shuffle(len(channels), func(i, j int) {
		channels[i], channels[j] = channels[j], channels[i]
	})

	m.mu.Lock()
	m.status.TotalChannels = len(channels)
	m.mu.Unlock()

	now := time.Now()
	if !force {
		channels = m.filterProbeCandidates(channels, now)
		if len(channels) == 0 {
			log.L().Info().Msg("scan: warm capability cache has no due candidates")
			return nil
		}
	}

	// 2. Iterate and Probe
	updates := 0
	scanned := 0

	defer func() {
		// Capture completion status if not already failed
		m.mu.Lock()
		if m.status.State == "running" {
			if ctx.Err() != nil {
				m.status.State = "cancelled" // Or failed
				m.status.LastError = ctx.Err().Error()
			} else {
				m.status.State = "complete"
			}
			m.status.FinishedAt = time.Now().Unix()
		}
		m.mu.Unlock()
		log.L().Info().Int("updates", updates).Msg("scan: completed")
	}()

	for _, ch := range channels {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := m.waitForPlaybackIdle(ctx); err != nil {
			return err
		}

		sRef := ExtractServiceRef(ch.URL)
		if sRef == "" {
			continue
		}

		// Hold the per-serviceRef lock across this channel's Get→probe→Update so a concurrent
		// targeted ProbeCapability for the same channel cannot lose the update. Released
		// before the rate-limit sleep below so the sleep does not serialize other channels.
		func() {
			unlock := m.lockServiceRefCap(sRef)
			defer unlock()

			existingCap, found := m.store.Get(sRef)

			probeURL := m.resolveProbeURL(ctx, ch, sRef)

			log.L().Debug().Str("sref", sRef).Msg("scan: probing channel")
			res, err := m.probeChannelMediaTruth(ctx, ch, sRef, probeURL, existingCap, found)
			fromStore := found && existingCap.Usable()

			scanned++
			m.mu.Lock()
			m.status.ScannedChannels = scanned
			m.mu.Unlock()

			if err != nil {
				log.L().Warn().Err(err).Str("sref", sRef).Msg("scan: probe failed")
				m.store.Update(m.mergeFailedAttempt(existingCap, found, sRef, ch.Name, time.Now(), err))
				if !fromStore {
					atomic.AddInt32(&m.consecutiveFailureCount, 1)
				}
			} else {
				atomic.StoreInt32(&m.consecutiveFailureCount, 0)
				cap := m.capabilityFromProbe(existingCap, found, sRef, ch.Name, time.Now(), res)

				log.L().Info().
					Str("sref", sRef).
					Str("container", cap.Container).
					Str("video_codec", cap.VideoCodec).
					Str("audio_codec", cap.AudioCodec).
					Bool("interlaced", cap.Interlaced).
					Str("res", cap.Resolution).
					Str("state", string(cap.State)).
					Msg("scan: result")

				m.store.Update(cap)
				updates++
				m.mu.Lock()
				m.status.UpdatedCount = updates
				m.mu.Unlock()
			}
		}()

		if err := m.applyScanRateLimit(ctx); err != nil {
			return err
		}
	}
	return nil
}

// applyScanRateLimit paces the scan loop: adaptive backoff on consecutive
// failures plus ±20% jitter, interruptible by ctx.
func (m *Manager) applyScanRateLimit(ctx context.Context) error {
	delay := m.ProbeDelay

	failCount := atomic.LoadInt32(&m.consecutiveFailureCount)
	if failCount > 0 {
		backoff := 30 * time.Second
		if failCount < 6 {
			multiplier := 1 << (failCount - 1)
			backoff = min(time.Duration(multiplier)*time.Second, 30*time.Second)
		}
		if backoff > delay {
			delay = backoff
		}
		log.L().Debug().Int32("consecutive_failures", failCount).Dur("adaptive_delay", delay).Msg("scan: applying adaptive backoff")
	}

	if delay > 0 {
		jitter := time.Duration(0)
		if jitterRange := int64(delay / 5); jitterRange > 0 {
			jitterN, err := cryptoRandInt63n(jitterRange)
			if err != nil {
				log.L().Warn().Err(err).Msg("scan: jitter entropy unavailable, continuing without jitter")
			} else {
				jitter = time.Duration(jitterN) - (delay / 10)
			}
		}
		if err := sleepCtx(ctx, delay+jitter); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) waitForPlaybackIdle(ctx context.Context) error {
	if m == nil || m.ActivePlaybackFn == nil {
		return nil
	}

	paused := false
	for {
		active, err := m.ActivePlaybackFn(ctx)
		if err != nil {
			log.L().Warn().Err(err).Msg("scan: active playback check failed, continuing without scan pause")
			return nil
		}
		if !active {
			if paused {
				log.L().Info().Msg("scan: playback idle, resuming capability scan")
			}
			return nil
		}
		if !paused {
			log.L().Info().Msg("scan: active playback detected, pausing capability scan")
			paused = true
		}
		if err := sleepCtx(ctx, time.Second); err != nil {
			return err
		}
	}
}

func (m *Manager) hasPendingProbeCandidates(now time.Time) (bool, error) {
	content, err := os.ReadFile(m.m3uPath)
	if err != nil {
		return false, err
	}
	channels := m3u.Parse(string(content))
	for _, ch := range channels {
		sRef := ExtractServiceRef(ch.URL)
		if sRef == "" {
			continue
		}
		if m.shouldProbeService(sRef, now) {
			return true, nil
		}
	}
	return false, nil
}

func (m *Manager) filterProbeCandidates(channels []m3u.Channel, now time.Time) []m3u.Channel {
	filtered := make([]m3u.Channel, 0, len(channels))
	seen := make(map[string]struct{}, len(channels))
	for _, ch := range channels {
		sRef := ExtractServiceRef(ch.URL)
		if sRef == "" {
			continue
		}
		if _, ok := seen[sRef]; ok {
			continue
		}
		seen[sRef] = struct{}{}
		if m.shouldProbeService(sRef, now) {
			filtered = append(filtered, ch)
		}
	}
	return filtered
}

func (m *Manager) shouldProbeService(serviceRef string, now time.Time) bool {
	cap, found := m.store.Get(serviceRef)
	if !found {
		return true
	}
	return cap.RetryDue(now)
}

func (m *Manager) lookupChannel(serviceRef string) (m3u.Channel, bool, error) {
	content, err := os.ReadFile(m.m3uPath)
	if err != nil {
		return m3u.Channel{}, false, err
	}
	channels := m3u.Parse(string(content))
	for _, ch := range channels {
		if ExtractServiceRef(ch.URL) == serviceRef {
			return ch, true, nil
		}
	}
	return m3u.Channel{}, false, nil
}

func (m *Manager) capabilityFromProbe(existing Capability, found bool, serviceRef string, channelName string, now time.Time, info *vod.StreamInfo) Capability {
	cap := existing
	if !found {
		cap = Capability{ServiceRef: serviceRef}
	}
	cap.ServiceRef = serviceRef
	cap.LastAttempt = now.UTC()
	cap.FailureReason = ""
	if info != nil {
		cap.Container = info.Container
		cap.VideoCodec = strings.TrimSpace(info.Video.CodecName)
		cap.AudioCodec = strings.TrimSpace(info.Audio.CodecName)
		if info.BitrateKbps > 0 {
			cap = cap.WithObservedBitrateKbps(info.BitrateKbps)
		}
		cap.Codec = ""
		if cap.VideoCodec != "" {
			cap.Codec = cap.VideoCodec
		} else if cap.AudioCodec != "" {
			cap.Codec = cap.AudioCodec
		}
		cap.Interlaced = info.Video.Interlaced
		cap.Width = info.Video.Width
		cap.Height = info.Video.Height
		cap.FPS = info.Video.FPS
		if info.Video.SignalFPS > 0 {
			cap.SignalFPS = info.Video.SignalFPS
		}
		if fieldOrder := strings.TrimSpace(info.Video.FieldOrder); fieldOrder != "" {
			cap.FieldOrder = fieldOrder
		}
		if info.Audio.Channels > 0 {
			cap.AudioChannels = info.Audio.Channels
		}
		if info.Audio.BitrateKbps > 0 {
			cap.AudioBitrateKbps = info.Audio.BitrateKbps
		}
		if info.Audio.SampleRate > 0 {
			cap.AudioSampleRate = info.Audio.SampleRate
		}
		if channelLayout := strings.TrimSpace(info.Audio.ChannelLayout); channelLayout != "" {
			cap.AudioChannelLayout = channelLayout
		}
	}
	if cap.Width > 0 && cap.Height > 0 {
		cap.Resolution = fmt.Sprintf("%dx%d", cap.Width, cap.Height)
	} else {
		cap.Resolution = ""
	}
	cap.State = inferCapabilityState(cap.Resolution, cap.Codec)
	if cap.State == CapabilityStateOK && !hasCompleteMediaTruth(cap.Container, cap.VideoCodec, cap.AudioCodec) {
		cap.State = CapabilityStatePartial
	}
	if cap.State == CapabilityStateFailed {
		if isLikelyInactiveEventFeed(channelName, nil) {
			cap.State = CapabilityStateInactiveEventFeed
			cap.FailureReason = "inactive_event_feed_no_media_metadata"
		} else {
			cap.FailureReason = "probe_returned_no_media_metadata"
		}
		cap.NextRetryAt = now.UTC().Add(failureRetryWindow)
		cap.LastScan = existing.LastScan
		cap.LastSuccess = existing.LastSuccess
		return cap.Normalized()
	}
	cap.LastScan = now.UTC()
	cap.LastSuccess = now.UTC()
	cap.NextRetryAt = now.UTC().Add(defaultRetryDelay(cap.State))
	return cap.Normalized()
}

func (m *Manager) mergeFailedAttempt(existing Capability, found bool, serviceRef string, channelName string, now time.Time, err error) Capability {
	cap := existing
	if !found {
		cap = Capability{
			ServiceRef: serviceRef,
			State:      CapabilityStateFailed,
		}
	}
	cap.ServiceRef = serviceRef
	cap.LastAttempt = now.UTC()
	cap.FailureReason = strings.TrimSpace(err.Error())
	if cap.FailureReason == "" {
		cap.FailureReason = "probe_failed"
	}
	normalized := cap.Normalized()
	if normalized.State == CapabilityStateInactiveEventFeed || (!normalized.Usable() && isLikelyInactiveEventFeed(channelName, err)) {
		normalized.State = CapabilityStateInactiveEventFeed
		normalized.NextRetryAt = now.UTC().Add(failureRetryWindow)
		return normalized.Normalized()
	}
	switch normalized.State {
	case CapabilityStatePartial:
		normalized.NextRetryAt = now.UTC().Add(partialRetryWindow)
	case CapabilityStateOK:
		normalized.NextRetryAt = now.UTC().Add(failureRetryWindow)
	default:
		normalized.State = CapabilityStateFailed
		normalized.NextRetryAt = now.UTC().Add(failureRetryWindow)
	}
	return normalized.Normalized()
}

func isLikelyInactiveEventFeed(channelName string, err error) bool {
	if !isLikelyEventFeedChannel(channelName) {
		return false
	}
	if err == nil {
		return true
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "stream ends prematurely") ||
		strings.Contains(msg, "signal: killed") ||
		strings.Contains(msg, "input/output error")
}

func isLikelyEventFeedChannel(channelName string) bool {
	name := strings.ToLower(strings.TrimSpace(channelName))
	switch name {
	case "sky sport top event", "sky sport tennis", "sky sport golf", "sky sport premier league", "sky sport mix", "sky sport news", "sky sport f1":
		return true
	}
	if n, ok := parseTrailingChannelNumber(name, "sky sport austria "); ok {
		return n >= 2
	}
	if n, ok := parseTrailingChannelNumber(name, "sky sport bundesliga "); ok {
		return n >= 8
	}
	if n, ok := parseTrailingChannelNumber(name, "sky sport "); ok {
		return n >= 7
	}
	return false
}

func parseTrailingChannelNumber(name string, prefix string) (int, bool) {
	if !strings.HasPrefix(name, prefix) {
		return 0, false
	}
	value := strings.TrimSpace(strings.TrimPrefix(name, prefix))
	if value == "" {
		return 0, false
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0, false
	}
	return n, true
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
