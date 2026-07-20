package config

import (
	"strings"
	"testing"
)

func TestValidateEnvUsage_UnknownSecurityKeyFails(t *testing.T) {
	values := map[string]string{
		"XG2G_STORE_PATH":      t.TempDir(),
		"XG2G_TLS_ENFORCE_ALL": "1",
	}

	SetRequiredTestSecrets(t)
	loader := NewLoaderWithEnv(
		"",
		"test",
		mapLookup(values),
		func() []string {
			return []string{"XG2G_STORE_PATH=" + values["XG2G_STORE_PATH"], "XG2G_TLS_ENFORCE_ALL=1"}
		},
	)

	err := loader.ValidateEnvUsage(true)
	if err == nil {
		t.Fatalf("expected unknown security key to fail")
	}
	if !strings.Contains(err.Error(), "XG2G_TLS_ENFORCE_ALL") {
		t.Fatalf("expected error to mention unknown key, got %v", err)
	}
}

func TestValidateEnvUsage_UnknownNonSecurityKeyWarnOnly(t *testing.T) {
	values := map[string]string{
		"XG2G_STORE_PATH":          t.TempDir(),
		"XG2G_EXPERIMENTAL_WIDGET": "on",
	}

	SetRequiredTestSecrets(t)
	loader := NewLoaderWithEnv(
		"",
		"test",
		mapLookup(values),
		func() []string {
			return []string{"XG2G_STORE_PATH=" + values["XG2G_STORE_PATH"], "XG2G_EXPERIMENTAL_WIDGET=on"}
		},
	)

	if err := loader.ValidateEnvUsage(true); err != nil {
		t.Fatalf("expected unknown non-security key to warn only, got %v", err)
	}
}

func TestValidateEnvUsage_RuntimeKeyAllowed(t *testing.T) {
	values := map[string]string{
		"XG2G_STORE_PATH":        t.TempDir(),
		"XG2G_PLAYLIST_FILENAME": "playlist.custom.m3u8",
		"XG2G_DECISION_SECRET":   "12345678901234567890123456789012",
	}

	SetRequiredTestSecrets(t)
	loader := NewLoaderWithEnv(
		"",
		"test",
		mapLookup(values),
		func() []string {
			return []string{
				"XG2G_STORE_PATH=" + values["XG2G_STORE_PATH"],
				"XG2G_PLAYLIST_FILENAME=" + values["XG2G_PLAYLIST_FILENAME"],
				"XG2G_DECISION_SECRET=" + values["XG2G_DECISION_SECRET"],
			}
		},
	)

	if err := loader.ValidateEnvUsage(true); err != nil {
		t.Fatalf("expected runtime env key to be treated as known, got %v", err)
	}
}

func TestKnownRuntimeEnvKeys_IncludesDirectReadEnvKeys(t *testing.T) {
	// These keys are read directly via env helpers (envIntBounded/envFloatBounded/
	// envBool/ParseBool) in the ffmpeg/pipeline packages, outside the config loader,
	// so they never enter the loader's ConsumedEnvKeys set. They must be in the
	// runtime allowlist or ValidateEnvUsage falsely flags them as
	// "unknown XG2G env key (dead flag or typo)". Negative control: drop any one
	// from runtimeEnvKeys and this goes red.
	want := []string{
		"XG2G_TRANSCODE_SHARPEN", "XG2G_TRANSCODE_DENOISE", "XG2G_TRANSCODE_DEBAND",
		"XG2G_AV1_QVBR", "XG2G_AV1_QVBR_QUALITY",
		"XG2G_AV1_NVENC_AUTO_RATIO_MAX", "XG2G_AV1_VAAPI_AUTO_RATIO_MAX",
		"XG2G_HEVC_NVENC_AUTO_RATIO_MAX", "XG2G_HEVC_VAAPI_AUTO_RATIO_MAX",
		"XG2G_ADAPTIVE_QUALITY_ENABLED",
		"XG2G_ADAPTIVE_AV1_QUALITY_ENABLED", "XG2G_ADAPTIVE_AV1_MAXRATE_K", "XG2G_ADAPTIVE_AV1_BUFSIZE_K",
		"XG2G_ADAPTIVE_HEVC_QUALITY_ENABLED", "XG2G_ADAPTIVE_HEVC_MAXRATE_K", "XG2G_ADAPTIVE_HEVC_BUFSIZE_K",
		"XG2G_ADAPTIVE_H264_QUALITY_ENABLED", "XG2G_ADAPTIVE_H264_MAXRATE_K", "XG2G_ADAPTIVE_H264_BUFSIZE_K",
		"XG2G_SAFARI_DIRTY_MAXRATE_K", "XG2G_SAFARI_DIRTY_BUFSIZE_K", "XG2G_SAFARI_DIRTY_AUDIO_BITRATE_K",
		"XG2G_SAFARI_CPU_PRESET", "XG2G_SAFARI_CPU_START_TIMEOUT_MS", "XG2G_SAFARI_DIRTY_VAAPI_QP", "XG2G_SAFARI_DIRTY_X264_TUNE",
		"XG2G_SAFARI_VAAPI_QP", "XG2G_SAFARI_VAAPI_MAXRATE_K", "XG2G_SAFARI_VAAPI_BUFSIZE_K",
		"XG2G_SAFARI_HEVC_VAAPI_QP", "XG2G_SAFARI_RUNTIME_PROBE_TIMEOUT_MS",
		"XG2G_FPS_PROBE_FFLAGS", "XG2G_FPS_PROBE_ERR_DETECT", "XG2G_FPS_PROBE_ANALYZE_DURATION", "XG2G_FPS_PROBE_SIZE",
		"XG2G_FPS_PROBE_RETRY_ANALYZE_DURATION", "XG2G_FPS_PROBE_RETRY_SIZE",
		"XG2G_FPS_MIN", "XG2G_FPS_MAX", "XG2G_FPS_FALLBACK", "XG2G_FPS_PROBE_TIMEOUT_MS",
		"XG2G_LIVE_ANALYZE_DURATION", "XG2G_LIVE_PROBE_SIZE", "XG2G_LIVE_USER_AGENT",
		"XG2G_INGEST_FFLAGS", "XG2G_INGEST_ERR_DETECT", "XG2G_INGEST_MAX_ERROR_RATE", "XG2G_INGEST_FLAGS2",
		"XG2G_STREAMRELAY_ANALYZE_DURATION", "XG2G_STREAMRELAY_PROBE_SIZE",
		"XG2G_ENABLE_SYNTHETIC_PATH_CORRECTNESS_PREFLIGHT",
		"XG2G_RUNTIME_PATH_CORRECTNESS_LOW_OBS", "XG2G_RUNTIME_PATH_CORRECTNESS_MIN_YAVG",
		"XG2G_LIVE_NOBUFFER", "XG2G_FORCE_IGNDTS", "XG2G_LIVE_AVSYNC_ATRIM", "XG2G_LIVE_AVSYNC_PIPE_NO_TRIM", "XG2G_INITIAL_REFRESH", "XG2G_RATE_LIMIT_ENABLED",
	}

	known := make(map[string]struct{})
	for _, k := range KnownRuntimeEnvKeys() {
		known[k] = struct{}{}
	}
	for _, k := range want {
		if _, ok := known[k]; !ok {
			t.Errorf("runtime-read env key %q missing from KnownRuntimeEnvKeys -> ValidateEnvUsage would falsely warn it as unknown", k)
		}
	}
}

func mapLookup(values map[string]string) envLookupFunc {
	return func(key string) (string, bool) {
		v, ok := values[key]
		return v, ok
	}
}
