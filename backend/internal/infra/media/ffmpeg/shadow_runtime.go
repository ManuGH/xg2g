package ffmpeg

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/pipeline/store"
	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog"
)

// ShadowRuntime holds the active RAM shadow store, publisher, and lifecycle handles
// for a session that mirrors native fMP4 output to RAM.
type ShadowRuntime struct {
	Store     *store.RAMShadowStore
	Pub       *store.ShadowPublisher
	Handle    store.RegistrationHandle
	sessionID string
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	logger    zerolog.Logger
	closeOnce sync.Once
}

// attachShadowStore creates and attaches a RAM shadow store for fMP4 sessions.
// It monitors the session directory for finalized init.mp4 and seg_*.m4s files and
// mirrors them into RAM after atomic finalization by FFmpeg.
func (a *LocalAdapter) attachShadowStore(ctx context.Context, sessionID string, plan ports.ExecutedFFmpegPlan, sessionDir string) (*ShadowRuntime, error) {
	if !a.Config.ShadowStoreEnabled || a.StoreRegistry == nil {
		return nil, nil
	}

	// Only mirror fMP4 output to RAM. MPEG-TS remains disk-only.
	if !strings.EqualFold(strings.TrimSpace(plan.Container), "fmp4") {
		return nil, nil
	}

	maxBytes := a.Config.ShadowStoreMaxBytes
	if maxBytes <= 0 {
		maxBytes = 134217728 // 128 MB default
	}

	queueBytes := a.Config.ShadowStoreQueueMaxBytes
	if queueBytes <= 0 {
		queueBytes = 16777216 // 16 MB default
	}

	maxObjects := a.Config.ShadowStoreMaxObjects
	if maxObjects <= 0 {
		maxObjects = 32 // 32 objects default
	}

	logger := a.Logger.With().
		Str("component", "shadow_store").
		Str("session_id", sessionID).
		Logger()

	ram, err := store.NewRAMShadowStore(maxBytes, maxObjects)
	if err != nil {
		logger.Error().Err(err).Msg("failed to create RAM shadow store for session")
		return nil, err
	}

	regHandle, err := a.StoreRegistry.Register(sessionID, ram)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to register RAM shadow store in registry")
		return nil, err
	}

	pub, err := store.NewShadowPublisher(ram, maxObjects, queueBytes, logger)
	if err != nil {
		_ = regHandle.Close()
		logger.Error().Err(err).Msg("failed to create shadow publisher for session")
		return nil, err
	}
	pub.Start()

	runCtx, cancel := context.WithCancel(ctx)
	sr := &ShadowRuntime{
		Store:     ram,
		Pub:       pub,
		Handle:    regHandle,
		sessionID: sessionID,
		ctx:       runCtx,
		cancel:    cancel,
		done:      make(chan struct{}),
		logger:    logger,
	}

	sr.startMonitoring(sessionDir)
	sr.startStatsLogging()
	return sr, nil
}

func (sr *ShadowRuntime) startMonitoring(sessionDir string) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		sr.logger.Error().Err(err).Msg("failed to create fsnotify watcher, falling back to pure polling")
	} else {
		err = watcher.Add(sessionDir)
		if err != nil {
			sr.logger.Error().Err(err).Msg("failed to add session directory to watcher")
			watcher.Close()
			watcher = nil
		}
	}

	go func() {
		defer close(sr.done)
		var watcherEvents <-chan fsnotify.Event
		var watcherErrors <-chan error
		if watcher != nil {
			defer watcher.Close()
			watcherEvents = watcher.Events
			watcherErrors = watcher.Errors
		}

		fallbackTicker := time.NewTicker(500 * time.Millisecond)
		defer fallbackTicker.Stop()

		seen := make(map[string]time.Time)

		processFile := func(name string) {
			if strings.HasSuffix(name, ".tmp") || strings.HasSuffix(name, ".m3u8") || strings.HasSuffix(name, ".ts") {
				return
			}

			var kind store.ObjectKind
			var contentType string
			if strings.HasPrefix(name, "init") && strings.HasSuffix(name, ".mp4") {
				kind = store.ObjectInit
				contentType = "video/mp4"
			} else if strings.HasPrefix(name, "seg_") && strings.HasSuffix(name, ".m4s") {
				kind = store.ObjectSegment
				contentType = "video/iso.segment"
			} else {
				return
			}
			
			if _, ok := seen[name]; ok {
				return
			}

			filePath := filepath.Join(sessionDir, name)
			info, err := os.Stat(filePath)
			if err != nil || info.Size() == 0 {
				return
			}

			modTime := info.ModTime()
			data, err := os.ReadFile(filePath)
			if err != nil || len(data) == 0 || int64(len(data)) != info.Size() {
				return
			}

			seen[name] = modTime
			err = sr.Pub.Publish(sr.ctx, store.StreamID(sr.sessionID), store.Object{
				Name:        name,
				Kind:        kind,
				ContentType: contentType,
				Data:        data,
				PublishedAt: modTime,
			})
			if err == nil {
				sr.logger.Debug().Str("file", name).Int("bytes", len(data)).Msg("mirrored finalized file to shadow store")
			} else {
				sr.logger.Warn().Err(err).Str("file", name).Msg("failed to publish mirrored file to shadow store")
			}
		}

		for {
			select {
			case <-sr.ctx.Done():
				return
			case err, ok := <-watcherErrors:
				if !ok {
					watcherErrors = nil
				} else if err != nil {
					sr.logger.Warn().Err(err).Msg("fsnotify watcher error")
				}
			case event, ok := <-watcherEvents:
				if !ok {
					watcherEvents = nil
					continue
				}
				name := filepath.Base(event.Name)
				if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
					delete(seen, name)
				}
				if event.Has(fsnotify.Create) || event.Has(fsnotify.Write) {
					processFile(name)
				}
			case <-fallbackTicker.C:
				entries, err := os.ReadDir(sessionDir)
				if err != nil {
					continue
				}
				
				currentFiles := make(map[string]bool, len(entries))
				for _, entry := range entries {
					if entry.IsDir() {
						continue
					}
					name := entry.Name()
					currentFiles[name] = true
					processFile(name)
				}

				// Garbage collection for seen map
				for name := range seen {
					if !currentFiles[name] {
						delete(seen, name)
					}
				}
			}
		}
	}()
}

func (sr *ShadowRuntime) startStatsLogging() {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-sr.ctx.Done():
				return
			case <-ticker.C:
				pubStats := sr.Pub.Stats()
				storeStats := sr.Store.Stats()

				sr.logger.Info().
					Int64("queued_bytes", pubStats.QueuedBytes).
					Int("queue_length", pubStats.QueueLength).
					Uint64("accepted_total", pubStats.AcceptedTotal).
					Uint64("dropped_total", pubStats.DroppedTotal).
					Uint64("delete_drops_total", pubStats.DeleteDropsTotal).
					Int64("stored_bytes", storeStats.CurrentBytes).
					Int("stored_objects", storeStats.CurrentObjects).
					Uint64("evictions_total", storeStats.EvictionsTotal).
					Uint64("publish_errors", storeStats.PublishErrors).
					Msg("shadow store stats (per-session)")
			}
		}
	}()
}

// Close gracefully terminates monitoring, empties the store, and unregisters from StoreRegistry.
func (sr *ShadowRuntime) Close() {
	if sr == nil {
		return
	}
	sr.closeOnce.Do(func() {
		sr.cancel()
		<-sr.done
		_ = sr.Pub.Close(context.Background())
		_ = sr.Store.DeleteStream(context.Background(), store.StreamID(sr.sessionID))
		if sr.Handle != nil {
			_ = sr.Handle.Close()
		}
	})
}
