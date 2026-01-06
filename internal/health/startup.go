// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package health

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/rs/zerolog"
)

// PerformStartupChecks validates the environment and dependencies before starting the server.
func PerformStartupChecks(ctx context.Context, cfg config.AppConfig) error {
	logger := log.WithComponent("startup-check")
	logger.Info().Msg("Running pre-flight startup checks...")

	// 1. Data Directory Permissions
	if err := checkDataDir(logger, cfg.DataDir); err != nil {
		return fmt.Errorf("data directory check failed: %w", err)
	}

	// 2. Targeted Validations
	if err := checkTargetedValidations(logger, cfg); err != nil {
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	logger.Info().Msg("✅ All startup checks passed")
	return nil
}

func checkDataDir(logger zerolog.Logger, path string) error {
	// Check if directory exists
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("directory does not exist: %s", path)
		}
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("path is not a directory: %s", path)
	}

	// Check write permissions by creating a temp file
	testFile := filepath.Join(path, ".write_test")
	if err := os.WriteFile(testFile, []byte("ok"), 0600); err != nil {
		return fmt.Errorf("directory is not writable: %s (error: %v)", path, err)
	}
	_ = os.Remove(testFile)

	logger.Info().Str("path", path).Msg("✓ Data directory is writable")
	return nil
}

// checkTargetedValidations performs security and runtime-critical validations
func checkTargetedValidations(logger zerolog.Logger, cfg config.AppConfig) error {
	// a. Listen Address (Parseable)
	if cfg.APIListenAddr != "" {
		_, port, err := net.SplitHostPort(cfg.APIListenAddr)
		if err != nil {
			return fmt.Errorf("invalid API listen address %q: %w", cfg.APIListenAddr, err)
		}
		portNum, err := strconv.Atoi(port)
		if err != nil || portNum < 0 || portNum > 65535 {
			return fmt.Errorf("invalid API listen port %q in %q", port, cfg.APIListenAddr)
		}
		logger.Info().Str("addr", cfg.APIListenAddr).Msg("✓ API listen address is valid")
	}

	// b. OWI Base URL (Syntax + Scheme)
	if cfg.OWIBase == "" {
		logger.Warn().Msg("OpenWebIF base URL not configured; running in setup mode")
	} else {
		u, err := url.Parse(cfg.OWIBase)
		if err != nil {
			return fmt.Errorf("invalid XG2G_OWI_BASE URL: %w", err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return fmt.Errorf("XG2G_OWI_BASE scheme must be http or https, got: %s", u.Scheme)
		}
		logger.Info().Str("url", cfg.OWIBase).Msg("✓ OWI Base URL is valid")
	}

	// c. TLS Config (Pair + Readable)
	if cfg.TLSCert != "" || cfg.TLSKey != "" {
		if cfg.TLSCert == "" || cfg.TLSKey == "" {
			return fmt.Errorf("TLS configuration requires BOTH Cert and Key to be set")
		}
		// Check readability
		if err := checkFileReadable(cfg.TLSCert); err != nil {
			return fmt.Errorf("TLS Cert error: %w", err)
		}
		if err := checkFileReadable(cfg.TLSKey); err != nil {
			return fmt.Errorf("TLS Key error: %w", err)
		}
		logger.Info().Msg("✓ TLS configuration is valid")
	}

	// d. Recording Roots (Enabled only if map non-empty)
	if len(cfg.RecordingRoots) > 0 {
		for id, path := range cfg.RecordingRoots {
			if path == "" {
				return fmt.Errorf("recording root '%s' path cannot be empty", id)
			}
			if !filepath.IsAbs(path) {
				return fmt.Errorf("recording root '%s' must be an absolute path: %s", id, path)
			}
			// Ensure existence with 0750
			// MkdirAll returns nil if exists
			if err := os.MkdirAll(path, 0750); err != nil {
				return fmt.Errorf("failed to ensure recording root '%s' (%s): %w", id, path, err)
			}
			// We could check writability similar to data dir, but existence is the main requirement here.
			// Ideally we chown/chmod if it was just created, but MkdirAll doesn't tell us if it created it.
			// Just ensuring it exists is enough for "startup".
		}
		logger.Info().Int("count", len(cfg.RecordingRoots)).Msg("✓ Recording roots validated")
	}

	// e. Engine dependencies + persistence safety
	if cfg.Engine.Enabled {
		if strings.EqualFold(cfg.Engine.Mode, "virtual") {
			logger.Info().Msg("Engine in virtual mode; skipping ffmpeg/curl dependency checks")
		} else {
			ffmpegBin := strings.TrimSpace(cfg.FFmpeg.Bin)
			if ffmpegBin == "" {
				ffmpegBin = "ffmpeg"
			}
			if _, err := exec.LookPath(ffmpegBin); err != nil {
				return fmt.Errorf("ffmpeg binary not found (%s): %w", ffmpegBin, err)
			}
			if _, err := exec.LookPath("curl"); err != nil {
				return fmt.Errorf("curl binary not found: %w", err)
			}
			logger.Info().Str("ffmpeg", ffmpegBin).Msg("✓ Engine dependencies available")
		}

		if strings.EqualFold(cfg.Store.Backend, "memory") {
			logger.Warn().
				Str("store_backend", cfg.Store.Backend).
				Msg("engine uses in-memory store; sessions are not persistent across restarts")
		}

		tempDir := filepath.Clean(os.TempDir())
		dataDir := filepath.Clean(cfg.DataDir)
		if tempDir != "." && (dataDir == tempDir || strings.HasPrefix(dataDir, tempDir+string(filepath.Separator))) {
			logger.Warn().
				Str("data_dir", cfg.DataDir).
				Msg("data directory is under temp; cached data and sessions may be lost on reboot")
		}
	}

	return nil
}

func checkFileReadable(path string) error {
	f, err := os.Open(path) // #nosec G304 -- path comes from operator config; verifying readability is expected
	if err != nil {
		return err
	}
	return f.Close()
}
