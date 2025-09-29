package main

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
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
)

func main() {
	logger := xglog.WithComponent("daemon")

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
		OWITimeout:    owiTimeout,
		OWIRetries:    owiRetries,
		OWIBackoff:    owiBackoff,
		OWIMaxBackoff: owiMaxBackoff,
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
		Str("owi", cfg.OWIBase).
		Str("bouquet", cfg.Bouquet).
		Str("xmltv", cfg.XMLTVPath).
		Int("fuzzy", cfg.FuzzyMax).
		Str("picon", cfg.PiconBase).
		Int("stream_port", cfg.StreamPort).
		Dur("owi_timeout", cfg.OWITimeout).
		Int("owi_retries", cfg.OWIRetries).
		Dur("owi_backoff", cfg.OWIBackoff).
		Msg("configuration loaded")

	// Start metrics server on separate port
	metricsAddr := ":9090"
	if metricsPort := os.Getenv("XG2G_METRICS_PORT"); metricsPort != "" {
		metricsAddr = ":" + metricsPort
	}

	go func() {
		logger.Info().
			Str("addr", metricsAddr).
			Str("event", "metrics.start").
			Msg("starting metrics server")
		if err := http.ListenAndServe(metricsAddr, promhttp.Handler()); err != nil {
			logger.Error().
				Err(err).
				Str("event", "metrics.failed").
				Msg("metrics server failed")
		}
	}()

	// Start main API server
	logger.Info().
		Str("addr", addr).
		Str("event", "server.start").
		Msg("starting main server")
	if err := http.ListenAndServe(addr, s.Handler()); err != nil {
		logger.Fatal().
			Err(err).
			Str("event", "server.failed").
			Msg("server failed")
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
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
