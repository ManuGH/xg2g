package v3

import "testing"

func TestResolveRecordingThumbnailSeekSeconds(t *testing.T) {
	tests := []struct {
		name     string
		duration float64
		wantMin  float64
		wantMax  float64
	}{
		{name: "unknown duration falls back", duration: 0, wantMin: 45, wantMax: 45},
		{name: "short clip stays near start", duration: 20, wantMin: 3.5, wantMax: 4.5},
		{name: "feature length prefers meaningful scene", duration: 3600, wantMin: 1079, wantMax: 1081},
		{name: "near eof clamps away from end", duration: 5, wantMin: 1, wantMax: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveRecordingThumbnailSeekSeconds(tt.duration)
			if got < tt.wantMin || got > tt.wantMax {
				t.Fatalf("resolveRecordingThumbnailSeekSeconds(%v) = %v, want between %v and %v", tt.duration, got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestFormatThumbnailSeekSeconds(t *testing.T) {
	if got := formatThumbnailSeekSeconds(12.34567); got != "12.346" {
		t.Fatalf("formatThumbnailSeekSeconds() = %q, want %q", got, "12.346")
	}
}
