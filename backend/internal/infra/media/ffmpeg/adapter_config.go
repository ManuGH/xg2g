// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ffmpeg

import (
	"strconv"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/infra/media/ffmpeg/capability"
)

// AdapterConfig holds the ENV-tunable knobs that shape FFmpeg live ingest, FPS
// probing and Safari dirty-source handling. It is resolved once by
// LoadAdapterConfig -- the single place that reads the media-pipeline XG2G_*
// variables -- and then copied verbatim into the LocalAdapter.
//
// Splitting it out of NewLocalAdapter keeps the constructor focused on wiring
// dependencies, and makes the env resolution (defaults, bounds clamping, and
// the resilient-ingest coupling several FFmpeg flag defaults share)
// independently unit-testable without constructing a full adapter.
type AdapterConfig struct {
	ShadowStoreEnabled       bool
	ShadowStoreMaxBytes      int64
	ShadowStoreQueueMaxBytes int64
	ShadowStoreMaxObjects    int

	// Live + stream-relay ingest probe depth.
	LiveAnalyzeDuration        string
	LiveProbeSize              string
	LiveUserAgent              string
	StreamRelayAnalyzeDuration string
	StreamRelayProbeSize       string

	// Ingest resilience (FFmpeg -fflags / -err_detect / -max_error_rate / -flags2).
	IngestFFlags       string
	IngestErrDetect    string
	IngestMaxErrorRate string
	IngestFlags2       string

	// Behaviour toggles.
	LiveNoBuffer         bool
	ForceIgnDTS          bool
	LiveAvsyncAtrim      bool
	LiveAvsyncPipeNoTrim bool

	// Safari dirty-source deinterlace + x264 tune.
	SafariDirtyFilter   string
	SafariDirtyX264Tune string

	// FPS probe shaping.
	FPSProbeTimeout   time.Duration
	FPSMin            int
	FPSMax            int
	FPSFallback       int
	FPSFallbackInter  int
	FPSProbeFFlags    string
	FPSProbeErrDetect string
	FPSProbeAnalyze   string
	FPSProbeSize      string
	FPSProbeRetryAn   string
	FPSProbeRetrySize string

	// FPS-probe skip + cache lifetime.
	SkipFPSProbeOnCache bool
	SkipFPSProbeWarmup  time.Duration
	FPSCacheTTL         time.Duration

	// Safari runtime path-correctness probe budget.
	SafariRuntimeProbeTimeout time.Duration

	// Command-planning and runtime-hardening tuning. These values are captured
	// once with the adapter and must not be re-read from process environment
	// while an FFmpeg plan is being constructed.
	SafariCPUStartTimeoutOverride time.Duration
	TranscodeSharpen              float64
	TranscodeDenoise              float64
	TranscodeDeband               bool
	AV1QVBR                       bool
	AV1QVBRQuality                int
	ExperimentalInterlacedCodecs  []string

	SafariForceCopyServiceRefs []string
	SafariHQServiceRefs        []string
	SafariHQ25ServiceRefs      []string
	SafariHQ50ServiceRefs      []string
	SafariHQ50MaxRateKOverride int
	SafariHQ50BufSizeKOverride int

	AdaptiveQualityEnabled     bool
	AdaptiveAV1QualityEnabled  bool
	AdaptiveHEVCQualityEnabled bool
	AdaptiveH264QualityEnabled bool
	AdaptiveAV1MaxRateK        int
	AdaptiveAV1BufSizeK        int
	AdaptiveHEVCMaxRateK       int
	AdaptiveHEVCBufSizeK       int
	AdaptiveH264MaxRateK       int
	AdaptiveH264BufSizeK       int

	HEVCVAAPIAutoRatioMax float64
	AV1VAAPIAutoRatioMax  float64
	HEVCNVENCAutoRatioMax float64
	AV1NVENCAutoRatioMax  float64
	RuntimePathMinYAvg    float64
	RuntimePathLowChecks  int
}

// LoadAdapterConfig resolves every ENV-tunable media-pipeline knob: defaults,
// bounds, and the resilient-ingest coupling that several FFmpeg flag defaults
// share. analyzeDuration/probeSize are the adapter's already-defaulted general
// probe depth; the FPS probe falls back to them when its own override is unset,
// so they are passed in rather than re-read here.
func LoadAdapterConfig(analyzeDuration, probeSize string) AdapterConfig {
	analyzeDuration = strings.TrimSpace(analyzeDuration)
	if analyzeDuration == "" {
		analyzeDuration = "2000000"
	}
	probeSize = strings.TrimSpace(probeSize)
	if probeSize == "" {
		probeSize = "5M"
	}
	liveAnalyzeDuration := strings.TrimSpace(config.ParseString("XG2G_LIVE_ANALYZE_DURATION", ""))
	if liveAnalyzeDuration == "" {
		liveAnalyzeDuration = "1000000" // 1s for low-latency live ingest
	}
	liveProbeSize := strings.TrimSpace(config.ParseString("XG2G_LIVE_PROBE_SIZE", ""))
	if liveProbeSize == "" {
		liveProbeSize = "1M" // 1MB for low-latency live ingest
	}
	// Live ingest user-agent. Default empty => use FFmpeg's built-in UA. A spoofed
	// "VLC" UA trips the OSCam stream-relay (oscam-emu) source-host heuristic: it
	// then fetches the scrambled source from the *client* IP:8001 instead of
	// honoring the Host header, yielding an empty stream ("Stream ends prematurely
	// at 0") for any remote client. A default (Lavf) UA makes OSCam honor the Host
	// header. Override via XG2G_LIVE_USER_AGENT only if a source explicitly needs one.
	liveUserAgent := strings.TrimSpace(config.ParseString("XG2G_LIVE_USER_AGENT", ""))
	// Stream-relay transcode inputs need a deeper probe than the general live
	// default to resolve dimensions/audio. 5s is the sharpened default: it
	// roughly halves the relay-transcode startup wait vs the old 10s while
	// keeping margin for slower/burstier relays. (3s was verified clean on a 4K
	// relay.) Raise XG2G_STREAMRELAY_ANALYZE_DURATION if a relay needs deeper
	// probing; lower it (e.g. 3000000) for the fastest start.
	streamRelayAnalyzeDuration := strings.TrimSpace(config.ParseString("XG2G_STREAMRELAY_ANALYZE_DURATION", ""))
	if streamRelayAnalyzeDuration == "" {
		streamRelayAnalyzeDuration = "5000000" // 5s
	}
	streamRelayProbeSize := strings.TrimSpace(config.ParseString("XG2G_STREAMRELAY_PROBE_SIZE", ""))
	if streamRelayProbeSize == "" {
		streamRelayProbeSize = "20M"
	}
	liveNoBuffer := envBool("XG2G_LIVE_NOBUFFER", false)
	forceIgnDTS := envBool("XG2G_FORCE_IGNDTS", false)
	liveAvsyncAtrim := envBool("XG2G_LIVE_AVSYNC_ATRIM", false)
	liveAvsyncPipeNoTrim := envBool("XG2G_LIVE_AVSYNC_PIPE_NO_TRIM", false)
	fpsProbeTimeoutMs := envIntBounded("XG2G_FPS_PROBE_TIMEOUT_MS", 1500, 300, 5000)
	fpsMin := envIntBounded("XG2G_FPS_MIN", 15, 10, 240)
	fpsMax := envIntBounded("XG2G_FPS_MAX", 120, fpsMin, 240)
	fpsFallback := envIntBounded("XG2G_FPS_FALLBACK", 25, 10, 120)
	fpsFallbackInter := envIntBounded("XG2G_FPS_FALLBACK_INTERLACED", 50, 10, 120)
	resilientIngest := envBool("XG2G_RESILIENT_INGEST", true)
	safariDirtyFilter := strings.TrimSpace(config.ParseString("XG2G_SAFARI_DIRTY_DEINTERLACE_FILTER", ""))
	if safariDirtyFilter == "" {
		safariDirtyFilter = "bwdif=mode=send_field:parity=auto:deint=all"
	}
	safariDirtyTune := strings.TrimSpace(config.ParseString("XG2G_SAFARI_DIRTY_X264_TUNE", ""))
	ingestFFlags := strings.TrimSpace(config.ParseString("XG2G_INGEST_FFLAGS", ""))
	if ingestFFlags == "" {
		if resilientIngest {
			ingestFFlags = "+genpts+discardcorrupt+flush_packets"
		} else {
			ingestFFlags = "+genpts"
		}
	}
	ingestErrDetect := strings.TrimSpace(config.ParseString("XG2G_INGEST_ERR_DETECT", ""))
	if ingestErrDetect == "" && resilientIngest {
		ingestErrDetect = "ignore_err"
	}
	ingestMaxErrorRate := strings.TrimSpace(config.ParseString("XG2G_INGEST_MAX_ERROR_RATE", ""))
	if ingestMaxErrorRate == "" && resilientIngest {
		ingestMaxErrorRate = "1.0"
	}
	// No resilient-ingest default for -flags2: +export_mvs is debug-only
	// overhead (motion-vector side data with no consumer) and +showall can
	// surface pre-keyframe garbage frames from glitchy DVB sources. Opt in
	// explicitly via XG2G_INGEST_FLAGS2 when needed.
	ingestFlags2 := strings.TrimSpace(config.ParseString("XG2G_INGEST_FLAGS2", ""))
	fpsProbeFFlags := strings.TrimSpace(config.ParseString("XG2G_FPS_PROBE_FFLAGS", ""))
	if fpsProbeFFlags == "" {
		if resilientIngest {
			fpsProbeFFlags = "+genpts+discardcorrupt"
		} else {
			fpsProbeFFlags = "+genpts"
		}
	}
	fpsProbeErrDetect := strings.TrimSpace(config.ParseString("XG2G_FPS_PROBE_ERR_DETECT", ""))
	if fpsProbeErrDetect == "" && resilientIngest {
		fpsProbeErrDetect = "ignore_err"
	}
	fpsProbeAnalyze := strings.TrimSpace(config.ParseString("XG2G_FPS_PROBE_ANALYZE_DURATION", ""))
	if fpsProbeAnalyze == "" {
		fpsProbeAnalyze = analyzeDuration
	}
	fpsProbeSize := strings.TrimSpace(config.ParseString("XG2G_FPS_PROBE_SIZE", ""))
	if fpsProbeSize == "" {
		fpsProbeSize = probeSize
	}
	fpsProbeRetryAnalyze := strings.TrimSpace(config.ParseString("XG2G_FPS_PROBE_RETRY_ANALYZE_DURATION", ""))
	if fpsProbeRetryAnalyze == "" {
		fpsProbeRetryAnalyze = "10000000"
	}
	fpsProbeRetrySize := strings.TrimSpace(config.ParseString("XG2G_FPS_PROBE_RETRY_SIZE", ""))
	if fpsProbeRetrySize == "" {
		fpsProbeRetrySize = "20M"
	}
	skipFPSProbeOnCache := envBool("XG2G_SKIP_FPS_PROBE_ON_CACHE_HIT", true)
	skipFPSProbeWarmup := config.ParseDuration("XG2G_SKIP_FPS_PROBE_WARMUP", 0)
	if skipFPSProbeWarmup < 0 {
		skipFPSProbeWarmup = 0
	}
	safariRuntimeProbeTimeoutMs := envIntBounded("XG2G_SAFARI_RUNTIME_PROBE_TIMEOUT_MS", 6000, 1000, 15000)
	fpsCacheTTL := config.ParseDuration("XG2G_FPS_CACHE_TTL", 24*time.Hour)
	if fpsCacheTTL <= 0 {
		fpsCacheTTL = 24 * time.Hour
	}
	safariCPUStartTimeoutMs := envOptionalIntBounded("XG2G_SAFARI_CPU_START_TIMEOUT_MS", 1, 120000)

	return AdapterConfig{
		LiveAnalyzeDuration:           liveAnalyzeDuration,
		LiveProbeSize:                 liveProbeSize,
		LiveUserAgent:                 liveUserAgent,
		StreamRelayAnalyzeDuration:    streamRelayAnalyzeDuration,
		StreamRelayProbeSize:          streamRelayProbeSize,
		IngestFFlags:                  ingestFFlags,
		IngestErrDetect:               ingestErrDetect,
		IngestMaxErrorRate:            ingestMaxErrorRate,
		IngestFlags2:                  ingestFlags2,
		LiveNoBuffer:                  liveNoBuffer,
		ForceIgnDTS:                   forceIgnDTS,
		LiveAvsyncAtrim:               liveAvsyncAtrim,
		LiveAvsyncPipeNoTrim:          liveAvsyncPipeNoTrim,
		SafariDirtyFilter:             safariDirtyFilter,
		SafariDirtyX264Tune:           safariDirtyTune,
		FPSProbeTimeout:               time.Duration(fpsProbeTimeoutMs) * time.Millisecond,
		FPSMin:                        fpsMin,
		FPSMax:                        fpsMax,
		FPSFallback:                   fpsFallback,
		FPSFallbackInter:              fpsFallbackInter,
		FPSProbeFFlags:                fpsProbeFFlags,
		FPSProbeErrDetect:             fpsProbeErrDetect,
		FPSProbeAnalyze:               fpsProbeAnalyze,
		FPSProbeSize:                  fpsProbeSize,
		FPSProbeRetryAn:               fpsProbeRetryAnalyze,
		FPSProbeRetrySize:             fpsProbeRetrySize,
		SkipFPSProbeOnCache:           skipFPSProbeOnCache,
		SkipFPSProbeWarmup:            skipFPSProbeWarmup,
		FPSCacheTTL:                   fpsCacheTTL,
		SafariRuntimeProbeTimeout:     time.Duration(safariRuntimeProbeTimeoutMs) * time.Millisecond,
		SafariCPUStartTimeoutOverride: time.Duration(safariCPUStartTimeoutMs) * time.Millisecond,
		TranscodeSharpen:              envFloatBounded("XG2G_TRANSCODE_SHARPEN", 1.5, 0.0, 3.0),
		TranscodeDenoise:              envFloatBounded("XG2G_TRANSCODE_DENOISE", 0.6, 0.0, 1.5),
		TranscodeDeband:               envBool("XG2G_TRANSCODE_DEBAND", true),
		AV1QVBR:                       envBool("XG2G_AV1_QVBR", true),
		AV1QVBRQuality:                envIntBounded("XG2G_AV1_QVBR_QUALITY", 90, 1, 255),
		ExperimentalInterlacedCodecs:  parseSnapshotList(config.ParseString(experimentalInterlacedVAAPICodecsEnv, ""), true),

		SafariForceCopyServiceRefs: parseSnapshotList(config.ParseString("XG2G_SAFARI_FORCE_COPY_SERVICE_REFS", ""), false),
		SafariHQServiceRefs:        parseSnapshotList(config.ParseString("XG2G_SAFARI_HQ_SERVICE_REFS", ""), false),
		SafariHQ25ServiceRefs:      parseSnapshotList(config.ParseString("XG2G_SAFARI_HQ25_SERVICE_REFS", ""), false),
		SafariHQ50ServiceRefs:      parseSnapshotList(config.ParseString("XG2G_SAFARI_HQ50_SERVICE_REFS", ""), false),
		SafariHQ50MaxRateKOverride: envOptionalIntBounded("XG2G_SAFARI_HQ50_MAXRATE_K", 4000, 60000),
		SafariHQ50BufSizeKOverride: envOptionalIntBounded("XG2G_SAFARI_HQ50_BUFSIZE_K", 8000, 120000),

		AdaptiveQualityEnabled:     envBool("XG2G_ADAPTIVE_QUALITY_ENABLED", true),
		AdaptiveAV1QualityEnabled:  envBool("XG2G_ADAPTIVE_AV1_QUALITY_ENABLED", true),
		AdaptiveHEVCQualityEnabled: envBool("XG2G_ADAPTIVE_HEVC_QUALITY_ENABLED", true),
		AdaptiveH264QualityEnabled: envBool("XG2G_ADAPTIVE_H264_QUALITY_ENABLED", true),
		AdaptiveAV1MaxRateK:        envOptionalIntBounded("XG2G_ADAPTIVE_AV1_MAXRATE_K", 4000, 60000),
		AdaptiveAV1BufSizeK:        envOptionalIntBounded("XG2G_ADAPTIVE_AV1_BUFSIZE_K", 8000, 120000),
		AdaptiveHEVCMaxRateK:       envOptionalIntBounded("XG2G_ADAPTIVE_HEVC_MAXRATE_K", 4000, 60000),
		AdaptiveHEVCBufSizeK:       envOptionalIntBounded("XG2G_ADAPTIVE_HEVC_BUFSIZE_K", 8000, 120000),
		AdaptiveH264MaxRateK:       envOptionalIntBounded("XG2G_ADAPTIVE_H264_MAXRATE_K", 4000, 60000),
		AdaptiveH264BufSizeK:       envOptionalIntBounded("XG2G_ADAPTIVE_H264_BUFSIZE_K", 8000, 120000),

		ShadowStoreEnabled:       envBool("XG2G_SHADOW_STORE_ENABLED", false),
		ShadowStoreMaxBytes:      int64(envOptionalIntBounded("XG2G_SHADOW_STORE_MAX_BYTES", 1024*1024, 1024*1024*1024)), // min 1MB, max 1GB
		ShadowStoreQueueMaxBytes: int64(envOptionalIntBounded("XG2G_SHADOW_STORE_QUEUE_MAX_BYTES", 1024*1024, 256*1024*1024)), // min 1MB, max 256MB
		ShadowStoreMaxObjects:    envOptionalIntBounded("XG2G_SHADOW_STORE_MAX_OBJECTS", 1, 1024),

		HEVCVAAPIAutoRatioMax: envFloatBounded("XG2G_HEVC_VAAPI_AUTO_RATIO_MAX", capability.DefaultHEVCVAAPIAutoRatioMax, 1.0, 10.0),
		AV1VAAPIAutoRatioMax:  envFloatBounded("XG2G_AV1_VAAPI_AUTO_RATIO_MAX", capability.DefaultAV1VAAPIAutoRatioMax, 1.0, 10.0),
		HEVCNVENCAutoRatioMax: envFloatBounded("XG2G_HEVC_NVENC_AUTO_RATIO_MAX", capability.DefaultHEVCNVENCAutoRatioMax, 1.0, 10.0),
		AV1NVENCAutoRatioMax:  envFloatBounded("XG2G_AV1_NVENC_AUTO_RATIO_MAX", capability.DefaultAV1NVENCAutoRatioMax, 1.0, 10.0),
		RuntimePathMinYAvg:    envFloatBounded("XG2G_RUNTIME_PATH_CORRECTNESS_MIN_YAVG", defaultRuntimePathCorrectnessMinYAvg, 1.0, 64.0),
		RuntimePathLowChecks:  envIntBounded("XG2G_RUNTIME_PATH_CORRECTNESS_LOW_OBS", defaultRuntimePathCorrectnessChecks, 1, 4),
	}
}

func cloneAdapterConfig(cfg AdapterConfig) AdapterConfig {
	cfg.ExperimentalInterlacedCodecs = append([]string(nil), cfg.ExperimentalInterlacedCodecs...)
	cfg.SafariForceCopyServiceRefs = append([]string(nil), cfg.SafariForceCopyServiceRefs...)
	cfg.SafariHQServiceRefs = append([]string(nil), cfg.SafariHQServiceRefs...)
	cfg.SafariHQ25ServiceRefs = append([]string(nil), cfg.SafariHQ25ServiceRefs...)
	cfg.SafariHQ50ServiceRefs = append([]string(nil), cfg.SafariHQ50ServiceRefs...)
	return cfg
}

func envOptionalIntBounded(key string, minValue, maxValue int) int {
	raw := strings.TrimSpace(config.ParseString(key, ""))
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	if n < minValue {
		return minValue
	}
	if n > maxValue {
		return maxValue
	}
	return n
}

func envIntBounded(key string, defaultValue, minValue, maxValue int) int {
	value := envOptionalIntBounded(key, minValue, maxValue)
	if value == 0 {
		return defaultValue
	}
	return value
}

func envFloatBounded(key string, defaultValue, minValue, maxValue float64) float64 {
	raw := strings.TrimSpace(config.ParseString(key, ""))
	if raw == "" {
		return defaultValue
	}
	n, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return defaultValue
	}
	if n < minValue {
		return minValue
	}
	if n > maxValue {
		return maxValue
	}
	return n
}

func envBool(key string, defaultValue bool) bool {
	return config.ParseBool(key, defaultValue)
}

func parseSnapshotList(raw string, splitWhitespace bool) []string {
	items := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || (splitWhitespace && (r == ' ' || r == '\t' || r == '\n' || r == '\r'))
	})
	out := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if splitWhitespace {
			item = normalizeRequestedCodec(item)
		} else if normalized := normalizeServiceRef(item); normalized != "" {
			item = normalized
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}
