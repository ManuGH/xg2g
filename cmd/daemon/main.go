// SPDX-License-Identifier: MIT
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ManuGH/xg2g/internal/api"
	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/jobs"
	xglog "github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/proxy"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
)

var Version = "dev"

const (
	defaultStreamPort = 8001
	defaultOWITimeout = 10 * time.Second       // Updated to match spec
	defaultOWIRetries = 3                      // Updated to match spec
	defaultOWIBackoff = 500 * time.Millisecond // Updated to match spec
	maxOWITimeout     = 60 * time.Second
	maxOWIRetries     = 10
	maxOWIBackoff     = 30 * time.Second // Updated to match spec (30s max)

	// Server hardening defaults (can be overridden by ENV)
	defaultServerReadTimeout    = 5 * time.Second
	defaultServerWriteTimeout   = 10 * time.Second
	defaultServerIdleTimeout    = 120 * time.Second
	defaultServerMaxHeaderBytes = 1 << 20 // 1 MB
	defaultShutdownTimeout      = 15 * time.Second
)

// maskURL removes user info from a URL string for safe logging.
func maskURL(rawURL string) string {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		// If parsing fails, return a redacted string to avoid leaking anything.
		return "invalid-url-redacted"
	}
	// Clear user info
	parsedURL.User = nil
	return parsedURL.String()
}

func main() {
	// Handle command-line flags
	showVersion := flag.Bool("version", false, "print version and exit")
	configPath := flag.String("config", "", "path to config file (YAML)")
	flag.Parse()

	if *showVersion {
		fmt.Println(Version)
		os.Exit(0)
	}

	logger := xglog.WithComponent("daemon")

	// Create a context that listens for the interrupt signal from the OS.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Load configuration with precedence: ENV > File > Defaults
	loader := config.NewLoader(*configPath, Version)
	cfg, err := loader.Load()
	if err != nil {
		logger.Fatal().
			Err(err).
			Str("event", "config.load_failed").
			Str("config_path", *configPath).
			Msg("failed to load configuration")
	}

	// Log config source
	if *configPath != "" {
		logger.Info().
			Str("event", "config.loaded").
			Str("source", "file").
			Str("path", *configPath).
			Msg("loaded configuration from file")
	} else {
		logger.Info().
			Str("event", "config.loaded").
			Str("source", "env+defaults").
			Msg("loaded configuration from environment and defaults")
	}

	// Legacy: Determine XMLTV path if EPG is enabled and no explicit path is set
	if cfg.EPGEnabled && cfg.XMLTVPath == "" {
		cfg.XMLTVPath = "xmltv.xml"
		logger.Info().
			Str("xmltv_path", cfg.XMLTVPath).
			Msg("EPG enabled but no XMLTV path set, using default")
	}

	// Ensure data directory is created and validated at startup
	if err := ensureDataDir(cfg.DataDir); err != nil {
		logger.Fatal().
			Err(err).
			Str("event", "config.invalid").
			Str("data_dir", cfg.DataDir).
			Msg("data directory validation failed")
	}

	s := api.New(cfg)
	addr := env("XG2G_LISTEN", ":8080")
	logger.Info().
		Str("event", "startup").
		Str("version", Version).
		Str("addr", addr).
		Msg("starting xg2g")
	logger.Info().
		Str("event", "config").
		Str("data", cfg.DataDir).
		Str("owi", maskURL(cfg.OWIBase)).
		Bool("owi_auth", cfg.OWIUsername != ""). // Log if auth is enabled
		Str("bouquet", cfg.Bouquet).
		Str("xmltv", cfg.XMLTVPath).
		Int("fuzzy", cfg.FuzzyMax).
		Str("picon", cfg.PiconBase).
		Int("stream_port", cfg.StreamPort).
		Bool("api_token_set", cfg.APIToken != ""). // Log if token is set, not the token itself
		Dur("owi_timeout", cfg.OWITimeout).
		Int("owi_retries", cfg.OWIRetries).
		Dur("owi_backoff", cfg.OWIBackoff).
		Msg("configuration loaded")

	// Resolve server tuning from environment
	serverReadTimeout := envDuration("XG2G_SERVER_READ_TIMEOUT", defaultServerReadTimeout)
	serverWriteTimeout := envDuration("XG2G_SERVER_WRITE_TIMEOUT", defaultServerWriteTimeout)
	serverIdleTimeout := envDuration("XG2G_SERVER_IDLE_TIMEOUT", defaultServerIdleTimeout)
	serverMaxHeaderBytes := envIntDefault("XG2G_SERVER_MAX_HEADER_BYTES", defaultServerMaxHeaderBytes)
	shutdownTimeout := envDuration("XG2G_SERVER_SHUTDOWN_TIMEOUT", defaultShutdownTimeout)

	// Start optional stream proxy server BEFORE initial refresh to support smart stream detection
	var proxySrv *proxy.Server
	if proxy.IsEnabled() {
		targetURL := proxy.GetTargetURL()
		if targetURL == "" {
			logger.Fatal().
				Str("event", "proxy.config.invalid").
				Msg("XG2G_ENABLE_STREAM_PROXY is true but XG2G_PROXY_TARGET is not set")
		}

		proxyLogger := xglog.WithComponent("proxy")
		var err error
		proxySrv, err = proxy.New(proxy.Config{
			ListenAddr: proxy.GetListenAddr(),
			TargetURL:  targetURL,
			Logger:     proxyLogger,
		})
		if err != nil {
			logger.Fatal().
				Err(err).
				Str("event", "proxy.init.failed").
				Msg("failed to create stream proxy server")
		}

		// Start proxy server in background
		go func() {
			if err := proxySrv.Start(); err != nil {
				logger.Error().
					Err(err).
					Str("event", "proxy.failed").
					Msg("stream proxy server failed")
			}
		}()

		// Give proxy a moment to start listening before smart stream detection runs
		time.Sleep(100 * time.Millisecond)

		// Ensure proxy server is shut down on exit
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := proxySrv.Shutdown(shutdownCtx); err != nil {
				logger.Warn().Err(err).Msg("proxy server shutdown failed")
			}
		}()
	}

	// Optional initial refresh after proxy is ready (for smart stream detection).
	if strings.ToLower(env("XG2G_INITIAL_REFRESH", "false")) == "true" {
		logger.Info().Msg("performing initial data refresh on startup")
		if _, err := jobs.Refresh(ctx, cfg); err != nil {
			logger.Error().Err(err).Msg("initial data refresh failed")
		} else {
			logger.Info().Msg("initial data refresh completed successfully")
		}
	}

	// Start metrics server on separate port if configured
	metricsAddr := resolveMetricsListen()
	if metricsAddr != "" {
		metricsSrv := &http.Server{
			Addr:              metricsAddr,
			Handler:           promhttp.Handler(),
			ReadHeaderTimeout: serverReadTimeout / 2,
		}
		go func() {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error().
						Str("event", "metrics.panic").
						Interface("panic_value", rec).
						Msg("panic recovered in metrics server goroutine")
				}
			}()
			logger.Info().
				Str("addr", metricsAddr).
				Str("event", "metrics.start").
				Msg("starting metrics server")
			if err := metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.Error().
					Err(err).
					Str("event", "metrics.failed").
					Msg("metrics server failed")
			}
		}()
		// Graceful shutdown for metrics server
		go func() {
			<-ctx.Done()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := metricsSrv.Shutdown(shutdownCtx); err != nil {
				logger.Warn().Err(err).Msg("metrics server shutdown failed")
			}
		}()
	} else {
		logger.Info().
			Str("event", "metrics.disabled").
			Msg("metrics server disabled (no XG2G_METRICS_LISTEN configured)")
	}

	// Configure main API server with hardening
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadTimeout:       serverReadTimeout,
		ReadHeaderTimeout: serverReadTimeout / 2,
		WriteTimeout:      serverWriteTimeout,
		IdleTimeout:       serverIdleTimeout,
		MaxHeaderBytes:    serverMaxHeaderBytes,
	}

	// Start main API server
	go func() {
		logger.Info().
			Str("addr", addr).
			Str("event", "server.start").
			Msg("starting main server")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal().
				Err(err).
				Str("event", "server.failed").
				Msg("server failed")
		}
	}()

	// Start SSDP announcer for HDHomeRun auto-discovery if enabled
	if hdhrSrv := s.HDHomeRunServer(); hdhrSrv != nil {
		go func() {
			if err := hdhrSrv.StartSSDPAnnouncer(ctx); err != nil {
				logger.Error().
					Err(err).
					Str("event", "ssdp.failed").
					Msg("SSDP announcer failed")
			}
		}()
	}

	// Wait for interrupt signal
	<-ctx.Done()

	// Restore default behavior on the interrupt signal and notify user of shutdown.
	stop()
	logger.Info().Msg("shutting down gracefully, press Ctrl+C again to force")

	// The context is used to inform the server it has 5 seconds to finish
	// the request it is currently handling
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Fatal().Err(err).Msg("server forced to shutdown")
	}

	logger.Info().Msg("server exiting")
}

// env reads an environment variable or returns a default value.
// It also logs the source of the value (default or environment).
func env(key, defaultValue string) string {
	return envWithLogger(xglog.WithComponent("config"), key, defaultValue)
}

// envWithLogger reads an environment variable or returns a default value with custom logger.
func envWithLogger(logger zerolog.Logger, key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		lowerKey := strings.ToLower(key)
		switch {
		case strings.Contains(lowerKey, "token") || strings.Contains(lowerKey, "password"):
			// For sensitive vars, just log that it was set
			logger.Debug().
				Str("key", key).
				Str("source", "environment").
				Bool("sensitive", true).
				Msg("using environment variable")
		case value == "":
			logger.Debug().
				Str("key", key).
				Str("default", defaultValue).
				Str("source", "default").
				Msg("using default value (environment variable is empty)")
			return defaultValue
		default:
			logger.Debug().
				Str("key", key).
				Str("value", value).
				Str("source", "environment").
				Msg("using environment variable")
		}
		return value
	}
	logger.Debug().
		Str("key", key).
		Str("default", defaultValue).
		Str("source", "default").
		Msg("using default value")
	return defaultValue
}

// envInt reads an integer from ENV with fallback on parse errors.
// Returns the parsed value and true if successful, or defaultVal and false on error.

// atoi is deprecated - use envInt instead.
// Kept for backward compatibility but will log warning.
// TODO: Remove after migrating all callers to envInt.
func atoi(s string) int {
	logger := xglog.WithComponent("config")
	i, err := strconv.Atoi(s)
	if err != nil {
		logger.Warn().
			Err(err).
			Str("value", s).
			Msg("DEPRECATED: atoi() called with invalid value - returning 0. Use envInt() instead")
		return 0
	}
	return i
}

// envDuration reads a duration from ENV in Go duration format (e.g. "5s").
// Falls back to default on parse errors or empty variables and logs the choice.
func envDuration(key string, def time.Duration) time.Duration {
	logger := xglog.WithComponent("config")
	if v, ok := os.LookupEnv(key); ok {
		if v == "" {
			logger.Debug().
				Str("key", key).
				Dur("default", def).
				Str("source", "default").
				Msg("using default value (environment variable is empty)")
			return def
		}
		if d, err := time.ParseDuration(v); err == nil {
			logger.Debug().
				Str("key", key).
				Dur("value", d).
				Str("source", "environment").
				Msg("using environment variable")
			return d
		}
		logger.Warn().
			Str("key", key).
			Str("value", v).
			Dur("default", def).
			Msg("invalid duration in environment variable, using default")
		return def
	}
	logger.Debug().
		Str("key", key).
		Dur("default", def).
		Str("source", "default").
		Msg("using default value")
	return def
}

// envIntDefault reads an int from ENV and falls back to the default on error.
func envIntDefault(key string, def int) int {
	logger := xglog.WithComponent("config")
	if v, ok := os.LookupEnv(key); ok {
		if v == "" {
			logger.Debug().
				Str("key", key).
				Int("default", def).
				Str("source", "default").
				Msg("using default value (environment variable is empty)")
			return def
		}
		if i, err := strconv.Atoi(v); err == nil {
			logger.Debug().
				Str("key", key).
				Int("value", i).
				Str("source", "environment").
				Msg("using environment variable")
			return i
		}
		logger.Warn().
			Str("key", key).
			Str("value", v).
			Int("default", def).
			Msg("invalid integer in environment variable, using default")
		return def
	}
	logger.Debug().
		Str("key", key).
		Int("default", def).
		Str("source", "default").
		Msg("using default value")
	return def
}

// resolveStreamPort gets the stream port from ENV, validates it, and returns it.
// It returns an error if the port is invalid.
func resolveStreamPort() (int, error) {
	portStr := env("XG2G_STREAM_PORT", strconv.Itoa(defaultStreamPort))
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return 0, fmt.Errorf("invalid port %q: %w", portStr, err)
	}
	if port <= 0 || port > 65535 {
		return 0, fmt.Errorf("%w: %d", jobs.ErrInvalidStreamPort, port)
	}
	return port, nil
}

// resolveMetricsListen gets the metrics listen address from ENV.
// Returns an empty string if not set, disabling the metrics server.
func resolveMetricsListen() string {
	return env("XG2G_METRICS_LISTEN", "")
}

// ensureDataDir checks if the data directory is valid and writable.
// It creates the directory if it doesn't exist.
// For security, it enforces several policies:
// - The path must be absolute.
// - It must not be a symlink to a sensitive system directory.
// - The final resolved path must be writable.
func ensureDataDir(path string) error {
	logger := xglog.WithComponent("config")

	if path == "" {
		return errors.New("data directory path cannot be empty")
	}

	// Security: Ensure the path is absolute to prevent traversal attacks like "../"
	if !filepath.IsAbs(path) {
		return fmt.Errorf("data directory path %q must be absolute", path)
	}

	// If 'path' itself is a symlink, ensure it resolves cleanly (catch broken symlinks early)
	if fi, lerr := os.Lstat(path); lerr == nil && (fi.Mode()&os.ModeSymlink) != 0 {
		if _, err := filepath.EvalSymlinks(path); err != nil {
			return fmt.Errorf("cannot resolve data directory symlinks for %q: %w", path, err)
		}
	}

	// Follow symlinks to get the real path
	realDataDir, err := filepath.EvalSymlinks(path)
	if err != nil {
		// If the path doesn't exist, EvalSymlinks fails. This is okay if we can create it.
		// If it's another error (like a broken symlink), we should fail.
		if !os.IsNotExist(err) {
			return fmt.Errorf("cannot resolve data directory symlinks for %q: %w", path, err)
		}
		// Path does not exist, so we will try to create it.
		realDataDir = path
	}

	// Security check for system directories
	systemDirs := []string{"/etc", "/bin", "/sbin", "/usr", "/var", "/root", "/System"}
	for _, sysDir := range systemDirs {
		// Resolve potential symlinks in system dirs (e.g., /etc -> /private/etc on macOS)
		resolvedSysDir, rerr := filepath.EvalSymlinks(sysDir)
		if rerr != nil {
			resolvedSysDir = sysDir
		}
		if realDataDir == sysDir || realDataDir == resolvedSysDir {
			return fmt.Errorf("data directory %q resolves to a system directory %q, which is forbidden", path, realDataDir)
		}
	}

	// Check if the directory exists. If not, create it.
	info, err := os.Stat(realDataDir)
	if os.IsNotExist(err) {
		logger.Info().
			Str("path", realDataDir).
			Msg("data directory does not exist, attempting to create it")
		if err := os.MkdirAll(realDataDir, 0750); err != nil {
			return fmt.Errorf("failed to create data directory %q: %w", realDataDir, err)
		}
		logger.Info().
			Str("path", realDataDir).
			Msg("successfully created data directory")
		info, err = os.Stat(realDataDir) // Stat again after creation
		if err != nil {
			return fmt.Errorf("failed to stat data directory %q after creation: %w", realDataDir, err)
		}
	} else if err != nil {
		return fmt.Errorf("could not stat data directory %q: %w", realDataDir, err)
	}

	if !info.IsDir() {
		return fmt.Errorf("data directory path %q is a file, not a directory", realDataDir)
	}

	// Check for write permissions by creating a temporary file.
	// This is more reliable than checking permission bits.
	tmpFile := filepath.Join(realDataDir, ".writable-check")
	if err := os.WriteFile(tmpFile, []byte(""), 0600); err != nil {
		return fmt.Errorf("data directory %q is not writable: %w", realDataDir, err)
	}
	_ = os.Remove(tmpFile) // Clean up the check file, ignore error

	logger.Debug().
		Str("path", realDataDir).
		Msg("data directory is valid and writable")
	return nil
}
