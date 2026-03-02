package vod

import "strings"

// DecideProfile determines the best transcoding profile based on stream info.
// Implements the "Chrome First" policy.
func DecideProfile(info *StreamInfo) (Profile, string) {
	// Video Policy
	if info.Video.CodecName == "hevc" || info.Video.CodecName == "h265" {
		return ProfileHigh, "HEVC detected - Chrome incompatible"
	}

	// 10-bit H.264
	if info.Video.BitDepth >= 10 || strings.Contains(info.Video.PixFmt, "10") {
		return ProfileHigh, "10-bit H.264 detected - Chrome incompatible"
	}

	// MPEG-2
	if info.Video.CodecName == "mpeg2video" {
		return ProfileHigh, "MPEG2 detected - Browser compatibility concern"
	}

	// Audio Policy: Always Ensure AAC implies...
	// Actually ProfileDefault usually copies video and transcodes audio if needed?
	// The previous logic had "StrategyDefault" which did copy video + transcode audio if needed.
	// Our new "ProfileDefault" implies "Smart Copy" (Copy Video, Ensure AAC).
	// Infra/Builder must handle "ProfileDefault" = "Copy Video, AAC Audio".

	return ProfileDefault, "Safe for Smart Copy"
}
