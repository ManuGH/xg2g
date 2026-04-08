// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ports

// MediaMode describes how a specific media stream should be handled.
type MediaMode string

const (
	MediaModeUnknown   MediaMode = ""
	MediaModeCopy      MediaMode = "copy"
	MediaModeTranscode MediaMode = "transcode"
	MediaModeDisabled  MediaMode = "disabled"
)

// Packaging describes the outer transport/container packaging for playback.
type Packaging string

const (
	PackagingUnknown Packaging = ""
	PackagingTS      Packaging = "ts"
	PackagingFMP4    Packaging = "fmp4"
	PackagingMP4     Packaging = "mp4"
)

// HWAccel describes the acceleration path chosen for transcoding.
type HWAccel string

const (
	HWAccelUnknown HWAccel = ""
	HWAccelNone    HWAccel = "none"
	HWAccelVAAPI   HWAccel = "vaapi"
	HWAccelNVENC   HWAccel = "nvenc"
)

// VideoConstraints captures upper bounds provided by the client.
type VideoConstraints struct {
	Width  int     `json:"width,omitempty"`
	Height int     `json:"height,omitempty"`
	FPS    float64 `json:"fps,omitempty"`
}

// SourceProfile captures truthful source media properties.
type SourceProfile struct {
	Container        string  `json:"container,omitempty"`
	VideoCodec       string  `json:"videoCodec,omitempty"`
	AudioCodec       string  `json:"audioCodec,omitempty"`
	BitrateKbps      int     `json:"bitrateKbps,omitempty"`
	Width            int     `json:"width,omitempty"`
	Height           int     `json:"height,omitempty"`
	FPS              float64 `json:"fps,omitempty"`
	Interlaced       bool    `json:"interlaced,omitempty"`
	AudioChannels    int     `json:"audioChannels,omitempty"`
	AudioBitrateKbps int     `json:"audioBitrateKbps,omitempty"`
}

// ClientPlaybackProfile describes the effective playback path on the client.
type ClientPlaybackProfile struct {
	DeviceType     string            `json:"deviceType,omitempty"`
	PlaybackEngine string            `json:"playbackEngine,omitempty"`
	Containers     []string          `json:"containers"`
	VideoCodecs    []string          `json:"videoCodecs"`
	AudioCodecs    []string          `json:"audioCodecs"`
	HLSPackaging   []string          `json:"hlsPackaging"`
	SupportsHLS    bool              `json:"supportsHls"`
	SupportsRange  bool              `json:"supportsRange"`
	AllowTranscode *bool             `json:"allowTranscode,omitempty"`
	MaxVideo       *VideoConstraints `json:"maxVideo,omitempty"`
}

// ServerTranscodeCapabilities describes what the running xg2g host can execute.
type ServerTranscodeCapabilities struct {
	FFmpegAvailable    bool     `json:"ffmpegAvailable"`
	HLSAvailable       bool     `json:"hlsAvailable"`
	HasVAAPI           bool     `json:"hasVaapi"`
	VAAPIReady         bool     `json:"vaapiReady"`
	HasNVENC           bool     `json:"hasNvenc"`
	NVENCReady         bool     `json:"nvencReady"`
	HardwareVideoCodec []string `json:"hardwareVideoCodecs"`
}

// HostCPUSnapshot captures read-only runtime CPU load context for playback decisions.
type HostCPUSnapshot struct {
	Load1m        float64 `json:"load1m,omitempty"`
	CoreCount     int     `json:"coreCount,omitempty"`
	SampleCount   int     `json:"sampleCount,omitempty"`
	WindowSeconds int     `json:"windowSeconds,omitempty"`
}

// HostConcurrencySnapshot captures read-only runtime concurrency context for playback decisions.
type HostConcurrencySnapshot struct {
	TunersAvailable   int `json:"tunersAvailable,omitempty"`
	SessionsActive    int `json:"sessionsActive,omitempty"`
	TranscodesActive  int `json:"transcodesActive,omitempty"`
	ActiveVAAPITokens int `json:"activeVaapiTokens,omitempty"`
	MaxSessions       int `json:"maxSessions,omitempty"`
	MaxVAAPITokens    int `json:"maxVaapiTokens,omitempty"`
}

// HostRuntimeSnapshot combines static executable capabilities and current runtime pressure inputs.
type HostRuntimeSnapshot struct {
	Capabilities ServerTranscodeCapabilities `json:"capabilities"`
	CPU          HostCPUSnapshot             `json:"cpu"`
	Concurrency  HostConcurrencySnapshot     `json:"concurrency"`
}

// VideoTarget describes the selected output video path.
type VideoTarget struct {
	Mode        MediaMode `json:"mode,omitempty"`
	Codec       string    `json:"codec,omitempty"`
	BitrateKbps int       `json:"bitrateKbps,omitempty"`
	CRF         int       `json:"crf,omitempty"`
	Preset      string    `json:"preset,omitempty"`
	Width       int       `json:"width,omitempty"`
	Height      int       `json:"height,omitempty"`
	FPS         float64   `json:"fps,omitempty"`
}

// AudioTarget describes the selected output audio path.
type AudioTarget struct {
	Mode        MediaMode `json:"mode,omitempty"`
	Codec       string    `json:"codec,omitempty"`
	Channels    int       `json:"channels,omitempty"`
	BitrateKbps int       `json:"bitrateKbps,omitempty"`
	SampleRate  int       `json:"sampleRate,omitempty"`
}

// HLSTarget carries HLS-specific delivery choices.
type HLSTarget struct {
	Enabled          bool   `json:"enabled"`
	SegmentContainer string `json:"segmentContainer,omitempty"`
	SegmentSeconds   int    `json:"segmentSeconds,omitempty"`
}

// TargetPlaybackProfile is the concrete output profile later consumed by the builder and cache.
type TargetPlaybackProfile struct {
	Container string      `json:"container,omitempty"`
	Packaging Packaging   `json:"packaging,omitempty"`
	Video     VideoTarget `json:"video"`
	Audio     AudioTarget `json:"audio"`
	HLS       HLSTarget   `json:"hls"`
	HWAccel   HWAccel     `json:"hwaccel,omitempty"`
}
