package vod_test

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/control/vod"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile/ports"
	"github.com/stretchr/testify/assert"
)

func TestValidateSourceTruth(t *testing.T) {
	tests := []struct {
		name     string
		truth    ports.SourceProfile
		probed   vod.StreamInfo
		expected *ports.TruthMismatch
	}{
		{
			name:   "zero truth",
			truth:  ports.SourceProfile{},
			probed: vod.StreamInfo{Container: "mp4", Video: vod.VideoStreamInfo{CodecName: "h264", Height: 1080}},
			expected: nil,
		},
		{
			name:   "exact match",
			truth:  ports.SourceProfile{VideoCodec: "h264", AudioCodec: "aac", Container: "mp4", BitDepth: 8, Height: 1080},
			probed: vod.StreamInfo{Container: "mp4", Video: vod.VideoStreamInfo{CodecName: "h264", BitDepth: 8, Height: 1080}, Audio: vod.AudioStreamInfo{CodecName: "aac"}},
			expected: nil,
		},
		{
			name:   "video codec mismatch",
			truth:  ports.SourceProfile{VideoCodec: "h264"},
			probed: vod.StreamInfo{Video: vod.VideoStreamInfo{CodecName: "hevc"}},
			expected: &ports.TruthMismatch{Field: "videoCodec", Expected: "h264", Actual: "hevc"},
		},
		{
			name:   "audio codec mismatch",
			truth:  ports.SourceProfile{AudioCodec: "aac"},
			probed: vod.StreamInfo{Audio: vod.AudioStreamInfo{CodecName: "ac3"}},
			expected: &ports.TruthMismatch{Field: "audioCodec", Expected: "aac", Actual: "ac3"},
		},
		{
			name:   "container exact mismatch",
			truth:  ports.SourceProfile{Container: "mp4"},
			probed: vod.StreamInfo{Container: "mpegts"},
			expected: &ports.TruthMismatch{Field: "container", Expected: "mp4", Actual: "mpegts"},
		},
		{
			name:   "container comma separated match",
			truth:  ports.SourceProfile{Container: "mp4"},
			probed: vod.StreamInfo{Container: "mov,mp4,m4a,3gp,3g2,mj2"},
			expected: nil,
		},
		{
			name:   "bit depth mismatch",
			truth:  ports.SourceProfile{BitDepth: 10},
			probed: vod.StreamInfo{Video: vod.VideoStreamInfo{BitDepth: 8}},
			expected: &ports.TruthMismatch{Field: "bitDepth", Expected: "10", Actual: "8"},
		},
		{
			name:   "resolution class mismatch SD to HD",
			truth:  ports.SourceProfile{Height: 480},
			probed: vod.StreamInfo{Video: vod.VideoStreamInfo{Height: 720}},
			expected: &ports.TruthMismatch{Field: "resolution", Expected: "SD", Actual: "HD"},
		},
		{
			name:   "resolution class mismatch HD to FHD",
			truth:  ports.SourceProfile{Height: 720},
			probed: vod.StreamInfo{Video: vod.VideoStreamInfo{Height: 1080}},
			expected: &ports.TruthMismatch{Field: "resolution", Expected: "HD", Actual: "FHD"},
		},
		{
			name:   "resolution soft deviation within HD class",
			truth:  ports.SourceProfile{Height: 720},
			probed: vod.StreamInfo{Video: vod.VideoStreamInfo{Height: 800}},
			expected: nil, // Soft mismatch
		},
		{
			name:   "resolution soft deviation within FHD class",
			truth:  ports.SourceProfile{Height: 1080},
			probed: vod.StreamInfo{Video: vod.VideoStreamInfo{Height: 1200}},
			expected: nil, // Soft mismatch
		},
		{
			name:   "duration deviation soft",
			truth:  ports.SourceProfile{Duration: 100.0},
			probed: vod.StreamInfo{Video: vod.VideoStreamInfo{Duration: 105.0}},
			expected: nil, // Always soft
		},
		{
			name:   "bitrate deviation soft",
			truth:  ports.SourceProfile{BitrateKbps: 5000},
			probed: vod.StreamInfo{BitrateKbps: 6000},
			expected: nil, // Always soft
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mismatch := vod.ValidateSourceTruth(tt.truth, tt.probed)
			assert.Equal(t, tt.expected, mismatch)
		})
	}
}
