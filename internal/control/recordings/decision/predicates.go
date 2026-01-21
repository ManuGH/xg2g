package decision

// computePredicates evaluates all compatibility predicates (Section 6.2).
// All predicates are pure boolean functions with no side effects.
func computePredicates(source Source, caps Capabilities, policy Policy) Predicates {
	// Element-wise compatibility checks
	canContainer := contains(caps.Containers, source.Container)
	canVideo := contains(caps.VideoCodecs, source.VideoCodec)
	canAudio := contains(caps.AudioCodecs, source.AudioCodec)

	// Direct play: client can play source container+codecs directly via static MP4
	// Strict: Container MUST be mp4/mov/m4v (Protocol Limitation)
	isMP4 := source.Container == "mp4" || source.Container == "mov" || source.Container == "m4v"
	directPlayPossible := canContainer && canVideo && canAudio && isMP4

	// Direct stream: no re-encode, but may remux/package to HLS
	// Requires: HLS support + compatible codecs (container may differ)
	directStreamPossible := caps.SupportsHLS && canVideo && canAudio

	// Transcode needed: any incompatibility that requires re-encode
	transcodeNeeded := !canVideo || !canAudio || (!canContainer && !directStreamPossible)

	// Transcode possible: purely policy-gated (no resource modeling in P4-2)
	transcodePossible := policy.AllowTranscode

	return Predicates{
		CanContainer:         canContainer,
		CanVideo:             canVideo,
		CanAudio:             canAudio,
		DirectPlayPossible:   directPlayPossible,
		DirectStreamPossible: directStreamPossible,
		TranscodeNeeded:      transcodeNeeded,
		TranscodePossible:    transcodePossible,
	}
}

// contains checks if a slice contains a specific string (case-sensitive).
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
