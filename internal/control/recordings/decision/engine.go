package decision

const (
	// Sentinel value for deny mode (ADR P4-2 requirement).
	sentinelNone = "none"
)

// evaluateDecisionTable implements the normative decision table (Section 6.3).
// Evaluates in order D-1 through D-5; first match wins.
// evaluateDecision implements the strict logic from ADR-P8.
// Returns Mode and a list of normative ReasonCodes.
func evaluateDecision(pred Predicates, caps Capabilities, policy Policy) (Mode, []ReasonCode) {
	// Step 2: Container Not Supported
	if !pred.CanContainer {
		return ModeDeny, []ReasonCode{ReasonContainerNotSupported}
	}

	// Step 3: Video Codec Not Supported
	if !pred.CanVideo {
		if policy.AllowTranscode {
			return ModeTranscode, []ReasonCode{ReasonVideoCodecNotSupported}
		}
		// Step 5: Transcode Required (implied by !CanVideo) BUT Policy Denies
		return ModeDeny, []ReasonCode{ReasonPolicyDeniesTranscode}
	}

	// Step 4: Audio Codec Not Supported
	if !pred.CanAudio {
		if policy.AllowTranscode {
			return ModeTranscode, []ReasonCode{ReasonAudioCodecNotSupported}
		}
		// Step 5: Transcode Required (implied by !CanAudio) BUT Policy Denies
		return ModeDeny, []ReasonCode{ReasonPolicyDeniesTranscode}
	}

	// Step 6: MP4 Fast-Path
	// Requires: Container=mp4/mov AND Codecs Supported AND Range Supported
	// Note: pred.DirectPlayPossible checks Container && Video && Audio (but not Range/HLS/etc)
	if pred.DirectPlayPossible {
		supportsRange := caps.SupportsRange != nil && *caps.SupportsRange
		if supportsRange {
			return ModeDirectPlay, []ReasonCode{ReasonDirectPlayMatch}
		}
		// If Range not supported, we fall through to next steps (HLS).
	}

	// Step 7: HLS Direct
	// Requires: Codecs Supported AND SupportsHLS
	if caps.SupportsHLS && pred.CanVideo && pred.CanAudio {
		// pred.CanVideo/Audio guaranteed true here if we passed Steps 3 & 4.
		// So we just check HLS support.
		return ModeDirectStream, []ReasonCode{ReasonDirectStreamMatch}
	}

	// Step 8: Fallback
	// No compatible playback path found (e.g. MP4 blocked by Range, HLS blocked by SupportsHLS=false).
	return ModeDeny, []ReasonCode{ReasonNoCompatiblePlaybackPath}
}

// buildDecision constructs the final Decision response.
func buildDecision(mode Mode, pred Predicates, input Input, reasons []ReasonCode) *Decision {
	outputs := buildOutputs(mode, input.Source)

	var selURL, selKind string
	if len(outputs) > 0 {
		selURL = outputs[0].URL
		selKind = outputs[0].Kind
	}

	decision := &Decision{
		Mode:        mode,
		Selected:    buildSelected(mode, input.Source),
		Outputs:     outputs,
		Constraints: []string{}, // Always empty array (no constraints in P4-2)
		Reasons:     reasons,
		Trace: Trace{
			RequestID: input.RequestID,
		},
		SelectedOutputURL:  selURL,
		SelectedOutputKind: selKind,
	}

	return decision
}

// buildSelected constructs the selected formats.
// For mode=deny, MUST use sentinel "none" (not null).
func buildSelected(mode Mode, source Source) SelectedFormats {
	if mode == ModeDeny {
		return SelectedFormats{
			Container:  sentinelNone,
			VideoCodec: sentinelNone,
			AudioCodec: sentinelNone,
		}
	}

	// For all other modes, use actual source formats
	return SelectedFormats{
		Container:  source.Container,
		VideoCodec: source.VideoCodec,
		AudioCodec: source.AudioCodec,
	}
}

// buildOutputs constructs the outputs array.
// For mode=deny, MUST be empty array.
func buildOutputs(mode Mode, source Source) []Output {
	if mode == ModeDeny {
		return []Output{} // Empty array for deny
	}

	// For P4-2, we return placeholder outputs
	// (actual URL construction is out of scope for pure engine)
	switch mode {
	case ModeDirectPlay:
		return []Output{
			{Kind: "file", URL: "placeholder://direct-play"},
		}
	case ModeDirectStream:
		return []Output{
			{Kind: "hls", URL: "placeholder://direct-stream.m3u8"},
		}
	case ModeTranscode:
		return []Output{
			{Kind: "hls", URL: "placeholder://transcode.m3u8"},
		}
	default:
		return []Output{}
	}
}
