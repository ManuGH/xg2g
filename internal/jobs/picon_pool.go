// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package jobs

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/rs/zerolog/log"
)

// PiconPoolConfig defines configuration for the PiconPool.
type PiconPoolConfig struct {
	Workers       int
	QueueSize     int
	NegTTL        time.Duration
	ClientTimeout time.Duration
}

// PiconPool manages concurrent picon downloads.
type PiconPool struct {
	upstreamBase string
	piconDir     string

	client http.Client

	jobs    chan string // Job queue
	workers int         // Number of workers

	ctx    context.Context    // Pool context for cancellation
	cancel context.CancelFunc // Cancel function for pool shutdown

	wg   sync.WaitGroup
	once sync.Once

	// inflight dedupe across calls
	inflightMu sync.Mutex
	inflight   map[string]struct{}

	// negative cache (ref -> expiresAt)
	negMu  sync.Mutex
	neg    map[string]time.Time
	negTTL time.Duration

	stopOnce sync.Once
}

var (
	globalPool     *PiconPool
	globalPoolOnce sync.Once
)

// InitPiconPool initializes the global PiconPool singleton.
func InitPiconPool(cfg config.AppConfig) {
	globalPoolOnce.Do(func() {
		upstreamBase := cfg.PiconBase
		if upstreamBase == "" {
			upstreamBase = cfg.OWIBase
		}

		piconDir := filepath.Join(cfg.DataDir, "picons")
		if err := os.MkdirAll(piconDir, 0750); err != nil {
			log.Error().Err(err).Msg("Picon: failed to create cache dir, worker pool will likely fail")
		}

		// Defaults
		conf := PiconPoolConfig{
			Workers:       8,
			QueueSize:     512,
			NegTTL:        10 * time.Minute,
			ClientTimeout: 30 * time.Second,
		}

		globalPool = NewPiconPool(upstreamBase, piconDir, conf)
		globalPool.Start()
		log.Info().
			Int("workers", conf.Workers).
			Int("queue_size", conf.QueueSize).
			Msg("Picon: Global worker pool initialized")
	})
}

// GetPiconPool returns the global singleton, taking care to initialize it logic safely.
func GetPiconPool(cfg config.AppConfig) *PiconPool {
	// Always call Init to ensure Once has run, handling the race where globalPool is nil
	// but Init is running elsewhere.
	InitPiconPool(cfg)
	return globalPool
}

func NewPiconPool(upstreamBase, piconDir string, cfg PiconPoolConfig) *PiconPool {
	// Ensure defaults if zero (double safety)
	if cfg.Workers <= 0 {
		cfg.Workers = 8
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 512
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &PiconPool{
		upstreamBase: upstreamBase,
		piconDir:     piconDir,
		client:       http.Client{Timeout: cfg.ClientTimeout},
		jobs:         make(chan string, cfg.QueueSize),
		workers:      cfg.Workers,
		ctx:          ctx,
		cancel:       cancel,
		inflight:     make(map[string]struct{}),
		neg:          make(map[string]time.Time),
		negTTL:       cfg.NegTTL,
	}
}

func (p *PiconPool) Start() {
	p.once.Do(func() {
		// Launch workers
		for i := 0; i < p.workers; i++ {
			p.wg.Add(1)
			go func() {
				defer p.wg.Done()
				for ref := range p.jobs {
					// Use pool context for common cancellation
					p.handle(p.ctx, ref)
				}
			}()
		}
		// periodic neg-cache cleanup
		p.wg.Add(1)
		go p.negCleanupLoop()
	})
}

func (p *PiconPool) Stop() {
	p.stopOnce.Do(func() {
		// Cancel in-flight requests
		p.cancel()
		// Stop accepting new jobs
		close(p.jobs)
		// Wait for workers to drain and cleanup loop to exit
		p.wg.Wait()
	})
}

// Enqueue attempts to add a ref to the download queue.
func (p *PiconPool) Enqueue(ctx context.Context, ref string) (enqueued bool) {
	if ref == "" {
		return false
	}

	// global inflight dedupe
	p.inflightMu.Lock()
	if _, ok := p.inflight[ref]; ok {
		p.inflightMu.Unlock()
		metrics.IncPiconFetch("dedup")
		return true // Considered "handled" even if redundant
	}
	p.inflight[ref] = struct{}{}
	p.inflightMu.Unlock()

	// fast negative-cache gate
	if p.isNegCached(ref) {
		p.clearInflight(ref)
		metrics.IncPiconFetch("negcache")
		return true // Handled via cache
	}

	select {
	case <-ctx.Done():
		p.clearInflight(ref)
		return false
	case p.jobs <- ref:
		// metrics: queue accepted
		return true
	default:
		// queue full -> drop
		p.clearInflight(ref)
		metrics.IncPiconFetch("dropped")
		return false
	}
}

func (p *PiconPool) handle(ctx context.Context, ref string) {
	defer p.clearInflight(ref)

	// Check on-disk first (cache hit)
	storeRef := strings.TrimRight(strings.ReplaceAll(ref, ":", "_"), "_")
	localPath := filepath.Join(p.piconDir, storeRef+".png")
	if _, err := os.Stat(localPath); err == nil {
		metrics.IncPiconFetch("hit_disk")
		return
	}

	// Check neg-cache again (race safety)
	if p.isNegCached(ref) {
		metrics.IncPiconFetch("negcache")
		return
	}

	// Attempt download (includes fallback normalization)
	status := p.downloadOne(ctx, ref, localPath)
	switch status {
	case http.StatusOK:
		metrics.IncPiconFetch("downloaded")
	case http.StatusNotFound:
		p.setNeg(ref)
		// Pragmatic optimization: also cache the normalized ref if it's different,
		// to avoid retrying the fallback logic next time.
		if normalized := openwebif.NormalizeServiceRefForPicon(ref); normalized != "" && normalized != ref {
			p.setNeg(normalized)
		}
		metrics.IncPiconFetch("notfound")
	default:
		metrics.IncPiconFetch("error")
	}
}

func (p *PiconPool) downloadOne(ctx context.Context, ref, localPath string) int {
	// 1) primary
	upURL := openwebif.PiconURL(p.upstreamBase, ref)
	code, ok := p.tryFetchToFile(ctx, upURL, localPath)
	if ok {
		return code
	}

	// 2) fallback normalized
	normalized := openwebif.NormalizeServiceRefForPicon(ref)
	if normalized != "" && normalized != ref {
		fURL := openwebif.PiconURL(p.upstreamBase, normalized)
		code2, ok2 := p.tryFetchToFile(ctx, fURL, localPath)
		if ok2 {
			return code2
		}
		// if normalized attempt was 404, treat as notfound
		if code2 == http.StatusNotFound {
			return http.StatusNotFound
		}
	}

	// if primary was 404 and fallback didn’t succeed -> notfound
	if code == http.StatusNotFound {
		return http.StatusNotFound
	}

	// otherwise treat as generic failure
	if code == 0 {
		return 500
	}
	return code
}

func (p *PiconPool) tryFetchToFile(ctx context.Context, url, localPath string) (status int, ok bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, false
	}
	// Use shared client
	resp, err := p.client.Do(req)
	if err != nil {
		return 0, false
	}
	defer func() { _ = resp.Body.Close() }()

	status = resp.StatusCode
	if status != http.StatusOK {
		return status, false
	}

	// Reuse existing tempfile logic
	if err := writeAtomic(localPath, p.piconDir, resp.Body); err != nil {
		return 500, false
	}
	return http.StatusOK, true
}

func (p *PiconPool) clearInflight(ref string) {
	p.inflightMu.Lock()
	delete(p.inflight, ref)
	p.inflightMu.Unlock()
}

func (p *PiconPool) isNegCached(ref string) bool {
	p.negMu.Lock()
	defer p.negMu.Unlock()
	exp, ok := p.neg[ref]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(p.neg, ref)
		return false
	}
	return true
}

func (p *PiconPool) setNeg(ref string) {
	p.negMu.Lock()
	p.neg[ref] = time.Now().Add(p.negTTL)
	p.negMu.Unlock()
}

func (p *PiconPool) negCleanupLoop() {
	defer p.wg.Done()
	t := time.NewTicker(2 * time.Minute)
	defer t.Stop()

	for {
		select {
		case <-p.ctx.Done(): // Stop signal
			return
		case <-t.C:
			now := time.Now()
			p.negMu.Lock()
			for k, exp := range p.neg {
				if now.After(exp) {
					delete(p.neg, k)
				}
			}
			p.negMu.Unlock()
		}
	}
}

// writeAtomic writes data to a temp file and renames it to dest atomically.
func writeAtomic(dest, dir string, r io.Reader) error {
	tempFile, err := os.CreateTemp(dir, "picon-warm-*.tmp")
	if err != nil {
		return err
	}
	defer func() {
		_ = os.Remove(tempFile.Name())
	}()

	if _, err := io.Copy(tempFile, r); err != nil {
		_ = tempFile.Close()
		return err
	}

	// Ensure data is on disk
	if err := tempFile.Sync(); err != nil {
		_ = tempFile.Close()
		return err
	}

	// Close the file explicitly before renaming
	if err := tempFile.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempFile.Name(), dest); err != nil {
		return err
	}
	return nil
}
