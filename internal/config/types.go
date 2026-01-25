// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"fmt"
	"time"
)

// RecordingPlaybackConfig holds recording playback configuration
const (
	PlaybackPolicyAuto         = "auto"
	PlaybackPolicyLocalOnly    = "local_only"
	PlaybackPolicyReceiverOnly = "receiver_only"
)

// FileConfig represents the YAML configuration structure
type FileConfig struct {
	Version       string `yaml:"version,omitempty"`
	ConfigVersion string `yaml:"configVersion,omitempty"`
	DataDir       string `yaml:"dataDir,omitempty"`
	LogLevel      string `yaml:"logLevel,omitempty"`

	// Registry-exposed root-level fields (Governance: FileConfig must transport Registry truth)
	ConfigStrict   *bool  `yaml:"configStrict,omitempty"`
	ReadyStrict    *bool  `yaml:"readyStrict,omitempty"`
	LogService     string `yaml:"logService,omitempty"`
	TrustedProxies string `yaml:"trustedProxies,omitempty"`

	OpenWebIF             OpenWebIFConfig         `yaml:"openWebIF"`
	Enigma2               Enigma2Config           `yaml:"enigma2,omitempty"`
	Bouquets              []string                `yaml:"bouquets,omitempty"`
	EPG                   EPGConfig               `yaml:"epg"`
	Recording             map[string]string       `yaml:"recording_roots,omitempty"`
	RecordingPlayback     RecordingPlaybackConfig `yaml:"recording_playback,omitempty"`
	API                   APIConfig               `yaml:"api"`
	Network               NetworkFileConfig       `yaml:"network,omitempty"`
	Metrics               MetricsConfig           `yaml:"metrics,omitempty"`
	Picons                PiconsConfig            `yaml:"picons,omitempty"`
	HDHR                  HDHRConfig              `yaml:"hdhr,omitempty"`
	Engine                EngineConfig            `yaml:"engine,omitempty"`
	TLS                   TLSConfig               `yaml:"tls,omitempty"`
	Library               LibraryConfig           `yaml:"library,omitempty"`
	RecordingPathMappings []RecordingPathMapping  `yaml:"recordingPathMappings,omitempty"`

	// Advanced/internal configuration (Registry-exposed)
	FFmpeg    *FFmpegConfig    `yaml:"ffmpeg,omitempty"`
	HLS       *HLSConfig       `yaml:"hls,omitempty"`
	VOD       *VODConfig       `yaml:"vod,omitempty"`
	RateLimit *RateLimitConfig `yaml:"rateLimit,omitempty"`
	Sessions  *SessionsConfig  `yaml:"sessions,omitempty"`
	Store     *StoreConfig     `yaml:"store,omitempty"`
	Streaming *StreamingConfig `yaml:"streaming,omitempty"`
}

// TLSConfig holds TLS settings
type TLSConfig struct {
	Enabled    *bool  `yaml:"enabled,omitempty"`
	Cert       string `yaml:"cert,omitempty"`
	Key        string `yaml:"key,omitempty"`
	ForceHTTPS *bool  `yaml:"forceHTTPS,omitempty"`
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
	// FFmpeg stream probing settings (for optional middleware compatibility)
	AnalyzeDuration string `yaml:"analyzeDuration,omitempty"` // e.g. "10s", default: "2s"
	ProbeSize       string `yaml:"probeSize,omitempty"`       // e.g. "10M", default: "10M"
	// Fallback to direct 8001 when StreamRelay (17999) fails preflight.
	FallbackTo8001   *bool  `yaml:"fallbackTo8001,omitempty"`
	PreflightTimeout string `yaml:"preflightTimeout,omitempty"` // e.g. "2s", default: "2s"
	// Registry-exposed fields (FileConfig mapping)
	AuthMode    string `yaml:"authMode,omitempty"`    // "inherit", "basic", "digest"
	TuneTimeout string `yaml:"tuneTimeout,omitempty"` // e.g. "10s"
	UseWebIF    *bool  `yaml:"useWebIFStreams,omitempty"`
	StreamPort  *int   `yaml:"streamPort,omitempty"`
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

// NetworkFileConfig holds network policy configuration.
type NetworkFileConfig struct {
	Outbound OutboundFileConfig `yaml:"outbound,omitempty"`
}

// OutboundFileConfig controls outbound HTTP(S) access.
type OutboundFileConfig struct {
	Enabled *bool             `yaml:"enabled,omitempty"`
	Allow   OutboundAllowlist `yaml:"allow,omitempty"`
}

// OutboundAllowlist defines outbound network allowlist rules.
type OutboundAllowlist struct {
	Hosts   []string `yaml:"hosts,omitempty"`
	CIDRs   []string `yaml:"cidrs,omitempty"`
	Ports   []int    `yaml:"ports,omitempty"`
	Schemes []string `yaml:"schemes,omitempty"`
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

// StreamingConfig defines streaming delivery policy (ADR-00X: universal default)
type StreamingConfig struct {
	DeliveryPolicy string `yaml:"delivery_policy" env:"XG2G_STREAMING_POLICY"`
}

// SessionsConfig holds session lease settings (ADR-009)
type SessionsConfig struct {
	LeaseTTL            time.Duration `yaml:"lease_ttl"`
	HeartbeatInterval   time.Duration `yaml:"heartbeat_interval"`
	ExpiryCheckInterval time.Duration `yaml:"expiry_check_interval"`
}

// AppConfig holds all configuration for the application
type AppConfig struct {
	Version           string
	ConfigVersion     string
	ConfigStrict      bool
	DataDir           string
	LogLevel          string
	LogService        string
	Bouquet           string // Comma-separated list of bouquets (empty = all)
	XMLTVPath         string
	PiconBase         string
	FuzzyMax          int
	APIToken          string // Optional: for securing API endpoints (e.g., /api/v3/*)
	APITokenScopes    []string
	APITokens         []ScopedToken
	apiTokensParseErr error
	APIListenAddr     string // Optional: API listen address (if set via config.yaml)
	TrustedProxies    string // Comma-separated list of trusted CIDRs
	MetricsEnabled    bool   // Optional: enable Prometheus metrics server
	MetricsAddr       string // Optional: metrics listen address (if enabled)

	// EPG Configuration
	EPGEnabled         bool
	EPGDays            int           // Number of days to fetch EPG data (1-14)
	EPGMaxConcurrency  int           // Max parallel EPG requests (1-10)
	EPGTimeoutMS       int           // Timeout per EPG request in milliseconds
	EPGRetries         int           // Retry attempts for EPG requests
	EPGSource          string        // EPG fetch strategy: "bouquet" (fast, single request) or "per-service" (default, per-channel requests)
	EPGRefreshInterval time.Duration // Interval for background EPG refreshes

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
	Network            NetworkConfig

	RecordingRoots map[string]string // ID -> Absolute Path (e.g. "hdd" -> "/media/hdd/movie")

	// Recording Playback Configuration
	RecordingPlaybackPolicy string                 // "auto" (default), "local_only", "receiver_only"
	RecordingStableWindow   time.Duration          // File stability check duration (default: 2s)
	RecordingPathMappings   []RecordingPathMapping // Receiver→Local path mappings

	// VOD Optimization (Legacy flat fields - kept for backwards compatibility)
	VODProbeSize       string        // ffmpeg probesize (e.g. "50M")
	VODAnalyzeDuration string        // ffmpeg analyzeduration (e.g. "50M")
	VODStallTimeout    time.Duration // Supervisor stall timeout
	VODMaxConcurrent   int           `json:"vodMaxConcurrentBuilds,omitempty"`
	VODCacheTTL        time.Duration `json:"vodCacheTTL,omitempty"`
	VODCacheMaxEntries int           `json:"vodCacheMaxEntries,omitempty"`

	// VOD (Typed config - source of truth for YAML/Registry)
	VOD VODConfig

	// HDHomeRun Configuration
	HDHR HDHRConfig

	// Streaming Configuration
	Streaming StreamingConfig

	// ADR-009: Session Lease Configuration
	Sessions SessionsConfig

	// Library Configuration (Phase 0 per ADR-ENG-002)
	Library LibraryConfig
}

// LibraryConfig holds library configuration
type LibraryConfig struct {
	Enabled bool                `yaml:"enabled"`
	DBPath  string              `yaml:"db_path"`
	Roots   []LibraryRootConfig `yaml:"roots"`
}

// LibraryRootConfig defines a library root
type LibraryRootConfig struct {
	ID         string   `yaml:"id"`
	Path       string   `yaml:"path"`
	Type       string   `yaml:"type"` // smb|nfs|local
	MaxDepth   int      `yaml:"max_depth"`
	IncludeExt []string `yaml:"include_ext"`
}

type EngineConfig struct {
	Enabled           bool          `yaml:"enabled"`
	Mode              string        `yaml:"mode"` // "standard" or "virtual"
	IdleTimeout       time.Duration `yaml:"idleTimeout"`
	TunerSlots        []int         `yaml:"tunerSlots"`
	MaxPool           int           `yaml:"maxPool"`
	GPULimit          int           `yaml:"gpuLimit"`
	CPUThresholdScale float64       `yaml:"cpuThresholdScale"`
}

// StoreConfig holds the state store settings
type StoreConfig struct {
	Backend string `yaml:"backend"` // "memory" or "sqlite" (per ADR-021: bolt/badger removed)
	Path    string `yaml:"path"`
}

// NetworkConfig holds outbound network policy.
type NetworkConfig struct {
	Outbound OutboundConfig
}

// OutboundConfig controls outbound HTTP(S) allowlist enforcement.
type OutboundConfig struct {
	Enabled bool
	Allow   OutboundAllowlist
}

// FFmpegConfig holds the FFmpeg binary settings
type FFmpegConfig struct {
	Bin         string        `yaml:"bin"`
	KillTimeout time.Duration `yaml:"killTimeout"`
}

// HLSConfig holds HLS output settings
type HLSConfig struct {
	Root           string        `yaml:"root"`
	DVRWindow      time.Duration `yaml:"dvrWindow"`
	SegmentSeconds int           `yaml:"segmentSeconds"` // Best Practice 2026: 6s
}

// VODConfig groups all VOD-specific tuning knobs under `vod.*` for YAML,
// schema generation, and registry-driven docs.
// Keep this minimal and 1:1 with the existing flat AppConfig VOD* fields.
type VODConfig struct {
	// ProbeSize controls how many bytes are probed for container/codec detection.
	// Example: "50M"
	ProbeSize string `yaml:"probeSize" json:"probeSize"`

	// AnalyzeDuration controls how long ffprobe analyzes the stream.
	// Example: "50000000" (microseconds)
	AnalyzeDuration string `yaml:"analyzeDuration" json:"analyzeDuration"`

	// StallTimeout is the timeout for detecting a stalled VOD pipeline.
	// Example: "1m"
	StallTimeout string `yaml:"stallTimeout" json:"stallTimeout"`

	// MaxConcurrent limits concurrent VOD probes/transcodes.
	MaxConcurrent int `yaml:"maxConcurrent" json:"maxConcurrent"`

	// CacheTTL controls how long VOD-derived artifacts are cached.
	// Example: "24h"
	CacheTTL string `yaml:"cacheTTL" json:"cacheTTL"`

	// CacheMaxEntries bounds the number of cached recording artifacts.
	CacheMaxEntries int `yaml:"cacheMaxEntries" json:"cacheMaxEntries"`
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
	StreamPort            int
	UseWebIFStreams       bool
	FallbackTo8001        bool
	PreflightTimeout      time.Duration
}

type scopedTokenJSON struct {
	Token  string   `json:"token"`
	Scopes []string `json:"scopes"`
}

// String implements fmt.Stringer to provide a redacted string representation of the config.
// This ensures that sensitive fields are not leaked in logs when printing the config struct.
func (c AppConfig) String() string {
	masked := MaskSecrets(c)
	// Use json for cleaner output, or simple struct dump
	// Since MaskSecrets returns map[string]any for structs, we can just print that
	return fmt.Sprintf("%+v", masked)
}
