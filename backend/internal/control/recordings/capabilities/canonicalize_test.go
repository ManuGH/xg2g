package capabilities

import "testing"

func TestCanonicalizeCapabilities_VideoCodecSignalsDedupesAndSorts(t *testing.T) {
	t.Parallel()

	truePtr := func(v bool) *bool { return &v }

	got := CanonicalizeCapabilities(PlaybackCapabilities{
		VideoCodecSignals: []VideoCodecSignal{
			{Codec: " HEVC ", Supported: true},
			{Codec: "av1", Supported: false, PowerEfficient: truePtr(true)},
			{Codec: "hevc", Supported: false, Smooth: truePtr(true)},
		},
	})

	if len(got.VideoCodecSignals) != 2 {
		t.Fatalf("expected 2 codec signals, got %#v", got.VideoCodecSignals)
	}
	if got.VideoCodecSignals[0].Codec != "av1" {
		t.Fatalf("expected av1 first after sort, got %#v", got.VideoCodecSignals)
	}
	if got.VideoCodecSignals[1].Codec != "hevc" || !got.VideoCodecSignals[1].Supported {
		t.Fatalf("expected merged hevc support, got %#v", got.VideoCodecSignals[1])
	}
	if got.VideoCodecSignals[1].Smooth == nil || !*got.VideoCodecSignals[1].Smooth {
		t.Fatalf("expected merged hevc smooth flag, got %#v", got.VideoCodecSignals[1])
	}
}
