package container

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/media/codec"
)

func TestMPEGTSCanCarryPracticalBroadcastCodecs(t *testing.T) {
	if !MPEGTS.CanCarry(codec.IDH264) {
		t.Fatalf("expected MPEGTS to carry h264")
	}
	if !MPEGTS.CanCarry(codec.IDHEVC) {
		t.Fatalf("expected MPEGTS to carry hevc")
	}
	if !MPEGTS.CanCarry(codec.IDMP2) {
		t.Fatalf("expected MPEGTS to carry mp2")
	}
}

func TestMPEGTSDeniesAV1(t *testing.T) {
	if MPEGTS.CanCarry(codec.IDAV1) {
		t.Fatalf("expected MPEGTS to deny av1 in the practical matrix")
	}
}

func TestFMP4CanCarryModernStreamingCodecs(t *testing.T) {
	if !FMP4.CanCarry(codec.IDH264) {
		t.Fatalf("expected fmp4 to carry h264")
	}
	if !FMP4.CanCarry(codec.IDHEVC) {
		t.Fatalf("expected fmp4 to carry hevc")
	}
	if !FMP4.CanCarry(codec.IDAV1) {
		t.Fatalf("expected fmp4 to carry av1")
	}
}

func TestFMP4DeniesMPEG2(t *testing.T) {
	if FMP4.CanCarry(codec.IDMPEG2) {
		t.Fatalf("expected fmp4 to deny mpeg2")
	}
}
