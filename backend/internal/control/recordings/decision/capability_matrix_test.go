package decision

import "testing"

// boolPtr is defined in generators_test.go.

// The matrix keeps the distinction the flat VideoCodecs list erases: "plays in
// hardware" vs "supported but the browser never confirmed smooth playback".
func TestCapabilityMatrix_TierDistinguishesHardwareFromUnverified(t *testing.T) {
	m := BuildCapabilityMatrix([]VideoCodecSignal{
		{Codec: "hevc", Supported: true, Smooth: boolPtr(true), PowerEfficient: boolPtr(true)},
		{Codec: "av1", Supported: true}, // supported only — no smooth/powerEfficient
	}, nil, nil)

	if got := m["hevc"].Tier(); got != TierHardware {
		t.Errorf("hevc tier = %v, want hardware", got)
	}
	if got := m["av1"].Tier(); got != TierUnverified {
		t.Errorf("av1 tier = %v, want unverified", got)
	}
}

// The flat list back-fills support for codecs that have no signal, so behaviour
// is never worse than today; signals only add nuance on top.
func TestCapabilityMatrix_FlatListBackfillsSupported(t *testing.T) {
	m := BuildCapabilityMatrix(nil, []string{"h264"}, nil)
	cc, ok := m["h264"]
	if !ok || !cc.Supported {
		t.Fatalf("h264 should be supported from the flat list, got %+v", cc)
	}
	if cc.Tier() != TierUnverified {
		t.Errorf("flat-list-only codec tier = %v, want unverified", cc.Tier())
	}
}

// Resolution is enforced per codec, not via one global cap.
func TestCapabilityMatrix_PerCodecResolutionCap(t *testing.T) {
	m := BuildCapabilityMatrix(
		[]VideoCodecSignal{{Codec: "hevc", Supported: true, Smooth: boolPtr(true), PowerEfficient: boolPtr(true)}},
		nil,
		&MaxVideoDimensions{Width: 1920, Height: 1080},
	)

	if ok, tier := m.CanDeliverVideo(Source{VideoCodec: "hevc", Width: 3840, Height: 2160}); ok || tier != TierNone {
		t.Errorf("4K over a 1080p cap: got ok=%v tier=%v, want false/none", ok, tier)
	}
	if ok, tier := m.CanDeliverVideo(Source{VideoCodec: "hevc", Width: 1920, Height: 1080}); !ok || tier != TierHardware {
		t.Errorf("1080p within cap: got ok=%v tier=%v, want true/hardware", ok, tier)
	}
}

// Unknown source dimensions do not block (Option A philosophy, now per codec).
func TestCapabilityMatrix_UnknownSourceDimsDoNotBlock(t *testing.T) {
	m := BuildCapabilityMatrix(
		[]VideoCodecSignal{{Codec: "h264", Supported: true, Smooth: boolPtr(true)}},
		nil,
		&MaxVideoDimensions{Width: 1920, Height: 1080, FPS: 60},
	)
	if ok, _ := m.CanDeliverVideo(Source{VideoCodec: "h264", Width: 0, Height: 0, FPS: 0}); !ok {
		t.Errorf("unknown source dims should not block a supported codec")
	}
}

// The headline result: the policy knob (minDirectTier) ends the under/over-
// promise argument, and the matrix catches an over-promise the flat predicate
// cannot even express.
func TestCanVideoFromMatrix_DivergesFromFlatPredicateOnUnverifiedCodec(t *testing.T) {
	caps := Capabilities{VideoCodecs: []string{"av1"}}
	signals := []VideoCodecSignal{{Codec: "av1", Supported: true}} // supported, not smooth
	src := Source{VideoCodec: "av1", Width: 1920, Height: 1080}

	// What computePredicates does today: codec is in the flat list, no cap → yes.
	flatPredicate := contains(caps.VideoCodecs, src.VideoCodec) && withinMaxVideo(src, caps.MaxVideo)
	if !flatPredicate {
		t.Fatalf("precondition: flat predicate should accept this source today")
	}

	// Matrix, requiring at least smooth software decode for direct play: NO.
	if CanVideoFromMatrix(src, caps, signals, TierSoftware) {
		t.Errorf("TierSoftware floor should reject a supported-but-not-smooth codec the flat list accepts")
	}
	// Same input, optimistic floor: matches the flat list's behaviour.
	if !CanVideoFromMatrix(src, caps, signals, TierUnverified) {
		t.Errorf("TierUnverified floor should accept it, matching today's flat-list behaviour")
	}
}
