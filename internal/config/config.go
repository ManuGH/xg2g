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
	"github.com/ManuGH/xg2g/internal/platform/paths"
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
	// FFmpeg stream probing settings (for OSCam-emu relay compatibility)
	AnalyzeDuration string `yaml:"analyzeDuration,omitempty"` // e.g. "10s", default: "2s"
	ProbeSize       string `yaml:"probeSize,omitempty"`       // e.g. "10M", default: "10M"
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
	User   string   `yaml:"user,omitempty"`
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

// StreamingConfig holds streaming profile configuration
type StreamingConfig struct {
	DefaultProfile  string   `yaml:"default_profile" env:"XG2G_STREAM_PROFILE"`
	AllowedProfiles []string `yaml:"allowed_profiles,omitempty"`
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
	TLSEnabled bool   // Enable TLS (auto-generate certs if cert/key empty)
	TLSCert    string // Path to TLS certificate file
	TLSKey     string // Path to TLS key file
	ForceHTTPS bool   // Redirect HTTP to HTTPS

	// Feature Flags
	// ReadyStrict enables strict readiness checks (e.g. OWI connectivity)
	ReadyStrict bool // Enable strict readiness checks (check upstream availability)

	// Engine Configuration (Canonical)
	Engine EngineConfig
	Store  StoreConfig
	HLS    HLSConfig

	// Enigma2 Config (Runtime settings with Durations)
	Enigma2 Enigma2Settings

	// FFmpeg Config
	FFmpeg FFmpegConfig

	// Global / Feature Flags
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

	// VOD Optimization
	VODProbeSize       string        // ffmpeg probesize (e.g. "50M")
	VODAnalyzeDuration string        // ffmpeg analyzeduration (e.g. "50M")
	VODStallTimeout    time.Duration // Supervisor stall timeout
	VODMaxConcurrent   int           `json:"vodMaxConcurrentBuilds,omitempty"`
	VODCacheTTL        time.Duration `json:"vodCacheTTL,omitempty"`

	// HDHomeRun Configuration
	HDHR HDHRConfig

	// Streaming Configuration
	Streaming StreamingConfig
}

// EngineConfig holds the Orchestrator engine settings
type EngineConfig struct {
	Enabled     bool          `yaml:"enabled"`
	Mode        string        `yaml:"mode"` // "standard" or "virtual"
	IdleTimeout time.Duration `yaml:"idleTimeout"`
	TunerSlots  []int         `yaml:"tunerSlots"`
}

// StoreConfig holds the state store settings
type StoreConfig struct {
	Backend string `yaml:"backend"` // "memory" or "bolt"
	Path    string `yaml:"path"`
}

// FFmpegConfig holds the FFmpeg binary settings
type FFmpegConfig struct {
	Bin         string        `yaml:"bin"`
	KillTimeout time.Duration `yaml:"killTimeout"`
}

// HLSConfig holds HLS output settings
type HLSConfig struct {
	Root      string        `yaml:"root"`
	DVRWindow time.Duration `yaml:"dvrWindow"`
}

// Enigma2Settings holds the runtime Enigma2 settings (using time.Duration)
type Enigma2Settings struct {
	BaseURL               string
	Username              string
	Password              string
	AuthMode              string // Authentication mode: "inherit" (default), "none", "explicit"
	Timeout               time.Duration
	ResponseHeaderTimeout time.Duration
	TuneTimeout           time.Duration // Logic compatibility
	Retries               int
	Backoff               time.Duration
	MaxBackoff            time.Duration
	RateLimit             int
	RateBurst             int
	UserAgent             string
	AnalyzeDuration       string
	ProbeSize             string
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
	// Pre-Release Guardrail: Fail fast if legacy keys are found
	CheckLegacyEnv()

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

	// 4. Validate E2 Auth Mode inputs (before resolution to catch conflicts)
	if err := validateE2AuthModeInputs(&cfg); err != nil {
		return cfg, fmt.Errorf("e2 auth mode: %w", err)
	}

	// 4.5. Resolve E2 Auth Mode (inherit/none/explicit)
	resolveE2AuthMode(&cfg)

	// 5. Version from binary
	cfg.Version = l.version

	// 6. Resolve HLS Root (Migration & Path Safety)
	// Must be done after DataDir is finalized
	hlsRes, err := paths.ResolveHLSRoot(cfg.DataDir)
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

	// Engine defaults
	cfg.Engine.Enabled = true
	cfg.Engine.Mode = "standard"
	cfg.Engine.IdleTimeout = 2 * time.Minute

	// Store defaults
	cfg.Store.Backend = "memory"
	cfg.Store.Path = "/var/lib/xg2g/store"

	// HLS Defaults
	// cfg.HLS.Root is handled by paths.ResolveHLSRoot in Load()
	cfg.HLS.DVRWindow = 45 * time.Minute

	// Enigma2 defaults
	cfg.Enigma2.AuthMode = "inherit"
	cfg.Enigma2.Timeout = 10 * time.Second
	cfg.Enigma2.ResponseHeaderTimeout = 10 * time.Second
	cfg.Enigma2.TuneTimeout = 10 * time.Second // Kept for logic compatibility
	cfg.Enigma2.Retries = 2
	cfg.Enigma2.Backoff = 200 * time.Millisecond
	cfg.Enigma2.MaxBackoff = 2 * time.Second
	cfg.Enigma2.RateLimit = 10
	cfg.Enigma2.RateBurst = 20
	cfg.Enigma2.UserAgent = "xg2g-engine"
	cfg.Enigma2.AnalyzeDuration = "10s"
	cfg.Enigma2.ProbeSize = "10M"

	// FFmpeg defaults
	cfg.FFmpeg.Bin = "ffmpeg"
	cfg.FFmpeg.KillTimeout = 5 * time.Second

	// EPG defaults - enabled by default for complete out-of-the-box experience
	cfg.EPGEnabled = true
	cfg.EPGDays = 7
	cfg.EPGMaxConcurrency = 5
	cfg.EPGTimeoutMS = 15000
	cfg.EPGRetries = 2
	cfg.EPGSource = "per-service" // Default to per-service for backward compatibility

	// TLS
	cfg.TLSEnabled = false

	// Feature Flags
	cfg.ReadyStrict = false

	// Rate Limiting (Secure by Default)
	cfg.RateLimitEnabled = true
	cfg.RateLimitGlobal = 100 // Reasonable RPS for single user
	cfg.RateLimitAuth = 10    // Strict limit for auth attempts (RPM)
	cfg.RateLimitBurst = 20   // Burst capacity

	// Recording Defaults
	cfg.RecordingRoots = nil
	cfg.RecordingPlaybackPolicy = "auto" // Local-first with receiver fallback
	// P2: Increase stable window to 10s to avoid premature VOD detection (Safari endlist risk)
	cfg.RecordingStableWindow = 10 * time.Second
	cfg.RecordingPathMappings = nil
	cfg.VODProbeSize = "50M"
	cfg.VODAnalyzeDuration = "50M"
	cfg.VODStallTimeout = 60 * time.Second
	cfg.VODMaxConcurrent = 2
	cfg.VODCacheTTL = 24 * time.Hour

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

	// Streaming Defaults (PR 5.1: Thin Client Fix)
	if cfg.Streaming.DefaultProfile == "" {
		cfg.Streaming.DefaultProfile = "auto"
	}
	if len(cfg.Streaming.AllowedProfiles) == 0 {
		cfg.Streaming.AllowedProfiles = []string{"auto", "safari", "safari_hevc_hw"}
	}
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
		// Initialize if map is nil
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

	// Enigma2 (Map FileConfig.Enigma2 to AppConfig.Enigma2)
	// This maps the flat YAML Enigma2Config to the nested Enigma2Config in AppConfig
	if src.Enigma2.BaseURL != "" {
		dst.Enigma2.BaseURL = expandEnv(src.Enigma2.BaseURL)
	}
	if src.Enigma2.Username != "" {
		dst.Enigma2.Username = expandEnv(src.Enigma2.Username)
	}
	if src.Enigma2.Password != "" {
		dst.Enigma2.Password = expandEnv(src.Enigma2.Password)
	}
	if src.Enigma2.Timeout != "" {
		d, err := time.ParseDuration(src.Enigma2.Timeout)
		if err != nil {
			return fmt.Errorf("invalid enigma2.timeout: %w", err)
		}
		dst.Enigma2.Timeout = d
	}
	if src.Enigma2.ResponseHeaderTimeout != "" {
		d, err := time.ParseDuration(src.Enigma2.ResponseHeaderTimeout)
		if err != nil {
			return fmt.Errorf("invalid enigma2.responseHeaderTimeout: %w", err)
		}
		dst.Enigma2.ResponseHeaderTimeout = d
	}
	if src.Enigma2.Backoff != "" {
		d, err := time.ParseDuration(src.Enigma2.Backoff)
		if err != nil {
			return fmt.Errorf("invalid enigma2.backoff: %w", err)
		}
		dst.Enigma2.Backoff = d
	}
	if src.Enigma2.MaxBackoff != "" {
		d, err := time.ParseDuration(src.Enigma2.MaxBackoff)
		if err != nil {
			return fmt.Errorf("invalid enigma2.maxBackoff: %w", err)
		}
		dst.Enigma2.MaxBackoff = d
	}
	if src.Enigma2.Retries > 0 {
		dst.Enigma2.Retries = src.Enigma2.Retries
	}
	if src.Enigma2.RateLimit > 0 {
		dst.Enigma2.RateLimit = src.Enigma2.RateLimit
	}
	if src.Enigma2.RateBurst > 0 {
		dst.Enigma2.RateBurst = src.Enigma2.RateBurst
	}
	if src.Enigma2.UserAgent != "" {
		dst.Enigma2.UserAgent = src.Enigma2.UserAgent
	}
	if src.Enigma2.AnalyzeDuration != "" {
		dst.Enigma2.AnalyzeDuration = src.Enigma2.AnalyzeDuration
	}
	if src.Enigma2.ProbeSize != "" {
		dst.Enigma2.ProbeSize = src.Enigma2.ProbeSize
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

	// Global strict mode
	cfg.ConfigStrict = ParseBool("XG2G_CONFIG_STRICT", cfg.ConfigStrict)

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

	// CORS: ENV overrides YAML if set
	if rawOrigins, ok := os.LookupEnv("XG2G_ALLOWED_ORIGINS"); ok {
		cfg.AllowedOrigins = parseCommaSeparated(rawOrigins, nil)
	}

	// Metrics
	metricsAddr := ParseString("XG2G_METRICS_LISTEN", "")
	if metricsAddr != "" {
		cfg.MetricsAddr = metricsAddr
		cfg.MetricsEnabled = true
	}

	// Picons
	cfg.PiconBase = ParseString("XG2G_PICON_BASE", cfg.PiconBase)

	// TLS
	cfg.TLSEnabled = ParseBool("XG2G_TLS_ENABLED", cfg.TLSEnabled)
	cfg.TLSCert = ParseString("XG2G_TLS_CERT", cfg.TLSCert)
	cfg.TLSKey = ParseString("XG2G_TLS_KEY", cfg.TLSKey)
	cfg.ForceHTTPS = ParseBool("XG2G_FORCE_HTTPS", cfg.ForceHTTPS)

	// Feature Flags
	cfg.ReadyStrict = ParseBool("XG2G_READY_STRICT", cfg.ReadyStrict)

	// CANONICAL ENGINE CONFIG
	cfg.Engine.Enabled = ParseBool("XG2G_ENGINE_ENABLED", cfg.Engine.Enabled)
	cfg.Engine.Mode = ParseString("XG2G_ENGINE_MODE", cfg.Engine.Mode)
	cfg.Engine.IdleTimeout = ParseDuration("XG2G_ENGINE_IDLE_TIMEOUT", cfg.Engine.IdleTimeout)

	// Tuner Slots: XG2G_TUNER_SLOTS
	if rawSlots, ok := os.LookupEnv("XG2G_TUNER_SLOTS"); ok {
		logger := log.WithComponent("config")
		if strings.TrimSpace(rawSlots) == "" {
			logger.Warn().Str("key", "XG2G_TUNER_SLOTS").Msg("empty tuner slots, keeping defaults")
		} else if slots, err := ParseTunerSlots(rawSlots); err == nil {
			cfg.Engine.TunerSlots = slots
		} else {
			logger.Warn().Str("key", "XG2G_TUNER_SLOTS").Str("value", rawSlots).Err(err).Msg("invalid tuner slots, keeping defaults")
		}
	}
	// Defaulting Rule: Virtual Mode defaults to [0] if empty
	if len(cfg.Engine.TunerSlots) == 0 && cfg.Engine.Mode == "virtual" {
		cfg.Engine.TunerSlots = []int{0}
	}

	// CANONICAL STORE CONFIG
	cfg.Store.Backend = ParseString("XG2G_STORE_BACKEND", cfg.Store.Backend)
	cfg.Store.Path = ParseString("XG2G_STORE_PATH", cfg.Store.Path)

	// CANONICAL HLS CONFIG
	cfg.HLS.Root = ParseString("XG2G_HLS_ROOT", cfg.HLS.Root)
	cfg.HLS.DVRWindow = ParseDuration("XG2G_DVR_WINDOW", cfg.HLS.DVRWindow)

	// CANONICAL ENIGMA2 CONFIG
	cfg.Enigma2.BaseURL = ParseString("XG2G_E2_HOST", cfg.Enigma2.BaseURL)
	cfg.Enigma2.Username = ParseString("XG2G_E2_USER", cfg.Enigma2.Username)
	cfg.Enigma2.Password = ParseString("XG2G_E2_PASS", cfg.Enigma2.Password)
	cfg.Enigma2.AuthMode = strings.ToLower(strings.TrimSpace(ParseString("XG2G_E2_AUTH_MODE", cfg.Enigma2.AuthMode)))
	cfg.Enigma2.Timeout = ParseDuration("XG2G_E2_TIMEOUT", cfg.Enigma2.Timeout)
	cfg.Enigma2.ResponseHeaderTimeout = ParseDuration("XG2G_E2_RESPONSE_HEADER_TIMEOUT", cfg.Enigma2.ResponseHeaderTimeout)
	cfg.Enigma2.Retries = ParseInt("XG2G_E2_RETRIES", cfg.Enigma2.Retries)
	cfg.Enigma2.Backoff = ParseDuration("XG2G_E2_BACKOFF", cfg.Enigma2.Backoff)
	cfg.Enigma2.MaxBackoff = ParseDuration("XG2G_E2_MAX_BACKOFF", cfg.Enigma2.MaxBackoff)
	cfg.Enigma2.RateLimit = ParseInt("XG2G_E2_RATE_LIMIT", cfg.Enigma2.RateLimit)
	cfg.Enigma2.RateBurst = ParseInt("XG2G_E2_RATE_BURST", cfg.Enigma2.RateBurst)
	cfg.Enigma2.UserAgent = ParseString("XG2G_E2_USER_AGENT", cfg.Enigma2.UserAgent)
	cfg.Enigma2.AnalyzeDuration = ParseString("XG2G_E2_ANALYZE_DURATION", cfg.Enigma2.AnalyzeDuration)
	cfg.Enigma2.ProbeSize = ParseString("XG2G_E2_PROBE_SIZE", cfg.Enigma2.ProbeSize)

	// Smart Defaulting: If E2Host is not set, inherit OWI config
	if cfg.Enigma2.BaseURL == "" && cfg.OWIBase != "" {
		cfg.Enigma2.BaseURL = cfg.OWIBase
	}

	// CANONICAL FFMPEG CONFIG
	cfg.FFmpeg.Bin = ParseString("XG2G_FFMPEG_BIN", cfg.FFmpeg.Bin)
	cfg.FFmpeg.KillTimeout = ParseDuration("XG2G_FFMPEG_KILL_TIMEOUT", cfg.FFmpeg.KillTimeout)

	// Rate Limiting
	cfg.RateLimitEnabled = ParseBool("XG2G_RATE_LIMIT_ENABLED", cfg.RateLimitEnabled)
	cfg.RateLimitGlobal = ParseInt("XG2G_RATE_LIMIT_GLOBAL", cfg.RateLimitGlobal)
	cfg.RateLimitAuth = ParseInt("XG2G_RATE_LIMIT_AUTH", cfg.RateLimitAuth)
	cfg.RateLimitBurst = ParseInt("XG2G_RATE_LIMIT_BURST", cfg.RateLimitBurst)
	if whitelist := ParseString("XG2G_RATE_LIMIT_WHITELIST", ""); whitelist != "" {
		cfg.RateLimitWhitelist = parseCommaSeparated(whitelist, cfg.RateLimitWhitelist)
	}

	// Trusted Proxies
	cfg.TrustedProxies = ParseString("XG2G_TRUSTED_PROXIES", cfg.TrustedProxies)
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
