// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	streamprofile "github.com/ManuGH/xg2g/internal/core/profile"
)

// Env captures all runtime settings sourced from environment variables.
// It is intended to be read once per load/reload and then treated as immutable.
type Env struct {
	Runtime RuntimeSnapshot
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
		PlaylistFilename: getString(getenv, "XG2G_PLAYLIST_FILENAME", "playlist.m3u"),
		PublicURL:        getString(getenv, "XG2G_PUBLIC_URL", ""),
		XTvgURL:          getString(getenv, "XG2G_X_TVG_URL", ""),
		UseProxyURLs:     getBool(getenv, "XG2G_USE_PROXY_URLS", false),
		ProxyBaseURL:     getString(getenv, "XG2G_PROXY_BASE_URL", "http://localhost:18000"),
		UseHashTvgID:     getBool(getenv, "XG2G_USE_HASH_TVGID", false),
		FFmpegLogLevel:   getString(getenv, "XG2G_FFMPEG_LOGLEVEL", ""),
	}

	rt.OpenWebIF = readOpenWebIFRuntime(getenv)
	rt.Transcoder = readTranscoderRuntime(getenv)
	rt.HLS = readHLSRuntime(getenv)

	return Env{Runtime: rt}, nil
}

func readOpenWebIFRuntime(getenv func(string) string) OpenWebIFRuntime {
	// Defaults mirror previous behaviour in internal/openwebif (but without env reads there).
	forceHTTP2 := strings.ToLower(getString(getenv, "XG2G_HTTP_ENABLE_HTTP2", "true")) != "false"

	return OpenWebIFRuntime{
		HTTPMaxIdleConns:        getInt(getenv, "XG2G_HTTP_MAX_IDLE_CONNS", 100),
		HTTPMaxIdleConnsPerHost: getInt(getenv, "XG2G_HTTP_MAX_IDLE_CONNS_PER_HOST", 20),
		HTTPMaxConnsPerHost:     getInt(getenv, "XG2G_HTTP_MAX_CONNS_PER_HOST", 50),
		HTTPIdleTimeout:         getDuration(getenv, "XG2G_HTTP_IDLE_TIMEOUT", 90*time.Second),
		HTTPEnableHTTP2:         forceHTTP2,
		StreamBaseURL:           getString(getenv, "XG2G_STREAM_BASE", ""),
	}
}

func readHLSRuntime(getenv func(string) string) HLSRuntime {
	outDir := getString(getenv, "XG2G_HLS_OUTPUT_DIR", "")
	if strings.TrimSpace(outDir) == "" {
		outDir = "" // keep proxy default (os.TempDir) unless explicitly configured
	}

	generic := streamprofile.DefaultGenericHLSConfig()
	if v := strings.TrimSpace(getString(getenv, "XG2G_HLS_DVR_SECONDS", "")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			generic.DVRWindowSize = n
		}
	}

	safari := streamprofile.DefaultSafariDVRConfig()
	if v := strings.TrimSpace(getString(getenv, "XG2G_SAFARI_DVR_SEGMENT_DURATION", "")); v != "" {
		// Valid range 2-10s. 2s is optimized for fast startup (Safari), 6s+ for stability.
		if n, err := strconv.Atoi(v); err == nil && n >= 2 && n <= 10 {
			safari.SegmentDuration = n
		}
	}
	if v := strings.TrimSpace(getString(getenv, "XG2G_SAFARI_DVR_WINDOW_SIZE", "")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1800 && n <= 7200 {
			safari.DVRWindowSize = n
		}
	}
	if v := strings.TrimSpace(getString(getenv, "XG2G_SAFARI_DVR_STARTUP_SEGMENTS", "")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 10 {
			safari.StartupSegments = n
		}
	}
	if v := strings.TrimSpace(getString(getenv, "XG2G_SAFARI_DVR_FFMPEG_PATH", "")); v != "" {
		safari.FFmpegPath = v
	} else if v := strings.TrimSpace(getString(getenv, "XG2G_WEB_FFMPEG_PATH", "")); v != "" {
		safari.FFmpegPath = v
	}
	if v := strings.TrimSpace(getString(getenv, "XG2G_SAFARI_DVR_FORCE_AAC", "")); v == "false" {
		safari.ForceAAC = false
	}
	if v := strings.TrimSpace(getString(getenv, "XG2G_SAFARI_DVR_AAC_BITRATE", "")); v != "" {
		safari.AACBitrate = v
	}

	llhls := streamprofile.DefaultLLHLSConfig()
	if v := strings.TrimSpace(getString(getenv, "XG2G_LLHLS_SEGMENT_DURATION", "")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 2 {
			llhls.SegmentDuration = n
		}
	}
	if v := strings.TrimSpace(getString(getenv, "XG2G_LLHLS_PLAYLIST_SIZE", "")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 6 && n <= 10 {
			llhls.PlaylistSize = n
		}
	}
	if v := strings.TrimSpace(getString(getenv, "XG2G_LLHLS_STARTUP_SEGMENTS", "")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 3 {
			llhls.StartupSegments = n
		}
	}
	if v := strings.TrimSpace(getString(getenv, "XG2G_LLHLS_PART_SIZE", "")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 65536 && n <= 1048576 {
			llhls.PartSize = n
		}
	}
	if v := strings.TrimSpace(getString(getenv, "XG2G_LLHLS_FFMPEG_PATH", "")); v != "" {
		llhls.FFmpegPath = v
	} else if v := strings.TrimSpace(getString(getenv, "XG2G_WEB_FFMPEG_PATH", "")); v != "" {
		llhls.FFmpegPath = v
	}

	if strings.TrimSpace(getString(getenv, "XG2G_LLHLS_HEVC_ENABLED", "")) == "true" || strings.TrimSpace(getString(getenv, "XG2G_WEB_HEVC_PROFILE_ENABLED", "")) == "true" {
		llhls.HevcEnabled = true
	}
	if v := strings.TrimSpace(getString(getenv, "XG2G_LLHLS_HEVC_BITRATE", "")); v != "" {
		llhls.HevcBitrate = v
	} else if v := strings.TrimSpace(getString(getenv, "XG2G_WEB_HEVC_BITRATE", "")); v != "" {
		llhls.HevcBitrate = v
	}
	if v := strings.TrimSpace(getString(getenv, "XG2G_LLHLS_HEVC_PEAK", "")); v != "" {
		llhls.HevcMaxBitrate = v
	} else if v := strings.TrimSpace(getString(getenv, "XG2G_WEB_HEVC_MAXBITRATE", "")); v != "" {
		llhls.HevcMaxBitrate = v
	}
	if v := strings.TrimSpace(getString(getenv, "XG2G_LLHLS_HEVC_ENCODER", "")); v != "" {
		llhls.HevcEncoder = v
	} else if v := strings.TrimSpace(getString(getenv, "XG2G_WEB_HEVC_ENCODER", "")); v != "" {
		llhls.HevcEncoder = v
	}
	if v := strings.TrimSpace(getString(getenv, "XG2G_LLHLS_HEVC_PROFILE", "")); v != "" {
		llhls.HevcProfile = v
	}
	if v := strings.TrimSpace(getString(getenv, "XG2G_LLHLS_HEVC_LEVEL", "")); v != "" {
		llhls.HevcLevel = v
	}
	if v := strings.TrimSpace(getString(getenv, "XG2G_LLHLS_VAAPI_DEVICE", "")); v != "" {
		llhls.VaapiDevice = v
	}
	if v := strings.TrimSpace(getString(getenv, "XG2G_WEB_LL_HLS_PART_DURATION", "")); v != "" {
		llhls.PartDuration = v
	}

	return HLSRuntime{
		OutputDir: outDir,
		Generic:   generic,
		Safari:    safari,
		LLHLS:     llhls,
	}
}

func readTranscoderRuntime(getenv func(string) string) TranscoderRuntime {
	// Keep defaults consistent with the legacy proxy package behaviour:
	// - Audio transcoding defaults to enabled for iOS Safari compatibility.
	// - H.264 stream repair defaults to enabled for Plex/Jellyfin compatibility.
	//
	// Both can be explicitly disabled via ENV.
	audioEnabled := getBool(getenv, "XG2G_ENABLE_AUDIO_TRANSCODING", true)
	h264Repair := getBool(getenv, "XG2G_H264_STREAM_REPAIR", false)
	gpuEnabled := getBool(getenv, "XG2G_GPU_TRANSCODE", false)
	videoTranscode := getBool(getenv, "XG2G_VIDEO_TRANSCODE", false)
	enabled := audioEnabled || h264Repair || gpuEnabled || videoTranscode

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
		GPUEnabled:        gpuEnabled,
		TranscoderURL:     getString(getenv, "XG2G_TRANSCODER_URL", ""),
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

func getDuration(getenv func(string) string, key string, defaultValue time.Duration) time.Duration {
	raw := getenv(key)
	if raw == "" {
		return defaultValue
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return defaultValue
	}
	return d
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
