package ffmpeg

import (
	"strings"
	"testing"
)

// TestSignalStatsLumaVF_Is8BitNormalized locks the luma-measurement filter to
// 8-bit normalization. Without the leading format=yuv420p, signalstats reports
// YAVG on the decoded bit depth (0-1023 for 10-bit p010 AV1), which makes the
// runtime black-detector ~4x too lenient on the interlaced-AV1 path and the
// measurement ffmpeg-build dependent. Empirically on staging av1_vaapi p010:
// raw signalstats YAVG 497.5 vs format=yuv420p,signalstats YAVG 124.4 (exactly
// /4, the 1023/255 ratio). Negative control: drop the "format=yuv420p," prefix
// and this test goes red.
func TestSignalStatsLumaVF_Is8BitNormalized(t *testing.T) {
	if !strings.HasPrefix(signalStatsLumaVF, "format=yuv420p,") {
		t.Fatalf("luma measurement filter must downconvert to 8-bit before signalstats so the 0-255 thresholds stay meaningful on 10-bit AV1; got %q", signalStatsLumaVF)
	}
	if !strings.Contains(signalStatsLumaVF, "signalstats") {
		t.Fatalf("luma measurement filter must run signalstats; got %q", signalStatsLumaVF)
	}
}
