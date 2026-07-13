// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ffmpeg

import (
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
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
	// Live + stream-relay ingest probe depth.
	LiveAnalyzeDuration        string
	LiveProbeSize              string
	LiveUserAgent              string
	StreamRelayAnalyzeDuration string
	StreamRelayProbeSize       string
	CachedRelayAnalyzeDuration string
	CachedRelayProbeSize       string

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
	ExperimentalCMAF     bool

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
}

// LoadAdapterConfig resolves every ENV-tunable media-pipeline knob: defaults,
// bounds, and the resilient-ingest coupling that several FFmpeg flag defaults
// share. analyzeDuration/probeSize are the adapter's already-defaulted general
// probe depth; the FPS probe falls back to them when its own override is unset,
// so they are passed in rather than re-read here.
func LoadAdapterConfig(analyzeDuration, probeSize string) AdapterConfig {
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
	cachedRelayAnalyzeDuration := strings.TrimSpace(config.ParseString("XG2G_STREAMRELAY_CACHED_ANALYZE_DURATION", ""))
	if cachedRelayAnalyzeDuration == "" {
		cachedRelayAnalyzeDuration = "2500000" // 2.5s after this channel produced a valid HLS segment
	}
	cachedRelayProbeSize := strings.TrimSpace(config.ParseString("XG2G_STREAMRELAY_CACHED_PROBE_SIZE", ""))
	if cachedRelayProbeSize == "" {
		cachedRelayProbeSize = "10M"
	}
	liveNoBuffer := envBool("XG2G_LIVE_NOBUFFER", false)
	forceIgnDTS := envBool("XG2G_FORCE_IGNDTS", false)
	liveAvsyncAtrim := envBool("XG2G_LIVE_AVSYNC_ATRIM", false)
	liveAvsyncPipeNoTrim := envBool("XG2G_LIVE_AVSYNC_PIPE_NO_TRIM", false)
	experimentalCMAF := envBool("XG2G_EXPERIMENTAL_CMAF_SEGMENTER", false)
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

	return AdapterConfig{
		LiveAnalyzeDuration:        liveAnalyzeDuration,
		LiveProbeSize:              liveProbeSize,
		LiveUserAgent:              liveUserAgent,
		StreamRelayAnalyzeDuration: streamRelayAnalyzeDuration,
		StreamRelayProbeSize:       streamRelayProbeSize,
		CachedRelayAnalyzeDuration: cachedRelayAnalyzeDuration,
		CachedRelayProbeSize:       cachedRelayProbeSize,
		IngestFFlags:               ingestFFlags,
		IngestErrDetect:            ingestErrDetect,
		IngestMaxErrorRate:         ingestMaxErrorRate,
		IngestFlags2:               ingestFlags2,
		LiveNoBuffer:               liveNoBuffer,
		ForceIgnDTS:                forceIgnDTS,
		LiveAvsyncAtrim:            liveAvsyncAtrim,
		LiveAvsyncPipeNoTrim:       liveAvsyncPipeNoTrim,
		ExperimentalCMAF:           experimentalCMAF,
		SafariDirtyFilter:          safariDirtyFilter,
		SafariDirtyX264Tune:        safariDirtyTune,
		FPSProbeTimeout:            time.Duration(fpsProbeTimeoutMs) * time.Millisecond,
		FPSMin:                     fpsMin,
		FPSMax:                     fpsMax,
		FPSFallback:                fpsFallback,
		FPSFallbackInter:           fpsFallbackInter,
		FPSProbeFFlags:             fpsProbeFFlags,
		FPSProbeErrDetect:          fpsProbeErrDetect,
		FPSProbeAnalyze:            fpsProbeAnalyze,
		FPSProbeSize:               fpsProbeSize,
		FPSProbeRetryAn:            fpsProbeRetryAnalyze,
		FPSProbeRetrySize:          fpsProbeRetrySize,
		SkipFPSProbeOnCache:        skipFPSProbeOnCache,
		SkipFPSProbeWarmup:         skipFPSProbeWarmup,
		FPSCacheTTL:                fpsCacheTTL,
		SafariRuntimeProbeTimeout:  time.Duration(safariRuntimeProbeTimeoutMs) * time.Millisecond,
	}
}
