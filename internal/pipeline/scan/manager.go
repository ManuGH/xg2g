package scan

import (
	"bufio"
	"context"
	"fmt"
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
	"github.com/ManuGH/xg2g/internal/pipeline/exec/enigma2"
)

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
		ProbeDelay: 1000 * time.Millisecond,
		status: ScanStatus{
			State: "idle",
		},
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
	return m.scanInternal(ctx)
}

// RunBackground triggers scan in background. Returns true if started, false if already running.
func (m *Manager) RunBackground() bool {
	if !m.isScanning.CompareAndSwap(false, true) {
		return false
	}

	go func() {
		defer m.isScanning.Store(false)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		if err := m.scanInternal(ctx); err != nil {
			log.L().Error().Err(err).Msg("scan: background scan failed")
		}
	}()
	return true
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
			if res, err := resolveStreamURL(ctx, ch.URL); err == nil && res != "" {
				probeURL = res
			}
		}

		// Probe with strict timeout (prevent hanging on zombie streams)
		probeCtx, probeCancel := context.WithTimeout(ctx, 8*time.Second)
		log.L().Debug().Str("sref", sRef).Msg("scan: probing channel")
		res, err := infra.Probe(probeCtx, probeURL)
		probeCancel() // Start fresh context for fallback if needed

		// Fallback Logic: If probe fails and we have a resolved URL (or even if not), try 8001
		// This handles the case where "official" stream URL (17999) is closed but 8001 works.
		if err != nil {
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

		scanned++
		m.mu.Lock()
		m.status.ScannedChannels = scanned
		m.mu.Unlock()

		if err != nil {
			log.L().Warn().Err(err).Str("sref", sRef).Msg("scan: probe failed")
			// We don't abort loop on probe failure, just continue
			// But check rate limiting before continue? Yes
		} else {
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

		// Rate limit
		if m.ProbeDelay > 0 {
			if err := sleepCtx(ctx, m.ProbeDelay); err != nil {
				// If sleep is interrupted by context, we should stop scanning
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
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
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
