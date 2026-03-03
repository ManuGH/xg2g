// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package daemon

import (
	"context"
	"net/http"

	"github.com/ManuGH/xg2g/internal/config"
	sessionports "github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/ManuGH/xg2g/internal/health"
	"github.com/ManuGH/xg2g/internal/pipeline/bus"
	"github.com/ManuGH/xg2g/internal/pipeline/resume"
	"github.com/ManuGH/xg2g/internal/pipeline/shadow"
	"github.com/rs/zerolog"
)

// Deps contains dependencies required by the daemon Manager.
// This allows for clean dependency injection and easier testing.
type Deps struct {
	// Logger is the structured logger for the daemon
	Logger zerolog.Logger

	// Config is the jobs configuration (OpenWebIF, EPG, etc.)
	Config config.AppConfig

	// ConfigManager handles configuration persistence
	ConfigManager *config.Manager

	// APIHandler is the HTTP handler for the API server
	APIHandler http.Handler

	// APIServerSetter provides daemon-visible API hooks (readiness, lifecycle).
	// Runtime v3 component wiring belongs to the composition root (cmd/daemon).
	APIServerSetter APIServerHooks

	// ProxyConfig contains proxy server configuration (if enabled)
	ProxyConfig *ProxyConfig

	// MetricsHandler is the HTTP handler for Prometheus metrics (if enabled)
	MetricsHandler http.Handler

	// MetricsAddr is the address the metrics server should listen on.
	// Empty disables the metrics server.
	MetricsAddr string

	// ProxyOnly disables API + metrics servers when true.
	// This is a runtime mode switch and must be computed at startup (and treated as immutable).
	ProxyOnly bool

	// V3 Components (Injected from main to allow shared state between API and Worker)
	V3Bus       bus.Bus
	V3Store     store.StateStore
	ResumeStore resume.Store
	ScanManager ScanStoreCloser
	// ReceiverHealthCheck probes receiver connectivity for health/readiness checks.
	// Keep the daemon package bound to behavior, not concrete receiver clients.
	ReceiverHealthCheck func(ctx context.Context) error
	MediaPipeline       sessionports.MediaPipeline
	// V3OrchestratorFactory builds the runtime worker orchestrator.
	// This keeps daemon package bound to ports, not concrete implementations.
	V3OrchestratorFactory V3OrchestratorFactory
}

// APIServerHooks exposes only daemon-safe hooks from the API server.
// Keep this narrow to avoid runtime ownership cycles between daemon and API.
type APIServerHooks interface {
	HealthManager() *health.Manager
}

// ScanStoreCloser is the minimal port required by daemon lifecycle code.
// Concrete scan implementations belong to composition root wiring.
type ScanStoreCloser interface {
	Close() error
}

// V3Orchestrator is the daemon-side runtime contract for session processing.
type V3Orchestrator interface {
	Run(ctx context.Context) error
}

// V3OrchestratorInputs contains the runtime dependencies needed to build an orchestrator.
type V3OrchestratorInputs struct {
	Bus      bus.Bus
	Store    store.StateStore
	Pipeline sessionports.MediaPipeline
}

// V3OrchestratorFactory builds a V3Orchestrator from config + injected ports.
// Concrete implementations belong in composition root (cmd/daemon).
type V3OrchestratorFactory interface {
	Build(cfg config.AppConfig, inputs V3OrchestratorInputs) (V3Orchestrator, error)
}

// ProxyConfig holds proxy server configuration.
type ProxyConfig struct {
	// ListenAddr is the proxy listen address (e.g., ":18000")
	ListenAddr string

	// TargetURL is the upstream target URL (optional if ReceiverHost is provided).
	TargetURL string

	// ReceiverHost is the receiver hostname/IP for fallback proxying.
	ReceiverHost string

	// Logger is the logger for the proxy
	Logger zerolog.Logger

	// TLS Configuration
	TLSCert string
	TLSKey  string

	// Playlist Configuration
	DataDir      string
	PlaylistPath string

	// Runtime holds the effective runtime settings (ENV-derived) required by the proxy.
	Runtime config.RuntimeSnapshot

	// AllowedOrigins for CORS
	AllowedOrigins []string

	// ShadowClient for v3 Canary (Optional)
	ShadowClient *shadow.Client
}

// Validate checks if the dependencies are valid.
func (d *Deps) Validate() error {
	if d.Logger.GetLevel() == zerolog.Disabled {
		return ErrMissingLogger
	}
	if !d.ProxyOnly && d.APIHandler == nil {
		return ErrMissingAPIHandler
	}
	if d.Config.Engine.Enabled {
		if d.MediaPipeline == nil {
			return ErrMissingMediaPipeline
		}
		if d.V3OrchestratorFactory == nil {
			return ErrMissingV3OrchestratorFactory
		}
		if d.ReceiverHealthCheck == nil {
			return ErrMissingReceiverHealthCheck
		}
	}

	// Config validation is done by config.Loader
	return nil
}
