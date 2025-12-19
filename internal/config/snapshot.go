// SPDX-License-Identifier: MIT

package config

import (
	"strconv"
	"strings"
	"time"

	streamprofile "github.com/ManuGH/xg2g/internal/core/profile"
)

// Snapshot is the immutable, effective runtime configuration for xg2g.
// It combines the validated AppConfig with additional runtime settings sourced from ENV.
type Snapshot struct {
	App     AppConfig
	Runtime RuntimeSnapshot
}

type RuntimeSnapshot struct {
	PlaylistFilename string
	PublicURL        string
	XTvgURL          string
	UseProxyURLs     bool
	ProxyBaseURL     string
	UseHashTvgID     bool

	StreamProxy StreamProxyRuntime
	OpenWebIF   OpenWebIFRuntime
	Transcoder  TranscoderRuntime
	HLS         HLSRuntime

	FFmpegLogLevel string
}

type StreamProxyRuntime struct {
	Enabled bool

	ListenAddr string // e.g. ":18000"
	TargetURL  string // optional

	// For API-side reverse proxying to the stream proxy (split deployments).
	UpstreamHost string // default: "127.0.0.1"

	MaxConcurrentStreams int64
	TranscodeFailOpen    bool
	IdleTimeout          time.Duration
}

type OpenWebIFRuntime struct {
	HTTPMaxIdleConns        int
	HTTPMaxIdleConnsPerHost int
	HTTPMaxConnsPerHost     int
	HTTPIdleTimeout         time.Duration
	HTTPEnableHTTP2         bool

	StreamBaseURL string
}

type TranscoderRuntime struct {
	Enabled bool

	H264RepairEnabled bool
	AudioEnabled      bool

	Codec      string
	Bitrate    string
	Channels   int
	FFmpegPath string

	GPUEnabled    bool
	TranscoderURL string

	UseRustRemuxer bool

	VideoTranscode bool
	VideoCodec     string
	VAAPIDevice    string
}

type HLSRuntime struct {
	OutputDir string

	Generic streamprofile.GenericHLSConfig
	Safari  streamprofile.SafariDVRConfig
	LLHLS   streamprofile.LLHLSConfig
}

// BuildSnapshot builds an effective, immutable runtime snapshot from an already validated AppConfig.
func BuildSnapshot(app AppConfig) Snapshot {
	rt := RuntimeSnapshot{
		PlaylistFilename: ParseString("XG2G_PLAYLIST_FILENAME", "playlist.m3u"),
		PublicURL:        ParseString("XG2G_PUBLIC_URL", ""),
		XTvgURL:          ParseString("XG2G_X_TVG_URL", ""),
		UseProxyURLs:     ParseBool("XG2G_USE_PROXY_URLS", false),
		ProxyBaseURL:     ParseString("XG2G_PROXY_BASE_URL", "http://localhost:18000"),
		UseHashTvgID:     ParseBool("XG2G_USE_HASH_TVGID", false),
		FFmpegLogLevel:   ParseString("XG2G_FFMPEG_LOGLEVEL", ""),
	}

	rt.StreamProxy = buildStreamProxyRuntime(app)
	rt.OpenWebIF = buildOpenWebIFRuntime()
	rt.Transcoder = buildTranscoderRuntime()
	rt.HLS = buildHLSRuntime(app)

	return Snapshot{App: app, Runtime: rt}
}

func buildStreamProxyRuntime(app AppConfig) StreamProxyRuntime {
	listen := ParseString("XG2G_PROXY_LISTEN", "")
	if strings.TrimSpace(listen) == "" {
		if port := strings.TrimSpace(ParseString("XG2G_PROXY_PORT", "")); port != "" {
			listen = ":" + port
		} else {
			listen = ":18000"
		}
	}

	return StreamProxyRuntime{
		Enabled:              ParseBool("XG2G_ENABLE_STREAM_PROXY", true),
		ListenAddr:           listen,
		TargetURL:            ParseString("XG2G_PROXY_TARGET", ""),
		UpstreamHost:         ParseString("XG2G_PROXY_HOST", "127.0.0.1"),
		MaxConcurrentStreams: int64(app.MaxConcurrentStreams),
		TranscodeFailOpen:    ParseBool("XG2G_TRANSCODE_FAIL_OPEN", false),
		IdleTimeout:          parseDurationOrSeconds("XG2G_PROXY_IDLE_TIMEOUT", 0),
	}
}

func buildOpenWebIFRuntime() OpenWebIFRuntime {
	// Defaults mirror previous behaviour in internal/openwebif (but without env reads there).
	forceHTTP2 := strings.ToLower(ParseString("XG2G_HTTP_ENABLE_HTTP2", "true")) != "false"

	return OpenWebIFRuntime{
		HTTPMaxIdleConns:        ParseInt("XG2G_HTTP_MAX_IDLE_CONNS", 100),
		HTTPMaxIdleConnsPerHost: ParseInt("XG2G_HTTP_MAX_IDLE_CONNS_PER_HOST", 20),
		HTTPMaxConnsPerHost:     ParseInt("XG2G_HTTP_MAX_CONNS_PER_HOST", 50),
		HTTPIdleTimeout:         ParseDuration("XG2G_HTTP_IDLE_TIMEOUT", 90*time.Second),
		HTTPEnableHTTP2:         forceHTTP2,
		StreamBaseURL:           ParseString("XG2G_STREAM_BASE", ""),
	}
}

func buildHLSRuntime(app AppConfig) HLSRuntime {
	outDir := ParseString("XG2G_HLS_OUTPUT_DIR", "")
	if strings.TrimSpace(outDir) == "" {
		outDir = "" // keep proxy default (os.TempDir) unless explicitly configured
	}

	generic := streamprofile.DefaultGenericHLSConfig()
	if v := strings.TrimSpace(ParseString("XG2G_HLS_DVR_SECONDS", "")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			generic.DVRWindowSize = n
		}
	}

	safari := streamprofile.DefaultSafariDVRConfig()
	if v := strings.TrimSpace(ParseString("XG2G_SAFARI_DVR_SEGMENT_DURATION", "")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 4 && n <= 10 {
			safari.SegmentDuration = n
		}
	}
	if v := strings.TrimSpace(ParseString("XG2G_SAFARI_DVR_WINDOW_SIZE", "")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1800 && n <= 7200 {
			safari.DVRWindowSize = n
		}
	}
	if v := strings.TrimSpace(ParseString("XG2G_SAFARI_DVR_STARTUP_SEGMENTS", "")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 10 {
			safari.StartupSegments = n
		}
	}
	if v := strings.TrimSpace(ParseString("XG2G_SAFARI_DVR_FFMPEG_PATH", "")); v != "" {
		safari.FFmpegPath = v
	} else if v := strings.TrimSpace(ParseString("XG2G_WEB_FFMPEG_PATH", "")); v != "" {
		safari.FFmpegPath = v
	}
	if v := strings.TrimSpace(ParseString("XG2G_SAFARI_DVR_FORCE_AAC", "")); v == "false" {
		safari.ForceAAC = false
	}
	if v := strings.TrimSpace(ParseString("XG2G_SAFARI_DVR_AAC_BITRATE", "")); v != "" {
		safari.AACBitrate = v
	}

	llhls := streamprofile.DefaultLLHLSConfig()
	if v := strings.TrimSpace(ParseString("XG2G_LLHLS_SEGMENT_DURATION", "")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 2 {
			llhls.SegmentDuration = n
		}
	}
	if v := strings.TrimSpace(ParseString("XG2G_LLHLS_PLAYLIST_SIZE", "")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 6 && n <= 10 {
			llhls.PlaylistSize = n
		}
	}
	if v := strings.TrimSpace(ParseString("XG2G_LLHLS_STARTUP_SEGMENTS", "")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 3 {
			llhls.StartupSegments = n
		}
	}
	if v := strings.TrimSpace(ParseString("XG2G_LLHLS_PART_SIZE", "")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 65536 && n <= 1048576 {
			llhls.PartSize = n
		}
	}
	if v := strings.TrimSpace(ParseString("XG2G_LLHLS_FFMPEG_PATH", "")); v != "" {
		llhls.FFmpegPath = v
	} else if v := strings.TrimSpace(ParseString("XG2G_WEB_FFMPEG_PATH", "")); v != "" {
		llhls.FFmpegPath = v
	}

	if strings.TrimSpace(ParseString("XG2G_LLHLS_HEVC_ENABLED", "")) == "true" || strings.TrimSpace(ParseString("XG2G_WEB_HEVC_PROFILE_ENABLED", "")) == "true" {
		llhls.HevcEnabled = true
	}
	if v := strings.TrimSpace(ParseString("XG2G_LLHLS_HEVC_BITRATE", "")); v != "" {
		llhls.HevcBitrate = v
	} else if v := strings.TrimSpace(ParseString("XG2G_WEB_HEVC_BITRATE", "")); v != "" {
		llhls.HevcBitrate = v
	}
	if v := strings.TrimSpace(ParseString("XG2G_LLHLS_HEVC_PEAK", "")); v != "" {
		llhls.HevcMaxBitrate = v
	} else if v := strings.TrimSpace(ParseString("XG2G_WEB_HEVC_MAXBITRATE", "")); v != "" {
		llhls.HevcMaxBitrate = v
	}
	if v := strings.TrimSpace(ParseString("XG2G_LLHLS_HEVC_ENCODER", "")); v != "" {
		llhls.HevcEncoder = v
	} else if v := strings.TrimSpace(ParseString("XG2G_WEB_HEVC_ENCODER", "")); v != "" {
		llhls.HevcEncoder = v
	}
	if v := strings.TrimSpace(ParseString("XG2G_LLHLS_HEVC_PROFILE", "")); v != "" {
		llhls.HevcProfile = v
	}
	if v := strings.TrimSpace(ParseString("XG2G_LLHLS_HEVC_LEVEL", "")); v != "" {
		llhls.HevcLevel = v
	}
	if v := strings.TrimSpace(ParseString("XG2G_LLHLS_VAAPI_DEVICE", "")); v != "" {
		llhls.VaapiDevice = v
	}
	if v := strings.TrimSpace(ParseString("XG2G_WEB_LL_HLS_PART_DURATION", "")); v != "" {
		llhls.PartDuration = v
	}

	// Keep profiles in sync with global data dir where relevant.
	_ = app

	return HLSRuntime{
		OutputDir: outDir,
		Generic:   generic,
		Safari:    safari,
		LLHLS:     llhls,
	}
}

func buildTranscoderRuntime() TranscoderRuntime {
	// Keep defaults consistent with the legacy proxy package behaviour:
	// - Audio transcoding defaults to enabled for iOS Safari compatibility.
	// - H.264 stream repair defaults to enabled for Plex/Jellyfin compatibility.
	//
	// Both can be explicitly disabled via ENV.
	audioEnabled := ParseBool("XG2G_ENABLE_AUDIO_TRANSCODING", true)
	h264Repair := ParseBool("XG2G_H264_STREAM_REPAIR", false)
	gpuEnabled := ParseBool("XG2G_GPU_TRANSCODE", false)
	videoTranscode := ParseBool("XG2G_VIDEO_TRANSCODE", false)
	enabled := audioEnabled || h264Repair || gpuEnabled || videoTranscode

	channels := 2
	if raw := strings.TrimSpace(ParseString("XG2G_AUDIO_CHANNELS", "")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 1 && n <= 8 {
			channels = n
		}
	}

	return TranscoderRuntime{
		Enabled:           enabled,
		H264RepairEnabled: h264Repair,
		AudioEnabled:      audioEnabled,
		Codec:             ParseString("XG2G_AUDIO_CODEC", "aac"),
		Bitrate:           ParseString("XG2G_AUDIO_BITRATE", "192k"),
		Channels:          channels,
		FFmpegPath:        ParseString("XG2G_FFMPEG_PATH", ""),
		GPUEnabled:        gpuEnabled,
		TranscoderURL:     ParseString("XG2G_TRANSCODER_URL", ""),
		UseRustRemuxer:    ParseBool("XG2G_USE_RUST_REMUXER", true),
		VideoTranscode:    videoTranscode,
		VideoCodec:        ParseString("XG2G_VIDEO_CODEC", ""),
		VAAPIDevice:       ParseString("XG2G_VAAPI_DEVICE", ""),
	}
}

func parseDurationOrSeconds(key string, def time.Duration) time.Duration {
	raw := strings.TrimSpace(ParseString(key, ""))
	if raw == "" {
		return def
	}
	if d, err := time.ParseDuration(raw); err == nil {
		return d
	}
	if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
		return time.Duration(n) * time.Second
	}
	return def
}
