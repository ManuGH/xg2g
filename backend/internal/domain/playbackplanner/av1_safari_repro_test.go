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

func TestSelectAutoTranscodeVideoCodec_StagingSafariPrefersVerifiedAV1(t *testing.T) {
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

	// Probe elapsed measures encoder startup, not sustained throughput. Once an
	// Apple client and the host have both admitted AV1, prefer its higher quality
	// instead of allowing a few startup milliseconds to demote it to HEVC.
	if codec != "av1" {
		t.Fatalf("codec = %q, want av1 for Safari with a verified, auto-eligible AV1 encoder", codec)
	}
}

func TestSelectAutoTranscodeVideoCodec_NonAppleKeepsProbeTimeRanking(t *testing.T) {
	ev := stagingSafariEvidence()
	ev.ClientEvidence.Family = "chromium_hlsjs"
	ev.ClientEvidence.DeviceType = "desktop"

	codec, ok := selectAutoTranscodeVideoCodec(ev)
	if !ok {
		t.Fatal("auto-codec selection bailed out")
	}
	if codec != "h264" {
		t.Fatalf("codec = %q, want h264 from the existing non-Apple probe-time ranking", codec)
	}
}

func TestSelectAutoTranscodeVideoCodec_SafariRequiresEligibleAV1(t *testing.T) {
	ev := stagingSafariEvidence()
	ev.HostSnapshot.EncoderCapabilities[2].AutoEligible = false

	codec, ok := selectAutoTranscodeVideoCodec(ev)
	if !ok {
		t.Fatal("auto-codec selection bailed out")
	}
	if codec != "hevc" {
		t.Fatalf("codec = %q, want hevc when the AV1 encoder is not auto-eligible", codec)
	}
}

func TestSelectAutoTranscodeVideoCodec_SafariPrefersStrongAV1OnMediumHost(t *testing.T) {
	ev := stagingSafariEvidence()
	ev.HostSnapshot.PerformanceClass = "medium"
	for i := range ev.HostSnapshot.EncoderCapabilities {
		ev.HostSnapshot.EncoderCapabilities[i].BenchmarkClass = "strong"
	}

	codec, ok := selectAutoTranscodeVideoCodec(ev)
	if !ok {
		t.Fatal("auto-codec selection bailed out")
	}
	if codec != "av1" {
		t.Fatalf("codec = %q, want av1 when Safari and the codec-specific benchmark admit AV1", codec)
	}
}

func TestSelectAutoTranscodeVideoCodec_SafariRejectsWeakAV1Benchmark(t *testing.T) {
	ev := stagingSafariEvidence()
	ev.HostSnapshot.PerformanceClass = "medium"
	for i := range ev.HostSnapshot.EncoderCapabilities {
		ev.HostSnapshot.EncoderCapabilities[i].BenchmarkClass = "strong"
	}
	ev.HostSnapshot.EncoderCapabilities[2].BenchmarkClass = "weak"

	codec, ok := selectAutoTranscodeVideoCodec(ev)
	if !ok {
		t.Fatal("auto-codec selection bailed out")
	}
	if codec != "hevc" {
		t.Fatalf("codec = %q, want hevc when the codec-specific AV1 benchmark is weak", codec)
	}
}
