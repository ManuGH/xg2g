package ffmpeg

import (
	"context"
	"time"

	"github.com/ManuGH/xg2g/internal/pipeline/store"
	"github.com/rs/zerolog"
)

// injectTestShadowStore creates a per-session ShadowStore for Sprint 1 testing
// if ShadowStoreEnabled=true. It also spawns a stats logging goroutine.
func injectTestShadowStore(ctx context.Context, logger zerolog.Logger, cfg AdapterConfig) *store.ShadowPublisher {
	if !cfg.ShadowStoreEnabled {
		return nil
	}

	maxBytes := cfg.ShadowStoreMaxBytes
	if maxBytes <= 0 {
		maxBytes = 134217728 // 128 MB default
	}

	queueBytes := cfg.ShadowStoreQueueMaxBytes
	if queueBytes <= 0 {
		queueBytes = 16777216 // 16 MB default
	}

	maxObjects := cfg.ShadowStoreMaxObjects
	if maxObjects <= 0 {
		maxObjects = 32 // 32 objects default
	}

	ram, err := store.NewRAMShadowStore(maxBytes, maxObjects)
	if err != nil {
		logger.Error().Err(err).Msg("failed to create RAM shadow store for session")
		return nil
	}

	pub, err := store.NewShadowPublisher(ram, maxObjects, queueBytes, logger)
	if err != nil {
		logger.Error().Err(err).Msg("failed to create shadow publisher for session")
		return nil
	}
	pub.Start()

	// Periodic stats logging for observability in staging
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pubStats := pub.Stats()
				storeStats := ram.Stats()

				logger.Info().
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

	return pub
}
