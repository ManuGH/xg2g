// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	xglog "github.com/ManuGH/xg2g/internal/log"
	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog"
)

// ConfigHolder holds configuration with atomic reloading capability.
// It provides thread-safe access to configuration and supports hot reloading
// from file or manual trigger via API.
type ConfigHolder struct {
	reloadOpMu sync.Mutex
	epoch      atomic.Uint64
	snapshot   atomic.Pointer[Snapshot]
	loader     *Loader
	configPath string
	configDir  string
	configFile string
	watcher    *fsnotify.Watcher
	logger     zerolog.Logger

	// Reload notifications
	reloadMu        sync.RWMutex
	reloadListeners []chan<- AppConfig
	snapListeners   []chan<- *Snapshot
}

// NewConfigHolder creates a new configuration holder with initial config.
func NewConfigHolder(initial AppConfig, loader *Loader, configPath string) *ConfigHolder {
	h := &ConfigHolder{
		loader:          loader,
		configPath:      configPath,
		logger:          xglog.WithComponent("config"),
		reloadListeners: make([]chan<- AppConfig, 0),
		snapListeners:   make([]chan<- *Snapshot, 0),
	}
	env, err := ReadOSRuntimeEnv()
	if err != nil {
		h.logger.Warn().Err(err).Str("event", "config.env_read_failed").Msg("failed to read runtime environment, using defaults")
		env = DefaultEnv()
	}

	snap := BuildSnapshot(initial, env)
	h.Swap(&snap)
	return h
}

// Get returns the current configuration (thread-safe read).
func (h *ConfigHolder) Get() AppConfig {
	return h.Snapshot().App
}

// Current returns the current immutable runtime snapshot pointer (thread-safe read).
func (h *ConfigHolder) Current() *Snapshot {
	return h.snapshot.Load()
}

// Swap atomically swaps the current snapshot.
// Swap assigns a new, monotonically increasing Epoch to next before storing it.
func (h *ConfigHolder) Swap(next *Snapshot) (prev *Snapshot) {
	if next == nil {
		return h.snapshot.Load()
	}

	next.Epoch = h.epoch.Add(1)
	return h.snapshot.Swap(next)
}

// Snapshot returns a copy of the current immutable runtime snapshot (thread-safe read).
func (h *ConfigHolder) Snapshot() Snapshot {
	snap := h.Current()
	if snap == nil {
		return Snapshot{}
	}
	return *snap
}

// Reload reloads configuration from file and validates it.
// If validation fails, the old configuration is kept and an error is returned.
// This ensures atomic config updates - either the full config is valid and applied,
// or the old config remains unchanged.
func (h *ConfigHolder) Reload(_ context.Context) error {
	h.reloadOpMu.Lock()
	defer h.reloadOpMu.Unlock()

	h.logger.Info().Str("event", "config.reload_start").Msg("reloading configuration")

	oldCfg := AppConfig{}
	if oldSnap := h.Current(); oldSnap != nil {
		oldCfg = oldSnap.App
	}

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
	env, err := ReadOSRuntimeEnv()
	if err != nil {
		h.logger.Warn().Err(err).Str("event", "config.env_read_failed").Msg("failed to read runtime environment, using defaults")
		env = DefaultEnv()
	}

	newSnap := BuildSnapshot(newCfg, env)
	newSnapPtr := &newSnap
	h.Swap(newSnapPtr)

	// Notify listeners of config change
	h.notifyListeners(newCfg)
	h.notifySnapshotListeners(newSnapPtr)

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

	// Watch the directory so we can handle atomic replace writes (tmp+rename) and file creation.
	h.configDir = filepath.Dir(h.configPath)
	h.configFile = filepath.Base(h.configPath)

	if err := watcher.Add(h.configDir); err != nil {
		_ = watcher.Close() // Ignore close error in error path
		return fmt.Errorf("watch config dir: %w", err)
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

			// Filter for our config file only.
			if h.configFile != "" && filepath.Base(event.Name) != h.configFile {
				continue
			}

			// Watch for Write/Create/Rename (covers vim/atomic replace, nano, echo).
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Rename) {
				h.logger.Debug().
					Str("event", "config.file_changed").
					Str("op", event.Op.String()).
					Str("name", event.Name).
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
func (h *ConfigHolder) RegisterListener(ch chan<- AppConfig) {
	h.reloadMu.Lock()
	defer h.reloadMu.Unlock()
	h.reloadListeners = append(h.reloadListeners, ch)
}

// RegisterSnapshotListener registers a channel to receive snapshot reload notifications.
// The channel will receive the new snapshot whenever a reload succeeds.
func (h *ConfigHolder) RegisterSnapshotListener(ch chan<- *Snapshot) {
	h.reloadMu.Lock()
	defer h.reloadMu.Unlock()
	h.snapListeners = append(h.snapListeners, ch)
}

// notifyListeners sends the new config to all registered listeners (non-blocking).
func (h *ConfigHolder) notifyListeners(newCfg AppConfig) {
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

func (h *ConfigHolder) notifySnapshotListeners(snap *Snapshot) {
	if snap == nil {
		return
	}

	h.reloadMu.RLock()
	defer h.reloadMu.RUnlock()

	for _, ch := range h.snapListeners {
		select {
		case ch <- snap:
		default:
			h.logger.Warn().
				Str("event", "config.snapshot_listener_skip").
				Msg("skipped notifying snapshot listener (channel full)")
		}
	}
}

// logChanges logs the differences between old and new configuration.
func (h *ConfigHolder) logChanges(old, newCfg AppConfig) {
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
	if old.Enigma2.StreamPort != newCfg.Enigma2.StreamPort {
		h.logger.Info().
			Int("old", old.Enigma2.StreamPort).
			Int("new", newCfg.Enigma2.StreamPort).
			Msg("config changed: StreamPort")
	}
	if old.Enigma2.UseWebIFStreams != newCfg.Enigma2.UseWebIFStreams {
		h.logger.Info().
			Bool("old", old.Enigma2.UseWebIFStreams).
			Bool("new", newCfg.Enigma2.UseWebIFStreams).
			Msg("config changed: UseWebIFStreams")
	}
	if old.RateLimitEnabled != newCfg.RateLimitEnabled {
		h.logger.Info().
			Bool("old", old.RateLimitEnabled).
			Bool("new", newCfg.RateLimitEnabled).
			Msg("config changed: RateLimitEnabled")
	}
	if old.RateLimitGlobal != newCfg.RateLimitGlobal {
		h.logger.Info().
			Int("old", old.RateLimitGlobal).
			Int("new", newCfg.RateLimitGlobal).
			Msg("config changed: RateLimitGlobal")
	}
	if old.RateLimitAuth != newCfg.RateLimitAuth {
		h.logger.Info().
			Int("old", old.RateLimitAuth).
			Int("new", newCfg.RateLimitAuth).
			Msg("config changed: RateLimitAuth")
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
