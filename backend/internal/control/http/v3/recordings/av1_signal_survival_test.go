package recordings

import (
	"context"
	"testing"

	domainrecordings "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
)

// Traces the AV1 evidence through the production capability path for the exact
// payload a macOS Safari client sent to staging (v3.8.1-127-g44fb7619):
//
//	videoCodecs: [av1 hevc h264]
//	signals:     av1/hevc/h264 all supported+smooth+powerEfficient
//	family:      safari_native, preferredHlsEngine: hlsjs, containers incl. fmp4
//
// The planner received an auto-codec list that produced h264, so something
// between the request and plannerAutoTranscodeVideoCodecs drops av1 and hevc.
// This walks the same three stages playback_info.go walks and reports where.
func stagingSafariClientCaps() *capabilities.PlaybackCapabilities {
	yes := true
	sig := func(codec string) capabilities.VideoCodecSignal {
		s, p := yes, yes
		return capabilities.VideoCodecSignal{Codec: codec, Supported: true, Smooth: &s, PowerEfficient: &p}
	}
	return &capabilities.PlaybackCapabilities{
		CapabilitiesVersion:  3,
		Containers:           []string{"mp4", "ts", "fmp4"},
		VideoCodecs:          []string{"av1", "hevc", "h264"},
		AudioCodecs:          []string{"aac", "mp3", "ac3"},
		VideoCodecSignals:    []capabilities.VideoCodecSignal{sig("av1"), sig("hevc"), sig("h264")},
		MaxVideo:             &capabilities.MaxVideo{Width: 3840, Height: 2160, Fps: 60},
		HLSEngines:           []string{"hlsjs", "native"},
		PreferredHLSEngine:   "hlsjs",
		SupportsHLS:          true,
		SupportsHLSExplicit:  true,
		SupportsRange:        &yes,
		AllowTranscode:       &yes,
		DeviceType:           "web",
		RuntimeProbeUsed:     true,
		RuntimeProbeVersion:  2,
		ClientCapsSource:     capabilities.ClientCapsSourceRuntimePlusFam,
		ClientFamilyFallback: "safari_native",
		DeviceContext:        &capabilities.DeviceContext{Platform: "macintel", OSName: "macos"},
	}
}

func TestAV1SignalSurvivesCapabilityCanonicalisation(t *testing.T) {
	clientCaps := stagingSafariClientCaps()
	req := PlaybackInfoRequest{
		SubjectKind:   PlaybackSubjectLive,
		APIVersion:    "v3",
		SchemaType:    "live",
		ClientProfile: "safari",
		Capabilities:  clientCaps,
	}

	contract := domainrecordings.ResolveCapabilityContract(
		context.Background(), "", req.APIVersion, req.RequestedProfile,
		req.Headers, req.Capabilities, string(req.SubjectKind), req.ClientProfile,
	)

	names := func(sigs []capabilities.VideoCodecSignal) []string {
		out := make([]string, 0, len(sigs))
		for _, s := range sigs {
			out = append(out, s.Codec)
		}
		return out
	}

	t.Logf("RAW       videoCodecs=%v signals=%v", contract.Raw.VideoCodecs, names(contract.Raw.VideoCodecSignals))
	t.Logf("VERIFIED  videoCodecs=%v signals=%v", contract.Verified.VideoCodecs, names(contract.Verified.VideoCodecSignals))
	t.Logf("EFFECTIVE videoCodecs=%v signals=%v", contract.Effective.VideoCodecs, names(contract.Effective.VideoCodecSignals))
	t.Logf("adjustments=%+v policyVersion=%q", contract.Adjustments, contract.PolicyVersion)

	autoCodecs := plannerAutoTranscodeVideoCodecs(req, contract.Effective, false)
	t.Logf("plannerAutoTranscodeVideoCodecs => %v", autoCodecs)

	if len(autoCodecs) == 1 && autoCodecs[0] == "h264" {
		t.Fatalf("auto-codec evidence narrowed to h264 only; av1/hevc lost. effective videoCodecs=%v signals=%v",
			contract.Effective.VideoCodecs, names(contract.Effective.VideoCodecSignals))
	}
	found := false
	for _, c := range autoCodecs {
		if c == "av1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("av1 missing from auto-codec evidence %v", autoCodecs)
	}
}
