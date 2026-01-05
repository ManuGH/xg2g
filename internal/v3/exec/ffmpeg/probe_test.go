// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ffmpeg

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStreamProbeResult_CanRemux(t *testing.T) {
	tests := []struct {
		name      string
		probe     StreamProbeResult
		maxWidth  int
		canRemux  bool
		reason    string
	}{
		{
			name: "H264 AAC Progressive - OK",
			probe: StreamProbeResult{
				CodecName:      "h264",
				AudioCodecName: "aac",
				Interlaced:     false,
				Width:          1920,
				Height:         1080,
			},
			maxWidth: 0,
			canRemux: true,
			reason:   "Compatible codecs, progressive",
		},
		{
			name: "HEVC AAC Progressive - OK",
			probe: StreamProbeResult{
				CodecName:      "hevc",
				AudioCodecName: "aac",
				Interlaced:     false,
				Width:          1920,
				Height:         1080,
			},
			maxWidth: 0,
			canRemux: true,
			reason:   "HEVC compatible",
		},
		{
			name: "H264 MP3 Progressive - OK",
			probe: StreamProbeResult{
				CodecName:      "h264",
				AudioCodecName: "mp3",
				Interlaced:     false,
				Width:          1920,
				Height:         1080,
			},
			maxWidth: 0,
			canRemux: true,
			reason:   "MP3 audio acceptable",
		},
		{
			name: "H264 AC3 - TRANSCODE (audio)",
			probe: StreamProbeResult{
				CodecName:      "h264",
				AudioCodecName: "ac3",
				Interlaced:     false,
				Width:          1920,
				Height:         1080,
			},
			maxWidth: 0,
			canRemux: false,
			reason:   "AC3 requires transcode to AAC for Safari",
		},
		{
			name: "MPEG2 AAC - TRANSCODE (video)",
			probe: StreamProbeResult{
				CodecName:      "mpeg2video",
				AudioCodecName: "aac",
				Interlaced:     false,
				Width:          1920,
				Height:         1080,
			},
			maxWidth: 0,
			canRemux: false,
			reason:   "MPEG2 not compatible",
		},
		{
			name: "H264 Interlaced - TRANSCODE (deinterlace)",
			probe: StreamProbeResult{
				CodecName:      "h264",
				AudioCodecName: "aac",
				Interlaced:     true,
				Width:          1920,
				Height:         1080,
			},
			maxWidth: 0,
			canRemux: false,
			reason:   "Interlaced requires deinterlacing",
		},
		{
			name: "H264 4K with 1080p limit - TRANSCODE (scale)",
			probe: StreamProbeResult{
				CodecName:      "h264",
				AudioCodecName: "aac",
				Interlaced:     false,
				Width:          3840,
				Height:         2160,
			},
			maxWidth: 1920,
			canRemux: false,
			reason:   "Exceeds maxWidth limit",
		},
		{
			name: "H264 1080p with 1080p limit - OK",
			probe: StreamProbeResult{
				CodecName:      "h264",
				AudioCodecName: "aac",
				Interlaced:     false,
				Width:          1920,
				Height:         1080,
			},
			maxWidth: 1920,
			canRemux: true,
			reason:   "Within width limit",
		},
		{
			name: "H264 no audio - OK",
			probe: StreamProbeResult{
				CodecName:      "h264",
				AudioCodecName: "",
				Interlaced:     false,
				Width:          1920,
				Height:         1080,
			},
			maxWidth: 0,
			canRemux: true,
			reason:   "Video-only stream acceptable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.probe.CanRemux(tt.maxWidth)
			assert.Equal(t, tt.canRemux, result, "Reason: %s", tt.reason)
		})
	}
}
