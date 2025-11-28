// SPDX-License-Identifier: MIT

package daemon

import (
	"net/http"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/rs/zerolog"
)

// Deps contains dependencies required by the daemon Manager.
// This allows for clean dependency injection and easier testing.
type Deps struct {
	// Logger is the structured logger for the daemon
	Logger zerolog.Logger

	// Config is the jobs configuration (OpenWebIF, EPG, etc.)
	Config config.AppConfig

	// APIHandler is the HTTP handler for the API server
	APIHandler http.Handler

	// ProxyConfig contains proxy server configuration (if enabled)
	ProxyConfig *ProxyConfig

	// MetricsHandler is the HTTP handler for Prometheus metrics (if enabled)
	MetricsHandler http.Handler
}

// ProxyConfig holds proxy server configuration.
type ProxyConfig struct {
	// ListenAddr is the proxy listen address (e.g., ":18000")
	ListenAddr string

	// TargetURL is the upstream target URL (optional if StreamDetector is provided)
	TargetURL string

	// ReceiverHost is the receiver hostname/IP for Smart Detection fallback
	ReceiverHost string

	// StreamDetector enables smart port detection (8001 vs 17999)
	StreamDetector *openwebif.StreamDetector

	// Logger is the logger for the proxy
	Logger zerolog.Logger

	// TLS Configuration
	TLSCert string
	TLSKey  string
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
