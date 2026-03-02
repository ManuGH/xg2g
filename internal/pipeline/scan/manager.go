package scan

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	infra "github.com/ManuGH/xg2g/internal/infra/ffmpeg"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/m3u"
	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/ManuGH/xg2g/internal/pipeline/exec/enigma2"
)

const (
	backgroundScanTimeout = 30 * time.Minute
	resolveM3UTimeout     = 2 * time.Second
)

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

	ProbeDelay time.Duration

	mu     sync.RWMutex
	status ScanStatus

	lifecycleMu sync.Mutex
	runtimeCtx  context.Context
	cancel      context.CancelFunc
	bgWG        sync.WaitGroup
	scanFn      func(context.Context) error

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
		status: ScanStatus{
			State: "idle",
		},
		ActivePlaybackFn: func(ctx context.Context) (bool, error) { return false, nil },
	}
}

func (m *Manager) GetCapability(serviceRef string) (Capability, bool) {
	return m.store.Get(serviceRef)
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
		return ref
	}

	// 2. Try path splitting (Direct Stream style)
	// Path should be /ServiceRef
	// Handle trailing slashes or query params
	path := strings.TrimSuffix(u.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return ""
}

// RunScan performs the scan synchronously. Returns error if failed.
// Returns nil if scan is already running (deduplicated).
func (m *Manager) RunScan(ctx context.Context) error {
	if !m.isScanning.CompareAndSwap(false, true) {
		return nil
	}
	defer m.isScanning.Store(false)
	return m.executeScan(ctx)
}

// RunBackground triggers scan in background. Returns true if started, false if already running.
func (m *Manager) RunBackground() bool {
	if !m.isScanning.CompareAndSwap(false, true) {
		return false
	}

	baseCtx := m.backgroundContext()
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

func (m *Manager) backgroundContext() context.Context {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()

	if m.runtimeCtx == nil {
		m.runtimeCtx, m.cancel = context.WithCancel(context.TODO())
		log.L().Warn().Msg("scan: lifecycle context not attached; using detached TODO context")
	}
	return m.runtimeCtx
}

func (m *Manager) executeScan(ctx context.Context) error {
	if m.scanFn != nil {
		return m.scanFn(ctx)
	}
	return m.scanInternal(ctx)
}

func (m *Manager) scanInternal(ctx context.Context) error {
	log.L().Info().Msg("scan: starting channel capability scan")

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

		sRef := ExtractServiceRef(ch.URL)
		if sRef == "" {
			continue
		}

		// Optimization: Check if we already have this capability in store
		// If found, skip expensive probing (especially critical for single-tuner setups with 20s lock time)
		if cap, found := m.store.Get(sRef); found && cap.Resolution != "" {
			log.L().Debug().Str("sref", sRef).Msg("scan: found in cache, skipping probe")
			scanned++ // Count as scanned for stats
			continue
		}

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

		// Probe with strict timeout (prevent hanging on zombie streams)
		probeCtx, probeCancel := context.WithTimeout(ctx, 8*time.Second)
		log.L().Debug().Str("sref", sRef).Msg("scan: probing channel")
		metrics.SetScanInflightProbes(1)
		res, err := infra.Probe(probeCtx, probeURL)
		metrics.SetScanInflightProbes(0)
		probeCancel() // Start fresh context for fallback if needed

		// Fallback Logic: If probe fails and we have a resolved URL (or even if not), try 8001
		// This handles the case where "official" stream URL (17999) is closed but 8001 works.
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				metrics.IncScanProbeTimeout()
			}
			log.L().Warn().Err(err).Str("sref", sRef).Msg("scan: initial probe failed, attempting port 8001 fallback")

			fallbackURL, buildErr := buildFallbackURL(probeURL, sRef)
			if buildErr == nil {
				// Use fresh timeout for fallback
				fbCtx, fbCancel := context.WithTimeout(ctx, 8*time.Second)
				resFallback, errFallback := infra.Probe(fbCtx, fallbackURL)
				fbCancel()

				if errFallback == nil {
					log.L().Info().Str("sref", sRef).Msg("scan: fallback to 8001 succeeded")
					res = resFallback
					err = nil
				} else {
					log.L().Warn().Err(errFallback).Str("sref", sRef).Msg("scan: fallback 8001 probe failed")
				}
			}
		}

		// Fallback Logic Level 3: Original URL (e.g. /web/stream.m3u)
		// If explicit ports failed, try the original M3U URL provided in the playlist.
		// This respects "UseWebIFStreams" scenarios where the user relies on the API endpoint itself.
		if err != nil {
			log.L().Warn().Str("sref", sRef).Msg("scan: attempting fallback to original URL (web)")
			fbCtx, fbCancel := context.WithTimeout(ctx, 8*time.Second)
			resOrig, errOrig := infra.Probe(fbCtx, ch.URL)
			fbCancel()

			if errOrig == nil {
				log.L().Info().Str("sref", sRef).Msg("scan: fallback to original URL succeeded")
				res = resOrig
				err = nil
			} else {
				log.L().Warn().Err(errOrig).Str("sref", sRef).Msg("scan: final fallback failed")
			}
		}
		fromStore := false
		if cap, found := m.store.Get(sRef); found && cap.Resolution != "" {
			fromStore = true
		}

		scanned++
		m.mu.Lock()
		m.status.ScannedChannels = scanned
		m.mu.Unlock()

		if err != nil {
			log.L().Warn().Err(err).Str("sref", sRef).Msg("scan: probe failed")
			if !fromStore {
				atomic.AddInt32(&m.consecutiveFailureCount, 1)
			}
		} else {
			atomic.StoreInt32(&m.consecutiveFailureCount, 0)
			cap := Capability{
				ServiceRef: sRef,
				Interlaced: res.Video.Interlaced,
				LastScan:   time.Now(),
				Resolution: fmt.Sprintf("%dx%d", res.Video.Width, res.Video.Height),
				Codec:      res.Video.CodecName,
			}

			log.L().Info().
				Str("sref", sRef).
				Bool("interlaced", cap.Interlaced).
				Str("res", cap.Resolution).
				Msg("scan: result")

			m.store.Update(cap)
			updates++
			m.mu.Lock()
			m.status.UpdatedCount = updates
			m.mu.Unlock()
		}

		// Phase 2: Production-Grade Rate Limiting
		delay := m.ProbeDelay

		// 1. Playback Awareness: Throttle if playback active
		if active, pbErr := m.ActivePlaybackFn(ctx); pbErr == nil && active {
			delay = 10 * time.Second
			log.L().Debug().Msg("scan: playback active detected, throttling scan delay to 10s")
		}

		// 2. Adaptive Backoff: Increase delay on consecutive failures
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

		// 3. Apply Delay with Jitter (±20%)
		if delay > 0 {
			jitter := time.Duration(rand.Int63n(int64(delay/5))) - (delay / 10)
			if err := sleepCtx(ctx, delay+jitter); err != nil {
				return err
			}
		}
	}
	return nil
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
