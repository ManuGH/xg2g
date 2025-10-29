// SPDX-License-Identifier: MIT

package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/proxy"
	"github.com/rs/zerolog"
)

// Manager manages the daemon lifecycle: starting servers, handling shutdown.
type Manager interface {
	// Start starts all configured servers and blocks until shutdown
	Start(ctx context.Context) error

	// Shutdown gracefully shuts down all servers
	Shutdown(ctx context.Context) error
}

// manager implements the Manager interface.
type manager struct {
	// Configuration
	serverCfg config.ServerConfig
	deps      Deps

	// Servers
	apiServer     *http.Server
	metricsServer *http.Server
	proxyServer   *proxy.Server

	// State
	started bool
	mu      sync.Mutex

	// Logger
	logger zerolog.Logger
}

// NewManager creates a new daemon manager with the given configuration and dependencies.
func NewManager(serverCfg config.ServerConfig, deps Deps) (Manager, error) {
	if err := deps.Validate(); err != nil {
		return nil, fmt.Errorf("invalid dependencies: %w", err)
	}

	return &manager{
		serverCfg: serverCfg,
		deps:      deps,
		logger:    deps.Logger.With().Str("component", "manager").Logger(),
	}, nil
}

// Start starts all configured servers and blocks until context is cancelled.
func (m *manager) Start(ctx context.Context) error {
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

	// Start proxy server if configured
	if m.deps.ProxyConfig != nil {
		if err := m.startProxyServer(ctx, errChan); err != nil {
			return fmt.Errorf("failed to start proxy server: %w", err)
		}
	}

	// Start metrics server if configured
	if m.deps.MetricsHandler != nil {
		if err := m.startMetricsServer(ctx, errChan); err != nil {
			return fmt.Errorf("failed to start metrics server: %w", err)
		}
	}

	// Start main API server
	if err := m.startAPIServer(ctx, errChan); err != nil {
		return fmt.Errorf("failed to start API server: %w", err)
	}

	// Wait for shutdown signal or server error
	select {
	case err := <-errChan:
		m.logger.Error().Err(err).Msg("Server error, initiating shutdown")
		return err
	case <-ctx.Done():
		m.logger.Info().Msg("Shutdown signal received")
		return m.Shutdown(context.Background())
	}
}

// startAPIServer starts the main API HTTP server.
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
		m.logger.Info().
			Str("addr", m.serverCfg.ListenAddr).
			Msg("API server listening")

		if err := m.apiServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			m.logger.Error().
				Err(err).
				Str("event", "api.server.failed").
				Msg("API server failed")
			errChan <- fmt.Errorf("API server: %w", err)
		}
	}()

	return nil
}

// startMetricsServer starts the Prometheus metrics HTTP server.
func (m *manager) startMetricsServer(_ context.Context, errChan chan<- error) error {
	metricsAddr := config.ParseMetricsAddr()
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

// startProxyServer starts the optional stream proxy server.
func (m *manager) startProxyServer(_ context.Context, errChan chan<- error) error {
	if m.deps.ProxyConfig == nil {
		return nil // Proxy disabled
	}

	var err error
	m.proxyServer, err = proxy.New(proxy.Config{
		ListenAddr: m.deps.ProxyConfig.ListenAddr,
		TargetURL:  m.deps.ProxyConfig.TargetURL,
		Logger:     m.deps.ProxyConfig.Logger,
	})
	if err != nil {
		return fmt.Errorf("failed to create proxy: %w", err)
	}

	go func() {
		m.logger.Info().
			Str("addr", m.deps.ProxyConfig.ListenAddr).
			Str("target", m.deps.ProxyConfig.TargetURL).
			Msg("Proxy server listening")

		if err := m.proxyServer.Start(); err != nil {
			m.logger.Error().
				Err(err).
				Str("event", "proxy.server.failed").
				Msg("Proxy server failed")
			errChan <- fmt.Errorf("proxy server: %w", err)
		}
	}()

	// Give proxy a moment to start listening
	time.Sleep(100 * time.Millisecond)

	return nil
}

// Shutdown gracefully shuts down all servers with the configured timeout.
func (m *manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return ErrManagerNotStarted
	}

	m.logger.Info().Msg("Shutting down daemon manager")

	// Create shutdown context with timeout
	shutdownCtx, cancel := context.WithTimeout(ctx, m.serverCfg.ShutdownTimeout)
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

	// Shutdown proxy server
	if m.proxyServer != nil {
		m.logger.Debug().Msg("Shutting down proxy server")
		if err := m.proxyServer.Shutdown(shutdownCtx); err != nil {
			errs = append(errs, fmt.Errorf("proxy server shutdown: %w", err))
		}
	}

	if len(errs) > 0 {
		m.logger.Error().
			Int("error_count", len(errs)).
			Msg("Shutdown completed with errors")
		return fmt.Errorf("shutdown errors: %v", errs)
	}

	m.logger.Info().Msg("Daemon manager stopped cleanly")
	return nil
}
