package scan

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/vod"
	infra "github.com/ManuGH/xg2g/internal/infra/ffmpeg"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/m3u"
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

	scanRetryInitialDelay = 5 * time.Minute
	scanRetryMaxDelay     = 30 * time.Minute
	scanRetryMaxAttempts  = 3
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

	// capLocks serializes the store Get→probe→Update read-modify-write PER serviceRef across
	// the background scan loop and targeted ProbeCapability. Without it, concurrent runs on
	// the same channel both read the old capability, probe, then the later Update clobbers
	// the earlier one (lost update — corrupting capability truth for up to 24h). Keyed per
	// serviceRef so different channels still scan in parallel; bounded by the channel count.
	// Lock ordering: capLocks (per key) may nest m.mu (scanInternal status updates); no path
	// takes m.mu before a capLocks key, so there is no cycle.
	//
	// The lock is held across the probe (real receiver I/O), which is required for RMW
	// atomicity, but every probe attempt is hard-bounded by runProbeAttempt's
	// context.WithTimeout (defaultProbeTimeout 8s / extendedProbeTimeout 20s), and the only
	// other I/O under the lock (resolveProbeURL) is bounded by resolveM3UTimeout. So a hung
	// receiver cannot hold a key's lock indefinitely — at most a finite sum of those
	// timeouts for that one channel; other channels stay parallel.
	capLocks sync.Map // serviceRef -> *sync.Mutex

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

// lockServiceRefCap acquires the per-serviceRef capability RMW lock and returns its unlock
// func. Callers must hold it across the whole Get→probe→Update sequence (but not around
// long unrelated waits such as the scan rate-limit sleep).
func (m *Manager) lockServiceRefCap(serviceRef string) func() {
	v, _ := m.capLocks.LoadOrStore(serviceRef, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func (m *Manager) GetStatus() ScanStatus {
	m.mu.RLock()
	status := m.status
	m.mu.RUnlock()

	if status.State != "idle" || status.TotalChannels > 0 || status.ScannedChannels > 0 || status.UpdatedCount > 0 {
		return status
	}

	return m.deriveIdleStatusFromCache(status)
}

func (m *Manager) deriveIdleStatusFromCache(status ScanStatus) ScanStatus {
	if m == nil || m.store == nil || strings.TrimSpace(m.m3uPath) == "" {
		return status
	}

	content, err := os.ReadFile(m.m3uPath)
	if err != nil {
		return status
	}

	channels := m3u.Parse(string(content))
	if len(channels) == 0 {
		return status
	}

	seen := make(map[string]struct{}, len(channels))
	total := 0
	cached := 0
	latestFinishedAt := int64(0)

	for _, ch := range channels {
		serviceRef := ExtractServiceRef(ch.URL)
		if serviceRef == "" {
			continue
		}
		if _, ok := seen[serviceRef]; ok {
			continue
		}
		seen[serviceRef] = struct{}{}
		total++

		cap, found := m.store.Get(serviceRef)
		if !found {
			continue
		}
		cached++

		anchor := cap.LastSuccess
		if anchor.IsZero() {
			anchor = cap.LastScan
		}
		if anchor.IsZero() {
			anchor = cap.LastAttempt
		}
		if ts := anchor.UTC().Unix(); ts > latestFinishedAt {
			latestFinishedAt = ts
		}
	}

	if total == 0 {
		return status
	}

	status.TotalChannels = total
	status.ScannedChannels = cached
	status.UpdatedCount = cached
	if latestFinishedAt > 0 {
		status.FinishedAt = latestFinishedAt
	}
	return status
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

		backoff := scanRetryInitialDelay
		for attempt := 1; attempt <= scanRetryMaxAttempts; attempt++ {
			ctx, cancel := context.WithTimeout(baseCtx, backgroundScanTimeout)
			err := m.executeScan(ctx)
			cancel()

			if err == nil {
				return
			}

			if errors.Is(err, context.Canceled) {
				if baseCtx.Err() != nil {
					log.L().Warn().Err(err).Int("attempt", attempt).Msg("scan: lifecycle cancelled, stopping retry")
				} else {
					log.L().Warn().Err(err).Int("attempt", attempt).Msg("scan: scan cancelled internally, stopping retry")
				}
				return
			}

			if attempt < scanRetryMaxAttempts {
				log.L().Error().Err(err).Int("attempt", attempt).Int("max_attempts", scanRetryMaxAttempts).Dur("next_retry_in", backoff).Msg("scan: background scan failed, scheduling retry")

				// Re-arm force scan so the next retry re-evaluates all channels.
				// mergeFailedAttempt sets NextRetryAt to failureRetryWindow (24h), which
				// would otherwise cause filterProbeCandidates to skip the failed channels.
				m.forceScan.Store(true)

				select {
				case <-time.After(backoff):
				case <-baseCtx.Done():
					log.L().Warn().Err(baseCtx.Err()).Int("attempt", attempt).Msg("scan: lifecycle cancelled during retry backoff")
					return
				}

				backoff *= 2
				if backoff > scanRetryMaxDelay {
					backoff = scanRetryMaxDelay
				}
			} else {
				log.L().Error().Err(err).Int("attempt", attempt).Int("max_attempts", scanRetryMaxAttempts).Msg("scan: background scan failed, max attempts reached")
			}
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
