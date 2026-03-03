package vod

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDecideProfile_HEVC(t *testing.T) {
	// HEVC -> Transcode
	info := &StreamInfo{
		Video: VideoStreamInfo{
			CodecName: "hevc",
			PixFmt:    "yuv420p",
			BitDepth:  8,
		},
		Audio: AudioStreamInfo{
			CodecName:  "aac",
			TrackCount: 2,
		},
	}
	profile, reason := DecideProfile(info)
	assert.Equal(t, ProfileHigh, profile, "HEVC should trigger High profile (transcode)")
	assert.Contains(t, reason, "HEVC")
}

func TestDecideProfile_H264_10bit(t *testing.T) {
	// H.264 10-bit -> Transcode
	info := &StreamInfo{
		Video: VideoStreamInfo{
			CodecName: "h264",
			PixFmt:    "yuv420p10le",
			BitDepth:  10,
		},
	}
	profile, reason := DecideProfile(info)
	assert.Equal(t, ProfileHigh, profile, "10-bit video should trigger High profile")
	assert.Contains(t, reason, "10-bit")
}

func TestDecideProfile_H264_8bit_AAC(t *testing.T) {
	// Standard -> Default (Copy)
	info := &StreamInfo{
		Video: VideoStreamInfo{CodecName: "h264", PixFmt: "yuv420p", BitDepth: 8},
		Audio: AudioStreamInfo{CodecName: "aac"},
	}
	profile, _ := DecideProfile(info)
	assert.Equal(t, ProfileDefault, profile, "Standard H264/AAC should use Default profile")
}

func TestDecideProfile_MPEG2(t *testing.T) {
	// MPEG2 -> Transcode
	info := &StreamInfo{
		Video: VideoStreamInfo{CodecName: "mpeg2video", PixFmt: "yuv420p"},
	}
	profile, reason := DecideProfile(info)
	assert.Equal(t, ProfileHigh, profile, "MPEG2 should trigger High profile")
	assert.Contains(t, reason, "MPEG2")
}
