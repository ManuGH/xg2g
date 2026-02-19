// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/health"
	"github.com/rs/zerolog"
)

// ShutdownHook is a function that performs cleanup during graceful shutdown.
// Hooks are executed in reverse registration order (LIFO).
type ShutdownHook func(ctx context.Context) error

// Manager manages the daemon lifecycle: starting servers, handling shutdown.
type Manager interface {
	// Start starts all configured servers and blocks until shutdown
	Start(ctx context.Context) error

	// Shutdown gracefully shuts down all servers
	Shutdown(ctx context.Context) error

	// RegisterShutdownHook registers a function to be called during shutdown
	RegisterShutdownHook(name string, hook ShutdownHook)
}

// manager implements the Manager interface.
type manager struct {
	// Configuration
	serverCfg config.ServerConfig
	deps      Deps

	// Servers
	apiServer     *http.Server
	metricsServer *http.Server

	// Shutdown hooks (LIFO order)
	shutdownHooks []namedHook

	// State
	started  bool
	stopping bool
	mu       sync.Mutex

	// Logger
	logger zerolog.Logger
}

// namedHook represents a shutdown hook with a name for logging
type namedHook struct {
	name string
	hook ShutdownHook
}

// NewManager creates a new daemon manager with the given configuration and dependencies.
func NewManager(serverCfg config.ServerConfig, deps Deps) (Manager, error) {
	if err := deps.Validate(); err != nil {
		return nil, fmt.Errorf("invalid dependencies: %w", err)
	}

	return &manager{
		serverCfg:     serverCfg,
		deps:          deps,
		logger:        deps.Logger.With().Str("component", "manager").Logger(),
		shutdownHooks: make([]namedHook, 0),
	}, nil
}

// Start starts all configured servers and blocks until context is cancelled.
func (m *manager) Start(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("start context is nil")
	}

	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return fmt.Errorf("manager already started")
	}
	m.started = true
	m.mu.Unlock()

	m.logger.Info().
		Str("listen", m.serverCfg.ListenAddr).
		Dur("read_timeout", m.serverCfg.ReadTimeout).
		Dur("write_timeout", m.serverCfg.WriteTimeout).
		Dur("shutdown_timeout", m.serverCfg.ShutdownTimeout).
		Msg("Starting daemon manager")

	// Error channel for server failures
	errChan := make(chan error, 3)
	// Register close hooks independent of engine mode so runtime stores are always
	// cleaned up during shutdown, even when the v3 worker is disabled.
	m.registerV3RuntimeCloseHooks()

	// Start metrics server if configured (skip in proxy-only mode)
	if !m.deps.ProxyOnly && m.deps.MetricsHandler != nil {
		if err := m.startMetricsServer(ctx, errChan); err != nil {
			return fmt.Errorf("failed to start metrics server: %w", err)
		}
	}

	// Always register store close hooks so resources are released even when the engine is disabled.
	m.registerV3RuntimeCloseHooks()

	// Phase 7A: Start v3 Worker (if enabled)
	if m.deps.Config.Engine.Enabled {
		if err := m.startV3Worker(ctx, errChan); err != nil {
			return fmt.Errorf("failed to start v3 worker: %w", err)
		}
	}

	// Start main API server (skip in proxy-only mode)
	if !m.deps.ProxyOnly {
		if err := m.startAPIServer(ctx, errChan); err != nil {
			return fmt.Errorf("failed to start API server: %w", err)
		}
	} else {
		m.logger.Info().Msg("Running in proxy-only mode (API server disabled)")
	}

	// Wait for shutdown signal or server error
	select {
	case err := <-errChan:
		m.logger.Error().Err(err).Msg("Server error, initiating shutdown")
		// Use a detached-but-bounded context so shutdown can complete even if parent is canceled.
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if shutdownErr := m.Shutdown(shutdownCtx); shutdownErr != nil {
			return fmt.Errorf("server error and shutdown failure: %w", errors.Join(err, shutdownErr))
		}
		return err
	case <-ctx.Done():
		m.logger.Info().Msg("Shutdown signal received")
		// Use a detached-but-bounded context so shutdown can complete even if parent is canceled.
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		return m.Shutdown(shutdownCtx)
	}
}

// startAPIServer starts the main API HTTP server.
//
//nolint:unparam // error return kept for consistency with other start methods
func (m *manager) startAPIServer(_ context.Context, errChan chan<- error) error {
	m.apiServer = &http.Server{
		Addr:              m.serverCfg.ListenAddr,
		Handler:           m.deps.APIHandler,
		ReadTimeout:       m.serverCfg.ReadTimeout,
		ReadHeaderTimeout: m.serverCfg.ReadTimeout / 2,
		WriteTimeout:      m.serverCfg.WriteTimeout,
		IdleTimeout:       m.serverCfg.IdleTimeout,
		MaxHeaderBytes:    m.serverCfg.MaxHeaderBytes,
	}

	go func() {
		// Check for TLS configuration
		tlsCert := m.deps.Config.TLSCert
		tlsKey := m.deps.Config.TLSKey

		if tlsCert != "" && tlsKey != "" {
			m.logger.Info().
				Str("addr", m.serverCfg.ListenAddr).
				Msg("API server listening (HTTPS)")

			if err := m.apiServer.ListenAndServeTLS(tlsCert, tlsKey); err != nil && !errors.Is(err, http.ErrServerClosed) {
				m.logger.Error().
					Err(err).
					Str("event", "api.server.failed").
					Msg("API server (HTTPS) failed")
				errChan <- fmt.Errorf("API server (HTTPS): %w", err)
			}
		} else {
			m.logger.Info().
				Str("addr", m.serverCfg.ListenAddr).
				Msg("API server listening (HTTP)")

			if err := m.apiServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				m.logger.Error().
					Err(err).
					Str("event", "api.server.failed").
					Msg("API server (HTTP) failed")
				errChan <- fmt.Errorf("API server (HTTP): %w", err)
			}
		}
	}()

	return nil
}

// startMetricsServer starts the Prometheus metrics HTTP server.
//
//nolint:unparam // error return kept for consistency with other start methods
func (m *manager) startMetricsServer(_ context.Context, errChan chan<- error) error {
	metricsAddr := m.deps.MetricsAddr
	if metricsAddr == "" {
		return nil // Metrics disabled
	}

	m.metricsServer = &http.Server{
		Addr:              metricsAddr,
		Handler:           m.deps.MetricsHandler,
		ReadHeaderTimeout: m.serverCfg.ReadTimeout / 2,
	}

	go func() {
		m.logger.Info().
			Str("addr", metricsAddr).
			Msg("Metrics server listening")

		if err := m.metricsServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			m.logger.Error().
				Err(err).
				Str("event", "metrics.server.failed").
				Msg("Metrics server failed")
			errChan <- fmt.Errorf("metrics server: %w", err)
		}
	}()

	return nil
}

func (m *manager) Shutdown(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("shutdown context is nil")
	}

	m.mu.Lock()
	if m.stopping {
		m.mu.Unlock()
		return nil
	}
	if !m.started {
		m.mu.Unlock()
		return ErrManagerNotStarted
	}
	m.stopping = true
	m.mu.Unlock()

	m.logger.Info().Msg("Shutting down daemon manager")

	// Create a bounded shutdown context independent from caller cancellation.
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), m.serverCfg.ShutdownTimeout)
	defer cancel()

	var errs []error

	// Shutdown API server
	if m.apiServer != nil {
		m.logger.Debug().Msg("Shutting down API server")
		if err := m.apiServer.Shutdown(shutdownCtx); err != nil {
			errs = append(errs, fmt.Errorf("API server shutdown: %w", err))
		}
	}

	// Shutdown metrics server
	if m.metricsServer != nil {
		m.logger.Debug().Msg("Shutting down metrics server")
		if err := m.metricsServer.Shutdown(shutdownCtx); err != nil {
			errs = append(errs, fmt.Errorf("metrics server shutdown: %w", err))
		}
	}

	// Execute shutdown hooks in reverse order (LIFO)
	m.logger.Debug().Int("hooks", len(m.shutdownHooks)).Msg("Executing shutdown hooks")
	for i := len(m.shutdownHooks) - 1; i >= 0; i-- {
		hook := m.shutdownHooks[i]
		m.logger.Debug().Str("hook", hook.name).Msg("Executing shutdown hook")

		hookStart := time.Now()
		if err := hook.hook(shutdownCtx); err != nil {
			m.logger.Error().
				Err(err).
				Str("hook", hook.name).
				Dur("duration", time.Since(hookStart)).
				Msg("Shutdown hook failed")
			errs = append(errs, fmt.Errorf("hook %s: %w", hook.name, err))
		} else {
			m.logger.Debug().
				Str("hook", hook.name).
				Dur("duration", time.Since(hookStart)).
				Msg("Shutdown hook completed")
		}
	}

	if len(errs) > 0 {
		m.logger.Error().
			Int("error_count", len(errs)).
			Msg("Shutdown completed with errors")
		return fmt.Errorf("shutdown errors: %w", errors.Join(errs...))
	}

	m.logger.Info().Msg("Daemon manager stopped cleanly")
	return nil
}

// RegisterShutdownHook registers a cleanup function to be called during shutdown.
// Hooks are executed in reverse registration order (LIFO).
func (m *manager) RegisterShutdownHook(name string, hook ShutdownHook) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.shutdownHooks = append(m.shutdownHooks, namedHook{
		name: name,
		hook: hook,
	})
	m.logger.Debug().Str("hook", name).Msg("Registered shutdown hook")
}

// registerV3Checks registers health and readiness checks for V3 components.
func (m *manager) registerV3Checks(cfg *config.AppConfig, receiverHealthCheck func(context.Context) error) {
	if m.deps.APIServerSetter == nil {
		m.logger.Warn().Msg("API server hooks not available, skipping V3 checks")
		return
	}

	hm := m.deps.APIServerSetter.HealthManager()
	if hm == nil {
		m.logger.Warn().Msg("HealthManager not available, skipping V3 checks")
		return
	}

	// 1. Storage Checks (Runtime Writeability)
	hm.RegisterChecker(health.Informational(health.NewWritableDirChecker("v3_store_path", cfg.Store.Path)))
	hm.RegisterChecker(health.Informational(health.NewWritableDirChecker("v3_hls_root", cfg.HLS.Root)))

	// 2. Connectivity Checks (Upstream/Receiver)
	// Use the injected health check port to keep daemon package decoupled
	// from concrete receiver client types.
	hm.RegisterChecker(health.Informational(health.NewNamedReceiverChecker("v3_receiver_connection", func(ctx context.Context) error {
		if receiverHealthCheck == nil {
			return fmt.Errorf("receiver health check is not available")
		}
		// Keep probe latency bounded for readiness health checks.
		checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		return receiverHealthCheck(checkCtx)
	})))

}
