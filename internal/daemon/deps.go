// SPDX-License-Identifier: MIT

package daemon

import (
	"net/http"

	"github.com/ManuGH/xg2g/internal/config"
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

	// ProxyConfig contains proxy server configuration (if enabled)
	ProxyConfig *ProxyConfig

	// MetricsHandler is the HTTP handler for Prometheus metrics (if enabled)
	MetricsHandler http.Handler

	// MetricsAddr is the address the metrics server should listen on.
	// Empty disables the metrics server.
	MetricsAddr string
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
}

// Validate checks if the dependencies are valid.
func (d *Deps) Validate() error {
	if d.Logger.GetLevel() == zerolog.Disabled {
		return ErrMissingLogger
	}
	if d.APIHandler == nil {
		return ErrMissingAPIHandler
	}
	// Config validation is done by config.Loader
	return nil
}
