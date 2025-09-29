// SPDX-License-Identifier: MIT
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
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
	defaultStreamPort    = 8001
	defaultOWITimeout    = 10 * time.Second       // Updated to match spec
	defaultOWIRetries    = 3                      // Updated to match spec
	defaultOWIBackoff    = 500 * time.Millisecond // Updated to match spec
	defaultOWIMaxBackoff = 2 * time.Second        // Updated to match spec
	maxOWITimeout        = 60 * time.Second
	maxOWIRetries        = 10
	maxOWIBackoff        = 30 * time.Second // Updated to match spec (30s max)

	// Server hardening defaults
	serverReadTimeout    = 5 * time.Second
	serverWriteTimeout   = 10 * time.Second
	serverIdleTimeout    = 120 * time.Second
	serverMaxHeaderBytes = 1 << 20 // 1 MB
	shutdownTimeout      = 15 * time.Second
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
		DataDir:       env("XG2G_DATA", "/data"),
		OWIBase:       env("XG2G_OWI_BASE", "http://10.10.55.57"),
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

	// Start metrics server on separate port if configured
	metricsAddr := resolveMetricsListen()
	if metricsAddr != "" {
		metricsSrv := &http.Server{
			Addr:              metricsAddr,
			Handler:           promhttp.Handler(),
			ReadHeaderTimeout: serverReadTimeout,
		}
		go func() {
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
		WriteTimeout:      serverWriteTimeout,
		IdleTimeout:       serverIdleTimeout,
		MaxHeaderBytes:    serverMaxHeaderBytes,
		ReadHeaderTimeout: serverReadTimeout,
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

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// resolveMetricsListen validates and returns the metrics server listen address.
// Returns empty string to disable metrics server.
func resolveMetricsListen() string {
	addr := os.Getenv("XG2G_METRICS_LISTEN")

	// Check if explicitly set (even if empty for disable)
	if _, exists := os.LookupEnv("XG2G_METRICS_LISTEN"); exists {
		if addr == "" {
			// Explicitly disabled
			return ""
		}
	} else {
		// Fallback to legacy XG2G_METRICS_PORT for compatibility
		if port := os.Getenv("XG2G_METRICS_PORT"); port != "" {
			addr = port
		} else {
			// Default: metrics enabled on :9090
			addr = ":9090"
		}
	}

	// Validate listen address format
	if err := validateListenAddr(addr); err != nil {
		log.Fatalf("invalid metrics listen address %q: %v", addr, err)
	}

	return addr
}

// validateListenAddr performs strict validation of listen addresses.
// Accepts: :port, host:port, [ipv6]:port
// Rejects: port (missing :), invalid formats, out-of-range ports
func validateListenAddr(addr string) error {
	if addr == "" {
		return nil // Empty = disabled
	}

	// Must contain at least one colon
	if !strings.Contains(addr, ":") {
		return fmt.Errorf("missing colon (use :port or host:port)")
	}

	// Try to resolve as listen address
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid format: %w", err)
	}

	// Validate port range
	if portNum, err := strconv.Atoi(port); err != nil {
		return fmt.Errorf("invalid port %q: %w", port, err)
	} else if portNum < 0 || portNum > 65535 {
		return fmt.Errorf("port %d out of range [0-65535]", portNum)
	}

	// Validate IPv6 addresses (if host is specified)
	if host != "" && strings.Contains(host, ":") {
		if net.ParseIP(host) == nil {
			return fmt.Errorf("invalid IPv6 address %q", host)
		}
	}

	return nil
}

func atoi(s string) int {
	if v, err := strconv.Atoi(s); err == nil {
		return v
	}
	return 0
}

func resolveStreamPort() (int, error) {
	raw := os.Getenv("XG2G_STREAM_PORT")
	if raw == "" {
		return defaultStreamPort, nil
	}
	port, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("parse XG2G_STREAM_PORT: %w", err)
	}
	if port <= 0 || port > 65535 {
		return 0, fmt.Errorf("XG2G_STREAM_PORT must be between 1 and 65535 (got %d)", port)
	}
	return port, nil
}

func resolveOWISettings() (time.Duration, int, time.Duration, time.Duration, error) {
	timeout, err := durationFromEnv("XG2G_OWI_TIMEOUT_MS", defaultOWITimeout, maxOWITimeout)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	retries, err := positiveIntFromEnv("XG2G_OWI_RETRIES", defaultOWIRetries, maxOWIRetries)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	backoff, err := durationFromEnv("XG2G_OWI_BACKOFF_MS", defaultOWIBackoff, maxOWIBackoff)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	maxBackoff, err := durationFromEnv("XG2G_OWI_MAX_BACKOFF_MS", defaultOWIMaxBackoff, maxOWIBackoff)
	if err != nil {
		return 0, 0, 0, 0, err
	}

	// Validate that max backoff >= base backoff
	if maxBackoff < backoff {
		return 0, 0, 0, 0, fmt.Errorf("XG2G_OWI_MAX_BACKOFF_MS (%s) must be >= XG2G_OWI_BACKOFF_MS (%s)", maxBackoff, backoff)
	}

	return timeout, retries, backoff, maxBackoff, nil
}

func durationFromEnv(key string, def, max time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return def, nil
	}
	ms, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	if ms <= 0 {
		return 0, fmt.Errorf("%s must be greater than 0", key)
	}
	d := time.Duration(ms) * time.Millisecond
	if d > max {
		return 0, fmt.Errorf("%s must be <= %s", key, max)
	}
	return d, nil
}

func positiveIntFromEnv(key string, def, max int) (int, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return def, nil
	}
	val, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	if val < 0 {
		return 0, fmt.Errorf("%s must be >= 0", key)
	}
	if val > max {
		return 0, fmt.Errorf("%s must be <= %d", key, max)
	}
	return val, nil
}

// ensureDataDir validates and creates the data directory at startup with symlink policy enforcement.
// This prevents runtime errors when the /files/* handler is accessed and blocks symlink escape attacks.
func ensureDataDir(dataDir string) error {
	if dataDir == "" {
		return fmt.Errorf("data directory is empty")
	}

	// Check for basic path traversal patterns
	if strings.Contains(dataDir, "..") {
		return fmt.Errorf("data directory contains path traversal sequences")
	}

	// Convert to absolute path and validate
	absDataDir, err := filepath.Abs(dataDir)
	if err != nil {
		return fmt.Errorf("invalid data directory path: %w", err)
	}

	// Check if the path exists and what type it is
	info, err := os.Lstat(absDataDir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cannot access data directory: %w", err)
	}

	// If it exists and is a symlink, validate the symlink target
	if err == nil && info.Mode()&os.ModeSymlink != 0 {

		// The directory itself is a symlink - resolve it and check boundaries
		realDataDir, err := filepath.EvalSymlinks(absDataDir)
		if err != nil {
			return fmt.Errorf("cannot resolve data directory symlinks: %w", err)
		}

		// For security, we'll be strict about symlinks - they should generally be avoided for data directories
		// Block symlinks that point to system directories or outside expected areas
		cleanReal := filepath.Clean(realDataDir)

		// Block obvious system directories (but allow temp/user areas)
		// Be specific about dangerous directories, not broad categories
		systemDirs := []string{"/etc", "/usr", "/bin", "/sbin", "/sys", "/proc", "/dev", "/root",
			"/private/etc"}
		for _, sysDir := range systemDirs {
			if strings.HasPrefix(cleanReal, sysDir+"/") || cleanReal == sysDir {
				return fmt.Errorf("data directory symlink points to system directory")
			}
		}

		// For maximum security, we could block all symlinks for XG2G_DATA itself
		// However, for now we'll allow symlinks to user directories (/tmp, /home, etc.)

		// Use the resolved path for further checks
		absDataDir = cleanReal
	}

	// Ensure the directory exists (create if needed)
	if err := os.MkdirAll(absDataDir, 0755); err != nil {
		return fmt.Errorf("cannot create data directory: %w", err)
	}

	// Final verification of the resolved directory
	realDataDir := absDataDir

	// Verify directory exists and is actually a directory
	info, err = os.Stat(realDataDir)
	if err != nil {
		return fmt.Errorf("cannot access data directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("data directory path is not a directory")
	}

	// Verify we can write to the directory
	testFile := filepath.Join(realDataDir, ".write-test")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		return fmt.Errorf("data directory is not writable: %w", err)
	}
	_ = os.Remove(testFile) // Clean up test file (ignore errors - best effort)

	return nil
}
