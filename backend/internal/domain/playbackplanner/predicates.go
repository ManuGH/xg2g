package playbackplanner

// hasValidEvidence checks if the evidence is structurally sound.
func hasValidEvidence(ev PlaybackEvidence) bool {
	if ev.SourceIdentity == "" {
		return false
	}
	if ev.SourceTruth.Container == "" || ev.SourceTruth.Container == "unknown" {
		return false
	}
	if ev.SourceTruth.VideoCodec == "unknown" || ev.SourceTruth.AudioCodec == "unknown" {
		return false
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
	return contains(ev.ClientEvidence.SupportedAudioCodecs, ev.SourceTruth.AudioCodec)
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
