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
	"os"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/admission"
	"github.com/ManuGH/xg2g/internal/core/urlutil"
	worker "github.com/ManuGH/xg2g/internal/domain/session/manager"
	"github.com/ManuGH/xg2g/internal/health"
	"github.com/ManuGH/xg2g/internal/infra/bus"
	"github.com/ManuGH/xg2g/internal/infra/media/ffmpeg"
	"github.com/ManuGH/xg2g/internal/infra/media/stub"
	"github.com/ManuGH/xg2g/internal/infra/platform"
	"github.com/ManuGH/xg2g/internal/pipeline/exec/enigma2"
	platformnet "github.com/ManuGH/xg2g/internal/platform/net"
	"github.com/google/uuid"
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

	// Start metrics server if configured (skip in proxy-only mode)
	if !m.deps.ProxyOnly && m.deps.MetricsHandler != nil {
		if err := m.startMetricsServer(ctx, errChan); err != nil {
			return fmt.Errorf("failed to start metrics server: %w", err)
		}
	}

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
		// Use bounded timeout for shutdown instead of Background
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if shutdownErr := m.Shutdown(shutdownCtx); shutdownErr != nil {
			return fmt.Errorf("%w (shutdown: %v)", err, shutdownErr)
		}
		return err
	case <-ctx.Done():
		m.logger.Info().Msg("Shutdown signal received")
		// Use bounded timeout for shutdown instead of Background
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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

// startV3Worker initializes v3 Bus, Store, and Orchestrator
func (m *manager) startV3Worker(ctx context.Context, errChan chan<- error) error {
	cfg := m.deps.Config
	// Use INFO level for visibility during canary
	m.logger.Info().
		Str("mode", cfg.Engine.Mode).
		Str("store", cfg.Store.Path).
		Str("hls_root", cfg.HLS.Root).
		Str("e2_host", urlutil.SanitizeURL(cfg.Enigma2.BaseURL)).
		Msg("starting v3 worker (Phase 7A)")

	// 1. Initialize Bus (Shared)
	v3Bus := m.deps.V3Bus

	// 2. Initialize Store (Shared)
	v3Store := m.deps.V3Store

	// Phase 7B-2: Register Shutdown Hook for Store (if needed)
	if c, ok := v3Store.(interface{ Close() error }); ok {
		m.RegisterShutdownHook("v3_store_close", func(ctx context.Context) error {
			return c.Close()
		})
	}

	// 2.5 Initialize Resume Store (Shared)
	resumeStore := m.deps.ResumeStore

	// Register Shutdown Hook for Resume Store
	if c, ok := resumeStore.(interface{ Close() error }); ok {
		m.RegisterShutdownHook("resume_store_close", func(ctx context.Context) error {
			return c.Close()
		})
	}

	// 2.6 Initialize E2 Client (Shared)
	e2Client := m.deps.E2Client

	// 2.7 Initialize Smart Profile Scanner (Shared)
	scanManager := m.deps.ScanManager

	// 2.8 Initialize Admission Control (Phase 5.2/5.3)
	// 2.8 Initialize Admission Control (Slice 2)
	adm := admission.NewController(cfg)
	m.logger.Info().
		Int("max_sessions", cfg.Limits.MaxSessions).
		Int("max_transcodes", cfg.Limits.MaxTranscodes).
		Msg("Admission control initialized")
		// CPU load sampler (fail-closed if samples are missing/invalid).
		// admission.StartCPUSampler(ctx, adm, 0, nil)

	// 3. Initialize Orchestrator
	// Generate stable worker identity (replacing domain-level OS calls)
	host, _ := os.Hostname()
	workerOwner := fmt.Sprintf("%s-%d-%s", host, os.Getpid(), uuid.New().String())

	orch := &worker.Orchestrator{
		Store:    v3Store,
		Bus:      bus.NewAdapter(v3Bus),    // Injected Adapter
		Platform: platform.NewOSPlatform(), // Platform Port
		// Admission:           adm,                      // Phase 5.2 Gatekeeper (Removed from Orchestrator)
		LeaseTTL:            30 * time.Second, // Explicit default
		HeartbeatEvery:      10 * time.Second, // Explicit default
		Owner:               workerOwner,      // Explicit generation
		TunerSlots:          cfg.Engine.TunerSlots,
		HLSRoot:             cfg.HLS.Root,
		PipelineStopTimeout: 5 * time.Second, // Explicit default (fallback if cfg missing)
		StartConcurrency:    10,              // Bounded Start concurrency
		StopConcurrency:     10,              // Bounded Stop concurrency
		Sweeper: worker.SweeperConfig{
			IdleTimeout:      cfg.Engine.IdleTimeout,
			Interval:         1 * time.Minute, // Explicit default
			SessionRetention: 24 * time.Hour,  // Explicit default
		},
		OutboundPolicy: platformnet.OutboundPolicy{
			Enabled: cfg.Network.Outbound.Enabled,
			Allow: platformnet.OutboundAllowlist{
				Hosts:   append([]string(nil), cfg.Network.Outbound.Allow.Hosts...),
				CIDRs:   append([]string(nil), cfg.Network.Outbound.Allow.CIDRs...),
				Ports:   append([]int(nil), cfg.Network.Outbound.Allow.Ports...),
				Schemes: append([]string(nil), cfg.Network.Outbound.Allow.Schemes...),
			},
		},
	}

	if cfg.Engine.Mode == "virtual" {
		orch.Pipeline = stub.NewAdapter()
	} else {
		adapter := ffmpeg.NewLocalAdapter(
			cfg.FFmpeg.Bin,
			cfg.FFmpeg.FFprobeBin,
			cfg.HLS.Root,
			e2Client,
			m.logger,
			cfg.Enigma2.AnalyzeDuration,
			cfg.Enigma2.ProbeSize,
			cfg.HLS.DVRWindow,
			cfg.FFmpeg.KillTimeout,
			cfg.Enigma2.FallbackTo8001,
			cfg.Enigma2.PreflightTimeout,
			cfg.HLS.SegmentSeconds,
			cfg.Timeouts.TranscodeStart,
			cfg.Timeouts.TranscodeNoProgress,
			cfg.FFmpeg.VaapiDevice,
		)
		// VAAPI preflight (fail-fast at startup if configured but broken)
		if cfg.FFmpeg.VaapiDevice != "" {
			if err := adapter.PreflightVAAPI(); err != nil {
				m.logger.Warn().Err(err).Str("device", cfg.FFmpeg.VaapiDevice).
					Msg("VAAPI preflight failed; GPU transcoding will be unavailable for sessions requesting it")
			}
		}
		orch.Pipeline = adapter
	}

	// 4. Inject into API Server (Shadow Receiving)
	if m.deps.APIServerSetter != nil {
		m.deps.APIServerSetter.WireV3Runtime(v3Bus, v3Store, resumeStore, scanManager, adm)
		m.logger.Info().Msg("v3 components and admission gate injected into API server")
	} else {
		m.logger.Warn().Msg("API Server Setter not available - shadow intents will not be processed")
	}

	// 5. Register Health/Readiness Checks (Phase 9-1)
	m.registerV3Checks(&cfg, e2Client)

	// 6. Run Orchestrator with managed lifecycle (cancel + join on shutdown)
	workerCtx, workerCancel := context.WithCancel(ctx)
	workerDone := make(chan error, 1)

	m.RegisterShutdownHook("v3_orchestrator_stop", func(shutdownCtx context.Context) error {
		workerCancel()
		select {
		case <-shutdownCtx.Done():
			return fmt.Errorf("timeout waiting for v3 orchestrator stop: %w", shutdownCtx.Err())
		case <-workerDone:
			return nil
		}
	})

	go func() {
		err := orch.Run(workerCtx)
		if err != nil && !errors.Is(err, context.Canceled) {
			m.logger.Error().Err(err).Msg("v3 worker orchestrator exited unexpected")
			errChan <- fmt.Errorf("v3 worker: %w", err)
		}
		workerDone <- err
		close(workerDone)
	}()

	return nil
}

func (m *manager) Shutdown(ctx context.Context) error {
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
		return fmt.Errorf("shutdown errors: %v", errs)
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
func (m *manager) registerV3Checks(cfg *config.AppConfig, e2Client *enigma2.Client) {
	hm := m.deps.APIServerSetter.HealthManager()
	if hm == nil {
		m.logger.Warn().Msg("HealthManager not available, skipping V3 checks")
		return
	}

	// 1. Storage Checks (Runtime Writeability)
	hm.RegisterChecker(health.Informational(health.NewWritableDirChecker("v3_store_path", cfg.Store.Path)))
	hm.RegisterChecker(health.Informational(health.NewWritableDirChecker("v3_hls_root", cfg.HLS.Root)))

	// 2. Connectivity Checks (Upstream/Receiver)
	// Re-uses the existing network logic but scoped to V3 dependencies.
	// We can use a ReceiverChecker pointing to E2Host.
	hm.RegisterChecker(health.Informational(health.NewNamedReceiverChecker("v3_receiver_connection", func(ctx context.Context) error {
		if e2Client == nil || e2Client.HTTPClient == nil {
			return fmt.Errorf("enigma2 client is not available")
		}
		if e2Client.BaseURL == "" {
			return fmt.Errorf("XG2G_V3_E2_HOST is empty")
		}
		// Quick connectivity check to E2Host
		// Use a 2s timeout to avoid blocking readiness probes too long
		checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(checkCtx, http.MethodHead, e2Client.BaseURL, nil)
		if err != nil {
			return err
		}
		if cfg.Enigma2.UserAgent != "" {
			req.Header.Set("User-Agent", cfg.Enigma2.UserAgent)
		}
		if cfg.Enigma2.Username != "" || cfg.Enigma2.Password != "" {
			req.SetBasicAuth(cfg.Enigma2.Username, cfg.Enigma2.Password)
		}
		resp, err := e2Client.HTTPClient.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode >= 500 {
			return fmt.Errorf("receiver returned status %d", resp.StatusCode)
		}
		return nil
	})))

}
