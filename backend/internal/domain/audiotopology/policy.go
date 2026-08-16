package audiotopology

import (
	"fmt"
)

// CodecStrategy defines whether audio is passed through untouched or transcoded.
type CodecStrategy string

const (
	CodecStrategyPassthrough CodecStrategy = "passthrough"
	CodecStrategyTranscode   CodecStrategy = "transcode"
	CodecStrategyUnsupported CodecStrategy = "unsupported"
)

// AudioRenditionKind distinguishes stereo from multichannel spatial renditions.
type AudioRenditionKind string

const (
	RenditionStereo   AudioRenditionKind = "stereo"
	RenditionSurround AudioRenditionKind = "surround"
)

// ClientAudioCapabilities captures the player platform's audio capabilities.
type ClientAudioCapabilities struct {
	SupportsAC3        bool   `json:"supportsAC3"`
	SupportsEAC3       bool   `json:"supportsEAC3"`
	SupportsAAC        bool   `json:"supportsAAC"`
	SupportsSpatial51  bool   `json:"supportsSpatial51"`
	PrefersPassthrough bool   `json:"prefersPassthrough"`
	PreferredLanguage  string `json:"preferredLanguage,omitempty"`
}

// TrackPlan specifies the planned encoding, FFmpeg args, and HLS rendition parameters.
type TrackPlan struct {
	PID          uint16             `json:"pid"`
	InputCodec   AudioCodec         `json:"inputCodec"`
	Strategy     CodecStrategy      `json:"strategy"`
	EncoderCodec string             `json:"encoderCodec"` // FFmpeg codec: "copy", "aac", "ac3", "eac3"
	HLSCodec     string             `json:"hlsCodec"`     // HLS RFC 6381 CODECS: "mp4a.40.2", "ac-3", "ec-3"
	Channels     int                `json:"channels"`     // 2 or 6
	BitrateKbps  int                `json:"bitrateKbps"`
	Rendition    AudioRenditionKind `json:"rendition"`
	HLSChannels  string             `json:"hlsChannels"` // "2" or "6"
	HLSGroupID   string             `json:"hlsGroupId"`  // "audio"
	Name         string             `json:"name"`
	Language     string             `json:"language"`
	IsDefault    bool               `json:"isDefault"` // Guaranteed: exactly ONE track is true
	IsAutoSelect bool               `json:"isAutoSelect"`
}

// PlannedAudioTopology represents the evaluated audio output policy for a session.
type PlannedAudioTopology struct {
	ServiceRef         string        `json:"serviceRef"`
	StructuralRevision uint64        `json:"structuralRevision"`
	Presence           PresenceState `json:"presence"`
	IsExecutable       bool          `json:"isExecutable"` // True only when Presence == PresenceVerified
	Tracks             []TrackPlan   `json:"tracks"`
}

// PlanAudioOutput derives the optimal playback plan and HLS rendition definitions
// from an AudioTopology and client capabilities.
func PlanAudioOutput(topo AudioTopology, clientCaps ClientAudioCapabilities) PlannedAudioTopology {
	// Presence Gate: Only physically verified topologies produce executable media plans.
	if topo.Presence != PresenceVerified || len(topo.Tracks) == 0 {
		return PlannedAudioTopology{
			ServiceRef:         topo.ServiceRef,
			StructuralRevision: topo.StructuralRevision,
			Presence:           topo.Presence,
			IsExecutable:       false,
			Tracks:             nil,
		}
	}

	// 1. Identify primary service language
	primaryLangCode := "deu"
	if clientCaps.PreferredLanguage != "" {
		primaryLangCode = clientCaps.PreferredLanguage
	} else {
		for _, t := range topo.Tracks {
			if t.BroadcastDefault && t.Language.ISO639_2 != "" {
				primaryLangCode = t.Language.ISO639_2
				break
			}
		}
	}

	// 2. Compute candidate plans
	candidates := make([]TrackPlan, len(topo.Tracks))
	for i, track := range topo.Tracks {
		candidates[i] = planSingleTrack(track, clientCaps)
	}

	// 3. Disambiguate Default: Select EXACTLY ONE default track
	bestIdx := 0
	bestScore := -10000

	for i, track := range topo.Tracks {
		score := evaluateDefaultScore(track, primaryLangCode)
		if score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}

	for i := range candidates {
		candidates[i].IsDefault = (i == bestIdx)
	}

	return PlannedAudioTopology{
		ServiceRef:         topo.ServiceRef,
		StructuralRevision: topo.StructuralRevision,
		Presence:           topo.Presence,
		IsExecutable:       true,
		Tracks:             candidates,
	}
}

func evaluateDefaultScore(track AudioTrack, primaryLangCode string) int {
	score := 0

	// Language match (+1000)
	if track.Language.ISO639_2 == primaryLangCode {
		score += 1000
	} else if !track.Language.IsUndefined {
		score -= 500
	}

	// Purpose (+500 for Main)
	if track.Purpose == AudioPurposeMain {
		score += 500
	} else if track.Purpose == AudioPurposeAlternate {
		score -= 200
	} else if track.Purpose == AudioPurposeCommentary {
		score -= 400
	}

	// Accessibility penalty for standard default (special tracks should not be default unless only option)
	if track.Accessibility.AudioDescription {
		score -= 600
	}
	if track.Accessibility.ClearDialogue {
		score -= 300
	}

	// Quality preference: 5.1 Surround (+100) over Stereo within same language/purpose
	if track.Channels == 6 {
		score += 100
	}

	// Broadcast default preference (+50)
	if track.BroadcastDefault {
		score += 50
	}

	// Receiver selected preference (+20)
	if track.ReceiverSelected {
		score += 20
	}

	return score
}

func planSingleTrack(
	track AudioTrack,
	clientCaps ClientAudioCapabilities,
) TrackPlan {
	plan := TrackPlan{
		PID:          track.PID,
		InputCodec:   track.Codec,
		Channels:     track.Channels,
		HLSGroupID:   "audio",
		Name:         track.Label,
		Language:     track.Language.ISO639_1,
		IsAutoSelect: true,
	}

	if plan.Language == "und" || plan.Language == "" {
		plan.Language = track.Language.ISO639_2
	}

	// 1. Multichannel source track (6 channels)
	if track.Channels == 6 {
		if clientCaps.SupportsSpatial51 {
			plan.Rendition = RenditionSurround
			plan.HLSChannels = "6"
			plan.Channels = 6

			if (track.Codec == CodecAC3 && clientCaps.SupportsAC3) || (track.Codec == CodecEAC3 && clientCaps.SupportsEAC3) {
				if clientCaps.PrefersPassthrough {
					// Bitstream passthrough
					plan.Strategy = CodecStrategyPassthrough
					plan.EncoderCodec = "copy"
					plan.HLSCodec = "ac-3"
					if track.Codec == CodecEAC3 {
						plan.HLSCodec = "ec-3"
					}
					plan.BitrateKbps = track.BitrateKbps
					if plan.BitrateKbps <= 0 {
						plan.BitrateKbps = 448
					}
					return plan
				}
			}

			if clientCaps.SupportsAAC {
				// AAC 5.1 Transcode (384k)
				plan.Strategy = CodecStrategyTranscode
				plan.EncoderCodec = "aac"
				plan.HLSCodec = "mp4a.40.2"
				plan.BitrateKbps = 384
				return plan
			}

			plan.Strategy = CodecStrategyUnsupported
			return plan
		}

		// Client does not support multichannel -> downmix to Stereo (2 channels)
		plan.Rendition = RenditionStereo
		plan.HLSChannels = "2"
		plan.Channels = 2
		if clientCaps.SupportsAAC {
			plan.Strategy = CodecStrategyTranscode
			plan.EncoderCodec = "aac"
			plan.HLSCodec = "mp4a.40.2"
			plan.BitrateKbps = 192
			return plan
		}
		plan.Strategy = CodecStrategyUnsupported
		return plan
	}

	// 2. Stereo (2 channels)
	plan.Rendition = RenditionStereo
	plan.HLSChannels = "2"
	plan.Channels = 2

	if (track.Codec == CodecAC3 && clientCaps.SupportsAC3) || (track.Codec == CodecEAC3 && clientCaps.SupportsEAC3) {
		if clientCaps.PrefersPassthrough {
			plan.Strategy = CodecStrategyPassthrough
			plan.EncoderCodec = "copy"
			plan.HLSCodec = "ac-3"
			if track.Codec == CodecEAC3 {
				plan.HLSCodec = "ec-3"
			}
			plan.BitrateKbps = track.BitrateKbps
			if plan.BitrateKbps <= 0 {
				plan.BitrateKbps = 448
			}
			return plan
		}
	}

	if clientCaps.SupportsAAC {
		// AAC Stereo Transcode (192k)
		plan.Strategy = CodecStrategyTranscode
		plan.EncoderCodec = "aac"
		plan.HLSCodec = "mp4a.40.2"
		plan.BitrateKbps = 192
		return plan
	}

	plan.Strategy = CodecStrategyUnsupported
	return plan
}

// FFmpegArgs converts a TrackPlan into concrete FFmpeg output audio arguments.
func (p TrackPlan) FFmpegArgs(streamIndex int) []string {
	if p.Strategy == CodecStrategyPassthrough {
		return []string{
			fmt.Sprintf("-c:a:%d", streamIndex), "copy",
		}
	}

	return []string{
		fmt.Sprintf("-c:a:%d", streamIndex), p.EncoderCodec,
		fmt.Sprintf("-b:a:%d", streamIndex), fmt.Sprintf("%dk", p.BitrateKbps),
		fmt.Sprintf("-ac:a:%d", streamIndex), fmt.Sprintf("%d", p.Channels),
		fmt.Sprintf("-ar:a:%d", streamIndex), "48000",
	}
}
