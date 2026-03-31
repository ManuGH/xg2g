package playback

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/media/codec"
	"github.com/ManuGH/xg2g/internal/media/container"
)

func TestEvaluatePackagingCompatibilityAcceptsExplicitSupportedTuple(t *testing.T) {
	matrix := ClientPlaybackMatrix{
		Video: []codec.VideoCapability{
			{Codec: codec.IDH264},
			{Codec: codec.IDHEVC},
		},
		Audio: []AudioCapability{
			{Codec: codec.IDAAC},
			{Codec: codec.IDAC3},
		},
		Packaging: []PackagingCapability{
			{
				Container:   container.MPEGTS,
				Delivery:    container.HLS,
				VideoCodecs: []codec.ID{codec.IDH264},
				AudioCodecs: []codec.ID{codec.IDAAC, codec.IDAC3},
			},
			{
				Container:   container.FMP4,
				Delivery:    container.HLS,
				VideoCodecs: []codec.ID{codec.IDHEVC},
				AudioCodecs: []codec.ID{codec.IDAAC, codec.IDAC3},
			},
		},
	}

	result := EvaluatePackagingCompatibility(matrix, StreamRequest{
		Video:     codec.IDHEVC,
		Audio:     codec.IDAAC,
		Container: container.FMP4,
		Delivery:  container.HLS,
	})
	if !result.Compatible() {
		t.Fatalf("expected explicit fmp4/hevc/hls tuple to be compatible, got %+v", result.Reasons)
	}
}

func TestEvaluatePackagingCompatibilityFindsCodecMismatchInsideMatchingTransport(t *testing.T) {
	matrix := ClientPlaybackMatrix{
		Packaging: []PackagingCapability{
			{
				Container:   container.MPEGTS,
				Delivery:    container.HLS,
				VideoCodecs: []codec.ID{codec.IDH264},
				AudioCodecs: []codec.ID{codec.IDAAC},
			},
		},
	}

	result := EvaluatePackagingCompatibility(matrix, StreamRequest{
		Video:     codec.IDHEVC,
		Audio:     codec.IDAAC,
		Container: container.MPEGTS,
		Delivery:  container.HLS,
	})
	if !result.Has(ReasonVideoCodecUnsupported) {
		t.Fatalf("expected video codec mismatch, got %+v", result.Reasons)
	}
	if result.Has(ReasonNoMatchingTransport) {
		t.Fatalf("expected an exact transport match with codec mismatch only, got %+v", result.Reasons)
	}
}

func TestEvaluatePackagingCompatibilityReportsNoMatchingTransport(t *testing.T) {
	matrix := ClientPlaybackMatrix{
		Packaging: []PackagingCapability{
			{
				Container:   container.FMP4,
				Delivery:    container.HLS,
				VideoCodecs: []codec.ID{codec.IDHEVC},
				AudioCodecs: []codec.ID{codec.IDAAC},
			},
		},
	}

	result := EvaluatePackagingCompatibility(matrix, StreamRequest{
		Video:     codec.IDHEVC,
		Audio:     codec.IDAAC,
		Container: container.MP4,
		Delivery:  container.DirectFile,
	})
	if !result.Has(ReasonNoMatchingTransport) {
		t.Fatalf("expected missing transport reason, got %+v", result.Reasons)
	}
}

func TestEvaluatePackagingCompatibilityRejectsCrossMatchedContainerAndDelivery(t *testing.T) {
	matrix := ClientPlaybackMatrix{
		Packaging: []PackagingCapability{
			{
				Container:   container.MPEGTS,
				Delivery:    container.HLS,
				VideoCodecs: []codec.ID{codec.IDH264},
				AudioCodecs: []codec.ID{codec.IDAAC},
			},
			{
				Container:   container.FMP4,
				Delivery:    container.DirectFile,
				VideoCodecs: []codec.ID{codec.IDHEVC},
				AudioCodecs: []codec.ID{codec.IDAAC},
			},
		},
	}

	result := EvaluatePackagingCompatibility(matrix, StreamRequest{
		Video:     codec.IDH264,
		Audio:     codec.IDAAC,
		Container: container.MPEGTS,
		Delivery:  container.DirectFile,
	})
	if !result.Has(ReasonNoMatchingTransport) {
		t.Fatalf("expected cross-matched transport to be rejected, got %+v", result.Reasons)
	}
	if result.Compatible() {
		t.Fatalf("cross-matched transport must not be treated as compatible")
	}
}

func TestEvaluatePackagingCompatibilityHonorsContainerPracticalMatrix(t *testing.T) {
	matrix := ClientPlaybackMatrix{
		Packaging: []PackagingCapability{
			{
				Container:   container.MPEGTS,
				Delivery:    container.HLS,
				VideoCodecs: []codec.ID{codec.IDAV1},
				AudioCodecs: []codec.ID{codec.IDAAC},
			},
		},
	}

	result := EvaluatePackagingCompatibility(matrix, StreamRequest{
		Video:     codec.IDAV1,
		Audio:     codec.IDAAC,
		Container: container.MPEGTS,
		Delivery:  container.HLS,
	})
	if !result.Has(ReasonContainerCannotCarry) {
		t.Fatalf("expected practical container rejection for av1-in-ts, got %+v", result.Reasons)
	}
}

func TestClientPlaybackMatrixFinders(t *testing.T) {
	matrix := ClientPlaybackMatrix{
		Video: []codec.VideoCapability{{Codec: codec.IDHEVC}},
		Audio: []AudioCapability{{Codec: codec.IDAC3}},
	}

	if _, ok := matrix.FindVideo(codec.IDHEVC); !ok {
		t.Fatalf("expected to find hevc video capability")
	}
	if !matrix.HasAudio(codec.IDAC3) {
		t.Fatalf("expected to find ac3 audio capability")
	}
	if matrix.HasAudio(codec.IDAAC) {
		t.Fatalf("did not expect aac audio capability")
	}
}
