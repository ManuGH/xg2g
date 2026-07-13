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

	SafariHEVCVAAPIQP          int
	ExperimentalAV1MPEGTS      bool
	SafariForceCopyServiceRefs []string
}

// DefaultConfigSnapshot returns the environment-independent production
// defaults. It is also the safe behavior of a zero-value Resolver used by
// legacy tests and recovery code that has not yet been wired explicitly.
func DefaultConfigSnapshot() ConfigSnapshot {
	return ConfigSnapshot{
		SafariVAAPIQP:              20,
		SafariVAAPIMaxRateK:        20000,
		SafariVAAPIBufSizeK:        40000,
		SafariCPUPreset:            "veryfast",
		SafariDirtyAudioBitrateK:   192,
		SafariDirtyGPUQP:           20,
		SafariDirtyGPUMaxRateK:     20000,
		SafariDirtyGPUBufSizeK:     40000,
		SafariDirtyCPUCRF:          18,
		SafariDirtyCPUMaxRateK:     8000,
		SafariDirtyCPUBufSizeK:     16000,
		SafariDirtyCPUPreset:       "veryfast",
		SafariHEVCVAAPIQP:          20,
		ExperimentalAV1MPEGTS:      false,
		SafariForceCopyServiceRefs: nil,
	}
}

// LoadConfigSnapshot is the sole environment boundary for profile resolution.
func LoadConfigSnapshot() ConfigSnapshot {
	defaults := DefaultConfigSnapshot()
	return ConfigSnapshot{
		SafariDirtyHWAccelMode: normalizeSnapshotToken(config.ParseString("XG2G_SAFARI_DIRTY_HWACCEL_MODE", "")),
		SafariDirtyUseGPU:      snapshotBool("XG2G_SAFARI_DIRTY_USE_GPU", false),

		SafariVAAPIQP:       snapshotInt("XG2G_SAFARI_VAAPI_QP", defaults.SafariVAAPIQP, 10, 40),
		SafariVAAPIMaxRateK: snapshotInt("XG2G_SAFARI_VAAPI_MAXRATE_K", defaults.SafariVAAPIMaxRateK, 4000, 60000),
		SafariVAAPIBufSizeK: snapshotInt("XG2G_SAFARI_VAAPI_BUFSIZE_K", defaults.SafariVAAPIBufSizeK, 8000, 120000),
		SafariCPUPreset:     snapshotPreset("XG2G_SAFARI_CPU_PRESET", defaults.SafariCPUPreset),

		SafariDirtyAudioBitrateK: snapshotInt("XG2G_SAFARI_DIRTY_AUDIO_BITRATE_K", defaults.SafariDirtyAudioBitrateK, 96, 384),
		SafariDirtyGPUQP:         snapshotInt("XG2G_SAFARI_DIRTY_VAAPI_QP", defaults.SafariDirtyGPUQP, 10, 40),
		SafariDirtyGPUMaxRateK:   snapshotInt("XG2G_SAFARI_DIRTY_MAXRATE_K", defaults.SafariDirtyGPUMaxRateK, 4000, 60000),
		SafariDirtyGPUBufSizeK:   snapshotInt("XG2G_SAFARI_DIRTY_BUFSIZE_K", defaults.SafariDirtyGPUBufSizeK, 8000, 120000),
		SafariDirtyCPUCRF:        snapshotInt("XG2G_SAFARI_DIRTY_CRF", defaults.SafariDirtyCPUCRF, 12, 30),
		SafariDirtyCPUMaxRateK:   snapshotInt("XG2G_SAFARI_DIRTY_MAXRATE_K", defaults.SafariDirtyCPUMaxRateK, 4000, 60000),
		SafariDirtyCPUBufSizeK:   snapshotInt("XG2G_SAFARI_DIRTY_BUFSIZE_K", defaults.SafariDirtyCPUBufSizeK, 8000, 120000),
		SafariDirtyCPUPreset:     snapshotPreset("XG2G_SAFARI_DIRTY_PRESET", defaults.SafariDirtyCPUPreset),

		SafariHEVCVAAPIQP:          snapshotInt("XG2G_SAFARI_HEVC_VAAPI_QP", defaults.SafariHEVCVAAPIQP, 10, 40),
		ExperimentalAV1MPEGTS:      snapshotBool("XG2G_EXPERIMENTAL_AV1_MPEGTS_ENABLED", defaults.ExperimentalAV1MPEGTS),
		SafariForceCopyServiceRefs: snapshotList("XG2G_SAFARI_FORCE_COPY_SERVICE_REFS"),
	}
}

func (c ConfigSnapshot) clone() ConfigSnapshot {
	c.SafariForceCopyServiceRefs = append([]string(nil), c.SafariForceCopyServiceRefs...)
	return c
}

func snapshotList(key string) []string {
	raw := config.ParseString(key, "")
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' || r == '\n' || r == '\r' })
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			out = append(out, value)
		}
	}
	return out
}

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
