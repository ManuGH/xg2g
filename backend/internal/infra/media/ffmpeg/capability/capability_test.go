package capability

import (
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
)

func TestDeriveVAAPIEncoderCapabilities_UsesRelativeStartupCost(t *testing.T) {
	t.Parallel()

	caps := DeriveVAAPIEncoderCapabilities(map[string]time.Duration{
		"h264_vaapi": 100 * time.Millisecond,
		"hevc_vaapi": 160 * time.Millisecond,
		"av1_vaapi":  320 * time.Millisecond,
	}, DefaultHEVCVAAPIAutoRatioMax, DefaultAV1VAAPIAutoRatioMax)

	if !caps["h264_vaapi"].Verified || !caps["h264_vaapi"].AutoEligible {
		t.Fatalf("expected h264_vaapi to stay auto-eligible: %#v", caps["h264_vaapi"])
	}
	if !caps["hevc_vaapi"].Verified || !caps["hevc_vaapi"].AutoEligible {
		t.Fatalf("expected hevc_vaapi to be auto-eligible within ratio budget: %#v", caps["hevc_vaapi"])
	}
	if !caps["av1_vaapi"].Verified {
		t.Fatalf("expected av1_vaapi to be verified: %#v", caps["av1_vaapi"])
	}
	if caps["av1_vaapi"].AutoEligible {
		t.Fatalf("expected av1_vaapi to be excluded from auto ladder when above ratio budget: %#v", caps["av1_vaapi"])
	}
}

func TestDeriveVAAPIEncoderCapabilities_FallsBackToFastestVerifiedBaseline(t *testing.T) {
	t.Parallel()

	caps := DeriveVAAPIEncoderCapabilities(map[string]time.Duration{
		"hevc_vaapi": 180 * time.Millisecond,
		"av1_vaapi":  500 * time.Millisecond,
	}, DefaultHEVCVAAPIAutoRatioMax, DefaultAV1VAAPIAutoRatioMax)

	if !caps["hevc_vaapi"].AutoEligible {
		t.Fatalf("expected fastest verified encoder to seed auto ladder: %#v", caps["hevc_vaapi"])
	}
	if caps["av1_vaapi"].AutoEligible {
		t.Fatalf("expected slower av1_vaapi to stay out of auto ladder: %#v", caps["av1_vaapi"])
	}
}

func TestDeriveProfileCapabilities_PreservesMeasuredProfiles(t *testing.T) {
	t.Parallel()

	caps := DeriveProfileCapabilities(map[string]time.Duration{
		playbackprofile.BenchmarkProfileAudioAACStereo:   35 * time.Millisecond,
		playbackprofile.BenchmarkProfileVideoH2641080P:   90 * time.Millisecond,
		playbackprofile.BenchmarkProfileVideoH2641080I:   180 * time.Millisecond,
		playbackprofile.BenchmarkProfileVideoH2641080I50: 310 * time.Millisecond,
		playbackprofile.BenchmarkProfileVideoH2642160P:   420 * time.Millisecond,
		playbackprofile.BenchmarkProfileVideoH2642160P50: 820 * time.Millisecond,
	})

	if !caps[playbackprofile.BenchmarkProfileAudioAACStereo].Verified || caps[playbackprofile.BenchmarkProfileAudioAACStereo].ProbeElapsed != 35*time.Millisecond {
		t.Fatalf("expected audio profile capability, got %#v", caps[playbackprofile.BenchmarkProfileAudioAACStereo])
	}
	if !caps[playbackprofile.BenchmarkProfileVideoH2641080P].Verified || caps[playbackprofile.BenchmarkProfileVideoH2641080P].ProbeElapsed != 90*time.Millisecond {
		t.Fatalf("expected 1080p profile capability, got %#v", caps[playbackprofile.BenchmarkProfileVideoH2641080P])
	}
	if !caps[playbackprofile.BenchmarkProfileVideoH2641080I].Verified || caps[playbackprofile.BenchmarkProfileVideoH2641080I].ProbeElapsed != 180*time.Millisecond {
		t.Fatalf("expected 1080i profile capability, got %#v", caps[playbackprofile.BenchmarkProfileVideoH2641080I])
	}
	if !caps[playbackprofile.BenchmarkProfileVideoH2641080I50].Verified || caps[playbackprofile.BenchmarkProfileVideoH2641080I50].ProbeElapsed != 310*time.Millisecond {
		t.Fatalf("expected 1080i50 profile capability, got %#v", caps[playbackprofile.BenchmarkProfileVideoH2641080I50])
	}
	if !caps[playbackprofile.BenchmarkProfileVideoH2642160P].Verified || caps[playbackprofile.BenchmarkProfileVideoH2642160P].ProbeElapsed != 420*time.Millisecond {
		t.Fatalf("expected 2160p profile capability, got %#v", caps[playbackprofile.BenchmarkProfileVideoH2642160P])
	}
	if !caps[playbackprofile.BenchmarkProfileVideoH2642160P50].Verified || caps[playbackprofile.BenchmarkProfileVideoH2642160P50].ProbeElapsed != 820*time.Millisecond {
		t.Fatalf("expected 2160p50 profile capability, got %#v", caps[playbackprofile.BenchmarkProfileVideoH2642160P50])
	}
}
