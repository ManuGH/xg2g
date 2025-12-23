// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package daemon

import (
	"net/http"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/v3/bus"
	"github.com/ManuGH/xg2g/internal/v3/shadow"
	"github.com/ManuGH/xg2g/internal/v3/store"
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

	// APIServerSetter allows injecting v3 components into the API server
	APIServerSetter V3ComponentSetter

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

	// V3Config contains v3 Worker configuration
	V3Config *V3Config
}

// V3ComponentSetter defines the interface for injecting v3 components
type V3ComponentSetter interface {
	SetV3Components(b bus.Bus, st store.StateStore)
}

// V3Config holds v3 worker/pipeline configuration.
type V3Config struct {
	Enabled           bool
	Mode              string // "standard" or "virtual"
	StoreBackend      string // "memory" or "bolt"
	StorePath         string // Path to db file
	TunerSlots        []int
	E2Host            string
	E2TuneTimeout     time.Duration
	FFmpegBin         string
	FFmpegKillTimeout time.Duration
	HLSRoot           string
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
	// Config validation is done by config.Loader
	return nil
}
