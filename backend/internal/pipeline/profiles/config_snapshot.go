package profiles

import (
	"strconv"
	"strings"

	"github.com/ManuGH/xg2g/internal/config"
)

// ConfigSnapshot is the immutable set of operator tuning values used while a
// profile is resolved. Environment variables are read only while the snapshot
// is constructed; ResolveWithConfig itself is deterministic and side-effect
// free with respect to process environment changes.
type ConfigSnapshot struct {
	SafariDirtyHWAccelMode string
	SafariDirtyUseGPU      bool

	SafariVAAPIQP       int
	SafariVAAPIMaxRateK int
	SafariVAAPIBufSizeK int
	SafariCPUPreset     string

	SafariDirtyAudioBitrateK int
	SafariDirtyGPUQP         int
	SafariDirtyGPUMaxRateK   int
	SafariDirtyGPUBufSizeK   int
	SafariDirtyCPUCRF        int
	SafariDirtyCPUMaxRateK   int
	SafariDirtyCPUBufSizeK   int
	SafariDirtyCPUPreset     string

	SafariHEVCVAAPIQP     int
	ExperimentalAV1MPEGTS bool
}

// LoadConfigSnapshot is the sole environment boundary for profile resolution.
func LoadConfigSnapshot() ConfigSnapshot {
	return ConfigSnapshot{
		SafariDirtyHWAccelMode: normalizeSnapshotToken(config.ParseString("XG2G_SAFARI_DIRTY_HWACCEL_MODE", "")),
		SafariDirtyUseGPU:      snapshotBool("XG2G_SAFARI_DIRTY_USE_GPU", false),

		SafariVAAPIQP:       snapshotInt("XG2G_SAFARI_VAAPI_QP", 20, 10, 40),
		SafariVAAPIMaxRateK: snapshotInt("XG2G_SAFARI_VAAPI_MAXRATE_K", 20000, 4000, 60000),
		SafariVAAPIBufSizeK: snapshotInt("XG2G_SAFARI_VAAPI_BUFSIZE_K", 40000, 8000, 120000),
		SafariCPUPreset:     snapshotPreset("XG2G_SAFARI_CPU_PRESET", "veryfast"),

		SafariDirtyAudioBitrateK: snapshotInt("XG2G_SAFARI_DIRTY_AUDIO_BITRATE_K", 192, 96, 384),
		SafariDirtyGPUQP:         snapshotInt("XG2G_SAFARI_DIRTY_VAAPI_QP", 20, 10, 40),
		SafariDirtyGPUMaxRateK:   snapshotInt("XG2G_SAFARI_DIRTY_MAXRATE_K", 20000, 4000, 60000),
		SafariDirtyGPUBufSizeK:   snapshotInt("XG2G_SAFARI_DIRTY_BUFSIZE_K", 40000, 8000, 120000),
		SafariDirtyCPUCRF:        snapshotInt("XG2G_SAFARI_DIRTY_CRF", 18, 12, 30),
		SafariDirtyCPUMaxRateK:   snapshotInt("XG2G_SAFARI_DIRTY_MAXRATE_K", 8000, 4000, 60000),
		SafariDirtyCPUBufSizeK:   snapshotInt("XG2G_SAFARI_DIRTY_BUFSIZE_K", 16000, 8000, 120000),
		SafariDirtyCPUPreset:     snapshotPreset("XG2G_SAFARI_DIRTY_PRESET", "veryfast"),

		SafariHEVCVAAPIQP:     snapshotInt("XG2G_SAFARI_HEVC_VAAPI_QP", 20, 10, 40),
		ExperimentalAV1MPEGTS: snapshotBool("XG2G_EXPERIMENTAL_AV1_MPEGTS_ENABLED", false),
	}
}

var processConfigSnapshot = LoadConfigSnapshot()

func normalizeSnapshotToken(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func snapshotBool(key string, defaultValue bool) bool {
	switch normalizeSnapshotToken(config.ParseString(key, "")) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return defaultValue
	}
}

func snapshotInt(key string, defaultValue, minValue, maxValue int) int {
	raw := strings.TrimSpace(config.ParseString(key, ""))
	if raw == "" {
		return defaultValue
	}
	n, err := strconv.Atoi(raw)
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

func snapshotPreset(key, defaultValue string) string {
	raw := normalizeSnapshotToken(config.ParseString(key, ""))
	switch raw {
	case "slow", "medium", "fast", "veryfast", "faster", "superfast", "ultrafast":
		return raw
	case "":
		return defaultValue
	default:
		return defaultValue
	}
}
