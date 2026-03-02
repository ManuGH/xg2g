package hls

import (
	"strings"
	"testing"
	"time"
)

func TestExtractSegmentTruth_VOD_NoPDT(t *testing.T) {
	playlist := `#EXTM3U
#EXT-X-PLAYLIST-TYPE:VOD
#EXT-X-TARGETDURATION:10
#EXTINF:10.0,
segment1.ts
#EXTINF:10.0,
segment2.ts
#EXT-X-ENDLIST`

	truth, err := ExtractSegmentTruth(playlist)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if !truth.IsVOD {
		t.Error("Expected IsVOD=true")
	}
	if truth.HasPDT {
		t.Error("Expected HasPDT=false")
	}
	if truth.TotalDuration != 20*time.Second {
		t.Errorf("Expected TotalDuration=20s, got %v", truth.TotalDuration)
	}
}

func TestExtractSegmentTruth_Live_FullPDT(t *testing.T) {
	playlist := `#EXTM3U
#EXT-X-TARGETDURATION:10
#EXT-X-PROGRAM-DATE-TIME:2024-01-01T12:00:00Z
#EXTINF:10.0,
segment1.ts
#EXT-X-PROGRAM-DATE-TIME:2024-01-01T12:00:10Z
#EXTINF:10.0,
segment2.ts`

	truth, err := ExtractSegmentTruth(playlist)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if truth.IsVOD {
		t.Error("Expected IsVOD=false")
	}
	if !truth.HasPDT {
		t.Error("Expected HasPDT=true")
	}

	expectedFirst := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	if !truth.FirstPDT.Equal(expectedFirst) {
		t.Errorf("Expected FirstPDT=%v, got %v", expectedFirst, truth.FirstPDT)
	}

	expectedLast := time.Date(2024, 1, 1, 12, 0, 10, 0, time.UTC)
	if !truth.LastPDT.Equal(expectedLast) {
		t.Errorf("Expected LastPDT=%v, got %v", expectedLast, truth.LastPDT)
	}

	if truth.LastDuration != 10*time.Second {
		t.Errorf("Expected LastDuration=10s, got %v", truth.LastDuration)
	}
}

func TestExtractSegmentTruth_Live_PartialPDT_FailClosed(t *testing.T) {
	// Stop-the-line check: fail if ANY segment misses PDT in Live
	playlist := `#EXTM3U
#EXT-X-TARGETDURATION:10
#EXT-X-PROGRAM-DATE-TIME:2024-01-01T12:00:00Z
#EXTINF:10.0,
segment1.ts
#EXTINF:10.0,
segment2.ts`
	// Segment 2 misses PDT

	_, err := ExtractSegmentTruth(playlist)
	if err == nil {
		t.Fatal("Expected error for partial PDT coverage, got nil")
	}
	if !strings.Contains(err.Error(), "partial PDT coverage") {
		t.Errorf("Expected partial coverage error, got: %v", err)
	}
}

func TestExtractSegmentTruth_Live_NonMonotonic_FailClosed(t *testing.T) {
	playlist := `#EXTM3U
#EXT-X-TARGETDURATION:10
#EXT-X-PROGRAM-DATE-TIME:2024-01-01T12:00:10Z
#EXTINF:10.0,
segment1.ts
#EXT-X-PROGRAM-DATE-TIME:2024-01-01T12:00:00Z
#EXTINF:10.0,
segment2.ts`
	// PDT jumps back 10s

	_, err := ExtractSegmentTruth(playlist)
	if err == nil {
		t.Fatal("Expected error for non-monotonic PDT, got nil")
	}
	if !strings.Contains(err.Error(), "non-monotonic") {
		t.Errorf("Expected non-monotonic error, got: %v", err)
	}
}

func TestExtractSegmentTruth_VOD_EndList_Implicit(t *testing.T) {
	// Even without explicit type, ENDLIST should trigger VOD logic
	playlist := `#EXTM3U
#EXTINF:10.0,
segment1.ts
#EXT-X-ENDLIST`

	truth, err := ExtractSegmentTruth(playlist)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !truth.IsVOD {
		t.Error("Expected IsVOD=true due to ENDLIST")
	}
}
