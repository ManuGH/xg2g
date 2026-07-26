package playbackplanner

import "testing"

// Reproduces the exact evidence a macOS Safari client produced on staging
// (v3.8.1-127-g44fb7619) after the AV1 consolidation: the client advertises
// av1/hevc/h264, the host has all three VAAPI encoders verified and
// auto-eligible with their measured probe times, and yet the planner emitted
// video codec h264.
//
// Probe times are the real preflight values from that host:
//
//	h264_vaapi 64.47ms, hevc_vaapi 69.53ms, av1_vaapi 74.72ms
func stagingSafariEvidence() PlaybackEvidence {
	return PlaybackEvidence{
		RequestedIntent: "",
		ClientEvidence: ClientEvidence{
			Family:                   "safari_native",
			DeviceType:               "safari",
			AllowTranscode:           true,
			SupportedContainers:      []string{"mp4", "ts", "fmp4"},
			SupportedVideoCodecs:     []string{"av1", "hevc", "h264"},
			SupportedAudioCodecs:     []string{"aac", "mp3", "ac3"},
			AutoTranscodeVideoCodecs: []string{"av1", "hevc", "h264"},
			MaxVideoWidth:            3840,
			MaxVideoHeight:           2160,
			MaxVideoFPS:              60,
			PreferredEngine:          "hlsjs",
			SupportedEngines:         []string{"hlsjs", "native"},
			PrefersFMP4:              true,
			SupportsHls:              true,
		},
		HostSnapshot: HostSnapshot{
			EncoderCapabilities: []HostEncoderCapability{
				{Codec: "h264", Verified: true, AutoEligible: true, ProbeElapsedMS: 64},
				{Codec: "hevc", Verified: true, AutoEligible: true, ProbeElapsedMS: 69},
				{Codec: "av1", Verified: true, AutoEligible: true, ProbeElapsedMS: 74},
			},
		},
	}
}

func TestSelectAutoTranscodeVideoCodec_StagingSafariDoesNotFallBackToH264(t *testing.T) {
	ev := stagingSafariEvidence()

	// None of the three bail-out conditions may fire for this evidence: the
	// client opted into auto transcode, did not request a forced transcode, and
	// supplied a non-empty auto-codec list.
	if requiresPlannedTranscode(ev) {
		t.Fatalf("requiresPlannedTranscode = true, want false for an empty requested intent")
	}
	if !usesAutoTranscodeProfile(ev.RequestedIntent) {
		t.Fatalf("usesAutoTranscodeProfile = false, want true for an empty requested intent")
	}
	if len(ev.ClientEvidence.AutoTranscodeVideoCodecs) == 0 {
		t.Fatalf("AutoTranscodeVideoCodecs is empty, want the client's three codecs")
	}

	codec, ok := selectAutoTranscodeVideoCodec(ev)
	if !ok {
		t.Fatalf("auto-codec selection bailed out (codec=%q) although no gate condition holds", codec)
	}

	// The production symptom this pins: staging emitted h264 for exactly this
	// client while the host had verified, auto-eligible hevc AND av1 encoders.
	// Given the full evidence the selector does NOT choose h264 — so a real h264
	// outcome means the evidence reaching the planner was already narrowed
	// (AutoTranscodeVideoCodecs reduced to h264), not that the selector chose it.
	if codec == "h264" {
		t.Fatalf("codec = h264 despite verified hevc and av1 encoders; the auto-codec selector regressed")
	}
	t.Logf("selector chose %q from av1/hevc/h264 (probe: h264 64ms, hevc 69ms, av1 74ms)", codec)
}
