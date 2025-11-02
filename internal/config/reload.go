// SPDX-License-Identifier: MIT

package config

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/jobs"
	xglog "github.com/ManuGH/xg2g/internal/log"
	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog"
)

// ConfigHolder holds configuration with atomic reloading capability.
// It provides thread-safe access to configuration and supports hot reloading
// from file or manual trigger via API.
type ConfigHolder struct {
	mu         sync.RWMutex
	current    jobs.Config
	loader     *Loader
	configPath string
	watcher    *fsnotify.Watcher
	logger     zerolog.Logger

	// Reload notifications
	reloadMu        sync.RWMutex
	reloadListeners []chan<- jobs.Config
}

// NewConfigHolder creates a new configuration holder with initial config.
func NewConfigHolder(initial jobs.Config, loader *Loader, configPath string) *ConfigHolder {
	return &ConfigHolder{
		current:         initial,
		loader:          loader,
		configPath:      configPath,
		logger:          xglog.WithComponent("config"),
		reloadListeners: make([]chan<- jobs.Config, 0),
	}
}

// Get returns the current configuration (thread-safe read).
func (h *ConfigHolder) Get() jobs.Config {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.current
}

// Reload reloads configuration from file and validates it.
// If validation fails, the old configuration is kept and an error is returned.
// This ensures atomic config updates - either the full config is valid and applied,
// or the old config remains unchanged.
func (h *ConfigHolder) Reload(_ context.Context) error {
	h.logger.Info().Str("event", "config.reload_start").Msg("reloading configuration")

	// Load new configuration
	newCfg, err := h.loader.Load()
	if err != nil {
		h.logger.Error().
			Err(err).
			Str("event", "config.reload_failed").
			Msg("failed to load new configuration")
		return fmt.Errorf("load config: %w", err)
	}

	// Validate new configuration
	if err := Validate(newCfg); err != nil {
		h.logger.Error().
			Err(err).
			Str("event", "config.validation_failed").
			Msg("new configuration failed validation")
		return fmt.Errorf("validate config: %w", err)
	}

	// Atomically swap configuration
	h.mu.Lock()
	oldCfg := h.current
	h.current = newCfg
	h.mu.Unlock()

	// Notify listeners of config change
	h.notifyListeners(newCfg)

	// Log configuration changes
	h.logChanges(oldCfg, newCfg)

	h.logger.Info().
		Str("event", "config.reload_success").
		Msg("configuration reloaded successfully")

	return nil
}

// StartWatcher starts watching the config file for changes.
// If configPath is empty, this is a no-op (config comes from ENV only).
func (h *ConfigHolder) StartWatcher(ctx context.Context) error {
	if h.configPath == "" {
		h.logger.Info().
			Str("event", "config.watcher_disabled").
			Msg("config file watcher disabled (using ENV-only configuration)")
		return nil
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create watcher: %w", err)
	}

	h.watcher = watcher

	// Add config file to watcher
	if err := watcher.Add(h.configPath); err != nil {
		_ = watcher.Close() // Ignore close error in error path
		return fmt.Errorf("watch config file: %w", err)
	}

	h.logger.Info().
		Str("event", "config.watcher_started").
		Str("path", h.configPath).
		Msg("watching config file for changes")

	// Start watcher goroutine
	go h.watchLoop(ctx)

	return nil
}

// watchLoop is the main file watcher loop.
func (h *ConfigHolder) watchLoop(ctx context.Context) {
	// Debounce timer to avoid multiple reloads for rapid file changes
	var debounceTimer *time.Timer
	debounceDuration := 500 * time.Millisecond

	for {
		select {
		case <-ctx.Done():
			h.logger.Info().Str("event", "config.watcher_stopped").Msg("config watcher stopped")
			if h.watcher != nil {
				_ = h.watcher.Close() // Ignore close error in error path
			}
			return

		case event, ok := <-h.watcher.Events:
			if !ok {
				return
			}

			// Watch for Write and Create events (covers vim, nano, echo)
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				h.logger.Debug().
					Str("event", "config.file_changed").
					Str("op", event.Op.String()).
					Msg("config file changed")

				// Debounce: reset timer on each event
				if debounceTimer != nil {
					debounceTimer.Stop()
				}
				debounceTimer = time.AfterFunc(debounceDuration, func() {
					if err := h.Reload(ctx); err != nil {
						h.logger.Error().
							Err(err).
							Str("event", "config.auto_reload_failed").
							Msg("automatic config reload failed")
					}
				})
			}

		case err, ok := <-h.watcher.Errors:
			if !ok {
				return
			}
			h.logger.Error().
				Err(err).
				Str("event", "config.watcher_error").
				Msg("config watcher error")
		}
	}
}

// Stop stops the config watcher (if running).
func (h *ConfigHolder) Stop() {
	if h.watcher != nil {
		_ = h.watcher.Close() // Ignore close error in error path
	}
}

// RegisterListener registers a channel to receive config reload notifications.
// The channel will receive the new config whenever a reload succeeds.
// The caller is responsible for closing the channel.
func (h *ConfigHolder) RegisterListener(ch chan<- jobs.Config) {
	h.reloadMu.Lock()
	defer h.reloadMu.Unlock()
	h.reloadListeners = append(h.reloadListeners, ch)
}

// notifyListeners sends the new config to all registered listeners (non-blocking).
func (h *ConfigHolder) notifyListeners(newCfg jobs.Config) {
	h.reloadMu.RLock()
	defer h.reloadMu.RUnlock()

	for _, ch := range h.reloadListeners {
		select {
		case ch <- newCfg:
		default:
			// Skip if channel is full (non-blocking send)
			h.logger.Warn().
				Str("event", "config.listener_skip").
				Msg("skipped notifying listener (channel full)")
		}
	}
}

// logChanges logs the differences between old and new configuration.
func (h *ConfigHolder) logChanges(old, newCfg jobs.Config) {
	if old.Bouquet != newCfg.Bouquet {
		h.logger.Info().
			Str("old", old.Bouquet).
			Str("new", newCfg.Bouquet).
			Msg("config changed: Bouquet")
	}
	if old.EPGEnabled != newCfg.EPGEnabled {
		h.logger.Info().
			Bool("old", old.EPGEnabled).
			Bool("new", newCfg.EPGEnabled).
			Msg("config changed: EPGEnabled")
	}
	if old.EPGDays != newCfg.EPGDays {
		h.logger.Info().
			Int("old", old.EPGDays).
			Int("new", newCfg.EPGDays).
			Msg("config changed: EPGDays")
	}
	if old.StreamPort != newCfg.StreamPort {
		h.logger.Info().
			Int("old", old.StreamPort).
			Int("new", newCfg.StreamPort).
			Msg("config changed: StreamPort")
	}
	if old.OWIBase != newCfg.OWIBase {
		h.logger.Info().
			Str("old", maskURL(old.OWIBase)).
			Str("new", maskURL(newCfg.OWIBase)).
			Msg("config changed: OWIBase")
	}
}

// maskURL is a helper to mask sensitive URLs for logging.
func maskURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	// Simple redaction: show only scheme and host
	return "***redacted***"
}
