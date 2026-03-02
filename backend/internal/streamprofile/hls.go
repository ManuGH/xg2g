// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package streamprofile

// LLHLSConfig holds configuration for LL-HLS profile.
type LLHLSConfig struct {
	SegmentDuration int    // Segment duration in seconds (1-2)
	PlaylistSize    int    // Number of segments in playlist (6-10)
	StartupSegments int    // Pre-buffer segments before serving (2-3)
	PartSize        int    // Partial segment size in bytes (256KB default)
	FFmpegPath      string // Path to FFmpeg binary

	// HEVC / Transcoding Config
	HevcEnabled    bool   // Enable HEVC transcoding
	HevcBitrate    string // Target video bitrate (e.g. "6000k")
	HevcMaxBitrate string // Max video bitrate (e.g. "8000k")
	HevcEncoder    string // Encoder name (hevc_nvenc, hevc_vaapi, libx265)
	HevcProfile    string // Encoding profile (main, main10)
	HevcLevel      string // Encoding level (5.0, 5.1)
	VaapiDevice    string // VAAPI Device path (default /dev/dri/renderD128)
	PartDuration   string // Partial segment duration (ms, e.g. "200ms")
}

func DefaultLLHLSConfig() LLHLSConfig {
	return LLHLSConfig{
		SegmentDuration: 1,
		PlaylistSize:    6,
		StartupSegments: 2,
		PartSize:        262144,
		FFmpegPath:      "/usr/bin/ffmpeg",
		HevcEnabled:     false,
		HevcBitrate:     "6000k",
		HevcMaxBitrate:  "8000k",
		HevcEncoder:     "hevc_nvenc",
		HevcProfile:     "main",
		HevcLevel:       "5.0",
		VaapiDevice:     "/dev/dri/renderD128",
		PartDuration:    "200ms",
	}
}

// SafariDVRConfig holds configuration for Safari DVR HLS profile.
type SafariDVRConfig struct {
	SegmentDuration int    // Segment duration in seconds (6 recommended)
	DVRWindowSize   int    // DVR window in seconds (1800-2700)
	StartupSegments int    // Pre-buffer segments before serving (3-5)
	FFmpegPath      string // Path to FFmpeg binary
	ForceAAC        bool   // Force AAC audio transcoding
	AACBitrate      string // AAC bitrate (e.g., "192k")
}

func DefaultSafariDVRConfig() SafariDVRConfig {
	return SafariDVRConfig{
		SegmentDuration: 6,
		DVRWindowSize:   2700,
		StartupSegments: 2,
		FFmpegPath:      "/usr/bin/ffmpeg",
		ForceAAC:        true,
		AACBitrate:      "192k",
	}
}

// GenericHLSConfig holds configuration for the generic MPEG-TS HLS profile.
type GenericHLSConfig struct {
	SegmentDuration int // seconds
	DVRWindowSize   int // seconds
}

func DefaultGenericHLSConfig() GenericHLSConfig {
	return GenericHLSConfig{
		SegmentDuration: 2,
		DVRWindowSize:   1800,
	}
}
