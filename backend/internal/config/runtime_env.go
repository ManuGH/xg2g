// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ManuGH/xg2g/internal/streamprofile"
)

// Env captures all runtime settings sourced from environment variables.
// It is intended to be read once per load/reload and then treated as immutable.
type Env struct {
	Runtime RuntimeSnapshot
}

var runtimeEnvKeys = []string{
	"XG2G_PLAYLIST_FILENAME",
	"XG2G_PUBLIC_URL",
	"XG2G_DECISION_SECRET",
	"XG2G_X_TVG_URL",
	"XG2G_USE_PROXY_URLS",
	"XG2G_PROXY_BASE_URL",
	"XG2G_USE_HASH_TVGID",
	"XG2G_FFMPEG_LOGLEVEL",
	"XG2G_HTTP_MAX_CONNS_PER_HOST",
	"XG2G_STREAM_BASE",
	"XG2G_HLS_OUTPUT_DIR",
	"XG2G_HLS_DVR_SECONDS",
	"XG2G_SAFARI_DVR_SEGMENT_DURATION",
	"XG2G_SAFARI_DVR_WINDOW_SIZE",
	"XG2G_SAFARI_DVR_STARTUP_SEGMENTS",
	"XG2G_SAFARI_DVR_FFMPEG_PATH",
	"XG2G_WEB_FFMPEG_PATH",
	"XG2G_SAFARI_DVR_FORCE_AAC",
	"XG2G_SAFARI_DVR_AAC_BITRATE",
	"XG2G_LLHLS_SEGMENT_DURATION",
	"XG2G_LLHLS_PLAYLIST_SIZE",
	"XG2G_LLHLS_STARTUP_SEGMENTS",
	"XG2G_LLHLS_PART_SIZE",
	"XG2G_LLHLS_FFMPEG_PATH",
	"XG2G_LLHLS_HEVC_ENABLED",
	"XG2G_WEB_HEVC_PROFILE_ENABLED",
	"XG2G_LLHLS_HEVC_BITRATE",
	"XG2G_WEB_HEVC_BITRATE",
	"XG2G_LLHLS_HEVC_PEAK",
	"XG2G_WEB_HEVC_MAXBITRATE",
	"XG2G_LLHLS_HEVC_ENCODER",
	"XG2G_WEB_HEVC_ENCODER",
	"XG2G_LLHLS_HEVC_PROFILE",
	"XG2G_LLHLS_HEVC_LEVEL",
	"XG2G_LLHLS_VAAPI_DEVICE",
	"XG2G_WEB_LL_HLS_PART_DURATION",
	"XG2G_ENABLE_AUDIO_TRANSCODING",
	"XG2G_H264_STREAM_REPAIR",
	"XG2G_VIDEO_TRANSCODE",
	"XG2G_AUDIO_CHANNELS",
	"XG2G_AUDIO_CODEC",
	"XG2G_AUDIO_BITRATE",
	"XG2G_FFMPEG_BIN",
	"XG2G_VIDEO_CODEC",
	"XG2G_VAAPI_DEVICE",
	"XG2G_SAFARI_DIRTY_HWACCEL_MODE",
	"XG2G_SAFARI_DIRTY_CRF",
	"XG2G_SAFARI_DIRTY_PRESET",
	"XG2G_SAFARI_DIRTY_DEINTERLACE_FILTER",
	"XG2G_SAFARI_DIRTY_USE_GPU",
	"XG2G_RESILIENT_INGEST",
	"XG2G_FPS_FALLBACK_INTERLACED",
	"XG2G_FPS_CACHE_TTL",
	"XG2G_SKIP_FPS_PROBE_ON_CACHE_HIT",
	"XG2G_SKIP_FPS_PROBE_WARMUP",
}

// KnownRuntimeEnvKeys returns all env keys read by ReadEnv.
func KnownRuntimeEnvKeys() []string {
	out := make([]string, len(runtimeEnvKeys))
	copy(out, runtimeEnvKeys)
	return out
}

// DefaultEnv returns an Env populated entirely from defaults (no environment values).
func DefaultEnv() Env {
	env, _ := ReadEnv(func(string) string { return "" })
	return env
}

// ReadEnv reads all runtime environment variables exactly once using the provided getenv.
// The returned Env is safe to pass into BuildSnapshot without further environment reads.
func ReadEnv(getenv func(string) string) (Env, error) {
	if getenv == nil {
		return Env{}, fmt.Errorf("getenv is nil")
	}

	rt := RuntimeSnapshot{
		PlaylistFilename: getString(getenv, "XG2G_PLAYLIST_FILENAME", "playlist.m3u8"),
		PublicURL:        getString(getenv, "XG2G_PUBLIC_URL", ""),
		XTvgURL:          getString(getenv, "XG2G_X_TVG_URL", ""),
		UseProxyURLs:     getBool(getenv, "XG2G_USE_PROXY_URLS", false),
		ProxyBaseURL:     getString(getenv, "XG2G_PROXY_BASE_URL", "http://localhost:18000"),
		UseHashTvgID:     getBool(getenv, "XG2G_USE_HASH_TVGID", false),
		FFmpegLogLevel:   getString(getenv, "XG2G_FFMPEG_LOGLEVEL", ""),
		UIDevProxyURL:    getString(getenv, "XG2G_UI_DEV_PROXY_URL", ""),
		UIDevDir:         getString(getenv, "XG2G_UI_DEV_DIR", ""),
	}

	rt.OpenWebIF = readOpenWebIFRuntime(getenv)
	rt.Transcoder = readTranscoderRuntime(getenv)
	rt.HLS = readHLSRuntime(getenv)

	return Env{Runtime: rt}, nil
}

func readOpenWebIFRuntime(getenv func(string) string) OpenWebIFRuntime {
	return OpenWebIFRuntime{
		HTTPMaxConnsPerHost: getInt(getenv, "XG2G_HTTP_MAX_CONNS_PER_HOST", 50),
		StreamBaseURL:       getString(getenv, "XG2G_STREAM_BASE", ""),
	}
}

func readHLSRuntime(getenv func(string) string) HLSRuntime {
	return HLSRuntime{
		OutputDir: readHLSOutputDir(getenv),
		Generic:   readGenericHLSConfig(getenv),
		Safari:    readSafariDVRConfig(getenv),
		LLHLS:     readLLHLSConfig(getenv),
	}
}

func readHLSOutputDir(getenv func(string) string) string {
	outDir := readTrimmedEnv(getenv, "XG2G_HLS_OUTPUT_DIR")
	if outDir == "" {
		return "" // keep proxy default (os.TempDir) unless explicitly configured
	}
	return outDir
}

func readGenericHLSConfig(getenv func(string) string) streamprofile.GenericHLSConfig {
	generic := streamprofile.DefaultGenericHLSConfig()
	applyPositiveInt(getenv, "XG2G_HLS_DVR_SECONDS", &generic.DVRWindowSize)
	return generic
}

func readSafariDVRConfig(getenv func(string) string) streamprofile.SafariDVRConfig {
	safari := streamprofile.DefaultSafariDVRConfig()

	// Valid range 2-10s. 2s is optimized for fast startup (Safari), 6s+ for stability.
	applyBoundedInt(getenv, "XG2G_SAFARI_DVR_SEGMENT_DURATION", 2, 10, &safari.SegmentDuration)
	applyBoundedInt(getenv, "XG2G_SAFARI_DVR_WINDOW_SIZE", 1800, 7200, &safari.DVRWindowSize)
	applyBoundedInt(getenv, "XG2G_SAFARI_DVR_STARTUP_SEGMENTS", 1, 10, &safari.StartupSegments)

	if ffmpegPath := firstNonEmptyEnv(getenv, "XG2G_SAFARI_DVR_FFMPEG_PATH", "XG2G_WEB_FFMPEG_PATH"); ffmpegPath != "" {
		safari.FFmpegPath = ffmpegPath
	}
	if readTrimmedEnv(getenv, "XG2G_SAFARI_DVR_FORCE_AAC") == "false" {
		safari.ForceAAC = false
	}
	if bitrate := readTrimmedEnv(getenv, "XG2G_SAFARI_DVR_AAC_BITRATE"); bitrate != "" {
		safari.AACBitrate = bitrate
	}

	return safari
}

func readLLHLSConfig(getenv func(string) string) streamprofile.LLHLSConfig {
	llhls := streamprofile.DefaultLLHLSConfig()

	applyBoundedInt(getenv, "XG2G_LLHLS_SEGMENT_DURATION", 1, 2, &llhls.SegmentDuration)
	applyBoundedInt(getenv, "XG2G_LLHLS_PLAYLIST_SIZE", 6, 10, &llhls.PlaylistSize)
	applyBoundedInt(getenv, "XG2G_LLHLS_STARTUP_SEGMENTS", 1, 3, &llhls.StartupSegments)
	applyBoundedInt(getenv, "XG2G_LLHLS_PART_SIZE", 65536, 1048576, &llhls.PartSize)

	if ffmpegPath := firstNonEmptyEnv(getenv, "XG2G_LLHLS_FFMPEG_PATH", "XG2G_WEB_FFMPEG_PATH"); ffmpegPath != "" {
		llhls.FFmpegPath = ffmpegPath
	}
	if readTrimmedEnv(getenv, "XG2G_LLHLS_HEVC_ENABLED") == "true" || readTrimmedEnv(getenv, "XG2G_WEB_HEVC_PROFILE_ENABLED") == "true" {
		llhls.HevcEnabled = true
	}
	if bitrate := firstNonEmptyEnv(getenv, "XG2G_LLHLS_HEVC_BITRATE", "XG2G_WEB_HEVC_BITRATE"); bitrate != "" {
		llhls.HevcBitrate = bitrate
	}
	if peak := firstNonEmptyEnv(getenv, "XG2G_LLHLS_HEVC_PEAK", "XG2G_WEB_HEVC_MAXBITRATE"); peak != "" {
		llhls.HevcMaxBitrate = peak
	}
	if encoder := firstNonEmptyEnv(getenv, "XG2G_LLHLS_HEVC_ENCODER", "XG2G_WEB_HEVC_ENCODER"); encoder != "" {
		llhls.HevcEncoder = encoder
	}
	if profile := readTrimmedEnv(getenv, "XG2G_LLHLS_HEVC_PROFILE"); profile != "" {
		llhls.HevcProfile = profile
	}
	if level := readTrimmedEnv(getenv, "XG2G_LLHLS_HEVC_LEVEL"); level != "" {
		llhls.HevcLevel = level
	}
	if device := readTrimmedEnv(getenv, "XG2G_LLHLS_VAAPI_DEVICE"); device != "" {
		llhls.VaapiDevice = device
	}
	if duration := readTrimmedEnv(getenv, "XG2G_WEB_LL_HLS_PART_DURATION"); duration != "" {
		llhls.PartDuration = duration
	}

	return llhls
}

func readTranscoderRuntime(getenv func(string) string) TranscoderRuntime {
	// Keep defaults consistent with the legacy proxy package behaviour:
	// - Audio transcoding defaults to enabled for iOS Safari compatibility.
	// - H.264 stream repair defaults to enabled for external player compatibility.
	//
	// Both can be explicitly disabled via ENV.
	audioEnabled := getBool(getenv, "XG2G_ENABLE_AUDIO_TRANSCODING", true)
	h264Repair := getBool(getenv, "XG2G_H264_STREAM_REPAIR", false)
	videoTranscode := getBool(getenv, "XG2G_VIDEO_TRANSCODE", false)
	enabled := audioEnabled || h264Repair || videoTranscode

	channels := 2
	if raw := strings.TrimSpace(getString(getenv, "XG2G_AUDIO_CHANNELS", "")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 1 && n <= 8 {
			channels = n
		}
	}

	return TranscoderRuntime{
		Enabled:           enabled,
		H264RepairEnabled: h264Repair,
		AudioEnabled:      audioEnabled,
		Codec:             getString(getenv, "XG2G_AUDIO_CODEC", "aac"),
		Bitrate:           getString(getenv, "XG2G_AUDIO_BITRATE", "192k"),
		Channels:          channels,
		FFmpegPath:        getString(getenv, "XG2G_FFMPEG_BIN", ""),
		VideoTranscode:    videoTranscode,
		VideoCodec:        getString(getenv, "XG2G_VIDEO_CODEC", ""),
		VAAPIDevice:       getString(getenv, "XG2G_VAAPI_DEVICE", ""),
	}
}

func getString(getenv func(string) string, key, defaultValue string) string {
	if v := getenv(key); v != "" {
		return v
	}
	return defaultValue
}

func readTrimmedEnv(getenv func(string) string, key string) string {
	return strings.TrimSpace(getString(getenv, key, ""))
}

func firstNonEmptyEnv(getenv func(string) string, keys ...string) string {
	for _, key := range keys {
		if value := readTrimmedEnv(getenv, key); value != "" {
			return value
		}
	}
	return ""
}

func applyPositiveInt(getenv func(string) string, key string, target *int) {
	if value, ok := readIntEnv(getenv, key); ok && value > 0 {
		*target = value
	}
}

func applyBoundedInt(getenv func(string) string, key string, min, max int, target *int) {
	if value, ok := readIntEnv(getenv, key); ok && value >= min && value <= max {
		*target = value
	}
}

func readIntEnv(getenv func(string) string, key string) (int, bool) {
	raw := readTrimmedEnv(getenv, key)
	if raw == "" {
		return 0, false
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	return value, true
}

func getInt(getenv func(string) string, key string, defaultValue int) int {
	raw := getenv(key)
	if raw == "" {
		return defaultValue
	}
	i, err := strconv.Atoi(raw)
	if err != nil {
		return defaultValue
	}
	return i
}

func getBool(getenv func(string) string, key string, defaultValue bool) bool {
	raw := getenv(key)
	if raw == "" {
		return defaultValue
	}
	switch strings.ToLower(raw) {
	case "true", "1", "yes":
		return true
	case "false", "0", "no":
		return false
	default:
		return defaultValue
	}
}
