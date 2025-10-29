// SPDX-License-Identifier: MIT

package config

import "time"

// ServerConfig holds HTTP server configuration.
type ServerConfig struct {
	// ListenAddr is the address to listen on (e.g., ":8080")
	ListenAddr string

	// ReadTimeout is the maximum duration for reading the entire request
	ReadTimeout time.Duration

	// WriteTimeout is the maximum duration before timing out writes of the response
	WriteTimeout time.Duration

	// IdleTimeout is the maximum amount of time to wait for the next request
	IdleTimeout time.Duration

	// MaxHeaderBytes controls the maximum number of bytes the server will read parsing the request header's keys and values
	MaxHeaderBytes int

	// ShutdownTimeout is the maximum duration to wait for graceful shutdown
	ShutdownTimeout time.Duration
}

const (
	// Default server timeouts
	defaultReadTimeout     = 5 * time.Second
	defaultWriteTimeout    = 10 * time.Second
	defaultIdleTimeout     = 120 * time.Second
	defaultMaxHeaderBytes  = 1 << 20 // 1 MB
	defaultShutdownTimeout = 15 * time.Second
)

// ParseServerConfig reads server configuration from environment variables.
// It returns a ServerConfig with sensible defaults that can be overridden via ENV.
func ParseServerConfig() ServerConfig {
	return ServerConfig{
		ListenAddr:      ParseString("XG2G_LISTEN", ":8080"),
		ReadTimeout:     ParseDuration("XG2G_SERVER_READ_TIMEOUT", defaultReadTimeout),
		WriteTimeout:    ParseDuration("XG2G_SERVER_WRITE_TIMEOUT", defaultWriteTimeout),
		IdleTimeout:     ParseDuration("XG2G_SERVER_IDLE_TIMEOUT", defaultIdleTimeout),
		MaxHeaderBytes:  ParseInt("XG2G_SERVER_MAX_HEADER_BYTES", defaultMaxHeaderBytes),
		ShutdownTimeout: ParseDuration("XG2G_SERVER_SHUTDOWN_TIMEOUT", defaultShutdownTimeout),
	}
}

// ParseMetricsAddr reads metrics server address from environment variables.
// Returns empty string if metrics should be disabled.
func ParseMetricsAddr() string {
	return ParseString("XG2G_METRICS_LISTEN", "")
}
