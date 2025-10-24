// SPDX-License-Identifier: MIT

// Package daemon provides the core daemon bootstrapping and lifecycle management.
package daemon

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/jobs"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/telemetry"
	"github.com/rs/zerolog"
)

// Config holds daemon configuration.
type Config struct {
	// Version is the build version
	Version string

	// ConfigPath is the path to the YAML config file
	ConfigPath string

	// ListenAddr is the HTTP server listen address
	ListenAddr string

	// Server timeouts
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration
	IdleTimeout    time.Duration
	MaxHeaderBytes int

	// ShutdownTimeout is the graceful shutdown timeout
	ShutdownTimeout time.Duration
}

// Daemon represents the xg2g daemon instance.
type Daemon struct {
	config    Config
	jobsCfg   jobs.Config
	server    *http.Server
	logger    zerolog.Logger
	telemetry *telemetry.Provider
}

// New creates a new daemon instance.
func New(cfg Config) (*Daemon, error) {
	// Initialize logger
	log.Configure(log.Config{
		Level:   "info",
		Output:  os.Stdout,
		Service: "xg2g",
	})
	logger := log.WithComponent("daemon")

	// Load jobs configuration
	loader := config.NewLoader(cfg.ConfigPath, cfg.Version)
	jobsCfg, err := loader.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	return &Daemon{
		config:  cfg,
		jobsCfg: jobsCfg,
		logger:  logger,
	}, nil
}

// Start starts the daemon and all its components.
func (d *Daemon) Start(ctx context.Context, handler http.Handler) error {
	d.logger.Info().
		Str("version", d.config.Version).
		Str("listen", d.config.ListenAddr).
		Msg("Starting xg2g daemon")

	// Initialize telemetry if enabled
	if err := d.initTelemetry(ctx); err != nil {
		d.logger.Warn().Err(err).Msg("Telemetry initialization failed, continuing without tracing")
	}

	// Create HTTP server
	d.server = &http.Server{
		Addr:           d.config.ListenAddr,
		Handler:        handler,
		ReadTimeout:    d.config.ReadTimeout,
		WriteTimeout:   d.config.WriteTimeout,
		IdleTimeout:    d.config.IdleTimeout,
		MaxHeaderBytes: d.config.MaxHeaderBytes,
	}

	// Start server in goroutine
	errChan := make(chan error, 1)
	go func() {
		d.logger.Info().Msgf("HTTP server listening on %s", d.config.ListenAddr)
		if err := d.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- fmt.Errorf("server error: %w", err)
		}
	}()

	// Wait for shutdown signal or server error
	select {
	case err := <-errChan:
		return err
	case <-ctx.Done():
		return d.Shutdown(context.Background())
	}
}

// Shutdown gracefully shuts down the daemon.
func (d *Daemon) Shutdown(ctx context.Context) error {
	d.logger.Info().Msg("Shutting down daemon...")

	// Create shutdown context with timeout
	shutdownCtx, cancel := context.WithTimeout(ctx, d.config.ShutdownTimeout)
	defer cancel()

	// Shutdown HTTP server
	if d.server != nil {
		if err := d.server.Shutdown(shutdownCtx); err != nil {
			d.logger.Error().Err(err).Msg("HTTP server shutdown error")
		}
	}

	// Shutdown telemetry
	if d.telemetry != nil {
		if err := d.telemetry.Shutdown(shutdownCtx); err != nil {
			d.logger.Error().Err(err).Msg("Telemetry shutdown error")
		}
	}

	d.logger.Info().Msg("Daemon stopped")
	return nil
}

// initTelemetry initializes OpenTelemetry tracing.
func (d *Daemon) initTelemetry(ctx context.Context) error {
	// Check if telemetry is enabled
	enabled := os.Getenv("XG2G_TELEMETRY_ENABLED") == "true"
	if !enabled {
		return nil
	}

	// Build telemetry config
	telCfg := telemetry.Config{
		Enabled:        true,
		ServiceName:    getEnvOrDefault("XG2G_SERVICE_NAME", "xg2g"),
		ServiceVersion: d.config.Version,
		Environment:    getEnvOrDefault("XG2G_ENVIRONMENT", "production"),
		ExporterType:   getEnvOrDefault("XG2G_TELEMETRY_EXPORTER", "grpc"),
		Endpoint:       getEnvOrDefault("XG2G_OTLP_ENDPOINT", "localhost:4317"),
		SamplingRate:   parseFloat(getEnvOrDefault("XG2G_SAMPLING_RATE", "1.0")),
	}

	// Initialize telemetry provider
	provider, err := telemetry.NewProvider(ctx, telCfg)
	if err != nil {
		return fmt.Errorf("telemetry init failed: %w", err)
	}

	d.telemetry = provider
	d.logger.Info().
		Str("service", telCfg.ServiceName).
		Str("endpoint", telCfg.Endpoint).
		Float64("sampling_rate", telCfg.SamplingRate).
		Msg("Telemetry initialized")

	return nil
}

// WaitForShutdown waits for interrupt/termination signals.
func WaitForShutdown() context.Context {
	ctx, _ := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	return ctx
}

// Helper functions

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func parseFloat(s string) float64 {
	var f float64
	_, _ = fmt.Sscanf(s, "%f", &f)
	return f
}
