package v3

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
	Validity           int64
	NetworkCaptureTime int64
	PolicyVersion      string

	// Source Truth
	Container  string
	VideoCodec string
	AudioCodec string
	Width      int
	Height     int
	FPS        int
	Interlaced bool

	// Client Evidence
	ClientFamily         string
	AllowTranscode       bool
	SupportedContainers  []string
	SupportedVideoCodecs []string
	SupportedAudioCodecs []string
	MaxVideoWidth        int
	MaxVideoHeight       int
	MaxVideoFPS          int
	PreferredEngine      string
	SupportedEngines     []string
	SupportsHls          bool
	SupportsRange        bool

	// Network Evidence
	DownlinkKbps      int
	RTTMillis         int
	InternetValidated bool

	// Host Snapshot
	HostPressureBand string
	AvailableEngines []string

	// Operator Policy
	DisableTranscoding bool
	MaxGlobalBitrate   int
}

// BuildPlaybackEvidence maps the legacy HTTP boundary inputs into the new pure domain model.
func BuildPlaybackEvidence(input LegacyPlanningInput) (playbackplanner.PlaybackEvidence, error) {
	ev := playbackplanner.PlaybackEvidence{
		EvaluatedAt:        input.EvaluatedAt,
		Scope:              input.Scope,
		RequestedIntent:    input.RequestedIntent,
		SourceIdentity:     input.SourceIdentity,
		Provenance:         input.Provenance,
		Confidence:         input.Confidence,
		ObservedAt:         input.ObservedAt,
		Validity:           input.Validity,
		NetworkCaptureTime: input.NetworkCaptureTime,
		PolicyVersion:      input.PolicyVersion,
		SourceTruth: playbackplanner.SourceTruth{
			Container:  input.Container,
			VideoCodec: input.VideoCodec,
			AudioCodec: input.AudioCodec,
			Width:      input.Width,
			Height:     input.Height,
			FPS:        input.FPS,
			Interlaced: input.Interlaced,
		},
		ClientEvidence: playbackplanner.ClientEvidence{
			Family:               input.ClientFamily,
			AllowTranscode:       input.AllowTranscode,
			SupportedContainers:  append([]string(nil), input.SupportedContainers...),
			SupportedVideoCodecs: append([]string(nil), input.SupportedVideoCodecs...),
			SupportedAudioCodecs: append([]string(nil), input.SupportedAudioCodecs...),
			MaxVideoWidth:        input.MaxVideoWidth,
			MaxVideoHeight:       input.MaxVideoHeight,
			MaxVideoFPS:          input.MaxVideoFPS,
			PreferredEngine:      input.PreferredEngine,
			SupportedEngines:     append([]string(nil), input.SupportedEngines...),
			SupportsHls:          input.SupportsHls,
			SupportsRange:        input.SupportsRange,
		},
		NetworkEvidence: playbackplanner.NetworkEvidence{
			DownlinkKbps:      input.DownlinkKbps,
			RTTMillis:         input.RTTMillis,
			InternetValidated: input.InternetValidated,
		},
		HostSnapshot: playbackplanner.HostSnapshot{
			PressureBand:     input.HostPressureBand,
			AvailableEngines: append([]string(nil), input.AvailableEngines...),
		},
		OperatorPolicy: playbackplanner.OperatorPolicy{
			DisableTranscoding: input.DisableTranscoding,
			MaxGlobalBitrate:   input.MaxGlobalBitrate,
		},
	}
	return ev, nil
}
