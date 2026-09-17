package recordings

import (
	"context"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/control/vod"
	"github.com/ManuGH/xg2g/internal/domain/recordings/model"
	"github.com/ManuGH/xg2g/internal/log"
)

// AutoPrepareConfig configures the background recording pre-caching worker.
type AutoPrepareConfig struct {
	Enabled       bool
	Interval      time.Duration
	MaxConcurrent int
}

// AutoPreparePreparer defines the minimal interface needed to pre-package recordings.
type AutoPreparePreparer interface {
	EnsurePrepared(ctx context.Context, recordingID string) error
}

// AutoPrepareWorker is a background daemon that proactively packages completed
// recordings from the receiver into the SSD HLS cache using smart stream-copy,
// enabling instant (<50ms) startup and zero-delay seeking when played by users.
type AutoPrepareWorker struct {
	cfg        AutoPrepareConfig
	recSvc     Service
	preparer   AutoPreparePreparer
	vodManager *vod.Manager
	failed     map[string]int
	mu         sync.Mutex
}

// NewAutoPrepareWorker constructs a new AutoPrepareWorker.
func NewAutoPrepareWorker(cfg AutoPrepareConfig, recSvc Service, preparer AutoPreparePreparer, vodManager *vod.Manager) *AutoPrepareWorker {
	if cfg.Interval <= 0 {
		cfg.Interval = 60 * time.Second
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 1
	}
	return &AutoPrepareWorker{
		cfg:        cfg,
		recSvc:     recSvc,
		preparer:   preparer,
		vodManager: vodManager,
		failed:     make(map[string]int),
	}
}

// Start launches the background worker in a goroutine tied to ctx.
func (w *AutoPrepareWorker) Start(ctx context.Context) {
	if !w.cfg.Enabled {
		log.L().Info().Msg("dvr_autoprepare: worker disabled by configuration")
		return
	}
	log.L().Info().Dur("interval", w.cfg.Interval).Int("max_concurrent", w.cfg.MaxConcurrent).Msg("dvr_autoprepare: background pre-caching worker started")
	go w.RunLoop(ctx)
}

// RunLoop runs the periodic auto-prepare check until ctx is cancelled.
func (w *AutoPrepareWorker) RunLoop(ctx context.Context) {
	// Initial pass after a brief delay
	select {
	case <-ctx.Done():
		return
	case <-time.After(5 * time.Second):
		w.RunOnce(ctx)
	}

	ticker := time.NewTicker(w.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.RunOnce(ctx)
		}
	}
}

// RunOnce performs a single pass over completed recordings.
func (w *AutoPrepareWorker) RunOnce(ctx context.Context) {
	if w.recSvc == nil || w.preparer == nil {
		return
	}

	// 1. Back off if transcoder is already busy
	if w.vodManager != nil {
		active := len(w.vodManager.ActiveJobIDs())
		if active >= w.cfg.MaxConcurrent {
			log.L().Debug().Int("active_jobs", active).Msg("dvr_autoprepare: transcoder busy, skipping cycle")
			return
		}
	}

	// 2. Fetch completed recordings
	res, err := w.recSvc.List(ctx, ListInput{})
	if err != nil {
		log.L().Debug().Err(err).Msg("dvr_autoprepare: failed to list recordings")
		return
	}

	// 3. Process completed recordings sequentially
	for _, item := range res.Recordings {
		if ctx.Err() != nil {
			return
		}
		if item.Status != model.RecordingStatusCompleted {
			continue
		}

		w.mu.Lock()
		failures := w.failed[item.ServiceRef]
		w.mu.Unlock()
		if failures >= 3 {
			continue // Avoid retry storm for corrupt/inaccessible files
		}

		// Ensure we don't exceed concurrency limit
		if w.vodManager != nil && len(w.vodManager.ActiveJobIDs()) >= w.cfg.MaxConcurrent {
			break
		}

		log.L().Info().Str("title", item.Title).Str("ref", item.ServiceRef).Msg("dvr_autoprepare: pre-packaging completed recording to SSD cache")
		if err := w.preparer.EnsurePrepared(ctx, item.ServiceRef); err != nil {
			log.L().Warn().Err(err).Str("ref", item.ServiceRef).Msg("dvr_autoprepare: failed to prepare recording")
			w.mu.Lock()
			w.failed[item.ServiceRef]++
			w.mu.Unlock()
		}
	}
}
