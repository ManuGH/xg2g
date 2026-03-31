package decision

import (
	"testing"

	mediacodec "github.com/ManuGH/xg2g/internal/media/codec"
)

func TestMapVideoCodecID(t *testing.T) {
	tests := []struct {
		in   string
		want mediacodec.ID
	}{
		{in: "h264", want: mediacodec.IDH264},
		{in: "avc1", want: mediacodec.IDH264},
		{in: "hev1", want: mediacodec.IDHEVC},
		{in: "video/av01", want: mediacodec.IDAV1},
		{in: "mpeg2video", want: mediacodec.IDMPEG2},
		{in: "vp09", want: mediacodec.IDVP9},
		{in: "mystery", want: mediacodec.IDUnknown},
	}

	for _, tc := range tests {
		if got := mapVideoCodecID(tc.in); got != tc.want {
			t.Fatalf("mapVideoCodecID(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFrameRateFromFloat(t *testing.T) {
	tests := []struct {
		in   float64
		want mediacodec.FrameRate
	}{
		{in: 25, want: mediacodec.FrameRate{Numerator: 25, Denominator: 1}},
		{in: 29.97, want: mediacodec.FrameRate{Numerator: 30000, Denominator: 1001}},
		{in: 59.94, want: mediacodec.FrameRate{Numerator: 60000, Denominator: 1001}},
		{in: 0, want: mediacodec.FrameRate{}},
	}

	for _, tc := range tests {
		if got := frameRateFromFloat(tc.in); got != tc.want {
			t.Fatalf("frameRateFromFloat(%v) = %#v, want %#v", tc.in, got, tc.want)
		}
	}
}

func TestClientToVideoCapabilityForSourcePrefersSourceCodecMatch(t *testing.T) {
	got := clientToVideoCapabilityForSource(
		Capabilities{
			VideoCodecs: []string{"h264", "hevc"},
			MaxVideo: &MaxVideoDimensions{
				Width:  3840,
				Height: 2160,
				FPS:    60,
			},
		},
		Source{VideoCodec: "hevc"},
	)

	if got.Codec != mediacodec.IDHEVC {
		t.Fatalf("expected source-matched codec, got %#v", got)
	}
	if got.MaxRes != (mediacodec.Resolution{Width: 3840, Height: 2160}) {
		t.Fatalf("unexpected max resolution: %#v", got.MaxRes)
	}
	if got.MaxFrameRate != (mediacodec.FrameRate{Numerator: 60, Denominator: 1}) {
		t.Fatalf("unexpected max frame rate: %#v", got.MaxFrameRate)
	}
}

func TestClientToVideoCapabilityForSourceFallsBackToUnknownWhenClientCodecListIsUnknown(t *testing.T) {
	got := clientToVideoCapabilityForSource(
		Capabilities{
			VideoCodecs: []string{"mystery"},
		},
		Source{VideoCodec: "h264"},
	)

	if got.Codec != mediacodec.IDUnknown {
		t.Fatalf("expected unknown codec fallback, got %#v", got)
	}
}

func TestClientToVideoCapabilityForSourceKeepsUnknownWhenNoExactSourceMatchExists(t *testing.T) {
	got := clientToVideoCapabilityForSource(
		Capabilities{
			VideoCodecs: []string{"hevc", "h264"},
		},
		Source{VideoCodec: "av1"},
	)

	if got.Codec != mediacodec.IDUnknown {
		t.Fatalf("expected unknown codec when no exact source match exists, got %#v", got)
	}
}
