package playbackplanner

import "strings"

// hasValidEvidence checks if the evidence is structurally sound.
func hasValidEvidence(ev PlaybackEvidence) bool {
	if ev.OperatorPolicy.DVRWindowSeconds < 0 {
		return false
	}
	if ev.SourceIdentity == "" {
		return false
	}
	if ev.SourceTruth.Container == "" || ev.SourceTruth.Container == "unknown" {
		return false
	}
	if ev.SourceTruth.VideoCodec == "unknown" || ev.SourceTruth.AudioCodec == "unknown" {
		return false
	}
	seenEncoderCodecs := make(map[string]struct{}, len(ev.HostSnapshot.EncoderCapabilities))
	for _, encoder := range ev.HostSnapshot.EncoderCapabilities {
		codec := strings.ToLower(strings.TrimSpace(encoder.Codec))
		if codec == "" {
			return false
		}
		if _, duplicate := seenEncoderCodecs[codec]; duplicate {
			// The evidence hash treats encoder-capability order as non-semantic.
			// Reject duplicate codec rows so Plan() can never resolve contradictory
			// last-wins values from two inputs with the same canonical hash.
			return false
		}
		seenEncoderCodecs[codec] = struct{}{}
	}
	return true
}

// isSourceTruthFresh returns true if the evidence isn't stale compared to its validity window.
func isSourceTruthFresh(ev PlaybackEvidence) bool {
	if ev.Confidence == "stale" {
		return false
	}
	if ev.ValidUntil > 0 && ev.EvaluatedAt > ev.ValidUntil {
		return false
	}
	return true
}

func contains(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}

// isContainerCompatible checks if the client explicitly supports the source container.
func isContainerCompatible(ev PlaybackEvidence) bool {
	return contains(ev.ClientEvidence.SupportedContainers, ev.SourceTruth.Container)
}

// isVideoCodecCompatible checks if the client explicitly supports the source video codec.
func isVideoCodecCompatible(ev PlaybackEvidence) bool {
	if ev.SourceTruth.VideoCodec == "" {
		return true // No video
	}
	return contains(ev.ClientEvidence.SupportedVideoCodecs, ev.SourceTruth.VideoCodec)
}

// isAudioCodecCompatible checks if the client explicitly supports the source audio codec.
func isAudioCodecCompatible(ev PlaybackEvidence) bool {
	if ev.SourceTruth.AudioCodec == "" {
		return true // No audio
	}
	// Browser audio caps over-report Dolby: WebKit's canPlayType/isTypeSupported
	// answers "maybe" for ac-3 while neither Safari MSE nor hls.js can actually
	// decode it — copied AC-3 plays back as silence (observed on ORF2, whose
	// first audio track is AC-3 5.1). Never copy AC-3/E-AC-3 for browser
	// clients; the AAC transcode path is the audible one.
	switch strings.ToLower(strings.TrimSpace(ev.SourceTruth.AudioCodec)) {
	case "ac3", "ac-3", "eac3", "ec-3":
		if isBrowserClient(ev.ClientEvidence) {
			return false
		}
	}
	return contains(ev.ClientEvidence.SupportedAudioCodecs, ev.SourceTruth.AudioCodec)
}

// isBrowserClient reports whether the evidence describes a browser-hosted
// player (MSE/hls.js or native WebKit HLS) as opposed to a native app player
// (e.g. ExoPlayer) that owns its own decoders.
func isBrowserClient(ce ClientEvidence) bool {
	if strings.EqualFold(strings.TrimSpace(ce.PreferredEngine), "hlsjs") {
		return true
	}
	family := strings.ToLower(ce.Family)
	for _, marker := range []string{"safari", "chrom", "firefox", "webkit", "ios"} {
		if strings.Contains(family, marker) {
			return true
		}
	}
	return false
}

// exceedsMaxVideoLimits returns true if the source resolution/framerate exceeds client caps.
func exceedsMaxVideoLimits(ev PlaybackEvidence) bool {
	ce := ev.ClientEvidence
	st := ev.SourceTruth

	if st.Width > 0 && ce.MaxVideoWidth > 0 && st.Width > ce.MaxVideoWidth {
		return true
	}
	if st.Height > 0 && ce.MaxVideoHeight > 0 && st.Height > ce.MaxVideoHeight {
		return true
	}
	if st.FPS > 0 && ce.MaxVideoFPS > 0 && st.FPS > ce.MaxVideoFPS {
		return true
	}
	return false
}

// requiresInterlaceRepair returns true if the source is interlaced and needs fixing.
func requiresInterlaceRepair(ev PlaybackEvidence) bool {
	return ev.SourceTruth.Interlaced
}

// supportsHLS returns true if the client engine supports HLS.
func supportsHLS(ev PlaybackEvidence) bool {
	return ev.ClientEvidence.SupportsHls
}

// supportsRange returns true if the client supports HTTP range requests.
func supportsRange(ev PlaybackEvidence) bool {
	if ev.ClientEvidence.SupportsRange != nil {
		return *ev.ClientEvidence.SupportsRange
	}
	return false // conservative fallback
}

func requiresPlannedTranscode(ev PlaybackEvidence) bool {
	requested := strings.ToLower(strings.TrimSpace(ev.OperatorPolicy.ForceIntent))
	if requested == "" {
		requested = strings.ToLower(strings.TrimSpace(ev.RequestedIntent))
	}
	switch requested {
	case "transcode", "repair":
		return true
	default:
		return false
	}
}
