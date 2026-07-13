package hardware

import (
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
)

func TestSnapshotHostBenchmark_AssignsPerCodecClasses(t *testing.T) {
	resetVaapiState(t)
	SetVAAPIPreflightResult(true)
	SetVAAPIEncoderCapabilities(map[string]VAAPIEncoderCapability{
		"h264_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 220 * time.Millisecond},
		"hevc_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 90 * time.Millisecond},
	})

	got := snapshotHostBenchmark()

	if got.Class != "moderate" {
		t.Fatalf("expected aggregate class from fastest codec, got %#v", got)
	}
	if len(got.Codecs) != 2 {
		t.Fatalf("expected two codec benchmarks, got %#v", got.Codecs)
	}
	if got.Codecs[0].Codec != "h264" || got.Codecs[0].Class != "weak" {
		t.Fatalf("expected weak h264 codec benchmark, got %#v", got.Codecs[0])
	}
	if got.Codecs[1].Codec != "hevc" || got.Codecs[1].Class != "moderate" {
		t.Fatalf("expected moderate hevc codec benchmark, got %#v", got.Codecs[1])
	}
	if len(got.Profiles) != 5 {
		t.Fatalf("expected five derived profile benchmarks, got %#v", got.Profiles)
	}
	if got.Profiles[0].ProfileID != "video_h264_1080p" || got.Profiles[0].Class != "weak" {
		t.Fatalf("expected h264 1080p profile benchmark, got %#v", got.Profiles[0])
	}
	if got.Profiles[1].ProfileID != "video_h264_1080i" || got.Profiles[1].Class != "weak" {
		t.Fatalf("expected h264 1080i profile benchmark, got %#v", got.Profiles[1])
	}
	if got.Profiles[2].ProfileID != "video_h264_1080i50" || got.Profiles[2].Class != "weak" {
		t.Fatalf("expected h264 1080i50 profile benchmark, got %#v", got.Profiles[2])
	}
	if got.Profiles[3].ProfileID != "video_h264_2160p" || got.Profiles[3].Class != "weak" {
		t.Fatalf("expected h264 2160p profile benchmark, got %#v", got.Profiles[3])
	}
	if got.Profiles[4].ProfileID != "video_h264_2160p50" || got.Profiles[4].Class != "weak" {
		t.Fatalf("expected h264 2160p50 profile benchmark, got %#v", got.Profiles[4])
	}
}

func TestSnapshotHostBenchmark_PrefersMeasuredProfileBenchmarksOverDerivedFallback(t *testing.T) {
	resetVaapiState(t)
	SetVAAPIPreflightResult(true)
	SetVAAPIEncoderCapabilities(map[string]VAAPIEncoderCapability{
		"h264_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 120 * time.Millisecond},
	})
	SetCPUProfileBenchmarks(map[string]HardwareProfileCapability{
		"audio_aac_stereo": {Verified: true, ProbeElapsed: 30 * time.Millisecond},
		"video_h264_1080p": {Verified: true, ProbeElapsed: 300 * time.Millisecond},
	})
	SetVAAPIProfileBenchmarks(map[string]HardwareProfileCapability{
		"video_h264_1080p":   {Verified: true, ProbeElapsed: 70 * time.Millisecond},
		"video_h264_1080i":   {Verified: true, ProbeElapsed: 180 * time.Millisecond},
		"video_h264_1080i50": {Verified: true, ProbeElapsed: 260 * time.Millisecond},
	})
	SetNVENCProfileBenchmarks(map[string]HardwareProfileCapability{
		"video_h264_2160p50": {Verified: true, ProbeElapsed: 600 * time.Millisecond},
	})

	got := snapshotHostBenchmark()

	profilesByID := make(map[string]playbackprofile.HostProfileBenchmark, len(got.Profiles))
	for _, benchmark := range got.Profiles {
		profilesByID[benchmark.ProfileID] = benchmark
	}

	if profilesByID["video_h264_1080p"].Backend != "vaapi" || profilesByID["video_h264_1080p"].Class != "strong" {
		t.Fatalf("expected measured vaapi 1080p profile benchmark, got %#v", profilesByID["video_h264_1080p"])
	}
	if profilesByID["video_h264_1080i"].Backend != "vaapi" || profilesByID["video_h264_1080i"].Class != "weak" {
		t.Fatalf("expected measured vaapi 1080i profile benchmark, got %#v", profilesByID["video_h264_1080i"])
	}
	if profilesByID["video_h264_1080i50"].Backend != "vaapi" || profilesByID["video_h264_1080i50"].Class != "weak" {
		t.Fatalf("expected measured vaapi 1080i50 profile benchmark, got %#v", profilesByID["video_h264_1080i50"])
	}
	if profilesByID["audio_aac_stereo"].Backend != "cpu" || profilesByID["audio_aac_stereo"].Class != "strong" {
		t.Fatalf("expected measured cpu audio profile benchmark, got %#v", profilesByID["audio_aac_stereo"])
	}
	if profilesByID["video_h264_2160p"].Backend != "vaapi" || profilesByID["video_h264_2160p"].Class != "weak" {
		t.Fatalf("expected derived 2160p fallback benchmark, got %#v", profilesByID["video_h264_2160p"])
	}
	if profilesByID["video_h264_2160p50"].Backend != "nvenc" || profilesByID["video_h264_2160p50"].Class != "weak" {
		t.Fatalf("expected measured nvenc 2160p50 profile benchmark, got %#v", profilesByID["video_h264_2160p50"])
	}
}
