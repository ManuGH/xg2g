// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"bytes"

	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/platform/paths"
	"gopkg.in/yaml.v3"
)

// Loader handles configuration loading with precedence
type Loader struct {
	configPath      string
	version         string
	ConsumedEnvKeys map[string]struct{} // Mechanical tracking of consumed keys
	filePresence    *aliasPresence
}

// NewLoader creates a new configuration loader
func NewLoader(configPath, version string) *Loader {
	return &Loader{
		configPath:      configPath,
		version:         version,
		ConsumedEnvKeys: make(map[string]struct{}),
	}
}

// Wrapper methods for mechanical connection tracking

func (l *Loader) envString(key, defaultVal string) string {
	l.ConsumedEnvKeys[key] = struct{}{}
	return ParseString(key, defaultVal)
}

func (l *Loader) envBool(key string, defaultVal bool) bool {
	l.ConsumedEnvKeys[key] = struct{}{}
	return ParseBool(key, defaultVal)
}

func (l *Loader) envInt(key string, defaultVal int) int {
	l.ConsumedEnvKeys[key] = struct{}{}
	return ParseInt(key, defaultVal)
}

func (l *Loader) envDuration(key string, defaultVal time.Duration) time.Duration {
	l.ConsumedEnvKeys[key] = struct{}{}
	return ParseDuration(key, defaultVal)
}

func (l *Loader) envFloat(key string, defaultVal float64) float64 {
	l.ConsumedEnvKeys[key] = struct{}{}
	return ParseFloat(key, defaultVal)
}

func (l *Loader) envLookup(key string) (string, bool) {
	l.ConsumedEnvKeys[key] = struct{}{}
	return os.LookupEnv(key)
}

// Load loads configuration with precedence: ENV > File > Defaults
// It enforces Strict Validated Order: Parse File (Strict) -> Apply Env -> Validate
func (l *Loader) Load() (AppConfig, error) {
	// Pre-Release Guardrail: Fail fast if legacy keys are found
	CheckLegacyEnv()
	WarnRemovedEnvKeys()

	cfg := AppConfig{}

	// 1. Set defaults
	if err := l.setDefaults(&cfg); err != nil {
		return cfg, fmt.Errorf("set defaults: %w", err)
	}

	// 2. Load from file (if provided)
	if l.configPath != "" {
		fileCfg, err := l.loadFile(l.configPath)
		if err != nil {
			return cfg, fmt.Errorf("load config file: %w", err)
		}
		if err := l.mergeFileConfig(&cfg, fileCfg); err != nil {
			return cfg, fmt.Errorf("merge file config: %w", err)
		}
	}

	// 3. Override with environment variables (highest priority)
	if err := l.checkAliasEnvToEnvConflicts(); err != nil {
		return cfg, err
	}
	l.mergeEnvConfig(&cfg)
	// Resolve ffprobe path from canonical config (ENV -> derive from ffmpeg.bin -> PATH fallback).
	cfg.FFmpeg.FFprobeBin = ResolveFFprobeBin(cfg.FFmpeg.FFprobeBin, cfg.FFmpeg.Bin)

	// 3.5. Enforce Deprecation Policy (P1.2)
	if err := l.CheckDeprecations(&cfg); err != nil {
		return cfg, err
	}

	// SAFETY: Ensure DataDir is absolute to prevent path traversal/platform errors
	if abs, err := filepath.Abs(cfg.DataDir); err == nil {
		cfg.DataDir = abs
	}

	// 4. Validate E2 Auth Mode inputs (before resolution to catch conflicts)
	if err := validateE2AuthModeInputs(&cfg); err != nil {
		return cfg, fmt.Errorf("e2 auth mode: %w", err)
	}

	// 4.5. Resolve E2 Auth Mode (inherit/none/explicit)
	resolveE2AuthMode(&cfg)

	// 4.6. ADR-00X: Fail-start if deprecated XG2G_STREAM_PROFILE is set
	if os.Getenv("XG2G_STREAM_PROFILE") != "" {
		return cfg, fmt.Errorf("XG2G_STREAM_PROFILE removed. Use XG2G_STREAMING_POLICY=universal (ADR-00X)")
	}

	// 5. Version from binary
	cfg.Version = l.version

	// 6. Resolve HLS Root (Migration & Path Safety)
	// Must be done after DataDir is finalized
	hlsRes, err := paths.ResolveHLSRoot(cfg.DataDir,
		os.Getenv(paths.EnvHLSRoot),
		os.Getenv(paths.EnvLegacyHLSRoot),
	)
	if err != nil {
		return cfg, fmt.Errorf("resolve hls root: %w", err)
	}
	// Prefer configured root if set
	if cfg.HLS.Root == "" {
		cfg.HLS.Root = hlsRes.EffectiveRoot
	}

	// 7. Validate final configuration
	if err := Validate(cfg); err != nil {
		return cfg, fmt.Errorf("config validation failed: %w", err)
	}

	return cfg, nil
}

// loadFile loads configuration from a YAML file with STRICT parsing.
// Unknown fields will cause a fatal error to prevent misconfiguration.
func (l *Loader) loadFile(path string) (*FileConfig, error) {
	path = filepath.Clean(path)

	// Check file extension
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".yaml" && ext != ".yml" {
		return nil, fmt.Errorf("unsupported config format: %s (only YAML supported)", ext)
	}

	// Read file
	// #nosec G304 -- configuration file paths are provided by the operator via CLI/ENV
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	presence, err := parseAliasPresence(data)
	if err != nil {
		return nil, fmt.Errorf("parse alias presence: %w", err)
	}
	l.filePresence = presence

	// Parse YAML with strict mode (unknown fields cause errors)
	var fileCfg FileConfig
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true) // Reject unknown fields

	if err := dec.Decode(&fileCfg); err != nil {
		if err == io.EOF {
			return &FileConfig{}, nil
		}
		if strings.Contains(err.Error(), "field") && strings.Contains(err.Error(), "not found") {
			return nil, fmt.Errorf("strict config parse error (legacy keys found? see docs/guides/CONFIGURATION.md): %w", err)
		}
		return nil, fmt.Errorf("strict config parse error: %w", err)
	}

	// Strict: Ensure no multiple documents or trailing content
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("config file contains multiple documents or trailing content")
	}

	return &fileCfg, nil
}
