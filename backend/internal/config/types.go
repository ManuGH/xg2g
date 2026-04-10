// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"sort"
	"strings"
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
	Server                *ServerFileConfig       `yaml:"server,omitempty"`
	Network               NetworkFileConfig       `yaml:"network,omitempty"`
	Connectivity          *ConnectivityFileConfig `yaml:"connectivity,omitempty"`
	Metrics               MetricsConfig           `yaml:"metrics,omitempty"`
	Picons                PiconsConfig            `yaml:"picons,omitempty"`
	HDHR                  HDHRConfig              `yaml:"hdhr,omitempty"`
	Engine                EngineFileConfig        `yaml:"engine,omitempty"`
	TLS                   TLSConfig               `yaml:"tls,omitempty"`
	Library               LibraryFileConfig       `yaml:"library,omitempty"`
	RecordingPathMappings []RecordingPathMapping  `yaml:"recordingPathMappings,omitempty"`

	// Advanced/internal configuration (Registry-exposed)
	FFmpeg       *FFmpegConfig           `yaml:"ffmpeg,omitempty"`
	HLS          *HLSConfig              `yaml:"hls,omitempty"`
	VOD          *VODConfig              `yaml:"vod,omitempty"`
	RateLimit    *RateLimitConfig        `yaml:"rateLimit,omitempty"`
	Sessions     *SessionsConfig         `yaml:"sessions,omitempty"`
	Store        *StoreConfig            `yaml:"store,omitempty"`
	Streaming    *StreamingConfig        `yaml:"streaming,omitempty"`
	Playback     *PlaybackFileConfig     `yaml:"playback,omitempty"`
	Monetization *MonetizationFileConfig `yaml:"monetization,omitempty"`
	Household    *HouseholdFileConfig    `yaml:"household,omitempty"`
	Limits       *LimitsConfig           `yaml:"limits,omitempty"`
	Timeouts     *TimeoutsConfig         `yaml:"timeouts,omitempty"`
	Breaker      *BreakerConfig          `yaml:"breaker,omitempty"`
	Verification *VerificationConfig     `yaml:"verification,omitempty"`
}

// VerificationConfig holds drift verification settings
type VerificationConfig struct {
	Enabled  *bool  `yaml:"enabled,omitempty"`
	Interval string `yaml:"interval,omitempty"` // e.g. "60s"
}

// TLSConfig holds TLS settings
type TLSConfig struct {
	Enabled    *bool  `yaml:"enabled,omitempty"`
	Cert       string `yaml:"cert,omitempty"`
	Key        string `yaml:"key,omitempty"`
	ForceHTTPS *bool  `yaml:"forceHTTPS,omitempty"`
}

// OpenWebIFConfig is retained only so the loader can emit a targeted migration
// error for legacy YAML files that still use openWebIF.*.
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
	Token                          string          `yaml:"token,omitempty"`
	TokenScopes                    []string        `yaml:"tokenScopes,omitempty"`
	Tokens                         []ScopedToken   `yaml:"tokens,omitempty"`
	DisableLegacyTokenSources      *bool           `yaml:"disableLegacyTokenSources,omitempty"`
	PlaybackDecisionSecret         string          `yaml:"playbackDecisionSecret,omitempty"`
	PlaybackDecisionKeyID          string          `yaml:"playbackDecisionKeyId,omitempty"`
	PlaybackDecisionPreviousKeys   []string        `yaml:"playbackDecisionPreviousKeys,omitempty"`
	PlaybackDecisionRotationWindow string          `yaml:"playbackDecisionRotationWindow,omitempty"`
	ListenAddr                     string          `yaml:"listenAddr,omitempty"`
	RateLimit                      RateLimitConfig `yaml:"rateLimit,omitempty"`
	AllowedOrigins                 []string        `yaml:"allowedOrigins,omitempty"`
}

// ServerFileConfig holds HTTP server runtime settings in YAML.
// Pointer fields preserve zero-values as explicit operator intent.
type ServerFileConfig struct {
	ReadTimeout     *time.Duration `yaml:"readTimeout,omitempty"`
	WriteTimeout    *time.Duration `yaml:"writeTimeout,omitempty"`
	IdleTimeout     *time.Duration `yaml:"idleTimeout,omitempty"`
	MaxHeaderBytes  *int           `yaml:"maxHeaderBytes,omitempty"`
	ShutdownTimeout *time.Duration `yaml:"shutdownTimeout,omitempty"`
}

// ServerRuntimeConfig holds HTTP server runtime settings in AppConfig.
type ServerRuntimeConfig struct {
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	MaxHeaderBytes  int
	ShutdownTimeout time.Duration
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
	LAN      LANFileConfig      `yaml:"lan,omitempty"`
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

// LANFileConfig defines LAN access policy.
type LANFileConfig struct {
	Allow LANAllowlist `yaml:"allow,omitempty"`
}

// LANAllowlist defines LAN CIDR allowlist rules.
type LANAllowlist struct {
	CIDRs []string `yaml:"cidrs,omitempty"`
}

// ConnectivityFileConfig defines operator-published endpoint truth for native
// and browser clients in YAML/env-backed config surfaces.
type ConnectivityFileConfig struct {
	Profile            string                    `yaml:"profile,omitempty" json:"profile,omitempty"`
	AllowLocalHTTP     *bool                     `yaml:"allowLocalHTTP,omitempty" json:"allowLocalHTTP,omitempty"`
	PublishedEndpoints []PublishedEndpointConfig `yaml:"publishedEndpoints,omitempty" json:"publishedEndpoints,omitempty"`
}

// ConnectivityConfig holds runtime-published endpoint truth.
type ConnectivityConfig struct {
	Profile            string                    `yaml:"profile" json:"profile"`
	AllowLocalHTTP     bool                      `yaml:"allowLocalHTTP" json:"allowLocalHTTP"`
	PublishedEndpoints []PublishedEndpointConfig `yaml:"publishedEndpoints" json:"publishedEndpoints"`
}

// PublishedEndpointConfig is the config/runtime representation of one
// operator-published connectivity candidate. The domain package performs the
// final normalization and policy validation.
type PublishedEndpointConfig struct {
	URL             string `yaml:"url,omitempty" json:"url"`
	Kind            string `yaml:"kind,omitempty" json:"kind"`
	Priority        int    `yaml:"priority,omitempty" json:"priority"`
	TLSMode         string `yaml:"tlsMode,omitempty" json:"tlsMode"`
	AllowPairing    bool   `yaml:"allowPairing,omitempty" json:"allowPairing"`
	AllowStreaming  bool   `yaml:"allowStreaming,omitempty" json:"allowStreaming"`
	AllowWeb        bool   `yaml:"allowWeb,omitempty" json:"allowWeb"`
	AllowNative     bool   `yaml:"allowNative,omitempty" json:"allowNative"`
	AdvertiseReason string `yaml:"advertiseReason,omitempty" json:"advertiseReason"`
	Source          string `yaml:"source,omitempty" json:"source"`
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

const (
	MonetizationModelFree           = "free"
	MonetizationModelOneTimeUnlock  = "one_time_unlock"
	MonetizationEnforcementNone     = "none"
	MonetizationEnforcementRequired = "required"
)

// MonetizationFileConfig holds operator-configured commercialization settings in YAML.
type MonetizationFileConfig struct {
	Enabled         *bool                                  `yaml:"enabled,omitempty"`
	Model           string                                 `yaml:"model,omitempty"`
	ProductName     string                                 `yaml:"productName,omitempty"`
	RequiredScopes  []string                               `yaml:"requiredScopes,omitempty"`
	PurchaseURL     string                                 `yaml:"purchaseUrl,omitempty"`
	Enforcement     string                                 `yaml:"enforcement,omitempty"`
	GooglePlay      *MonetizationGooglePlayFileConfig      `yaml:"googlePlay,omitempty"`
	Amazon          *MonetizationAmazonFileConfig          `yaml:"amazon,omitempty"`
	ProductMappings []MonetizationProductMappingFileConfig `yaml:"productMappings,omitempty"`
}

type MonetizationGooglePlayFileConfig struct {
	PackageName                   string `yaml:"packageName,omitempty"`
	ServiceAccountCredentialsFile string `yaml:"serviceAccountCredentialsFile,omitempty"`
}

type MonetizationAmazonFileConfig struct {
	SharedSecretFile string `yaml:"sharedSecretFile,omitempty"`
	UseSandbox       *bool  `yaml:"useSandbox,omitempty"`
}

type MonetizationProductMappingFileConfig struct {
	Provider  string   `yaml:"provider,omitempty"`
	ProductID string   `yaml:"productId,omitempty"`
	Scopes    []string `yaml:"scopes,omitempty"`
}

type HouseholdFileConfig struct {
	Pin       string `yaml:"pin,omitempty"`
	PinHash   string `yaml:"pinHash,omitempty"`
	UnlockTTL string `yaml:"unlockTTL,omitempty"`
}

// MonetizationConfig holds runtime commercialization settings.
type MonetizationConfig struct {
	Enabled         bool
	Model           string
	ProductName     string
	RequiredScopes  []string
	PurchaseURL     string
	Enforcement     string
	GooglePlay      MonetizationGooglePlayConfig
	Amazon          MonetizationAmazonConfig
	ProductMappings []MonetizationProductMapping
}

type MonetizationGooglePlayConfig struct {
	PackageName                   string
	ServiceAccountCredentialsFile string
}

type MonetizationAmazonConfig struct {
	SharedSecretFile string
	UseSandbox       bool
}

type MonetizationProductMapping struct {
	Provider  string
	ProductID string
	Scopes    []string
}

type HouseholdConfig struct {
	PinHash   string
	UnlockTTL time.Duration
}

func (c HouseholdConfig) PinConfigured() bool {
	return strings.TrimSpace(c.PinHash) != ""
}

// Normalized returns a canonicalized monetization config with defaults applied.
func (c MonetizationConfig) Normalized() MonetizationConfig {
	out := c
	out.Model = strings.ToLower(strings.TrimSpace(out.Model))
	if out.Model == "" {
		out.Model = MonetizationModelFree
	}
	out.ProductName = strings.TrimSpace(out.ProductName)
	if out.ProductName == "" {
		out.ProductName = "xg2g Unlock"
	}
	if len(out.RequiredScopes) > 0 {
		normalizedScopes := make([]string, len(out.RequiredScopes))
		for i, scope := range out.RequiredScopes {
			normalizedScopes[i] = strings.ToLower(strings.TrimSpace(scope))
		}
		sort.Strings(normalizedScopes)
		out.RequiredScopes = normalizedScopes
	}
	out.PurchaseURL = strings.TrimSpace(out.PurchaseURL)
	out.Enforcement = strings.ToLower(strings.TrimSpace(out.Enforcement))
	if out.Enforcement == "" {
		out.Enforcement = MonetizationEnforcementNone
	}
	out.GooglePlay = MonetizationGooglePlayConfig{
		PackageName:                   strings.TrimSpace(out.GooglePlay.PackageName),
		ServiceAccountCredentialsFile: strings.TrimSpace(out.GooglePlay.ServiceAccountCredentialsFile),
	}
	out.Amazon = MonetizationAmazonConfig{
		SharedSecretFile: strings.TrimSpace(out.Amazon.SharedSecretFile),
		UseSandbox:       out.Amazon.UseSandbox,
	}
	if len(out.ProductMappings) > 0 {
		normalizedMappings := make([]MonetizationProductMapping, len(out.ProductMappings))
		for i, mapping := range out.ProductMappings {
			normalizedMappings[i] = MonetizationProductMapping{
				Provider:  strings.ToLower(strings.TrimSpace(mapping.Provider)),
				ProductID: strings.TrimSpace(mapping.ProductID),
				Scopes:    append([]string(nil), mapping.Scopes...),
			}
			for j, scope := range normalizedMappings[i].Scopes {
				normalizedMappings[i].Scopes[j] = strings.ToLower(strings.TrimSpace(scope))
			}
			sort.Strings(normalizedMappings[i].Scopes)
		}
		sort.Slice(normalizedMappings, func(i, j int) bool {
			if normalizedMappings[i].Provider != normalizedMappings[j].Provider {
				return normalizedMappings[i].Provider < normalizedMappings[j].Provider
			}
			return normalizedMappings[i].ProductID < normalizedMappings[j].ProductID
		})
		out.ProductMappings = normalizedMappings
	}
	return out
}

// RequiresUnlock reports whether the config describes a paid one-time unlock.
func (c MonetizationConfig) RequiresUnlock() bool {
	normalized := c.Normalized()
	return normalized.Enabled && normalized.Model == MonetizationModelOneTimeUnlock
}

// PlaybackOperatorFileConfig holds operator-side playback overrides in YAML.
type PlaybackOperatorFileConfig struct {
	ForceIntent           string                           `yaml:"force_intent,omitempty"`
	MaxQualityRung        string                           `yaml:"max_quality_rung,omitempty"`
	DisableClientFallback *bool                            `yaml:"disable_client_fallback,omitempty"`
	SourceRules           []PlaybackOperatorRuleFileConfig `yaml:"source_rules,omitempty"`
}

// PlaybackOperatorRuleFileConfig defines an ordered per-source playback override rule in YAML.
type PlaybackOperatorRuleFileConfig struct {
	Name                  string `yaml:"name,omitempty"`
	Mode                  string `yaml:"mode,omitempty"` // "live", "recording", "any"
	ServiceRef            string `yaml:"service_ref,omitempty"`
	ServiceRefPrefix      string `yaml:"service_ref_prefix,omitempty"`
	ForceIntent           string `yaml:"force_intent,omitempty"`
	MaxQualityRung        string `yaml:"max_quality_rung,omitempty"`
	DisableClientFallback *bool  `yaml:"disable_client_fallback,omitempty"`
}

// PlaybackFileConfig holds playback runtime policy in YAML.
type PlaybackFileConfig struct {
	Operator PlaybackOperatorFileConfig `yaml:"operator,omitempty"`
}

// PlaybackOperatorConfig holds runtime operator-side playback overrides.
type PlaybackOperatorConfig struct {
	ForceIntent           string                       `yaml:"force_intent"`
	MaxQualityRung        string                       `yaml:"max_quality_rung"`
	DisableClientFallback bool                         `yaml:"disable_client_fallback"`
	SourceRules           []PlaybackOperatorRuleConfig `yaml:"source_rules"`
}

// PlaybackOperatorRuleConfig defines an ordered per-source playback override rule at runtime.
type PlaybackOperatorRuleConfig struct {
	Name                  string `yaml:"name"`
	Mode                  string `yaml:"mode"`
	ServiceRef            string `yaml:"service_ref"`
	ServiceRefPrefix      string `yaml:"service_ref_prefix"`
	ForceIntent           string `yaml:"force_intent"`
	MaxQualityRung        string `yaml:"max_quality_rung"`
	DisableClientFallback *bool  `yaml:"disable_client_fallback,omitempty"`
}

// PlaybackConfig holds runtime playback policy settings.
type PlaybackConfig struct {
	Operator PlaybackOperatorConfig `yaml:"operator"`
}

// SessionsConfig holds session lease settings (ADR-009)
type SessionsConfig struct {
	LeaseTTL            time.Duration `yaml:"lease_ttl"`
	HeartbeatInterval   time.Duration `yaml:"heartbeat_interval"`
	ExpiryCheckInterval time.Duration `yaml:"expiry_check_interval"`
}

// LimitsConfig holds admission limits
type LimitsConfig struct {
	MaxSessions   int `yaml:"max_sessions"`
	MaxTranscodes int `yaml:"max_transcodes"`
}

// TimeoutsConfig holds execution timeouts
type TimeoutsConfig struct {
	TranscodeStart      time.Duration `yaml:"transcode_start"`
	TranscodeNoProgress time.Duration `yaml:"transcode_no_progress"`
	KillGrace           time.Duration `yaml:"kill_grace"`
}

// BreakerConfig holds circuit breaker settings
type BreakerConfig struct {
	Window               time.Duration `yaml:"window"`
	MinAttempts          int           `yaml:"min_attempts"`
	FailuresThreshold    int           `yaml:"failures_threshold"`
	ConsecutiveThreshold int           `yaml:"consecutive_threshold"`
}

// AppConfig holds all configuration for the application
type AppConfig struct {
	Version                        string
	ConfigVersion                  string
	ConfigStrict                   bool
	DataDir                        string
	LogLevel                       string
	LogService                     string
	Bouquet                        string // Comma-separated list of bouquets (empty = all)
	XMLTVPath                      string
	PiconBase                      string
	FuzzyMax                       int
	APIToken                       string // Optional: for securing API endpoints (e.g., /api/v3/*)
	APITokenScopes                 []string
	APITokens                      []ScopedToken
	APIDisableLegacyTokenSources   bool
	PlaybackDecisionSecret         string
	PlaybackDecisionKeyID          string
	PlaybackDecisionPreviousKeys   []string
	PlaybackDecisionRotationWindow time.Duration
	apiTokensParseErr              error
	APIListenAddr                  string // Optional: API listen address (if set via config.yaml)
	Server                         ServerRuntimeConfig
	TrustedProxies                 string // Comma-separated list of trusted CIDRs
	MetricsEnabled                 bool   // Optional: enable Prometheus metrics server
	MetricsAddr                    string // Optional: metrics listen address (if enabled)

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
	RateLimitEnabled     bool // Enable rate limiting
	RateLimitGlobal      int  // Requests per second (global)
	RateLimitAuth        int  // Requests per minute (auth)
	RateLimitBurst       int
	RateLimitWhitelist   []string
	AllowedOrigins       []string
	Network              NetworkConfig
	Connectivity         ConnectivityConfig
	connectivityParseErr error

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
	Streaming    StreamingConfig
	Playback     PlaybackConfig
	Monetization MonetizationConfig
	Household    HouseholdConfig

	// ADR-009: Session Lease Configuration
	Sessions SessionsConfig

	// Sprint 1: Resilience Core
	Limits   LimitsConfig
	Timeouts TimeoutsConfig
	Breaker  BreakerConfig

	// Library Configuration (Phase 0 per ADR-ENG-002)
	Library LibraryConfig

	// Verification (Drift Detection)
	Verification VerificationSettings
}

// VerificationSettings holds runtime verification settings
type VerificationSettings struct {
	Enabled  bool
	Interval time.Duration
}

// LibraryFileConfig holds library configuration for FileConfig (YAML)
type LibraryFileConfig struct {
	Enabled *bool               `yaml:"enabled,omitempty"`
	DBPath  string              `yaml:"db_path,omitempty"`
	Roots   []LibraryRootConfig `yaml:"roots,omitempty"`
}

// LibraryConfig holds library configuration (Runtime)
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

// EngineFileConfig holds Engine configuration for FileConfig (YAML)
type EngineFileConfig struct {
	Enabled           *bool         `yaml:"enabled,omitempty"`
	Mode              string        `yaml:"mode,omitempty"`
	IdleTimeout       time.Duration `yaml:"idleTimeout,omitempty"`
	TunerSlots        []int         `yaml:"tunerSlots,omitempty"`
	MaxPool           int           `yaml:"maxPool,omitempty"`
	GPULimit          int           `yaml:"gpuLimit,omitempty"`
	CPUThresholdScale float64       `yaml:"cpuThresholdScale,omitempty"`
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

// FFmpegConfig holds the FFmpeg binary settings
type FFmpegConfig struct {
	Bin         string        `yaml:"bin"`
	FFprobeBin  string        `yaml:"ffprobeBin,omitempty"`
	KillTimeout time.Duration `yaml:"killTimeout"`
	// VaapiDevice is the DRM render node for VAAPI GPU transcoding.
	// Explicit values (file or XG2G_VAAPI_DEVICE env) stay authoritative.
	// When left unset, xg2g auto-detects the first visible /dev/dri/renderD*
	// node at startup so containerized GPU/iGPU deployments work by default
	// once the render device is mounted. An explicitly empty XG2G_VAAPI_DEVICE
	// disables that auto-detect path for CPU-only operation.
	// Any selected device still runs a real encode preflight at startup.
	VaapiDevice string `yaml:"vaapiDevice,omitempty"`
}

// HLSConfig holds HLS output settings
type HLSConfig struct {
	Root           string        `yaml:"root"`
	DVRWindow      time.Duration `yaml:"dvrWindow"`
	SegmentSeconds int           `yaml:"segmentSeconds"` // Best Practice 2026: 6s
	ReadySegments  int           `yaml:"readySegments"`
}

// StoreConfig holds the state store settings
type StoreConfig struct {
	Backend string `yaml:"backend"` // "memory" or "sqlite" (per ADR-021: bolt/badger removed)
	Path    string `yaml:"path"`
}

// NetworkConfig holds outbound network policy.
type NetworkConfig struct {
	Outbound OutboundConfig
	LAN      LANConfig
}

// OutboundConfig controls outbound HTTP(S) allowlist enforcement.
type OutboundConfig struct {
	Enabled bool
	Allow   OutboundAllowlist
}

// LANConfig defines runtime LAN access policy.
type LANConfig struct {
	Allow LANAllowlist
}
