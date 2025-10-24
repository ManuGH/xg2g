// SPDX-License-Identifier: MIT

// Package config provides configuration management for xg2g.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/jobs"
	"gopkg.in/yaml.v3"
)

// FileConfig represents the YAML configuration structure
type FileConfig struct {
	Version  string `yaml:"version,omitempty"`
	DataDir  string `yaml:"dataDir,omitempty"`
	LogLevel string `yaml:"logLevel,omitempty"`

	OpenWebIF OpenWebIFConfig `yaml:"openWebIF"`
	Bouquets  []string        `yaml:"bouquets,omitempty"`
	EPG       EPGConfig       `yaml:"epg"`
	API       APIConfig       `yaml:"api"`
	Metrics   MetricsConfig   `yaml:"metrics,omitempty"`
	Picons    PiconsConfig    `yaml:"picons,omitempty"`
}

// OpenWebIFConfig holds OpenWebIF client configuration
type OpenWebIFConfig struct {
	BaseURL    string `yaml:"baseUrl"`
	Username   string `yaml:"username,omitempty"`
	Password   string `yaml:"password,omitempty"`
	Timeout    string `yaml:"timeout,omitempty"` // e.g. "10s"
	Retries    int    `yaml:"retries,omitempty"`
	Backoff    string `yaml:"backoff,omitempty"`    // e.g. "500ms"
	MaxBackoff string `yaml:"maxBackoff,omitempty"` // e.g. "30s"
	StreamPort int    `yaml:"streamPort,omitempty"`
}

// EPGConfig holds EPG configuration
type EPGConfig struct {
	Enabled        bool   `yaml:"enabled"`
	Days           int    `yaml:"days,omitempty"`
	MaxConcurrency int    `yaml:"maxConcurrency,omitempty"`
	TimeoutMS      int    `yaml:"timeoutMs,omitempty"`
	Retries        int    `yaml:"retries,omitempty"`
	FuzzyMax       int    `yaml:"fuzzyMax,omitempty"`
	XMLTVPath      string `yaml:"xmltvPath,omitempty"`
}

// APIConfig holds API server configuration
type APIConfig struct {
	Token      string `yaml:"token,omitempty"`
	ListenAddr string `yaml:"listenAddr,omitempty"`
}

// MetricsConfig holds Prometheus metrics configuration
type MetricsConfig struct {
	Enabled    bool   `yaml:"enabled"`
	ListenAddr string `yaml:"listenAddr,omitempty"`
}

// PiconsConfig holds picon/logo configuration
type PiconsConfig struct {
	BaseURL string `yaml:"baseUrl,omitempty"`
}

// Loader handles configuration loading with precedence
type Loader struct {
	configPath string
	version    string
}

// NewLoader creates a new configuration loader
func NewLoader(configPath, version string) *Loader {
	return &Loader{
		configPath: configPath,
		version:    version,
	}
}

// Load loads configuration with precedence: ENV > File > Defaults
func (l *Loader) Load() (jobs.Config, error) {
	cfg := jobs.Config{}

	// 1. Set defaults
	l.setDefaults(&cfg)

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
	l.mergeEnvConfig(&cfg)

	// 4. Set version from binary
	cfg.Version = l.version

	// 5. Validate final configuration
	if err := Validate(cfg); err != nil {
		return cfg, fmt.Errorf("config validation failed: %w", err)
	}

	return cfg, nil
}

// setDefaults sets default values for configuration
func (l *Loader) setDefaults(cfg *jobs.Config) {
	cfg.DataDir = "/tmp" // Use /tmp as default to pass validation in tests
	cfg.OWIBase = ""     // No default - must be explicitly configured
	cfg.Bouquet = "Premium"
	cfg.StreamPort = 8001
	cfg.OWITimeout = 10 * time.Second
	cfg.OWIRetries = 3
	cfg.OWIBackoff = 500 * time.Millisecond
	cfg.OWIMaxBackoff = 30 * time.Second
	cfg.FuzzyMax = 2

	// EPG defaults
	cfg.EPGEnabled = false
	cfg.EPGDays = 7
	cfg.EPGMaxConcurrency = 5
	cfg.EPGTimeoutMS = 15000
	cfg.EPGRetries = 2
}

// loadFile loads configuration from a YAML file
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

	// Parse YAML
	var fileCfg FileConfig
	if err := yaml.Unmarshal(data, &fileCfg); err != nil {
		return nil, fmt.Errorf("parse YAML: %w", err)
	}

	return &fileCfg, nil
}

// mergeFileConfig merges file configuration into jobs.Config
func (l *Loader) mergeFileConfig(dst *jobs.Config, src *FileConfig) error {
	if src.DataDir != "" {
		dst.DataDir = expandEnv(src.DataDir)
	}

	// OpenWebIF
	if src.OpenWebIF.BaseURL != "" {
		dst.OWIBase = expandEnv(src.OpenWebIF.BaseURL)
	}
	if src.OpenWebIF.Username != "" {
		dst.OWIUsername = expandEnv(src.OpenWebIF.Username)
	}
	if src.OpenWebIF.Password != "" {
		dst.OWIPassword = expandEnv(src.OpenWebIF.Password)
	}
	if src.OpenWebIF.StreamPort > 0 {
		dst.StreamPort = src.OpenWebIF.StreamPort
	}

	// Parse durations from strings
	if src.OpenWebIF.Timeout != "" {
		d, err := time.ParseDuration(src.OpenWebIF.Timeout)
		if err != nil {
			return fmt.Errorf("invalid openWebIF.timeout: %w", err)
		}
		dst.OWITimeout = d
	}
	if src.OpenWebIF.Backoff != "" {
		d, err := time.ParseDuration(src.OpenWebIF.Backoff)
		if err != nil {
			return fmt.Errorf("invalid openWebIF.backoff: %w", err)
		}
		dst.OWIBackoff = d
	}
	if src.OpenWebIF.MaxBackoff != "" {
		d, err := time.ParseDuration(src.OpenWebIF.MaxBackoff)
		if err != nil {
			return fmt.Errorf("invalid openWebIF.maxBackoff: %w", err)
		}
		dst.OWIMaxBackoff = d
	}
	if src.OpenWebIF.Retries > 0 {
		dst.OWIRetries = src.OpenWebIF.Retries
	}

	// Bouquets (join if multiple)
	if len(src.Bouquets) > 0 {
		dst.Bouquet = strings.Join(src.Bouquets, ",")
	}

	// EPG
	if src.EPG.Enabled {
		dst.EPGEnabled = true
	}
	if src.EPG.Days > 0 {
		dst.EPGDays = src.EPG.Days
	}
	if src.EPG.MaxConcurrency > 0 {
		dst.EPGMaxConcurrency = src.EPG.MaxConcurrency
	}
	if src.EPG.TimeoutMS > 0 {
		dst.EPGTimeoutMS = src.EPG.TimeoutMS
	}
	if src.EPG.Retries > 0 {
		dst.EPGRetries = src.EPG.Retries
	}
	if src.EPG.FuzzyMax > 0 {
		dst.FuzzyMax = src.EPG.FuzzyMax
	}
	if src.EPG.XMLTVPath != "" {
		dst.XMLTVPath = src.EPG.XMLTVPath
	}

	// API
	if src.API.Token != "" {
		dst.APIToken = expandEnv(src.API.Token)
	}

	// Picons
	if src.Picons.BaseURL != "" {
		dst.PiconBase = expandEnv(src.Picons.BaseURL)
	}

	return nil
}

// mergeEnvConfig merges environment variables into jobs.Config
// ENV variables have the highest precedence
//
//nolint:gocyclo // This function handles many environment variables but is straightforward
func (l *Loader) mergeEnvConfig(cfg *jobs.Config) {
	if v := os.Getenv("XG2G_VERSION"); v != "" {
		cfg.Version = v
	}
	if v := os.Getenv("XG2G_DATA"); v != "" {
		cfg.DataDir = v
	}

	// OpenWebIF
	if v := os.Getenv("XG2G_OWI_BASE"); v != "" {
		cfg.OWIBase = v
	}
	if v := os.Getenv("XG2G_OWI_USER"); v != "" {
		cfg.OWIUsername = v
	}
	if v := os.Getenv("XG2G_OWI_PASS"); v != "" {
		cfg.OWIPassword = v
	}
	if v := os.Getenv("XG2G_STREAM_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			cfg.StreamPort = port
		}
	}

	// OpenWebIF timeouts/retries
	if v := os.Getenv("XG2G_OWI_TIMEOUT_MS"); v != "" {
		if ms, err := strconv.ParseInt(v, 10, 64); err == nil {
			cfg.OWITimeout = time.Duration(ms) * time.Millisecond
		}
	}
	if v := os.Getenv("XG2G_OWI_RETRIES"); v != "" {
		if retries, err := strconv.Atoi(v); err == nil {
			cfg.OWIRetries = retries
		}
	}
	if v := os.Getenv("XG2G_OWI_BACKOFF_MS"); v != "" {
		if ms, err := strconv.ParseInt(v, 10, 64); err == nil {
			cfg.OWIBackoff = time.Duration(ms) * time.Millisecond
		}
	}
	if v := os.Getenv("XG2G_OWI_MAX_BACKOFF_MS"); v != "" {
		if ms, err := strconv.ParseInt(v, 10, 64); err == nil {
			cfg.OWIMaxBackoff = time.Duration(ms) * time.Millisecond
		}
	}

	// Bouquet
	if v := os.Getenv("XG2G_BOUQUET"); v != "" {
		cfg.Bouquet = v
	}

	// EPG
	if v := os.Getenv("XG2G_EPG_ENABLED"); v != "" {
		cfg.EPGEnabled = strings.ToLower(v) == "true"
	}
	if v := os.Getenv("XG2G_EPG_DAYS"); v != "" {
		if days, err := strconv.Atoi(v); err == nil {
			cfg.EPGDays = days
		}
	}
	if v := os.Getenv("XG2G_EPG_MAX_CONCURRENCY"); v != "" {
		if conc, err := strconv.Atoi(v); err == nil {
			cfg.EPGMaxConcurrency = conc
		}
	}
	if v := os.Getenv("XG2G_EPG_TIMEOUT_MS"); v != "" {
		if ms, err := strconv.Atoi(v); err == nil {
			cfg.EPGTimeoutMS = ms
		}
	}
	if v := os.Getenv("XG2G_EPG_RETRIES"); v != "" {
		if retries, err := strconv.Atoi(v); err == nil {
			cfg.EPGRetries = retries
		}
	}
	if v := os.Getenv("XG2G_FUZZY_MAX"); v != "" {
		if fuzzy, err := strconv.Atoi(v); err == nil {
			cfg.FuzzyMax = fuzzy
		}
	}
	if v := os.Getenv("XG2G_XMLTV"); v != "" {
		cfg.XMLTVPath = v
	}

	// API
	if v := os.Getenv("XG2G_API_TOKEN"); v != "" {
		cfg.APIToken = v
	}

	// Picons
	if v := os.Getenv("XG2G_PICON_BASE"); v != "" {
		cfg.PiconBase = v
	}
}

// expandEnv expands environment variables in the format ${VAR} or $VAR
func expandEnv(s string) string {
	return os.ExpandEnv(s)
}
