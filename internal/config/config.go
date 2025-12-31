// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Package config provides configuration management for xg2g.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
	"gopkg.in/yaml.v3"
)

// FileConfig represents the YAML configuration structure
type FileConfig struct {
	Version       string `yaml:"version,omitempty"`
	ConfigVersion string `yaml:"configVersion,omitempty"`
	DataDir       string `yaml:"dataDir,omitempty"`
	LogLevel      string `yaml:"logLevel,omitempty"`

	OpenWebIF         OpenWebIFConfig         `yaml:"openWebIF"`
	Enigma2           Enigma2Config           `yaml:"enigma2,omitempty"`
	Bouquets          []string                `yaml:"bouquets,omitempty"`
	EPG               EPGConfig               `yaml:"epg"`
	Recording         map[string]string       `yaml:"recording_roots,omitempty"`
	RecordingPlayback RecordingPlaybackConfig `yaml:"recording_playback,omitempty"`
	API               APIConfig               `yaml:"api"`
	Metrics           MetricsConfig           `yaml:"metrics,omitempty"`
	Picons            PiconsConfig            `yaml:"picons,omitempty"`
	HDHR              HDHRConfig              `yaml:"hdhr,omitempty"`
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
	UseWebIF   *bool  `yaml:"useWebIFStreams,omitempty"`
}

// Enigma2Config holds v3 Enigma2 client configuration
type Enigma2Config struct {
	BaseURL               string `yaml:"baseUrl,omitempty"`
	Username              string `yaml:"username,omitempty"`
	Password              string `yaml:"password,omitempty"`
	Timeout               string `yaml:"timeout,omitempty"`               // e.g. "5s"
	ResponseHeaderTimeout string `yaml:"responseHeaderTimeout,omitempty"` // e.g. "5s"
	Retries               int    `yaml:"retries,omitempty"`
	Backoff               string `yaml:"backoff,omitempty"`    // e.g. "200ms"
	MaxBackoff            string `yaml:"maxBackoff,omitempty"` // e.g. "2s"
	RateLimit             int    `yaml:"rateLimit,omitempty"`  // requests/sec
	RateBurst             int    `yaml:"rateBurst,omitempty"`
	UserAgent             string `yaml:"userAgent,omitempty"`
}

// EPGConfig holds EPG configuration
// Uses pointers for optional fields to distinguish between "not set" and "explicitly set to zero/false"
type EPGConfig struct {
	Enabled        *bool  `yaml:"enabled,omitempty"`
	Days           *int   `yaml:"days,omitempty"`
	MaxConcurrency *int   `yaml:"maxConcurrency,omitempty"`
	TimeoutMS      *int   `yaml:"timeoutMs,omitempty"`
	Retries        *int   `yaml:"retries,omitempty"`
	FuzzyMax       *int   `yaml:"fuzzyMax,omitempty"`
	XMLTVPath      string `yaml:"xmltvPath,omitempty"`
	Source         string `yaml:"source,omitempty"` // "bouquet" or "per-service" (default)
}

// RecordingPlaybackConfig holds recording playback configuration
type RecordingPlaybackConfig struct {
	PlaybackPolicy string                 `yaml:"playback_policy,omitempty"` // "auto" (default), "local_only", "receiver_only"
	StableWindow   string                 `yaml:"stable_window,omitempty"`   // Duration string (e.g., "2s")
	Mappings       []RecordingPathMapping `yaml:"mappings,omitempty"`
}

// RecordingPathMapping defines Receiver→Local path mapping
type RecordingPathMapping struct {
	ReceiverRoot string `yaml:"receiver_root"` // e.g., "/media/net/movie"
	LocalRoot    string `yaml:"local_root"`    // e.g., "/media/nfs-recordings"
}

// ScopedToken defines a token and its associated scopes.
type ScopedToken struct {
	Token  string   `yaml:"token"`
	Scopes []string `yaml:"scopes"`
}

// APIConfig holds API server configuration
type APIConfig struct {
	Token          string          `yaml:"token,omitempty"`
	TokenScopes    []string        `yaml:"tokenScopes,omitempty"`
	Tokens         []ScopedToken   `yaml:"tokens,omitempty"`
	ListenAddr     string          `yaml:"listenAddr,omitempty"`
	RateLimit      RateLimitConfig `yaml:"rateLimit,omitempty"`
	AllowedOrigins []string        `yaml:"allowedOrigins,omitempty"`
}

// RateLimitConfig holds rate limiting settings
type RateLimitConfig struct {
	Enabled   *bool    `yaml:"enabled,omitempty"`   // Pointer to distinguish from zero value
	Global    *int     `yaml:"global,omitempty"`    // Requests per second
	Auth      *int     `yaml:"auth,omitempty"`      // Requests per minute (stricter)
	Burst     *int     `yaml:"burst,omitempty"`     // Burst capacity
	Whitelist []string `yaml:"whitelist,omitempty"` // CIDRs or IPs to exempt
}

// MetricsConfig holds Prometheus metrics configuration
// Uses pointer for Enabled to distinguish between "not set" and "explicitly disabled"
type MetricsConfig struct {
	Enabled    *bool  `yaml:"enabled,omitempty"`
	ListenAddr string `yaml:"listenAddr,omitempty"`
}

// PiconsConfig holds picon/logo configuration
type PiconsConfig struct {
	BaseURL string `yaml:"baseUrl,omitempty"`
}

// HDHRConfig holds HDHomeRun emulation configuration (optional pointers for YAML merge)
type HDHRConfig struct {
	Enabled      *bool  `yaml:"enabled,omitempty"`
	DeviceID     string `yaml:"deviceId,omitempty"`
	FriendlyName string `yaml:"friendlyName,omitempty"`
	ModelNumber  string `yaml:"modelNumber,omitempty"`
	FirmwareName string `yaml:"firmwareName,omitempty"`
	BaseURL      string `yaml:"baseUrl,omitempty"`
	TunerCount   *int   `yaml:"tunerCount,omitempty"`
	PlexForceHLS *bool  `yaml:"plexForceHls,omitempty"`
}

// AppConfig holds all configuration for the application
type AppConfig struct {
	Version           string
	ConfigVersion     string
	ConfigStrict      bool
	DataDir           string
	LogLevel          string
	LogService        string
	OWIBase           string
	OWIUsername       string // Optional: HTTP Basic Auth username
	OWIPassword       string // Optional: HTTP Basic Auth password
	Bouquet           string // Comma-separated list of bouquets (empty = all)
	XMLTVPath         string
	PiconBase         string
	FuzzyMax          int
	StreamPort        int
	UseWebIFStreams   bool
	APIToken          string // Optional: for securing API endpoints (e.g., /api/v3/*)
	APITokenScopes    []string
	APITokens         []ScopedToken
	apiTokensParseErr error
	APIListenAddr     string // Optional: API listen address (if set via config.yaml)
	TrustedProxies    string // Comma-separated list of trusted CIDRs
	MetricsEnabled    bool   // Optional: enable Prometheus metrics server
	MetricsAddr       string // Optional: metrics listen address (if enabled)
	OWITimeout        time.Duration
	OWIRetries        int
	OWIBackoff        time.Duration
	OWIMaxBackoff     time.Duration

	// EPG Configuration
	EPGEnabled        bool
	EPGDays           int    // Number of days to fetch EPG data (1-14)
	EPGMaxConcurrency int    // Max parallel EPG requests (1-10)
	EPGTimeoutMS      int    // Timeout per EPG request in milliseconds
	EPGRetries        int    // Retry attempts for EPG requests
	EPGSource         string // EPG fetch strategy: "bouquet" (fast, single request) or "per-service" (default, per-channel requests)

	// TLS Configuration
	TLSCert    string // Path to TLS certificate file
	TLSKey     string // Path to TLS key file
	ForceHTTPS bool   // Redirect HTTP to HTTPS

	// Feature Flags
	// TODO(cleanup): Remove InstantTuneEnabled - feature flag exists but no implementation
	InstantTuneEnabled bool // Enable "Instant Tune" stream pre-warming (UNIMPLEMENTED)

	// TODO(cleanup): Review ReadyStrict usage - only used in tests, not in actual health check logic
	ReadyStrict bool // Enable strict readiness checks (check upstream availability)
	// TODO(cleanup): Remove ShadowIntentsEnabled - fully implemented but not integrated into API flow
	ShadowIntentsEnabled bool   // v3 Shadow Canary: mirror intents to v3 API (default OFF, UNUSED)
	ShadowTarget         string // v3 Shadow Canary: target URL (e.g. http://localhost:8080/api/v3/intents)

	// v3 Worker Config
	WorkerEnabled bool   // Enable v3 worker
	WorkerMode    string // "standard" or "virtual" (dry-run)
	StoreBackend  string // "memory" or "bolt"
	StorePath     string // Path to v3 store data
	TunerSlots    []int  // Parsed tuner slots (e.g. [0, 1])

	// Enigma2 Config (Phase 8-3)
	E2Host        string
	E2TuneTimeout time.Duration
	E2Username    string
	E2Password    string
	E2Timeout     time.Duration
	E2RespTimeout time.Duration
	E2Retries     int
	E2Backoff     time.Duration
	E2MaxBackoff  time.Duration
	E2RateLimit   int
	E2RateBurst   int
	E2UserAgent   string

	// FFmpeg Config (Phase 8-4)
	FFmpegBin         string
	FFmpegKillTimeout time.Duration

	// HLS Config (Phase 8-5)
	HLSRoot       string        // e.g. /var/lib/xg2g/v3-hls
	DVRWindowSec  int           // DVR window duration in seconds (default: 2700 = 45 minutes)
	V3IdleTimeout time.Duration // Stop v3 sessions after idle (0 disables)
	// TODO(v3.2): Remove V3APILeases - Phase 1 legacy code, default is Phase 2 (false) since ADR-003
	V3APILeases bool // Feature Flag: If true, API acquires lease (Phase 1). If false, Worker does (Phase 2).

	RateLimitEnabled   bool // Enable rate limiting
	RateLimitGlobal    int  // Requests per second (global)
	RateLimitAuth      int  // Requests per minute (auth)
	RateLimitBurst     int
	RateLimitWhitelist []string
	AllowedOrigins     []string

	RecordingRoots map[string]string // ID -> Absolute Path (e.g. "hdd" -> "/media/hdd/movie")

	// Recording Playback Configuration
	RecordingPlaybackPolicy string                 // "auto" (default), "local_only", "receiver_only"
	RecordingStableWindow   time.Duration          // File stability check duration (default: 2s)
	RecordingPathMappings   []RecordingPathMapping // Receiver→Local path mappings

	// HDHomeRun Configuration
	HDHR HDHRConfig // Reusing the struct as it fits well (using value types locally)
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
// It enforces Strict Validated Order: Parse File (Strict) -> Apply Env -> Validate
func (l *Loader) Load() (AppConfig, error) {
	cfg := AppConfig{}

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
func (l *Loader) setDefaults(cfg *AppConfig) {
	cfg.DataDir = "/tmp" // Use /tmp as default to pass validation in tests
	cfg.OWIBase = ""     // No default - must be explicitly configured
	cfg.Bouquet = ""
	cfg.StreamPort = 8001
	cfg.UseWebIFStreams = true
	cfg.APIListenAddr = ""
	cfg.MetricsEnabled = false
	cfg.MetricsAddr = ":9090"
	cfg.OWITimeout = 10 * time.Second
	cfg.OWIRetries = 3
	cfg.OWIBackoff = 500 * time.Millisecond
	cfg.OWIMaxBackoff = 30 * time.Second
	cfg.FuzzyMax = 2
	cfg.LogLevel = "info"
	cfg.ConfigVersion = V3ConfigVersion
	cfg.ConfigStrict = true

	// Enigma2 (v3) defaults
	cfg.E2TuneTimeout = 10 * time.Second
	cfg.E2Timeout = 10 * time.Second
	cfg.E2RespTimeout = 10 * time.Second
	cfg.E2Retries = 2
	cfg.E2Backoff = 200 * time.Millisecond
	cfg.E2MaxBackoff = 2 * time.Second
	cfg.E2RateLimit = 10
	cfg.E2RateBurst = 20
	cfg.E2UserAgent = "xg2g-v3"
	cfg.WorkerMode = "standard"
	cfg.StoreBackend = "memory"
	cfg.StorePath = "/var/lib/xg2g/v3-store"
	cfg.FFmpegBin = "ffmpeg"
	cfg.FFmpegKillTimeout = 5 * time.Second
	cfg.HLSRoot = "/var/lib/xg2g/v3-hls"
	cfg.DVRWindowSec = 2700
	cfg.V3IdleTimeout = 2 * time.Minute

	// EPG defaults - enabled by default for complete out-of-the-box experience
	cfg.EPGEnabled = true
	cfg.EPGDays = 7
	cfg.EPGMaxConcurrency = 5
	cfg.EPGTimeoutMS = 15000
	cfg.EPGRetries = 2
	cfg.EPGSource = "per-service" // Default to per-service for backward compatibility

	// Feature Flags
	cfg.V3APILeases = false // Phase 2 Default: Worker manages leases
	cfg.InstantTuneEnabled = false
	cfg.ReadyStrict = false

	// Rate Limiting (Secure by Default)
	cfg.RateLimitEnabled = true
	cfg.RateLimitGlobal = 100 // Reasonable RPS for single user
	cfg.RateLimitAuth = 10    // Strict limit for auth attempts (RPM)

	// Recording Defaults
	cfg.RecordingRoots = nil
	cfg.RecordingPlaybackPolicy = "auto" // Local-first with receiver fallback
	cfg.RecordingStableWindow = 2 * time.Second
	cfg.RecordingPathMappings = nil

	// HDHomeRun Defaults
	cfg.HDHR.Enabled = new(bool)
	*cfg.HDHR.Enabled = false // Disabled by default
	cfg.HDHR.DeviceID = ""    // Auto-generated if empty
	cfg.HDHR.FriendlyName = "xg2g"
	cfg.HDHR.ModelNumber = "HDHR-xg2g"
	cfg.HDHR.FirmwareName = "xg2g-1.4.0"
	cfg.HDHR.TunerCount = new(int)
	*cfg.HDHR.TunerCount = 4
	cfg.HDHR.PlexForceHLS = new(bool)
	*cfg.HDHR.PlexForceHLS = false
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

// mergeFileConfig merges file configuration into jobs.Config
func (l *Loader) mergeFileConfig(dst *AppConfig, src *FileConfig) error {
	if src.DataDir != "" {
		dst.DataDir = expandEnv(src.DataDir)
	}
	if src.LogLevel != "" {
		dst.LogLevel = src.LogLevel
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
	if src.OpenWebIF.UseWebIF != nil {
		dst.UseWebIFStreams = *src.OpenWebIF.UseWebIF
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

	// EPG - use pointer types to allow false/0 values from YAML
	if src.EPG.Enabled != nil {
		dst.EPGEnabled = *src.EPG.Enabled
	}
	if src.EPG.Days != nil {
		dst.EPGDays = *src.EPG.Days
	}
	if src.EPG.MaxConcurrency != nil {
		dst.EPGMaxConcurrency = *src.EPG.MaxConcurrency
	}
	if src.EPG.TimeoutMS != nil {
		dst.EPGTimeoutMS = *src.EPG.TimeoutMS
	}
	if src.EPG.Retries != nil {
		dst.EPGRetries = *src.EPG.Retries
	}
	if src.EPG.FuzzyMax != nil {
		dst.FuzzyMax = *src.EPG.FuzzyMax
	}
	if src.EPG.XMLTVPath != "" {
		dst.XMLTVPath = src.EPG.XMLTVPath
	}
	if src.EPG.Source != "" {
		dst.EPGSource = src.EPG.Source
	}

	// Recording Roots
	if len(src.Recording) > 0 {
		// Initialize if map is nil (which it shouldn't be due to setDefaults, but safe for merge)
		if dst.RecordingRoots == nil {
			dst.RecordingRoots = make(map[string]string)
		}
		for k, v := range src.Recording {
			dst.RecordingRoots[k] = v
		}
	}

	// Recording Playback
	if src.RecordingPlayback.PlaybackPolicy != "" {
		dst.RecordingPlaybackPolicy = src.RecordingPlayback.PlaybackPolicy
	}
	if src.RecordingPlayback.StableWindow != "" {
		d, err := time.ParseDuration(src.RecordingPlayback.StableWindow)
		if err != nil {
			return fmt.Errorf("invalid recording_playback.stable_window: %w", err)
		}
		dst.RecordingStableWindow = d
	}
	if len(src.RecordingPlayback.Mappings) > 0 {
		dst.RecordingPathMappings = append([]RecordingPathMapping(nil), src.RecordingPlayback.Mappings...)
	}

	// API
	if src.API.Token != "" {
		dst.APIToken = expandEnv(src.API.Token)
	}
	if len(src.API.TokenScopes) > 0 {
		dst.APITokenScopes = append([]string(nil), src.API.TokenScopes...)
	}
	if len(src.API.Tokens) > 0 {
		dst.APITokens = append([]ScopedToken(nil), src.API.Tokens...)
	}
	if src.API.ListenAddr != "" {
		dst.APIListenAddr = expandEnv(src.API.ListenAddr)
	}
	if src.API.RateLimit.Enabled != nil {
		dst.RateLimitEnabled = *src.API.RateLimit.Enabled
	}
	if src.API.RateLimit.Global != nil {
		dst.RateLimitGlobal = *src.API.RateLimit.Global
	}
	if src.API.RateLimit.Auth != nil {
		dst.RateLimitAuth = *src.API.RateLimit.Auth
	}
	if src.API.RateLimit.Burst != nil {
		dst.RateLimitBurst = *src.API.RateLimit.Burst
	}
	if len(src.API.RateLimit.Whitelist) > 0 {
		dst.RateLimitWhitelist = src.API.RateLimit.Whitelist
	}
	if len(src.API.AllowedOrigins) > 0 {
		dst.AllowedOrigins = src.API.AllowedOrigins
	}

	// Metrics
	if src.Metrics.Enabled != nil {
		dst.MetricsEnabled = *src.Metrics.Enabled
	}
	if src.Metrics.ListenAddr != "" {
		dst.MetricsAddr = expandEnv(src.Metrics.ListenAddr)
	}

	// Picons
	if src.Picons.BaseURL != "" {
		dst.PiconBase = expandEnv(src.Picons.BaseURL)
	}

	// Enigma2 (v3)
	if src.Enigma2.BaseURL != "" {
		dst.E2Host = expandEnv(src.Enigma2.BaseURL)
	}
	if src.Enigma2.Username != "" {
		dst.E2Username = expandEnv(src.Enigma2.Username)
	}
	if src.Enigma2.Password != "" {
		dst.E2Password = expandEnv(src.Enigma2.Password)
	}
	if src.Enigma2.Timeout != "" {
		d, err := time.ParseDuration(src.Enigma2.Timeout)
		if err != nil {
			return fmt.Errorf("invalid enigma2.timeout: %w", err)
		}
		dst.E2Timeout = d
	}
	if src.Enigma2.ResponseHeaderTimeout != "" {
		d, err := time.ParseDuration(src.Enigma2.ResponseHeaderTimeout)
		if err != nil {
			return fmt.Errorf("invalid enigma2.responseHeaderTimeout: %w", err)
		}
		dst.E2RespTimeout = d
	}
	if src.Enigma2.Backoff != "" {
		d, err := time.ParseDuration(src.Enigma2.Backoff)
		if err != nil {
			return fmt.Errorf("invalid enigma2.backoff: %w", err)
		}
		dst.E2Backoff = d
	}
	if src.Enigma2.MaxBackoff != "" {
		d, err := time.ParseDuration(src.Enigma2.MaxBackoff)
		if err != nil {
			return fmt.Errorf("invalid enigma2.maxBackoff: %w", err)
		}
		dst.E2MaxBackoff = d
	}
	if src.Enigma2.Retries > 0 {
		dst.E2Retries = src.Enigma2.Retries
	}
	if src.Enigma2.RateLimit > 0 {
		dst.E2RateLimit = src.Enigma2.RateLimit
	}
	if src.Enigma2.RateBurst > 0 {
		dst.E2RateBurst = src.Enigma2.RateBurst
	}
	if src.Enigma2.UserAgent != "" {
		dst.E2UserAgent = src.Enigma2.UserAgent
	}

	// HDHomeRun
	if src.HDHR.Enabled != nil {
		dst.HDHR.Enabled = src.HDHR.Enabled
	}
	if src.HDHR.DeviceID != "" {
		dst.HDHR.DeviceID = expandEnv(src.HDHR.DeviceID)
	}
	if src.HDHR.FriendlyName != "" {
		dst.HDHR.FriendlyName = expandEnv(src.HDHR.FriendlyName)
	}
	if src.HDHR.ModelNumber != "" {
		dst.HDHR.ModelNumber = expandEnv(src.HDHR.ModelNumber)
	}
	if src.HDHR.FirmwareName != "" {
		dst.HDHR.FirmwareName = expandEnv(src.HDHR.FirmwareName)
	}
	if src.HDHR.BaseURL != "" {
		dst.HDHR.BaseURL = expandEnv(src.HDHR.BaseURL)
	}
	if src.HDHR.TunerCount != nil {
		dst.HDHR.TunerCount = src.HDHR.TunerCount
	}
	if src.HDHR.PlexForceHLS != nil {
		dst.HDHR.PlexForceHLS = src.HDHR.PlexForceHLS
	}

	return nil
}

// mergeEnvConfig merges environment variables into jobs.Config
// ENV variables have the highest precedence
// Uses consistent ParseBool/ParseInt/ParseDuration helpers from env.go
func (l *Loader) mergeEnvConfig(cfg *AppConfig) {
	// String values (direct assignment)
	cfg.Version = ParseString("XG2G_VERSION", cfg.Version)
	cfg.DataDir = ParseString("XG2G_DATA", cfg.DataDir)
	cfg.LogLevel = ParseString("XG2G_LOG_LEVEL", cfg.LogLevel)
	cfg.LogService = ParseString("XG2G_LOG_SERVICE", cfg.LogService)

	// OpenWebIF
	cfg.OWIBase = ParseString("XG2G_OWI_BASE", cfg.OWIBase)

	// Username: XG2G_OWI_USER
	if v := ParseString("XG2G_OWI_USER", ""); v != "" {
		cfg.OWIUsername = v
	}

	// Password: XG2G_OWI_PASS
	if v := ParseString("XG2G_OWI_PASS", ""); v != "" {
		cfg.OWIPassword = v
	}
	cfg.StreamPort = ParseInt("XG2G_STREAM_PORT", cfg.StreamPort)
	cfg.UseWebIFStreams = ParseBool("XG2G_USE_WEBIF_STREAMS", cfg.UseWebIFStreams)

	// OpenWebIF timeouts/retries
	// Convert millisecond ENV values to time.Duration
	// OpenWebIF timeouts/retries
	// Convert millisecond ENV values to time.Duration
	if ms := ParseInt("XG2G_OWI_TIMEOUT_MS", 0); ms > 0 {
		cfg.OWITimeout = time.Duration(ms) * time.Millisecond
	}
	cfg.OWIRetries = ParseInt("XG2G_OWI_RETRIES", cfg.OWIRetries)
	if ms := ParseInt("XG2G_OWI_BACKOFF_MS", 0); ms > 0 {
		cfg.OWIBackoff = time.Duration(ms) * time.Millisecond
	}
	if ms := ParseInt("XG2G_OWI_MAX_BACKOFF_MS", 0); ms > 0 {
		cfg.OWIMaxBackoff = time.Duration(ms) * time.Millisecond
	}

	cfg.ConfigStrict = ParseBool("XG2G_V3_CONFIG_STRICT", cfg.ConfigStrict)

	// Bouquet
	cfg.Bouquet = ParseString("XG2G_BOUQUET", cfg.Bouquet)

	// EPG
	cfg.EPGEnabled = ParseBool("XG2G_EPG_ENABLED", cfg.EPGEnabled)
	cfg.EPGDays = ParseInt("XG2G_EPG_DAYS", cfg.EPGDays)
	cfg.EPGMaxConcurrency = ParseInt("XG2G_EPG_MAX_CONCURRENCY", cfg.EPGMaxConcurrency)
	cfg.EPGTimeoutMS = ParseInt("XG2G_EPG_TIMEOUT_MS", cfg.EPGTimeoutMS)
	cfg.EPGSource = ParseString("XG2G_EPG_SOURCE", cfg.EPGSource)
	cfg.EPGRetries = ParseInt("XG2G_EPG_RETRIES", cfg.EPGRetries)
	cfg.FuzzyMax = ParseInt("XG2G_FUZZY_MAX", cfg.FuzzyMax)
	cfg.XMLTVPath = ParseString("XG2G_XMLTV", cfg.XMLTVPath)

	// API
	cfg.APIToken = ParseString("XG2G_API_TOKEN", cfg.APIToken)
	cfg.APITokenScopes = parseCommaSeparated(ParseString("XG2G_API_TOKEN_SCOPES", ""), cfg.APITokenScopes)
	if tokens, err := parseScopedTokens(ParseString("XG2G_API_TOKENS", ""), cfg.APITokens); err != nil {
		cfg.apiTokensParseErr = err
	} else {
		cfg.APITokens = tokens
	}
	cfg.APIListenAddr = ParseString("XG2G_LISTEN", cfg.APIListenAddr)

	// Metrics
	// Primary configuration is via XG2G_METRICS_LISTEN (empty disables).
	metricsAddr := ParseString("XG2G_METRICS_LISTEN", "")
	if metricsAddr != "" {
		cfg.MetricsAddr = metricsAddr
		cfg.MetricsEnabled = true
	}

	// Picons
	cfg.PiconBase = ParseString("XG2G_PICON_BASE", cfg.PiconBase)

	// TLS
	cfg.TLSCert = ParseString("XG2G_TLS_CERT", cfg.TLSCert)
	cfg.TLSKey = ParseString("XG2G_TLS_KEY", cfg.TLSKey)
	cfg.ForceHTTPS = ParseBool("XG2G_FORCE_HTTPS", cfg.ForceHTTPS)

	// Feature Flags
	cfg.InstantTuneEnabled = ParseBool("XG2G_INSTANT_TUNE", cfg.InstantTuneEnabled)

	cfg.ReadyStrict = ParseBool("XG2G_READY_STRICT", cfg.ReadyStrict)
	cfg.ShadowIntentsEnabled = ParseBool("XG2G_V3_SHADOW_INTENTS", cfg.ShadowIntentsEnabled)
	cfg.ShadowTarget = ParseString("XG2G_V3_SHADOW_TARGET", cfg.ShadowTarget)

	cfg.WorkerEnabled = ParseBool("XG2G_V3_WORKER_ENABLED", cfg.WorkerEnabled)
	cfg.WorkerMode = ParseString("XG2G_V3_WORKER_MODE", cfg.WorkerMode)
	cfg.StoreBackend = ParseString("XG2G_V3_STORE_BACKEND", cfg.StoreBackend)
	cfg.StorePath = ParseString("XG2G_V3_STORE_PATH", cfg.StorePath)

	if rawSlots, ok := os.LookupEnv("XG2G_V3_TUNER_SLOTS"); ok {
		logger := log.WithComponent("config")
		if strings.TrimSpace(rawSlots) == "" {
			logger.Warn().
				Str("key", "XG2G_V3_TUNER_SLOTS").
				Msg("empty tuner slots, keeping existing config")
		} else if slots, err := ParseTunerSlots(rawSlots); err == nil {
			cfg.TunerSlots = slots
		} else {
			logger.Warn().
				Str("key", "XG2G_V3_TUNER_SLOTS").
				Str("value", rawSlots).
				Err(err).
				Msg("invalid tuner slots, keeping existing config")
		}
	}
	// Defaulting Rule: Virtual Mode defaults to [0] if empty
	if len(cfg.TunerSlots) == 0 && cfg.WorkerMode == "virtual" {
		cfg.TunerSlots = []int{0}
	}

	cfg.V3APILeases = ParseBool("XG2G_V3_API_LEASES", cfg.V3APILeases)

	// Smart defaulting: If XG2G_V3_E2_HOST is not set, inherit from config or OWI_BASE
	// This prevents Docker networking issues where "localhost" doesn't work
	e2Host := ParseString("XG2G_V3_E2_HOST", "")
	if e2Host == "" {
		if cfg.E2Host != "" {
			e2Host = cfg.E2Host
		} else if cfg.OWIBase != "" {
			e2Host = cfg.OWIBase
		} else {
			e2Host = "http://localhost"
		}
	}
	cfg.E2Host = e2Host
	cfg.E2TuneTimeout = ParseDuration("XG2G_V3_TUNE_TIMEOUT", cfg.E2TuneTimeout)
	cfg.E2Timeout = ParseDuration("XG2G_V3_E2_TIMEOUT", cfg.E2Timeout)
	cfg.E2RespTimeout = ParseDuration("XG2G_V3_E2_RESPONSE_HEADER_TIMEOUT", cfg.E2RespTimeout)
	cfg.E2Retries = ParseInt("XG2G_V3_E2_RETRIES", cfg.E2Retries)
	cfg.E2Backoff = ParseDuration("XG2G_V3_E2_BACKOFF", cfg.E2Backoff)
	cfg.E2MaxBackoff = ParseDuration("XG2G_V3_E2_MAX_BACKOFF", cfg.E2MaxBackoff)
	cfg.E2RateLimit = ParseInt("XG2G_V3_E2_RATE_LIMIT", cfg.E2RateLimit)
	cfg.E2RateBurst = ParseInt("XG2G_V3_E2_RATE_BURST", cfg.E2RateBurst)
	cfg.E2UserAgent = ParseString("XG2G_V3_E2_USER_AGENT", cfg.E2UserAgent)

	if v := ParseString("XG2G_V3_E2_USER", ""); v != "" {
		cfg.E2Username = v
	}
	if v := ParseString("XG2G_V3_E2_PASS", ""); v != "" {
		cfg.E2Password = v
	}

	cfg.FFmpegBin = ParseString("XG2G_V3_FFMPEG_BIN", cfg.FFmpegBin)
	cfg.FFmpegKillTimeout = ParseDuration("XG2G_V3_FFMPEG_KILL_TIMEOUT", cfg.FFmpegKillTimeout)

	cfg.HLSRoot = ParseString("XG2G_V3_HLS_ROOT", cfg.HLSRoot)
	cfg.DVRWindowSec = ParseInt("XG2G_V3_DVR_WINDOW", cfg.DVRWindowSec) // Default: 45 minutes
	cfg.V3IdleTimeout = ParseDuration("XG2G_V3_IDLE_TIMEOUT", cfg.V3IdleTimeout)

	// Rate Limiting
	cfg.RateLimitEnabled = ParseBool("XG2G_RATELIMIT", cfg.RateLimitEnabled)
	cfg.RateLimitGlobal = ParseInt("XG2G_RATELIMIT_GLOBAL", cfg.RateLimitGlobal)
	cfg.RateLimitAuth = ParseInt("XG2G_RATELIMIT_AUTH", cfg.RateLimitAuth)
	cfg.RateLimitBurst = ParseInt("XG2G_RATELIMIT_BURST", cfg.RateLimitBurst)
	cfg.RateLimitWhitelist = parseCommaSeparated(ParseString("XG2G_RATELIMIT_WHITELIST", ""), cfg.RateLimitWhitelist)

	// CSRF / CORS (Env Aliases)
	cfg.AllowedOrigins = parseCommaSeparated(ParseString("XG2G_ALLOWED_ORIGINS", ""), cfg.AllowedOrigins)

	// Trusted Proxies
	cfg.TrustedProxies = ParseString("XG2G_TRUSTED_PROXIES", cfg.TrustedProxies)

	// Recording Roots (Env Override)
	cfg.RecordingRoots = parseRecordingRoots(ParseString("XG2G_RECORDING_ROOTS", ""), cfg.RecordingRoots)

	// Recording Playback (Env Override)
	cfg.RecordingPlaybackPolicy = ParseString("XG2G_RECORDINGS_POLICY", cfg.RecordingPlaybackPolicy)
	cfg.RecordingStableWindow = ParseDuration("XG2G_RECORDINGS_STABLE_WINDOW", cfg.RecordingStableWindow)
	cfg.RecordingPathMappings = parseRecordingMappings(ParseString("XG2G_RECORDINGS_MAP", ""), cfg.RecordingPathMappings)

	// HDHomeRun Emulation
	hdhrEnabled := ParseBool("XG2G_HDHR_ENABLED", *cfg.HDHR.Enabled)
	cfg.HDHR.Enabled = &hdhrEnabled

	cfg.HDHR.DeviceID = ParseString("XG2G_HDHR_DEVICE_ID", cfg.HDHR.DeviceID)
	cfg.HDHR.FriendlyName = ParseString("XG2G_HDHR_FRIENDLY_NAME", cfg.HDHR.FriendlyName)
	cfg.HDHR.ModelNumber = ParseString("XG2G_HDHR_MODEL", cfg.HDHR.ModelNumber)
	cfg.HDHR.FirmwareName = ParseString("XG2G_HDHR_FIRMWARE", cfg.HDHR.FirmwareName)
	cfg.HDHR.BaseURL = ParseString("XG2G_HDHR_BASE_URL", cfg.HDHR.BaseURL)

	hdhrTunerCount := ParseInt("XG2G_HDHR_TUNER_COUNT", *cfg.HDHR.TunerCount)
	cfg.HDHR.TunerCount = &hdhrTunerCount

	plexForceHLS := ParseBool("XG2G_PLEX_FORCE_HLS", *cfg.HDHR.PlexForceHLS)
	cfg.HDHR.PlexForceHLS = &plexForceHLS
}

// expandEnv expands environment variables in the format ${VAR} or $VAR
func expandEnv(s string) string {
	return os.ExpandEnv(s)
}

// Helper to parse map string: "id=path,id2=path2"
func parseRecordingRoots(envVal string, defaults map[string]string) map[string]string {
	if envVal == "" {
		return defaults
	}
	out := make(map[string]string)
	// preserve defaults? usually env overrides completely.
	// let's say env overrides completely for simplicity.
	parts := strings.Split(envVal, ",")
	for _, p := range parts {
		kv := strings.SplitN(p, "=", 2)
		if len(kv) == 2 {
			key := strings.TrimSpace(kv[0])
			val := strings.TrimSpace(kv[1])
			if key != "" && val != "" {
				out[key] = val
			}
		}
	}
	if len(out) == 0 {
		return defaults // Fallback if parsing failed completely
	}
	return out
}

// Helper to parse recording path mappings: "/receiver/path=/local/path;/other=/mount"
func parseRecordingMappings(envVal string, defaults []RecordingPathMapping) []RecordingPathMapping {
	if envVal == "" {
		return defaults
	}
	var out []RecordingPathMapping
	entries := strings.Split(envVal, ";")
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		kv := strings.SplitN(entry, "=", 2)
		if len(kv) != 2 {
			continue
		}
		receiverRoot := strings.TrimSpace(kv[0])
		localRoot := strings.TrimSpace(kv[1])
		if receiverRoot == "" || localRoot == "" {
			continue
		}
		out = append(out, RecordingPathMapping{
			ReceiverRoot: receiverRoot,
			LocalRoot:    localRoot,
		})
	}
	if len(out) == 0 {
		return defaults
	}
	return out
}

// Helper to parse scoped tokens from XG2G_API_TOKENS.
// JSON array format is canonical; legacy "token=scopes;token2=scopes2" remains supported.
func parseScopedTokens(envVal string, defaults []ScopedToken) ([]ScopedToken, error) {
	trimmed := strings.TrimSpace(envVal)
	if trimmed == "" {
		return defaults, nil
	}
	if strings.HasPrefix(trimmed, "[") {
		return parseScopedTokensJSON(trimmed)
	}
	if strings.HasPrefix(trimmed, "{") {
		return nil, fmt.Errorf("XG2G_API_TOKENS JSON must be an array of objects")
	}

	logger := log.WithComponent("config")
	logger.Warn().
		Str("key", "XG2G_API_TOKENS").
		Msg("legacy token format detected; JSON array is recommended")
	return parseScopedTokensLegacy(trimmed)
}

type scopedTokenJSON struct {
	Token  string   `json:"token"`
	Scopes []string `json:"scopes"`
}

func parseScopedTokensJSON(raw string) ([]ScopedToken, error) {
	var entries []scopedTokenJSON
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil, fmt.Errorf("XG2G_API_TOKENS JSON parse failed: %w", err)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("XG2G_API_TOKENS JSON array is empty")
	}
	seen := make(map[string]struct{}, len(entries))
	out := make([]ScopedToken, 0, len(entries))
	for _, entry := range entries {
		token := strings.TrimSpace(entry.Token)
		if token == "" {
			return nil, fmt.Errorf("XG2G_API_TOKENS token is empty")
		}
		if _, ok := seen[token]; ok {
			return nil, fmt.Errorf("XG2G_API_TOKENS duplicate token %q", token)
		}
		seen[token] = struct{}{}

		scopes := make([]string, 0, len(entry.Scopes))
		for _, scope := range entry.Scopes {
			scope = strings.TrimSpace(scope)
			if scope == "" {
				return nil, fmt.Errorf("XG2G_API_TOKENS scopes must not be empty for token %q", token)
			}
			scopes = append(scopes, scope)
		}
		if len(scopes) == 0 {
			return nil, fmt.Errorf("XG2G_API_TOKENS scopes must be set for token %q", token)
		}
		out = append(out, ScopedToken{
			Token:  token,
			Scopes: scopes,
		})
	}
	return out, nil
}

func parseScopedTokensLegacy(raw string) ([]ScopedToken, error) {
	entries := strings.Split(raw, ";")
	var out []ScopedToken
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		kv := strings.SplitN(entry, "=", 2)
		if len(kv) != 2 {
			return nil, fmt.Errorf("XG2G_API_TOKENS legacy entry must be token=scopes: %q", entry)
		}
		token := strings.TrimSpace(kv[0])
		scopesRaw := strings.TrimSpace(kv[1])
		if token == "" {
			return nil, fmt.Errorf("XG2G_API_TOKENS token is empty")
		}
		if _, ok := seen[token]; ok {
			return nil, fmt.Errorf("XG2G_API_TOKENS duplicate token %q", token)
		}
		seen[token] = struct{}{}
		if scopesRaw == "" {
			return nil, fmt.Errorf("XG2G_API_TOKENS scopes must be set for token %q", token)
		}
		scopes := parseCommaSeparated(scopesRaw, nil)
		if len(scopes) == 0 {
			return nil, fmt.Errorf("XG2G_API_TOKENS scopes must be set for token %q", token)
		}
		out = append(out, ScopedToken{
			Token:  token,
			Scopes: scopes,
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("XG2G_API_TOKENS has no valid token entries")
	}
	return out, nil
}

// Helper to parse comma-separated list
func parseCommaSeparated(envVal string, defaults []string) []string {
	if envVal == "" {
		return defaults
	}
	var out []string
	parts := strings.Split(envVal, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// String implements fmt.Stringer to provide a redacted string representation of the config.
// This ensures that sensitive fields are not leaked in logs when printing the config struct.
func (c AppConfig) String() string {
	masked := MaskSecrets(c)
	// Use json for cleaner output, or simple struct dump
	// Since MaskSecrets returns map[string]any for structs, we can just print that
	return fmt.Sprintf("%+v", masked)
}
