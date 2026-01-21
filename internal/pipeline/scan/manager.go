package scan

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	infra "github.com/ManuGH/xg2g/internal/infra/ffmpeg"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/m3u"
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
	store      *Store
	m3uPath    string
	isScanning atomic.Bool

	ProbeDelay time.Duration

	mu     sync.RWMutex
	status ScanStatus
}

func NewManager(store *Store, m3uPath string) *Manager {
	return &Manager{
		store:      store,
		m3uPath:    m3uPath,
		ProbeDelay: 500 * time.Millisecond,
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
		m.store.Save()
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

		// Probe
		log.L().Debug().Str("sref", sRef).Msg("scan: probing channel")
		res, err := infra.Probe(ctx, ch.URL)

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
