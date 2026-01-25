// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package config

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"
)

// Profile defines the operator persona for a configuration option.
type Profile string

const (
	ProfileSimple     Profile = "Simple"
	ProfileAdvanced   Profile = "Advanced"
	ProfileIntegrator Profile = "Integrator"
	ProfileInternal   Profile = "Internal"
)

// Status defines the lifecycle state of a configuration option.
type Status string

const (
	StatusActive     Status = "Active"
	StatusDeprecated Status = "Deprecated"
	StatusCandidate  Status = "Candidate" // Zombie candidate
	StatusInternal   Status = "Internal"
)

// ConfigEntry defines a single configuration option's metadata.
type ConfigEntry struct {
	Path      string  // User-facing Path (e.g. "api.listenAddr")
	Env       string  // Environment Variable (e.g. "XG2G_LISTEN")
	FieldPath string  // Internal Field Path (e.g. "APIListenAddr")
	Profile   Profile // Operator Profile
	Status    Status  // Lifecycle Status
	Default   any     // Default value
}

// Registry manages the configuration surface inventory.
type Registry struct {
	ByPath  map[string]ConfigEntry
	ByField map[string]ConfigEntry
	ByEnv   map[string]ConfigEntry
}

var (
	globalRegistry    *Registry
	globalRegistryErr error
	registryOnce      sync.Once
)

// GetRegistry returns the global configuration registry.
// It returns an error if the registry contains duplicates or is otherwise invalid.
// Thread-safe via sync.Once.
func GetRegistry() (*Registry, error) {
	registryOnce.Do(func() {
		globalRegistry, globalRegistryErr = buildRegistry()
	})
	return globalRegistry, globalRegistryErr
}

func buildRegistry() (*Registry, error) {
	r := &Registry{
		ByPath:  make(map[string]ConfigEntry),
		ByField: make(map[string]ConfigEntry),
		ByEnv:   make(map[string]ConfigEntry),
	}

	entries := []ConfigEntry{
		// --- CORE ---
		{Path: "version", Env: "XG2G_VERSION", FieldPath: "Version", Profile: ProfileSimple, Status: StatusActive},
		{Path: "configVersion", Env: "", FieldPath: "ConfigVersion", Profile: ProfileInternal, Status: StatusInternal, Default: "v3"},
		{Path: "configStrict", Env: "XG2G_CONFIG_STRICT", FieldPath: "ConfigStrict", Profile: ProfileAdvanced, Status: StatusActive, Default: true},
		{Path: "dataDir", Env: "XG2G_DATA", FieldPath: "DataDir", Profile: ProfileSimple, Status: StatusActive, Default: "/tmp"},
		{Path: "logLevel", Env: "XG2G_LOG_LEVEL", FieldPath: "LogLevel", Profile: ProfileSimple, Status: StatusActive, Default: "info"},
		{Path: "logService", Env: "XG2G_LOG_SERVICE", FieldPath: "LogService", Profile: ProfileAdvanced, Status: StatusActive},
		{Path: "bouquets", Env: "XG2G_BOUQUET", FieldPath: "Bouquet", Profile: ProfileSimple, Status: StatusActive},

		// --- ENIGMA2 / OPENWEBIF ---
		{Path: "enigma2.baseUrl", Env: "XG2G_OWI_BASE", FieldPath: "Enigma2.BaseURL", Profile: ProfileSimple, Status: StatusActive},
		{Path: "enigma2.username", Env: "XG2G_OWI_USER", FieldPath: "Enigma2.Username", Profile: ProfileSimple, Status: StatusActive},
		{Path: "enigma2.password", Env: "XG2G_OWI_PASS", FieldPath: "Enigma2.Password", Profile: ProfileSimple, Status: StatusActive},
		{Path: "enigma2.timeout", Env: "XG2G_OWI_TIMEOUT_MS", FieldPath: "Enigma2.Timeout", Profile: ProfileAdvanced, Status: StatusActive, Default: 10 * time.Second},
		{Path: "enigma2.responseHeaderTimeout", Env: "", FieldPath: "Enigma2.ResponseHeaderTimeout", Profile: ProfileAdvanced, Status: StatusActive, Default: 10 * time.Second},
		{Path: "enigma2.tuneTimeout", Env: "", FieldPath: "Enigma2.TuneTimeout", Profile: ProfileAdvanced, Status: StatusActive, Default: 10 * time.Second},
		{Path: "enigma2.retries", Env: "XG2G_OWI_RETRIES", FieldPath: "Enigma2.Retries", Profile: ProfileAdvanced, Status: StatusActive, Default: 2},
		{Path: "enigma2.backoff", Env: "XG2G_OWI_BACKOFF_MS", FieldPath: "Enigma2.Backoff", Profile: ProfileAdvanced, Status: StatusActive, Default: 200 * time.Millisecond},
		{Path: "enigma2.maxBackoff", Env: "XG2G_OWI_MAX_BACKOFF_MS", FieldPath: "Enigma2.MaxBackoff", Profile: ProfileAdvanced, Status: StatusActive, Default: 30 * time.Second},
		{Path: "enigma2.streamPort", Env: "XG2G_STREAM_PORT", FieldPath: "Enigma2.StreamPort", Profile: ProfileAdvanced, Status: StatusDeprecated, Default: 8001},
		{Path: "enigma2.useWebIFStreams", Env: "XG2G_USE_WEBIF_STREAMS", FieldPath: "Enigma2.UseWebIFStreams", Profile: ProfileAdvanced, Status: StatusActive, Default: true},
		{Path: "enigma2.fallbackTo8001", Env: "XG2G_E2_FALLBACK_TO_8001", FieldPath: "Enigma2.FallbackTo8001", Profile: ProfileIntegrator, Status: StatusActive, Default: false},
		{Path: "enigma2.authMode", Env: "", FieldPath: "Enigma2.AuthMode", Profile: ProfileAdvanced, Status: StatusActive, Default: "inherit"},
		{Path: "enigma2.rateLimit", Env: "", FieldPath: "Enigma2.RateLimit", Profile: ProfileAdvanced, Status: StatusActive},
		{Path: "enigma2.rateBurst", Env: "", FieldPath: "Enigma2.RateBurst", Profile: ProfileAdvanced, Status: StatusActive},
		{Path: "enigma2.userAgent", Env: "", FieldPath: "Enigma2.UserAgent", Profile: ProfileAdvanced, Status: StatusActive},
		{Path: "enigma2.analyzeDuration", Env: "", FieldPath: "Enigma2.AnalyzeDuration", Profile: ProfileAdvanced, Status: StatusActive, Default: "10000000"},
		{Path: "enigma2.probeSize", Env: "", FieldPath: "Enigma2.ProbeSize", Profile: ProfileAdvanced, Status: StatusActive, Default: "32M"},
		{Path: "enigma2.preflightTimeout", Env: "XG2G_E2_PREFLIGHT_TIMEOUT", FieldPath: "Enigma2.PreflightTimeout", Profile: ProfileAdvanced, Status: StatusActive, Default: 10 * time.Second},
		// --- API ---
		{Path: "api.listenAddr", Env: "XG2G_LISTEN", FieldPath: "APIListenAddr", Profile: ProfileSimple, Status: StatusActive, Default: ":8088"},
		{Path: "api.token", Env: "XG2G_API_TOKEN", FieldPath: "APIToken", Profile: ProfileSimple, Status: StatusActive},
		{Path: "api.tokenScopes", Env: "XG2G_API_TOKEN_SCOPES", FieldPath: "APITokenScopes", Profile: ProfileAdvanced, Status: StatusActive},
		{Path: "api.tokens", Env: "XG2G_API_TOKENS", FieldPath: "APITokens", Profile: ProfileAdvanced, Status: StatusActive},
		{Path: "api.allowedOrigins", Env: "XG2G_ALLOWED_ORIGINS", FieldPath: "AllowedOrigins", Profile: ProfileAdvanced, Status: StatusActive},

		// --- EPG ---
		{Path: "epg.enabled", Env: "XG2G_EPG_ENABLED", FieldPath: "EPGEnabled", Profile: ProfileSimple, Status: StatusActive, Default: true},
		{Path: "epg.days", Env: "XG2G_EPG_DAYS", FieldPath: "EPGDays", Profile: ProfileSimple, Status: StatusActive, Default: 14},
		{Path: "epg.maxConcurrency", Env: "XG2G_EPG_MAX_CONCURRENCY", FieldPath: "EPGMaxConcurrency", Profile: ProfileAdvanced, Status: StatusActive, Default: 5},
		{Path: "epg.timeoutMs", Env: "XG2G_EPG_TIMEOUT_MS", FieldPath: "EPGTimeoutMS", Profile: ProfileAdvanced, Status: StatusActive, Default: 5000},
		{Path: "epg.retries", Env: "XG2G_EPG_RETRIES", FieldPath: "EPGRetries", Profile: ProfileAdvanced, Status: StatusActive, Default: 2},
		{Path: "epg.source", Env: "XG2G_EPG_SOURCE", FieldPath: "EPGSource", Profile: ProfileAdvanced, Status: StatusActive, Default: "per-service"},
		{Path: "epg.xmltvPath", Env: "XG2G_XMLTV", FieldPath: "XMLTVPath", Profile: ProfileAdvanced, Status: StatusActive, Default: "xmltv.xml"},
		{Path: "epg.fuzzyMax", Env: "XG2G_FUZZY_MAX", FieldPath: "FuzzyMax", Profile: ProfileAdvanced, Status: StatusActive, Default: 2},
		{FieldPath: "EPGRefreshInterval", Profile: ProfileInternal, Status: StatusInternal, Default: 6 * time.Hour},

		// --- ENGINE ---
		{Path: "engine.enabled", Env: "XG2G_ENGINE_ENABLED", FieldPath: "Engine.Enabled", Profile: ProfileAdvanced, Status: StatusActive, Default: false}, // Fix A: Secure by default
		{Path: "engine.mode", Env: "XG2G_ENGINE_MODE", FieldPath: "Engine.Mode", Profile: ProfileAdvanced, Status: StatusActive, Default: "standard"},
		{Path: "engine.idleTimeout", Env: "XG2G_ENGINE_IDLE_TIMEOUT", FieldPath: "Engine.IdleTimeout", Profile: ProfileAdvanced, Status: StatusActive, Default: 1 * time.Minute},
		{Path: "engine.tunerSlots", Env: "XG2G_TUNER_SLOTS", FieldPath: "Engine.TunerSlots", Profile: ProfileAdvanced, Status: StatusActive},
		{Path: "engine.maxPool", Env: "XG2G_ENGINE_MAX_POOL", FieldPath: "Engine.MaxPool", Profile: ProfileAdvanced, Status: StatusActive, Default: 2},
		{Path: "engine.gpuLimit", Env: "XG2G_ENGINE_GPU_LIMIT", FieldPath: "Engine.GPULimit", Profile: ProfileAdvanced, Status: StatusActive, Default: 8},
		{Path: "engine.cpuThresholdScale", Env: "XG2G_ENGINE_CPU_SCALE", FieldPath: "Engine.CPUThresholdScale", Profile: ProfileAdvanced, Status: StatusActive, Default: 1.5},

		// --- STORE ---
		{Path: "store.backend", Env: "XG2G_STORE_BACKEND", FieldPath: "Store.Backend", Profile: ProfileAdvanced, Status: StatusActive, Default: "memory"}, // Consistent with setDefaults
		{Path: "store.path", Env: "XG2G_STORE_PATH", FieldPath: "Store.Path", Profile: ProfileAdvanced, Status: StatusActive, Default: "/var/lib/xg2g/store"},

		// --- HLS ---
		{Path: "hls.root", Env: "XG2G_HLS_ROOT", FieldPath: "HLS.Root", Profile: ProfileAdvanced, Status: StatusActive},
		{Path: "hls.dvrWindow", Env: "XG2G_HLS_DVR_WINDOW", FieldPath: "HLS.DVRWindow", Profile: ProfileAdvanced, Status: StatusActive, Default: 45 * time.Minute}, // Fix B key
		{Path: "hls.segmentSeconds", Env: "XG2G_HLS_SEGMENT_SECONDS", FieldPath: "HLS.SegmentSeconds", Profile: ProfileAdvanced, Status: StatusActive, Default: 6}, // Best Practice 2026

		// --- FFMPEG ---
		{Path: "ffmpeg.bin", Env: "XG2G_FFMPEG_BIN", FieldPath: "FFmpeg.Bin", Profile: ProfileAdvanced, Status: StatusActive, Default: "ffmpeg"},
		{Path: "ffmpeg.killTimeout", Env: "", FieldPath: "FFmpeg.KillTimeout", Profile: ProfileAdvanced, Status: StatusActive, Default: 5 * time.Second},

		// --- TLS ---
		{Path: "tls.enabled", Env: "XG2G_TLS_ENABLED", FieldPath: "TLSEnabled", Profile: ProfileAdvanced, Status: StatusActive, Default: false},
		{Path: "tls.cert", Env: "XG2G_TLS_CERT", FieldPath: "TLSCert", Profile: ProfileAdvanced, Status: StatusActive},
		{Path: "tls.key", Env: "XG2G_TLS_KEY", FieldPath: "TLSKey", Profile: ProfileAdvanced, Status: StatusActive},
		{Path: "tls.forceHTTPS", Env: "XG2G_FORCE_HTTPS", FieldPath: "ForceHTTPS", Profile: ProfileAdvanced, Status: StatusActive, Default: false},

		// --- SECURITY / PROXIES ---
		{Path: "trustedProxies", Env: "XG2G_TRUSTED_PROXIES", FieldPath: "TrustedProxies", Profile: ProfileAdvanced, Status: StatusActive}, // Fix D: Active now
		{Path: "network.outbound.enabled", Env: "XG2G_OUTBOUND_ENABLED", FieldPath: "Network.Outbound.Enabled", Profile: ProfileAdvanced, Status: StatusActive, Default: false},
		{Path: "network.outbound.allow.hosts", Env: "XG2G_OUTBOUND_ALLOW_HOSTS", FieldPath: "Network.Outbound.Allow.Hosts", Profile: ProfileAdvanced, Status: StatusActive},
		{Path: "network.outbound.allow.cidrs", Env: "XG2G_OUTBOUND_ALLOW_CIDRS", FieldPath: "Network.Outbound.Allow.CIDRs", Profile: ProfileAdvanced, Status: StatusActive},
		{Path: "network.outbound.allow.ports", Env: "XG2G_OUTBOUND_ALLOW_PORTS", FieldPath: "Network.Outbound.Allow.Ports", Profile: ProfileAdvanced, Status: StatusActive},
		{Path: "network.outbound.allow.schemes", Env: "XG2G_OUTBOUND_ALLOW_SCHEMES", FieldPath: "Network.Outbound.Allow.Schemes", Profile: ProfileAdvanced, Status: StatusActive},

		// --- METRICS ---
		{Path: "metrics.enabled", Env: "", FieldPath: "MetricsEnabled", Profile: ProfileAdvanced, Status: StatusActive, Default: false},
		{Path: "metrics.listenAddr", Env: "XG2G_METRICS_LISTEN", FieldPath: "MetricsAddr", Profile: ProfileAdvanced, Status: StatusActive, Default: ""},

		// --- RATE LIMIT ---
		{Path: "rateLimit.enabled", Env: "", FieldPath: "RateLimitEnabled", Profile: ProfileAdvanced, Status: StatusActive, Default: true},
		{Path: "rateLimit.global", Env: "", FieldPath: "RateLimitGlobal", Profile: ProfileAdvanced, Status: StatusActive, Default: 100},
		{Path: "rateLimit.auth", Env: "", FieldPath: "RateLimitAuth", Profile: ProfileAdvanced, Status: StatusActive, Default: 10},
		{Path: "rateLimit.burst", Env: "", FieldPath: "RateLimitBurst", Profile: ProfileAdvanced, Status: StatusActive, Default: 20},
		{Path: "rateLimit.whitelist", Env: "XG2G_RATE_LIMIT_WHITELIST", FieldPath: "RateLimitWhitelist", Profile: ProfileAdvanced, Status: StatusActive},

		// --- PICONS ---
		{Path: "picons.baseUrl", Env: "XG2G_PICON_BASE", FieldPath: "PiconBase", Profile: ProfileSimple, Status: StatusActive},

		// --- STREAMING (ADR-00X) ---
		{Path: "streaming.delivery_policy", Env: "XG2G_STREAMING_POLICY", FieldPath: "Streaming.DeliveryPolicy", Profile: ProfileSimple, Status: StatusActive, Default: "universal"},

		// --- VERIFICATION ---
		{Path: "verification.enabled", Env: "XG2G_VERIFY_ENABLED", FieldPath: "Verification.Enabled", Profile: ProfileAdvanced, Status: StatusActive, Default: true},
		{Path: "verification.interval", Env: "XG2G_VERIFY_INTERVAL", FieldPath: "Verification.Interval", Profile: ProfileAdvanced, Status: StatusActive, Default: 60 * time.Second},

		// --- SESSIONS (ADR-009) ---
		{Path: "sessions.lease_ttl", Env: "", FieldPath: "Sessions.LeaseTTL", Profile: ProfileAdvanced, Status: StatusActive, Default: 2 * time.Hour},
		{Path: "sessions.heartbeat_interval", Env: "", FieldPath: "Sessions.HeartbeatInterval", Profile: ProfileAdvanced, Status: StatusActive, Default: 30 * time.Second},
		{Path: "sessions.expiry_check_interval", Env: "", FieldPath: "Sessions.ExpiryCheckInterval", Profile: ProfileAdvanced, Status: StatusActive, Default: 1 * time.Minute},

		// --- RECORDINGS ---
		{Path: "recording_roots", Env: "", FieldPath: "RecordingRoots", Profile: ProfileAdvanced, Status: StatusActive},
		{Path: "recording_playback.playback_policy", Env: "", FieldPath: "RecordingPlaybackPolicy", Profile: ProfileAdvanced, Status: StatusActive, Default: "auto"},
		{Path: "recording_playback.stable_window", Env: "", FieldPath: "RecordingStableWindow", Profile: ProfileAdvanced, Status: StatusActive, Default: 10 * time.Second},
		{Path: "recording_playback.mappings", Env: "", FieldPath: "RecordingPathMappings", Profile: ProfileAdvanced, Status: StatusActive},

		// --- VOD (Typed Config) ---
		{Path: "vod.probeSize", Env: "", FieldPath: "VOD.ProbeSize", Profile: ProfileAdvanced, Status: StatusActive, Default: "50M"},
		{Path: "vod.analyzeDuration", Env: "", FieldPath: "VOD.AnalyzeDuration", Profile: ProfileAdvanced, Status: StatusActive, Default: "50000000"},
		{Path: "vod.stallTimeout", Env: "", FieldPath: "VOD.StallTimeout", Profile: ProfileAdvanced, Status: StatusActive, Default: "1m"},
		{Path: "vod.maxConcurrent", Env: "", FieldPath: "VOD.MaxConcurrent", Profile: ProfileAdvanced, Status: StatusActive, Default: 2},
		{Path: "vod.cacheTTL", Env: "", FieldPath: "VOD.CacheTTL", Profile: ProfileAdvanced, Status: StatusActive, Default: "24h"},
		{Path: "vod.cacheMaxEntries", Env: "", FieldPath: "VOD.CacheMaxEntries", Profile: ProfileAdvanced, Status: StatusActive, Default: 256},

		// --- FEATURE FLAGS ---
		{Path: "readyStrict", Env: "XG2G_READY_STRICT", FieldPath: "ReadyStrict", Profile: ProfileAdvanced, Status: StatusActive, Default: false},

		// --- HDHR ---
		{Path: "hdhr.enabled", Env: "", FieldPath: "HDHR.Enabled", Profile: ProfileAdvanced, Status: StatusActive, Default: false},
		{Path: "hdhr.deviceId", Env: "", FieldPath: "HDHR.DeviceID", Profile: ProfileAdvanced, Status: StatusActive},
		{Path: "hdhr.friendlyName", Env: "", FieldPath: "HDHR.FriendlyName", Profile: ProfileAdvanced, Status: StatusActive},
		{Path: "hdhr.modelNumber", Env: "", FieldPath: "HDHR.ModelNumber", Profile: ProfileAdvanced, Status: StatusActive},
		{Path: "hdhr.firmwareName", Env: "", FieldPath: "HDHR.FirmwareName", Profile: ProfileAdvanced, Status: StatusActive},
		{Path: "hdhr.baseUrl", Env: "", FieldPath: "HDHR.BaseURL", Profile: ProfileAdvanced, Status: StatusActive},
		{Path: "hdhr.tunerCount", Env: "", FieldPath: "HDHR.TunerCount", Profile: ProfileAdvanced, Status: StatusActive},
		{Path: "hdhr.plexForceHls", Env: "", FieldPath: "HDHR.PlexForceHLS", Profile: ProfileAdvanced, Status: StatusActive},

		// --- LIBRARY ---
		{Path: "library.enabled", Env: "", FieldPath: "Library.Enabled", Profile: ProfileAdvanced, Status: StatusActive, Default: false},
		{Path: "library.db_path", Env: "", FieldPath: "Library.DBPath", Profile: ProfileAdvanced, Status: StatusActive},
		{Path: "library.roots", Env: "", FieldPath: "Library.Roots", Profile: ProfileAdvanced, Status: StatusActive},

		// --- INTERNAL / CANDIDATES ---
		{FieldPath: "apiTokensParseErr", Profile: ProfileInternal, Status: StatusInternal},
		// Legacy VOD flat fields (backwards-compat only, use typed VOD.* instead)
		{FieldPath: "VODProbeSize", Profile: ProfileInternal, Status: StatusInternal},
		{FieldPath: "VODAnalyzeDuration", Profile: ProfileInternal, Status: StatusInternal},
		{FieldPath: "VODStallTimeout", Profile: ProfileInternal, Status: StatusInternal},
		{FieldPath: "VODMaxConcurrent", Profile: ProfileInternal, Status: StatusInternal},
		{FieldPath: "VODCacheTTL", Profile: ProfileInternal, Status: StatusInternal, Default: 24 * time.Hour},
		{FieldPath: "VODCacheMaxEntries", Profile: ProfileInternal, Status: StatusInternal, Default: 256},

		// Zombie candidate examples (mapping existing env that might not be in AppConfig)
		{Env: "XG2G_HTTP_ENABLE_HTTP2", FieldPath: "??", Profile: ProfileIntegrator, Status: StatusCandidate},
	}

	for _, e := range entries {
		if e.Path != "" {
			if _, dup := r.ByPath[e.Path]; dup {
				return nil, fmt.Errorf("duplicate registry path: %s", e.Path)
			}
			r.ByPath[e.Path] = e
		}
		if e.FieldPath != "" && e.FieldPath != "??" {
			if _, dup := r.ByField[e.FieldPath]; dup {
				return nil, fmt.Errorf("duplicate registry field: %s", e.FieldPath)
			}
			r.ByField[e.FieldPath] = e
		}
		if e.Env != "" {
			if _, dup := r.ByEnv[e.Env]; dup {
				return nil, fmt.Errorf("duplicate registry env: %s", e.Env)
			}
			r.ByEnv[e.Env] = e
		}
	}

	return r, nil
}

// ValidateFieldCoverage uses reflection to ensure every field in AppConfig is registered.
func (r *Registry) ValidateFieldCoverage(cfg AppConfig) error {
	t := reflect.TypeOf(cfg)
	return r.validateStruct("", t)
}

func (r *Registry) validateStruct(prefix string, t reflect.Type) error {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}

		fieldPath := f.Name
		if prefix != "" {
			fieldPath = prefix + "." + f.Name
		}

		fieldType := f.Type
		if fieldType.Kind() == reflect.Ptr {
			fieldType = fieldType.Elem()
		}

		// If it's a nested struct (and not a primitive-like type), recurse
		if fieldType.Kind() == reflect.Struct && !isSimpleStruct(fieldType) {
			if err := r.validateStruct(fieldPath, fieldType); err != nil {
				return err
			}
			continue
		}

		// Check if registered
		if _, ok := r.ByField[fieldPath]; !ok {
			return fmt.Errorf("field %q is not registered in the config registry", fieldPath)
		}
	}
	return nil
}

// ApplyDefaults applies registered default values to the given AppConfig.
// Returns an error if any default cannot be set (indicates registry misconfiguration).
func (r *Registry) ApplyDefaults(cfg *AppConfig) error {
	v := reflect.ValueOf(cfg).Elem()
	for _, entry := range r.ByField {
		if entry.Default == nil {
			continue
		}

		err := setField(v, entry.FieldPath, entry.Default)
		if err != nil {
			return fmt.Errorf("failed to set default for %s: %w", entry.FieldPath, err)
		}
	}
	return nil
}

func setField(v reflect.Value, fieldPath string, value any) error {
	parts := strings.Split(fieldPath, ".")
	curr := v
	for i, p := range parts {
		if curr.Kind() == reflect.Ptr {
			if curr.IsNil() {
				// Initialize pointer if it's a struct we need to go into
				curr.Set(reflect.New(curr.Type().Elem()))
			}
			curr = curr.Elem()
		}

		f := curr.FieldByName(p)
		if !f.IsValid() {
			return fmt.Errorf("field %s not found", p)
		}

		if i == len(parts)-1 {
			// Last part, set the value
			val := reflect.ValueOf(value)

			// Handle assignment to pointer leaf
			if f.Kind() == reflect.Ptr && val.Kind() != reflect.Ptr {
				// CRITICAL: Only set default if pointer is nil (unset).
				// If pointer is not nil, it means it has been set explicitly (possibly to zero value like false/0),
				// and we MUST NOT overwrite it with a default.
				if !f.IsNil() {
					return nil
				}

				// Allocate and set
				f.Set(reflect.New(f.Type().Elem()))

				// Assign to elem
				elem := f.Elem()
				if elem.Type() != val.Type() {
					if val.Type().ConvertibleTo(elem.Type()) {
						elem.Set(val.Convert(elem.Type()))
					} else {
						return fmt.Errorf("type mismatch for %s (elem): expected %v, got %v", fieldPath, elem.Type(), val.Type())
					}
				} else {
					elem.Set(val)
				}
				return nil
			}

			if f.Type() != val.Type() {
				// Try to convert if possible (e.g. int to int64)
				if val.Type().ConvertibleTo(f.Type()) {
					f.Set(val.Convert(f.Type()))
				} else {
					return fmt.Errorf("type mismatch for %s: expected %v, got %v", fieldPath, f.Type(), val.Type())
				}
			} else {
				f.Set(val)
			}
			return nil
		}
		curr = f
	}
	return nil
}

func isSimpleStruct(t reflect.Type) bool {
	// Add types that should be treated as leaves even if they are structs (e.g. time.Duration, time.Time)
	path := t.PkgPath()
	name := t.Name()
	return (path == "time" && (name == "Duration" || name == "Time"))
}
