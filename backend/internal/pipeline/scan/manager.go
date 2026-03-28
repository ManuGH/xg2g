package scan

import (
	"bufio"
	"context"
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/vod"
	infra "github.com/ManuGH/xg2g/internal/infra/ffmpeg"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/m3u"
	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/ManuGH/xg2g/internal/normalize"
	"github.com/ManuGH/xg2g/internal/pipeline/exec/enigma2"
)

const (
	backgroundScanTimeout = 30 * time.Minute
	resolveM3UTimeout     = 2 * time.Second
	defaultProbeTimeout   = 8 * time.Second
	extendedProbeTimeout  = 20 * time.Second

	extendedProbeAnalyzeDuration = 15 * time.Second
	extendedProbeSizeBytes       = 8 << 20
)

var errLifecycleContextNotAttached = errors.New("scan: lifecycle context not attached")

var resolveM3UHTTPClient = &http.Client{
	Timeout: resolveM3UTimeout,
	Transport: &http.Transport{
		MaxIdleConns:        16,
		MaxIdleConnsPerHost: 4,
		IdleConnTimeout:     30 * time.Second,
		DisableCompression:  true,
	},
}

type ScanStatus struct {
	State           string `json:"state"`
	StartedAt       int64  `json:"started_at,omitempty"`
	FinishedAt      int64  `json:"finished_at,omitempty"`
	TotalChannels   int    `json:"total_channels"`
	ScannedChannels int    `json:"scanned_channels"`
	UpdatedCount    int    `json:"updated_count"`
	LastError       string `json:"last_error,omitempty"`
}

type Manager struct {
	store      CapabilityStore
	m3uPath    string
	e2Client   *enigma2.Client
	isScanning atomic.Bool
	forceScan  atomic.Bool

	ProbeDelay time.Duration

	mu     sync.RWMutex
	status ScanStatus

	lifecycleMu sync.Mutex
	runtimeCtx  context.Context
	cancel      context.CancelFunc
	bgWG        sync.WaitGroup
	scanFn      func(context.Context) error
	probeFn     func(context.Context, string, infra.ProbeOptions) (*vod.StreamInfo, error)

	// Phase 2: Production-Grade Robustness
	ActivePlaybackFn        func(ctx context.Context) (bool, error)
	consecutiveFailureCount int32
}

func NewManager(store CapabilityStore, m3uPath string, e2Client *enigma2.Client) *Manager {
	if e2Client == nil {
		log.L().Warn().Msg("scan: manager created with NIL enigma2 client (dumb mode)")
	} else {
		log.L().Info().Msg("scan: manager created with enigma2 client (smart mode)")
	}
	return &Manager{
		store:      store,
		m3uPath:    m3uPath,
		e2Client:   e2Client,
		ProbeDelay: 5000 * time.Millisecond,
		probeFn:    infra.ProbeWithOptions,
		status: ScanStatus{
			State: "idle",
		},
		ActivePlaybackFn: func(ctx context.Context) (bool, error) { return false, nil },
	}
}

func (m *Manager) GetCapability(serviceRef string) (Capability, bool) {
	cap, ok := m.store.Get(serviceRef)
	if !ok || !cap.Usable() {
		return Capability{}, false
	}
	return cap, true
}

func (m *Manager) GetStatus() ScanStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status
}

// Close releases the underlying capability store resources.
func (m *Manager) Close() error {
	m.Stop()
	if m == nil || m.store == nil {
		return nil
	}
	return m.store.Close()
}

// AttachLifecycle binds background scans to a parent context (daemon runtime).
func (m *Manager) AttachLifecycle(parent context.Context) {
	if m == nil || parent == nil {
		return
	}

	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()

	if m.cancel != nil {
		m.cancel()
	}
	m.runtimeCtx, m.cancel = context.WithCancel(parent)
}

// Stop cancels all background scans and waits for active goroutines to finish.
func (m *Manager) Stop() {
	if m == nil {
		return
	}

	m.lifecycleMu.Lock()
	cancel := m.cancel
	m.lifecycleMu.Unlock()
	if cancel != nil {
		cancel()
	}
	m.bgWG.Wait()
}

// ExtractServiceRef extracts the service reference from a stream URL
// Robust implementation using net/url
func ExtractServiceRef(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		// Fallback for extremely broken URLs
		parts := strings.Split(rawURL, "/")
		if len(parts) > 0 {
			return parts[len(parts)-1]
		}
		return ""
	}

	// 1. Try 'ref' query parameter (OpenWebIF style)
	if ref := u.Query().Get("ref"); ref != "" {
		return normalize.ServiceRef(ref)
	}

	// 2. Try path splitting (Direct Stream style)
	// Path should be /ServiceRef
	// Handle trailing slashes or query params
	path := strings.TrimSuffix(u.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) > 0 {
		return normalize.ServiceRef(parts[len(parts)-1])
	}
	return ""
}

// RunScan performs the scan synchronously. Returns error if failed.
// Returns nil if scan is already running (deduplicated).
func (m *Manager) RunScan(ctx context.Context) error {
	if m.scanFn == nil {
		hasWork, err := m.hasPendingProbeCandidates(time.Now())
		if err == nil && !hasWork {
			log.L().Info().Msg("scan: warm capability cache, skipping synchronous scan")
			return nil
		}
	}
	if !m.isScanning.CompareAndSwap(false, true) {
		return nil
	}
	defer m.isScanning.Store(false)
	return m.executeScan(ctx)
}

// RunBackground triggers scan in background. Returns true if started, false if already running.
func (m *Manager) RunBackground() bool {
	if m.scanFn == nil {
		hasWork, err := m.hasPendingProbeCandidates(time.Now())
		if err == nil && !hasWork {
			log.L().Info().Msg("scan: warm capability cache, skipping startup/background scan")
			return false
		}
		if err != nil {
			log.L().Warn().Err(err).Msg("scan: failed to evaluate warm cache, continuing with background scan")
		}
	}
	return m.runBackground(false)
}

// RunBackgroundForce triggers a full rescan even when the capability cache is warm.
func (m *Manager) RunBackgroundForce() bool {
	return m.runBackground(true)
}

func (m *Manager) runBackground(force bool) bool {
	if !m.isScanning.CompareAndSwap(false, true) {
		return false
	}
	m.forceScan.Store(force)

	baseCtx, err := m.backgroundContext()
	if err != nil {
		m.isScanning.Store(false)
		m.mu.Lock()
		m.status.State = "failed"
		m.status.FinishedAt = time.Now().Unix()
		m.status.LastError = err.Error()
		m.mu.Unlock()
		log.L().Error().Err(err).Msg("scan: background scan rejected")
		return false
	}
	m.bgWG.Add(1)
	go func() {
		defer m.bgWG.Done()
		defer m.isScanning.Store(false)
		ctx, cancel := context.WithTimeout(baseCtx, backgroundScanTimeout)
		defer cancel()
		if err := m.executeScan(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.L().Error().Err(err).Msg("scan: background scan failed")
		}
	}()
	return true
}

func (m *Manager) backgroundContext() (context.Context, error) {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()

	if m.runtimeCtx == nil {
		return nil, errLifecycleContextNotAttached
	}
	return m.runtimeCtx, nil
}

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

		existingCap, found := m.store.Get(sRef)

		// Resolve stream URL:
		// 1. Try Enigma2 Client resolution (Smart Player Logic)
		// 2. Fallback to M3U resolution (Stale Playlist)
		probeURL := ch.URL
		resolved := false

		if m.e2Client != nil && sRef != "" {
			// Use a short context for resolution
			resCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			freshURL, err := m.e2Client.ResolveStreamURL(resCtx, sRef)
			cancel()

			if err == nil && freshURL != "" {
				probeURL = freshURL
				resolved = true
				log.L().Debug().Str("sref", sRef).Str("fresh_url", freshURL).Msg("scan: resolved fresh stream url")
			} else {
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

		log.L().Debug().Str("sref", sRef).Msg("scan: probing channel")
		res, successfulProbeURL, err := m.probeWithFallbacks(ctx, sRef, ch.URL, probeURL, infra.ProbeOptions{}, defaultProbeTimeout)
		if shouldAttemptExtendedRetry(existingCap, found, res, err) {
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
				log.L().Warn().Err(retryErr).Str("sref", sRef).Msg("scan: extended probe retry failed")
			case isRicherMediaTruth(retryBase, retryRes):
				res = retryRes
				err = nil
				log.L().Info().Str("sref", sRef).Msg("scan: extended probe retry enriched media truth")
			default:
				log.L().Warn().Str("sref", sRef).Msg("scan: extended probe retry returned conflicting or non-additive media truth; keeping original result")
			}
		}
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

		// Phase 2: Production-Grade Rate Limiting
		delay := m.ProbeDelay

		// 1. Adaptive Backoff: Increase delay on consecutive failures
		failCount := atomic.LoadInt32(&m.consecutiveFailureCount)
		if failCount > 0 {
			multiplier := 1 << (failCount - 1)
			backoff := time.Duration(multiplier) * time.Second
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
			if backoff > delay {
				delay = backoff
			}
			log.L().Debug().Int32("consecutive_failures", failCount).Dur("adaptive_delay", delay).Msg("scan: applying adaptive backoff")
		}

		// 2. Apply Delay with Jitter (±20%)
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
		(strings.TrimSpace(info.Container) == "" || strings.TrimSpace(info.Audio.CodecName) == "")
}

func streamInfoFromCapability(cap Capability) *vod.StreamInfo {
	normalized := cap.Normalized()
	if normalized.Container == "" &&
		normalized.VideoCodec == "" &&
		normalized.AudioCodec == "" &&
		normalized.Width == 0 &&
		normalized.Height == 0 &&
		normalized.FPS == 0 {
		return nil
	}
	return &vod.StreamInfo{
		Container: normalized.Container,
		Video: vod.VideoStreamInfo{
			CodecName:  normalized.VideoCodec,
			Width:      normalized.Width,
			Height:     normalized.Height,
			FPS:        normalized.FPS,
			Interlaced: normalized.Interlaced,
		},
		Audio: vod.AudioStreamInfo{
			CodecName: normalized.AudioCodec,
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
			candidate.Video.Width > 0 ||
			candidate.Video.Height > 0 ||
			candidate.Video.FPS > 0
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
	if !compareStrictAdditiveInt(base.Video.Width, candidate.Video.Width, &added) {
		return false
	}
	if !compareStrictAdditiveInt(base.Video.Height, candidate.Video.Height, &added) {
		return false
	}
	if !compareStrictAdditiveFloat(base.Video.FPS, candidate.Video.FPS, &added) {
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
	}
	if cap.Width > 0 && cap.Height > 0 {
		cap.Resolution = fmt.Sprintf("%dx%d", cap.Width, cap.Height)
	} else {
		cap.Resolution = ""
	}
	cap.State = inferCapabilityState(cap.Resolution, cap.Codec)
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

func cryptoRandInt63n(maxExclusive int64) (int64, error) {
	if maxExclusive <= 0 {
		return 0, nil
	}
	n, err := cryptorand.Int(cryptorand.Reader, big.NewInt(maxExclusive))
	if err != nil {
		return 0, err
	}
	return n.Int64(), nil
}

// resolveStreamURL follows M3U playlists to find the actual stream URL.
// Returns original URL if not M3U or on error.
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
