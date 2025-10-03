// SPDX-License-Identifier: MIT
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
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
	"github.com/ManuGH/xg2g/internal/jobs"
	xglog "github.com/ManuGH/xg2g/internal/log"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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
	// Handle --version flag
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(Version)
		os.Exit(0)
	}

	logger := xglog.WithComponent("daemon")

	// Create a context that listens for the interrupt signal from the OS.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	streamPort, err := resolveStreamPort()
	if err != nil {
		logger.Fatal().
			Err(err).
			Str("event", "config.invalid").
			Msg("invalid stream port configuration")
	}

	owiTimeout, owiRetries, owiBackoff, owiMaxBackoff, err := resolveOWISettings()
	if err != nil {
		logger.Fatal().
			Err(err).
			Str("event", "config.invalid").
			Msg("invalid OpenWebIF client configuration")
	}

	cfg := jobs.Config{
		Version:       Version,
		DataDir:       env("XG2G_DATA", "/data"),
		OWIBase:       env("XG2G_OWI_BASE", "http://10.10.55.57"),
		OWIUsername:   env("XG2G_OWI_USER", ""),
		OWIPassword:   env("XG2G_OWI_PASS", ""),
		Bouquet:       env("XG2G_BOUQUET", "Premium"),
		PiconBase:     env("XG2G_PICON_BASE", ""),
		XMLTVPath:     env("XG2G_XMLTV", ""),
		FuzzyMax:      atoi(env("XG2G_FUZZY_MAX", "2")),
		StreamPort:    streamPort,
		APIToken:      env("XG2G_API_TOKEN", ""), // Read API token from environment
		OWITimeout:    owiTimeout,
		OWIRetries:    owiRetries,
		OWIBackoff:    owiBackoff,
		OWIMaxBackoff: owiMaxBackoff,
		// EPG Configuration
		EPGEnabled:        env("XG2G_EPG_ENABLED", "false") == "true",
		EPGDays:           atoi(env("XG2G_EPG_DAYS", "7")),
		EPGMaxConcurrency: atoi(env("XG2G_EPG_MAX_CONCURRENCY", "5")),
		EPGTimeoutMS:      atoi(env("XG2G_EPG_TIMEOUT_MS", "15000")),
		EPGRetries:        atoi(env("XG2G_EPG_RETRIES", "2")),
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

	// Optional initial refresh before starting servers to avoid shutdown races.
	if strings.ToLower(env("XG2G_INITIAL_REFRESH", "false")) == "true" {
		logger.Info().Msg("performing initial data refresh on startup")
		if _, err := jobs.Refresh(ctx, cfg); err != nil {
			logger.Error().Err(err).Msg("initial data refresh failed")
		} else {
			logger.Info().Msg("initial data refresh completed successfully")
		}
	}

	// Resolve server tuning from environment
	serverReadTimeout := envDuration("XG2G_SERVER_READ_TIMEOUT", defaultServerReadTimeout)
	serverWriteTimeout := envDuration("XG2G_SERVER_WRITE_TIMEOUT", defaultServerWriteTimeout)
	serverIdleTimeout := envDuration("XG2G_SERVER_IDLE_TIMEOUT", defaultServerIdleTimeout)
	serverMaxHeaderBytes := envIntDefault("XG2G_SERVER_MAX_HEADER_BYTES", defaultServerMaxHeaderBytes)
	shutdownTimeout := envDuration("XG2G_SERVER_SHUTDOWN_TIMEOUT", defaultShutdownTimeout)

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
	if value, exists := os.LookupEnv(key); exists {
		// For sensitive vars, just log that it was set
		if strings.Contains(strings.ToLower(key), "token") || strings.Contains(strings.ToLower(key), "password") {
			log.Printf("config: using %s from environment (set)", key)
		} else if value == "" {
			log.Printf("config: using default for %s (%q) because environment variable is empty", key, defaultValue)
			return defaultValue
		} else {
			log.Printf("config: using %s from environment (%q)", key, value)
		}
		return value
	}
	log.Printf("config: using default for %s (%q)", key, defaultValue)
	return defaultValue
}

// atoi is a wrapper for strconv.Atoi that panics on error.
// Used for parsing environment variables that are expected to be integers.
func atoi(s string) int {
	i, err := strconv.Atoi(s)
	if err != nil {
		// Use the logger from the main package
		log.Fatalf("config: failed to parse integer from string %q: %v", s, err)
	}
	return i
}

// envDuration reads a duration from ENV in Go duration format (e.g. "5s").
// Falls back to default on parse errors or empty variables and logs the choice.
func envDuration(key string, def time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok {
		if v == "" {
			log.Printf("config: using default for %s (%s) because environment variable is empty", key, def)
			return def
		}
		if d, err := time.ParseDuration(v); err == nil {
			log.Printf("config: using %s from environment (%s)", key, d)
			return d
		}
		log.Printf("config: invalid duration for %s (%q), using default %s", key, v, def)
		return def
	}
	log.Printf("config: using default for %s (%s)", key, def)
	return def
}

// envIntDefault reads an int from ENV and falls back to the default on error.
func envIntDefault(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok {
		if v == "" {
			log.Printf("config: using default for %s (%d) because environment variable is empty", key, def)
			return def
		}
		if i, err := strconv.Atoi(v); err == nil {
			log.Printf("config: using %s from environment (%d)", key, i)
			return i
		}
		log.Printf("config: invalid int for %s (%q), using default %d", key, v, def)
		return def
	}
	log.Printf("config: using default for %s (%d)", key, def)
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

// resolveOWISettings reads, validates, and returns all OpenWebIF client settings.
func resolveOWISettings() (time.Duration, int, time.Duration, time.Duration, error) {
	timeoutMsStr := env("XG2G_OWI_TIMEOUT_MS", fmt.Sprintf("%d", defaultOWITimeout.Milliseconds()))
	timeoutMs, err := strconv.ParseInt(timeoutMsStr, 10, 64)
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("invalid timeout %q: %w", timeoutMsStr, err)
	}
	timeout := time.Duration(timeoutMs) * time.Millisecond
	if timeout <= 0 || timeout > maxOWITimeout {
		return 0, 0, 0, 0, fmt.Errorf("timeout %v out of range (0, %v]", timeout, maxOWITimeout)
	}

	retriesStr := env("XG2G_OWI_RETRIES", fmt.Sprintf("%d", defaultOWIRetries))
	retries, err := strconv.Atoi(retriesStr)
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("invalid retries %q: %w", retriesStr, err)
	}
	if retries < 0 || retries > maxOWIRetries {
		return 0, 0, 0, 0, fmt.Errorf("retries %d out of range [0, %d]", retries, maxOWIRetries)
	}

	backoffMsStr := env("XG2G_OWI_BACKOFF_MS", fmt.Sprintf("%d", defaultOWIBackoff.Milliseconds()))
	backoffMs, err := strconv.ParseInt(backoffMsStr, 10, 64)
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("invalid backoff %q: %w", backoffMsStr, err)
	}
	backoff := time.Duration(backoffMs) * time.Millisecond
	if backoff <= 0 || backoff > maxOWIBackoff {
		return 0, 0, 0, 0, fmt.Errorf("backoff %v out of range (0, %v]", backoff, maxOWIBackoff)
	}

	// Max backoff is derived from base backoff, not independently configured.
	// This ensures a reasonable ceiling.
	maxBackoff := time.Duration(1<<retries) * backoff
	if maxBackoff > maxOWIBackoff {
		maxBackoff = maxOWIBackoff
	}

	return timeout, retries, backoff, maxBackoff, nil
}

// ensureDataDir checks if the data directory is valid and writable.
// It creates the directory if it doesn't exist.
// For security, it enforces several policies:
// - The path must be absolute.
// - It must not be a symlink to a sensitive system directory.
// - The final resolved path must be writable.
func ensureDataDir(path string) error {
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
		log.Printf("config: data directory %q does not exist, attempting to create it", realDataDir)
		if err := os.MkdirAll(realDataDir, 0750); err != nil {
			return fmt.Errorf("failed to create data directory %q: %w", realDataDir, err)
		}
		log.Printf("config: successfully created data directory %q", realDataDir)
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

	log.Printf("config: data directory %q is valid and writable", realDataDir)
	return nil
}
