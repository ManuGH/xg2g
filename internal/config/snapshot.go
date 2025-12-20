// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// SPDX-License-Identifier: MIT

package config

import (
	"time"

	streamprofile "github.com/ManuGH/xg2g/internal/core/profile"
)

// Snapshot is the immutable, effective runtime configuration for xg2g.
// It combines the validated AppConfig with additional runtime settings sourced from Env.
type Snapshot struct {
	// Epoch increments monotonically on every successful Swap().
	// It can be used to assert "no mixed config" within a single operation.
	Epoch   uint64
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

// BuildSnapshot builds an effective, immutable runtime snapshot from an already validated AppConfig
// and a previously frozen Env (read once during load/reload).
func BuildSnapshot(app AppConfig, env Env) Snapshot {
	rt := env.Runtime
	rt.StreamProxy.MaxConcurrentStreams = int64(app.MaxConcurrentStreams)
	return Snapshot{App: app, Runtime: rt}
}
