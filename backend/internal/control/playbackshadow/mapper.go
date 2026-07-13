package playbackshadow

import (
	"github.com/ManuGH/xg2g/internal/domain/playbackplanner"
)

// LegacyPlanningInput encapsulates all the pre-resolved inputs from the legacy v3 HTTP layer.
type LegacyPlanningInput struct {
	EvaluatedAt        int64
	Scope              string
	RequestedIntent    string
	SourceIdentity     string
	Provenance         string
	Confidence         string
	ObservedAt         int64
	ValidUntil         int64
	NetworkCaptureTime int64
	PolicyVersion      string

	// Source Truth
	Container         string
	VideoCodec        string
	AudioCodec        string
	Width             int
	Height            int
	FPS               int
	Interlaced        bool
	BitrateKbps       int
	BitrateConfidence string

	// Client Evidence
	ClientFamily            string
	DeviceType              string
	CapabilityVersion       string
	AllowTranscode          bool
	SupportedContainers     []string
	SupportedVideoCodecs    []string
	SupportedAudioCodecs    []string
	MaxVideoWidth           int
	MaxVideoHeight          int
	MaxVideoFPS             int
	PreferredEngine         string
	SupportedEngines        []string
	PrefersFMP4             bool
	PrefersFMP4ForTranscode bool
	SupportsHls             bool
	SupportsRange           *bool

	// Network Evidence
	DownlinkKbps      int
	RTTMillis         int
	InternetValidated bool

	// Host Snapshot
	HostPressureBand string
	AvailableEngines []string
	PerformanceClass string
	BenchmarkClass   string

	// Operator Policy
	ForceIntent        string
	MaxQualityRung     string
	DisableTranscoding bool
	MaxGlobalBitrate   int
	StrictFreshness    bool
}

// BuildPlaybackEvidence maps the legacy HTTP boundary inputs into the new pure domain model.
func BuildPlaybackEvidence(input LegacyPlanningInput) (playbackplanner.PlaybackEvidence, error) {
	confidence := input.Confidence
	if confidence == "" {
		confidence = "unknown"
	}
	bitrateConfidence := input.BitrateConfidence
	if bitrateConfidence == "" {
		bitrateConfidence = "unknown"
	}
	hostPressureBand := input.HostPressureBand
	if hostPressureBand == "" {
		hostPressureBand = "unknown"
	}
	perfClass := input.PerformanceClass
	if perfClass == "" {
		perfClass = "unknown"
	}

	ev := playbackplanner.PlaybackEvidence{
		EvaluatedAt:        input.EvaluatedAt,
		Scope:              input.Scope,
		RequestedIntent:    input.RequestedIntent,
		SourceIdentity:     input.SourceIdentity,
		Provenance:         input.Provenance,
		Confidence:         confidence,
		ObservedAt:         input.ObservedAt,
		ValidUntil:         input.ValidUntil,
		NetworkCaptureTime: input.NetworkCaptureTime,
		PolicyVersion:      input.PolicyVersion,
		SourceTruth: playbackplanner.SourceTruth{
			Container:         input.Container,
			VideoCodec:        input.VideoCodec,
			AudioCodec:        input.AudioCodec,
			Width:             input.Width,
			Height:            input.Height,
			FPS:               input.FPS,
			Interlaced:        input.Interlaced,
			BitrateKbps:       input.BitrateKbps,
			BitrateConfidence: bitrateConfidence,
		},
		ClientEvidence: playbackplanner.ClientEvidence{
			Family:                  input.ClientFamily,
			DeviceType:              input.DeviceType,
			CapabilityVersion:       input.CapabilityVersion,
			AllowTranscode:          input.AllowTranscode,
			SupportedContainers:     append([]string(nil), input.SupportedContainers...),
			SupportedVideoCodecs:    append([]string(nil), input.SupportedVideoCodecs...),
			SupportedAudioCodecs:    append([]string(nil), input.SupportedAudioCodecs...),
			MaxVideoWidth:           input.MaxVideoWidth,
			MaxVideoHeight:          input.MaxVideoHeight,
			MaxVideoFPS:             input.MaxVideoFPS,
			PreferredEngine:         input.PreferredEngine,
			SupportedEngines:        append([]string(nil), input.SupportedEngines...),
			PrefersFMP4:             input.PrefersFMP4,
			PrefersFMP4ForTranscode: input.PrefersFMP4ForTranscode,
			SupportsHls:             input.SupportsHls,
			SupportsRange:           input.SupportsRange,
		},
		NetworkEvidence: playbackplanner.NetworkEvidence{
			DownlinkKbps:      input.DownlinkKbps,
			RTTMillis:         input.RTTMillis,
			InternetValidated: input.InternetValidated,
		},
		HostSnapshot: playbackplanner.HostSnapshot{
			PressureBand:     hostPressureBand,
			AvailableEngines: append([]string(nil), input.AvailableEngines...),
			PerformanceClass: perfClass,
			BenchmarkClass:   input.BenchmarkClass,
		},
		OperatorPolicy: playbackplanner.OperatorPolicy{
			ForceIntent:        input.ForceIntent,
			MaxQualityRung:     input.MaxQualityRung,
			DisableTranscoding: input.DisableTranscoding,
			MaxGlobalBitrate:   input.MaxGlobalBitrate,
			StrictFreshness:    input.StrictFreshness,
		},
	}
	return ev, nil
}
