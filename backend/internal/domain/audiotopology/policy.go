package audiotopology

import (
	"fmt"
)

// CodecStrategy defines whether audio is passed through untouched or transcoded.
type CodecStrategy string

const (
	CodecStrategyPassthrough CodecStrategy = "passthrough"
	CodecStrategyTranscode   CodecStrategy = "transcode"
)

// AudioRenditionKind distinguishes stereo from multichannel spatial renditions.
type AudioRenditionKind string

const (
	RenditionStereo   AudioRenditionKind = "stereo"
	RenditionSurround AudioRenditionKind = "surround"
)

// ClientAudioCapabilities captures the player platform's audio capabilities.
type ClientAudioCapabilities struct {
	SupportsAC3        bool `json:"supportsAC3"`
	SupportsEAC3       bool `json:"supportsEAC3"`
	SupportsAAC        bool `json:"supportsAAC"`
	SupportsSpatial51  bool `json:"supportsSpatial51"`
	PrefersPassthrough bool `json:"prefersPassthrough"`
}

// TrackPlan specifies the planned encoding and HLS rendition parameters for an audio track.
type TrackPlan struct {
	PID          uint16             `json:"pid"`
	InputCodec   AudioCodec         `json:"inputCodec"`
	Strategy     CodecStrategy      `json:"strategy"`
	OutputCodec  string             `json:"outputCodec"` // e.g. "ac-3", "mp4a.40.2"
	Channels     int                `json:"channels"`    // 2 or 6
	BitrateKbps  int                `json:"bitrateKbps"`
	Rendition    AudioRenditionKind `json:"rendition"`
	HLSChannels  string             `json:"hlsChannels"` // "2" or "6"
	HLSGroupID   string             `json:"hlsGroupId"`  // "audio"
	Name         string             `json:"name"`
	Language     string             `json:"language"`
	IsDefault    bool               `json:"isDefault"`
	IsAutoSelect bool               `json:"isAutoSelect"`
}

// PlannedAudioTopology represents the evaluated audio output policy for a session.
type PlannedAudioTopology struct {
	ServiceRef         string      `json:"serviceRef"`
	StructuralRevision uint64      `json:"structuralRevision"`
	Tracks             []TrackPlan `json:"tracks"`
}

// PlanAudioOutput derives the optimal playback plan and HLS rendition definitions
// from an AudioTopology and client capabilities.
func PlanAudioOutput(topo AudioTopology, clientCaps ClientAudioCapabilities) PlannedAudioTopology {
	var planned []TrackPlan

	hasSurround := false
	for _, t := range topo.Tracks {
		if t.Channels == 6 {
			hasSurround = true
			break
		}
	}

	for i, track := range topo.Tracks {
		plan := planSingleTrack(track, clientCaps, hasSurround, i == 0)
		planned = append(planned, plan)
	}

	return PlannedAudioTopology{
		ServiceRef:         topo.ServiceRef,
		StructuralRevision: topo.StructuralRevision,
		Tracks:             planned,
	}
}

func planSingleTrack(
	track AudioTrack,
	clientCaps ClientAudioCapabilities,
	serviceHasSurround bool,
	isFirstTrack bool,
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

	// Default selection logic:
	// If 5.1 surround exists, prefer it as default; otherwise prefer broadcast default or first track.
	if serviceHasSurround {
		plan.IsDefault = (track.Channels == 6)
	} else {
		plan.IsDefault = track.BroadcastDefault || (isFirstTrack && !track.Accessibility.AudioDescription)
	}

	// 1. Multichannel (5.1 / 6 channels)
	if track.Channels == 6 {
		plan.Rendition = RenditionSurround
		plan.HLSChannels = "6"

		if (track.Codec == CodecAC3 && clientCaps.SupportsAC3) || (track.Codec == CodecEAC3 && clientCaps.SupportsEAC3) {
			if clientCaps.PrefersPassthrough || clientCaps.SupportsSpatial51 {
				// Native bitstream passthrough
				plan.Strategy = CodecStrategyPassthrough
				plan.OutputCodec = string(track.Codec)
				plan.BitrateKbps = track.BitrateKbps
				if plan.BitrateKbps <= 0 {
					plan.BitrateKbps = 448
				}
				return plan
			}
		}

		// AAC 5.1 Transcode (384k)
		plan.Strategy = CodecStrategyTranscode
		plan.OutputCodec = "mp4a.40.2"
		plan.BitrateKbps = 384
		return plan
	}

	// 2. Stereo (2 channels)
	plan.Rendition = RenditionStereo
	plan.HLSChannels = "2"
	plan.Channels = 2

	if (track.Codec == CodecAC3 && clientCaps.SupportsAC3) || (track.Codec == CodecEAC3 && clientCaps.SupportsEAC3) {
		if clientCaps.PrefersPassthrough {
			plan.Strategy = CodecStrategyPassthrough
			plan.OutputCodec = string(track.Codec)
			plan.BitrateKbps = track.BitrateKbps
			if plan.BitrateKbps <= 0 {
				plan.BitrateKbps = 448
			}
			return plan
		}
	}

	// AAC Stereo Transcode (192k matched to DVB source budget)
	plan.Strategy = CodecStrategyTranscode
	plan.OutputCodec = "mp4a.40.2"
	plan.BitrateKbps = 192
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
		fmt.Sprintf("-c:a:%d", streamIndex), "aac",
		fmt.Sprintf("-b:a:%d", streamIndex), fmt.Sprintf("%dk", p.BitrateKbps),
		fmt.Sprintf("-ac:a:%d", streamIndex), fmt.Sprintf("%d", p.Channels),
		fmt.Sprintf("-ar:a:%d", streamIndex), "48000",
	}
}
