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
	platformnet "github.com/ManuGH/xg2g/internal/platform/net"
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
	return checkWritableDir(logger, "data directory", path, false)
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

	// b. Enigma2 base URL (Syntax + Scheme)
	if cfg.Enigma2.BaseURL == "" {
		logger.Warn().Msg("Enigma2 base URL not configured; running in setup mode")
	} else {
		u, err := url.Parse(cfg.Enigma2.BaseURL)
		if err != nil {
			return fmt.Errorf("invalid XG2G_E2_HOST URL: %w", err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return fmt.Errorf("XG2G_E2_HOST scheme must be http or https, got: %s", u.Scheme)
		}
		logger.Info().Str("url", platformnet.SanitizeURL(cfg.Enigma2.BaseURL)).Msg("✓ Enigma2 base URL is valid")
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
			if err := checkWritableDir(logger, fmt.Sprintf("recording root '%s'", id), path, true); err != nil {
				return fmt.Errorf("recording root '%s' check failed: %w", id, err)
			}
		}
		logger.Info().Int("count", len(cfg.RecordingRoots)).Msg("✓ Recording roots validated")
	}

	storeBackend := strings.ToLower(strings.TrimSpace(cfg.Store.Backend))
	if storeBackend != "memory" {
		if err := checkWritableDir(logger, "store path", cfg.Store.Path, true); err != nil {
			return fmt.Errorf("store path check failed: %w", err)
		}
	}

	// e. Engine dependencies + persistence safety
	if cfg.Engine.Enabled {
		if err := checkWritableDir(logger, "HLS root", cfg.HLS.Root, true); err != nil {
			return fmt.Errorf("HLS root check failed: %w", err)
		}

		if strings.EqualFold(cfg.Engine.Mode, "virtual") {
			logger.Info().Msg("Engine in virtual mode; skipping ffmpeg/curl dependency checks")
		} else {
			ffmpegBin := strings.TrimSpace(cfg.FFmpeg.Bin)
			if ffmpegBin == "" {
				ffmpegBin = "ffmpeg"
			}
			resolvedPath, err := exec.LookPath(ffmpegBin)
			if err != nil {
				return fmt.Errorf("ffmpeg binary not found (%s): %w", ffmpegBin, err)
			}
			logger.Info().Str("ffmpeg", resolvedPath).Msg("✓ Engine dependencies available")
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
		storePath := filepath.Clean(cfg.Store.Path)
		if storeBackend != "memory" && tempDir != "." && (storePath == tempDir || strings.HasPrefix(storePath, tempDir+string(filepath.Separator))) {
			logger.Warn().
				Str("store_path", cfg.Store.Path).
				Msg("store path is under temp; durable state may be lost on reboot")
		}
	}

	return nil
}

func checkWritableDir(logger zerolog.Logger, label, path string, createIfMissing bool) error {
	created, err := probeWritableDir(path, createIfMissing)
	if err != nil {
		return err
	}
	if created {
		logger.Info().Str("path", path).Msgf("✓ %s created and writable", label)
		return nil
	}
	logger.Info().Str("path", path).Msgf("✓ %s is writable", label)
	return nil
}

func checkFileReadable(path string) error {
	f, err := os.Open(path) // #nosec G304 -- path comes from operator config; verifying readability is expected
	if err != nil {
		return err
	}
	return f.Close()
}
