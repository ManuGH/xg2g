package hardware

import (
	"testing"
	"time"

	admissionmonitor "github.com/ManuGH/xg2g/internal/admission"
	admissionruntime "github.com/ManuGH/xg2g/internal/control/admission"
)

func TestSnapshotHostRuntime_CombinesCapabilitiesAndRuntime(t *testing.T) {
	resetVaapiState(t)
	SetVAAPIPreflightResult(true)
	SetVAAPIEncoderCapabilities(map[string]VAAPIEncoderCapability{
		"h264_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 120 * time.Millisecond},
		"hevc_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 90 * time.Millisecond},
	})
	SetPathCapabilities(map[string]HardwarePathCapability{
		PathVAAPIFullInterlacedHEVC: {
			Verified: true,
			Status:   PathStatusVerified,
			Reason:   "synthetic yavg 118.2",
		},
	})

	got := SnapshotHostRuntime(true, true, admissionruntime.RuntimeState{
		TunerSlots:       3,
		SessionsActive:   5,
		TranscodesActive: 2,
	}, admissionmonitor.MonitorSnapshot{
		CPU: admissionmonitor.CPUSnapshot{
			Load1m:        2.5,
			CoreCount:     8,
			SampleCount:   15,
			WindowSeconds: 30,
		},
		ActiveVAAPITokens: 1,
		MaxSessions:       8,
		MaxVAAPITokens:    1,
	})

	if !got.Capabilities.FFmpegAvailable || !got.Capabilities.HLSAvailable || !got.Capabilities.VAAPIReady {
		t.Fatalf("expected capabilities to be preserved, got %#v", got.Capabilities)
	}
	if got.CPU.Load1m != 2.5 || got.CPU.CoreCount != 8 || got.CPU.SampleCount != 15 || got.CPU.WindowSeconds != 30 {
		t.Fatalf("unexpected CPU snapshot: %#v", got.CPU)
	}
	if got.PerformanceClass != "high" {
		t.Fatalf("unexpected performance class: %#v", got.PerformanceClass)
	}
	if got.Benchmark.Class != "moderate" {
		t.Fatalf("unexpected benchmark class: %#v", got.Benchmark)
	}
	if got.Benchmark.PreferredCodec != "hevc" || got.Benchmark.PreferredBackend != "vaapi" {
		t.Fatalf("unexpected preferred benchmark: %#v", got.Benchmark)
	}
	if got.Benchmark.FastestProbeElapsedMs != 90 {
		t.Fatalf("unexpected fastest probe ms: %#v", got.Benchmark)
	}
	if len(got.Benchmark.Codecs) != 2 {
		t.Fatalf("unexpected benchmark codecs: %#v", got.Benchmark.Codecs)
	}
	if got.Benchmark.Codecs[0].Codec != "h264" || got.Benchmark.Codecs[0].Class != "moderate" {
		t.Fatalf("unexpected h264 benchmark codec summary: %#v", got.Benchmark.Codecs[0])
	}
	if got.Benchmark.Codecs[1].Codec != "hevc" || got.Benchmark.Codecs[1].Class != "moderate" {
		t.Fatalf("unexpected hevc benchmark codec summary: %#v", got.Benchmark.Codecs[1])
	}
	if len(got.Benchmark.Profiles) != 5 {
		t.Fatalf("unexpected benchmark profiles: %#v", got.Benchmark.Profiles)
	}
	if len(got.Benchmark.Paths) != 1 || got.Benchmark.Paths[0].PathID != PathVAAPIFullInterlacedHEVC || got.Benchmark.Paths[0].Status != PathStatusVerified {
		t.Fatalf("unexpected benchmark paths: %#v", got.Benchmark.Paths)
	}
	profilesByID := make(map[string]string, len(got.Benchmark.Profiles))
	for _, benchmark := range got.Benchmark.Profiles {
		profilesByID[benchmark.ProfileID] = benchmark.Class
	}
	if profilesByID["video_h264_1080p"] != "moderate" {
		t.Fatalf("unexpected 1080p profile benchmark summary: %#v", got.Benchmark.Profiles)
	}
	if profilesByID["video_h264_1080i"] != "weak" {
		t.Fatalf("unexpected 1080i profile benchmark summary: %#v", got.Benchmark.Profiles)
	}
	if profilesByID["video_h264_1080i50"] != "weak" {
		t.Fatalf("unexpected 1080i50 profile benchmark summary: %#v", got.Benchmark.Profiles)
	}
	if profilesByID["video_h264_2160p50"] != "weak" {
		t.Fatalf("unexpected 2160p50 profile benchmark summary: %#v", got.Benchmark.Profiles)
	}
	if got.Concurrency.TunersAvailable != 3 || got.Concurrency.SessionsActive != 5 || got.Concurrency.TranscodesActive != 2 || got.Concurrency.ActiveVAAPITokens != 1 {
		t.Fatalf("unexpected concurrency snapshot: %#v", got.Concurrency)
	}
	if got.Concurrency.MaxSessions != 8 || got.Concurrency.MaxVAAPITokens != 1 {
		t.Fatalf("unexpected concurrency limits: %#v", got.Concurrency)
	}
	if len(got.Capabilities.HardwareVideoCodec) != 2 || got.Capabilities.HardwareVideoCodec[0] != "h264" || got.Capabilities.HardwareVideoCodec[1] != "hevc" {
		t.Fatalf("unexpected hardware codec snapshot: %#v", got.Capabilities.HardwareVideoCodec)
	}
}

func TestSnapshotHostRuntime_DemotesPerformanceClassUnderConstrainedCPULoad(t *testing.T) {
	resetVaapiState(t)
	SetVAAPIPreflightResult(true)
	SetVAAPIEncoderCapabilities(map[string]VAAPIEncoderCapability{
		"h264_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 120 * time.Millisecond},
		"hevc_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 90 * time.Millisecond},
	})

	got := SnapshotHostRuntime(true, true, admissionruntime.RuntimeState{
		TunerSlots:       3,
		SessionsActive:   2,
		TranscodesActive: 1,
	}, admissionmonitor.MonitorSnapshot{
		CPU: admissionmonitor.CPUSnapshot{
			Load1m:        8.6,
			CoreCount:     8,
			SampleCount:   15,
			WindowSeconds: 30,
		},
		MaxSessions:    8,
		MaxVAAPITokens: 2,
	})

	if got.PerformanceClass != "medium" {
		t.Fatalf("expected constrained cpu load to demote high host to medium, got %#v", got.PerformanceClass)
	}
}

func TestSnapshotHostRuntime_DemotesPerformanceClassUnderHeavyTranscodeDensity(t *testing.T) {
	resetVaapiState(t)
	SetVAAPIPreflightResult(true)
	SetVAAPIEncoderCapabilities(map[string]VAAPIEncoderCapability{
		"h264_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 120 * time.Millisecond},
		"hevc_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 90 * time.Millisecond},
	})

	got := SnapshotHostRuntime(true, true, admissionruntime.RuntimeState{
		TunerSlots:       3,
		SessionsActive:   5,
		TranscodesActive: 4,
	}, admissionmonitor.MonitorSnapshot{
		CPU: admissionmonitor.CPUSnapshot{
			Load1m:        2.5,
			CoreCount:     8,
			SampleCount:   15,
			WindowSeconds: 30,
		},
		MaxSessions:    8,
		MaxVAAPITokens: 2,
	})

	if got.PerformanceClass != "medium" {
		t.Fatalf("expected heavy transcode density to demote high host to medium, got %#v", got.PerformanceClass)
	}
}

func TestSnapshotHostRuntime_LeavesBenchmarkEmptyWithoutMeasuredHardware(t *testing.T) {
	resetVaapiState(t)

	got := SnapshotHostRuntime(true, true, admissionruntime.RuntimeState{}, admissionmonitor.MonitorSnapshot{
		CPU: admissionmonitor.CPUSnapshot{
			Load1m:        0.4,
			CoreCount:     4,
			SampleCount:   10,
			WindowSeconds: 30,
		},
	})

	if got.Benchmark.Class != "" || got.Benchmark.PreferredCodec != "" || got.Benchmark.FastestProbeElapsedMs != 0 {
		t.Fatalf("expected empty benchmark summary without measured hardware, got %#v", got.Benchmark)
	}
}
