package playback

import "strings"

// Decide determines the playback strategy based on facts using a deterministic logic matrix.
// This function is TOTAL: it handles every input combination with a valid Decision.
// It performs NO side effects (IO/Network).
func Decide(profile ClientProfile, media MediaInfo, policy Policy) (Decision, error) {
	// 0. Invalid Inputs
	if media.AbsPath == "" || media.Duration <= 0 {
		return Decision{Mode: ModeError, Artifact: ArtifactNone, Reason: ReasonProbeFailed}, nil
	}

	// 1. Policy Overrides (e.g. from Settings or URL params)
	if policy.ForceHLS {
		return Decision{Mode: ModeTranscode, Artifact: ArtifactHLS, Reason: ReasonForceHLS}, nil
	}

	// 2. Safari (Mobile/Desktop)
	if profile.IsSafari {
		// Safari requires HLS for MPEG-TS. Native player blocks TS.
		if media.Container == "mpegts" {
			return Decision{Mode: ModeTranscode, Artifact: ArtifactHLS, Reason: ReasonSafariTSNeedsHLS}, nil
		}
		// MP4/MOV is native.
		if isMP4Container(media.Container) {
			// We assume standard usage (H264/HEVC + AAC/AC3) which Safari handles.
			// Ideally we verify codecs, but container check matches v2 behavior and is safe enough for v4 MVP.
			return Decision{Mode: ModeDirectPlay, Artifact: ArtifactMP4, Reason: ReasonSafariDirectMP4}, nil
		}
		// Known unsupported container (MKV etc) -> Transcode
		return Decision{Mode: ModeTranscode, Artifact: ArtifactHLS, Reason: ReasonTranscodeRequired}, nil
	}

	// 3. Chrome
	if profile.IsChrome {
		if isMP4Container(media.Container) {
			// Chrome supports MP4 only if codecs are compatible (H264/VP9/AV1 + AAC/MP3/Opus).
			// Chrome DOES NOT support AC3 in MP4 (usually).
			// So we check stricter constraints.
			if isChromeCompatibleVideo(media.VideoCodec) && isChromeCompatibleAudio(media.AudioCodec) {
				return Decision{Mode: ModeDirectPlay, Artifact: ArtifactMP4, Reason: ReasonChromeDirectMP4}, nil
			}
			return Decision{Mode: ModeTranscode, Artifact: ArtifactHLS, Reason: ReasonTranscodeRequired}, nil
		}
		// Chrome doesn't play MKV/TS well natively
		return Decision{Mode: ModeTranscode, Artifact: ArtifactHLS, Reason: ReasonTranscodeRequired}, nil
	}

	// 4. VLC / Native / Generic Fallback
	// If UserAgent implies a capable player (VLC), allow generic DirectPlay.
	if strings.Contains(profile.UserAgent, "VLC") {
		return Decision{Mode: ModeDirectPlay, Artifact: ArtifactMP4, Reason: ReasonDirectPlayMatch}, nil
	}

	// 5. Generic "Best Effort"
	// If files are MP4, we default to DirectPlay as most modern clients handle it.
	if isMP4Container(media.Container) {
		return Decision{Mode: ModeDirectPlay, Artifact: ArtifactMP4, Reason: ReasonDirectPlayMatch}, nil
	}

	// Default safe fallback: If we don't know the client or the container, safe option is HLS Transcode.
	// This covers TS files on unknown browsers, MKV, etc.
	return Decision{Mode: ModeTranscode, Artifact: ArtifactHLS, Reason: ReasonUnknownContainer}, nil
}

func isMP4Container(c string) bool {
	c = strings.ToLower(c)
	return c == "mp4" || c == "mov" || c == "m4v"
}

func isChromeCompatibleVideo(v string) bool {
	v = strings.ToLower(v)
	// H264, VP8, VP9, AV1
	return v == "h264" || v == "vp8" || v == "vp9" || v == "av1"
}

func isChromeCompatibleAudio(a string) bool {
	a = strings.ToLower(a)
	// AAC, MP3, Opus, FLAC. (AC3 is usually NOT supported in standard Chrome without passthrough)
	return a == "aac" || a == "mp3" || a == "opus" || a == "flac"
}
