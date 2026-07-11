// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ffmpeg

import (
	"os"
	"testing"
	"time"
)

// adapterEnvKeys are every XG2G_* var LoadAdapterConfig reads. The defaults test
// clears them so the no-env path is exercised regardless of the ambient
// environment.
var adapterEnvKeys = []string{
	"XG2G_LIVE_ANALYZE_DURATION",
	"XG2G_LIVE_PROBE_SIZE",
	"XG2G_LIVE_USER_AGENT",
	"XG2G_STREAMRELAY_ANALYZE_DURATION",
	"XG2G_STREAMRELAY_PROBE_SIZE",
	"XG2G_STREAMRELAY_CACHED_ANALYZE_DURATION",
	"XG2G_STREAMRELAY_CACHED_PROBE_SIZE",
	"XG2G_LIVE_NOBUFFER",
	"XG2G_FORCE_IGNDTS",
	"XG2G_LIVE_AVSYNC_ATRIM",
	"XG2G_LIVE_AVSYNC_PIPE_NO_TRIM",
	"XG2G_EXPERIMENTAL_CMAF_SEGMENTER",
	"XG2G_FPS_PROBE_TIMEOUT_MS",
	"XG2G_FPS_MIN",
	"XG2G_FPS_MAX",
	"XG2G_FPS_FALLBACK",
	"XG2G_FPS_FALLBACK_INTERLACED",
	"XG2G_RESILIENT_INGEST",
	"XG2G_SAFARI_DIRTY_DEINTERLACE_FILTER",
	"XG2G_SAFARI_DIRTY_X264_TUNE",
	"XG2G_INGEST_FFLAGS",
	"XG2G_INGEST_ERR_DETECT",
	"XG2G_INGEST_MAX_ERROR_RATE",
	"XG2G_INGEST_FLAGS2",
	"XG2G_FPS_PROBE_FFLAGS",
	"XG2G_FPS_PROBE_ERR_DETECT",
	"XG2G_FPS_PROBE_ANALYZE_DURATION",
	"XG2G_FPS_PROBE_SIZE",
	"XG2G_FPS_PROBE_RETRY_ANALYZE_DURATION",
	"XG2G_FPS_PROBE_RETRY_SIZE",
	"XG2G_SKIP_FPS_PROBE_ON_CACHE_HIT",
	"XG2G_SKIP_FPS_PROBE_WARMUP",
	"XG2G_SAFARI_RUNTIME_PROBE_TIMEOUT_MS",
	"XG2G_FPS_CACHE_TTL",
}

func clearAdapterEnv(t *testing.T) {
	t.Helper()
	for _, k := range adapterEnvKeys {
		if v, ok := os.LookupEnv(k); ok {
			key, orig := k, v
			t.Cleanup(func() { _ = os.Setenv(key, orig) })
			_ = os.Unsetenv(k)
		}
	}
}

func TestLoadAdapterConfig_Defaults(t *testing.T) {
	clearAdapterEnv(t)

	cfg := LoadAdapterConfig("2000000", "5M")

	checks := []struct {
		name string
		got  any
		want any
	}{
		{"LiveAnalyzeDuration", cfg.LiveAnalyzeDuration, "1000000"},
		{"LiveProbeSize", cfg.LiveProbeSize, "1M"},
		{"LiveUserAgent", cfg.LiveUserAgent, ""},
		{"StreamRelayAnalyzeDuration", cfg.StreamRelayAnalyzeDuration, "5000000"},
		{"StreamRelayProbeSize", cfg.StreamRelayProbeSize, "20M"},
		{"CachedRelayAnalyzeDuration", cfg.CachedRelayAnalyzeDuration, "2500000"},
		{"CachedRelayProbeSize", cfg.CachedRelayProbeSize, "10M"},
		{"LiveNoBuffer", cfg.LiveNoBuffer, false},
		{"ForceIgnDTS", cfg.ForceIgnDTS, false},
		{"LiveAvsyncAtrim", cfg.LiveAvsyncAtrim, false},
		{"LiveAvsyncPipeNoTrim", cfg.LiveAvsyncPipeNoTrim, false},
		{"ExperimentalCMAF", cfg.ExperimentalCMAF, false},
		{"SafariDirtyFilter", cfg.SafariDirtyFilter, "bwdif=mode=send_field:parity=auto:deint=all"},
		{"SafariDirtyX264Tune", cfg.SafariDirtyX264Tune, ""},
		// resilient ingest defaults to true -> resilient flag set.
		{"IngestFFlags", cfg.IngestFFlags, "+genpts+discardcorrupt+flush_packets"},
		{"IngestErrDetect", cfg.IngestErrDetect, "ignore_err"},
		{"IngestMaxErrorRate", cfg.IngestMaxErrorRate, "1.0"},
		{"IngestFlags2", cfg.IngestFlags2, ""},
		{"FPSProbeFFlags", cfg.FPSProbeFFlags, "+genpts+discardcorrupt"},
		{"FPSProbeErrDetect", cfg.FPSProbeErrDetect, "ignore_err"},
		{"FPSProbeTimeout", cfg.FPSProbeTimeout, 1500 * time.Millisecond},
		{"FPSMin", cfg.FPSMin, 15},
		{"FPSMax", cfg.FPSMax, 120},
		{"FPSFallback", cfg.FPSFallback, 25},
		{"FPSFallbackInter", cfg.FPSFallbackInter, 50},
		// FPS probe falls back to the passed-in general analyze/probe depth.
		{"FPSProbeAnalyze", cfg.FPSProbeAnalyze, "2000000"},
		{"FPSProbeSize", cfg.FPSProbeSize, "5M"},
		{"FPSProbeRetryAn", cfg.FPSProbeRetryAn, "10000000"},
		{"FPSProbeRetrySize", cfg.FPSProbeRetrySize, "20M"},
		{"SkipFPSProbeOnCache", cfg.SkipFPSProbeOnCache, true},
		{"SkipFPSProbeWarmup", cfg.SkipFPSProbeWarmup, time.Duration(0)},
		{"FPSCacheTTL", cfg.FPSCacheTTL, 24 * time.Hour},
		{"SafariRuntimeProbeTimeout", cfg.SafariRuntimeProbeTimeout, 6000 * time.Millisecond},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestLoadAdapterConfig_OverridesAndBounds(t *testing.T) {
	clearAdapterEnv(t)

	// Plain overrides.
	t.Setenv("XG2G_LIVE_ANALYZE_DURATION", "7000000")
	t.Setenv("XG2G_LIVE_NOBUFFER", "true")
	t.Setenv("XG2G_SAFARI_DIRTY_X264_TUNE", "zerolatency")
	t.Setenv("XG2G_FPS_CACHE_TTL", "1h")
	t.Setenv("XG2G_FPS_PROBE_ANALYZE_DURATION", "3000000")
	// Out-of-bounds ints must clamp to the declared range.
	t.Setenv("XG2G_FPS_PROBE_TIMEOUT_MS", "99999")          // > 5000 -> 5000
	t.Setenv("XG2G_FPS_MIN", "5")                           // < 10  -> 10
	t.Setenv("XG2G_FPS_MAX", "999")                         // > 240 -> 240
	t.Setenv("XG2G_SAFARI_RUNTIME_PROBE_TIMEOUT_MS", "100") // < 1000 -> 1000

	cfg := LoadAdapterConfig("2000000", "5M")

	if cfg.LiveAnalyzeDuration != "7000000" {
		t.Errorf("LiveAnalyzeDuration = %q, want 7000000", cfg.LiveAnalyzeDuration)
	}
	if !cfg.LiveNoBuffer {
		t.Error("LiveNoBuffer = false, want true")
	}
	if cfg.SafariDirtyX264Tune != "zerolatency" {
		t.Errorf("SafariDirtyX264Tune = %q, want zerolatency", cfg.SafariDirtyX264Tune)
	}
	if cfg.FPSCacheTTL != time.Hour {
		t.Errorf("FPSCacheTTL = %v, want 1h", cfg.FPSCacheTTL)
	}
	// An explicit FPS-probe override wins over the analyze-depth fallback.
	if cfg.FPSProbeAnalyze != "3000000" {
		t.Errorf("FPSProbeAnalyze = %q, want 3000000 (override beats fallback)", cfg.FPSProbeAnalyze)
	}
	if cfg.FPSProbeTimeout != 5000*time.Millisecond {
		t.Errorf("FPSProbeTimeout = %v, want clamp to 5000ms", cfg.FPSProbeTimeout)
	}
	if cfg.FPSMin != 10 {
		t.Errorf("FPSMin = %d, want clamp to 10", cfg.FPSMin)
	}
	if cfg.FPSMax != 240 {
		t.Errorf("FPSMax = %d, want clamp to 240", cfg.FPSMax)
	}
	if cfg.SafariRuntimeProbeTimeout != 1000*time.Millisecond {
		t.Errorf("SafariRuntimeProbeTimeout = %v, want clamp to 1000ms", cfg.SafariRuntimeProbeTimeout)
	}
}

func TestLoadAdapterConfig_ResilientIngestOff(t *testing.T) {
	clearAdapterEnv(t)
	t.Setenv("XG2G_RESILIENT_INGEST", "false")

	cfg := LoadAdapterConfig("2000000", "5M")

	checks := []struct {
		name string
		got  string
		want string
	}{
		{"IngestFFlags", cfg.IngestFFlags, "+genpts"},
		{"IngestErrDetect", cfg.IngestErrDetect, ""},
		{"IngestMaxErrorRate", cfg.IngestMaxErrorRate, ""},
		{"IngestFlags2", cfg.IngestFlags2, ""},
		{"FPSProbeFFlags", cfg.FPSProbeFFlags, "+genpts"},
		{"FPSProbeErrDetect", cfg.FPSProbeErrDetect, ""},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}
