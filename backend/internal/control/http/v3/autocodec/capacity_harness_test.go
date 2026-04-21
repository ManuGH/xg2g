package autocodec

import (
	"context"
	"testing"
	"time"

	admissionmonitor "github.com/ManuGH/xg2g/internal/admission"
	"github.com/ManuGH/xg2g/internal/config"
	controladmission "github.com/ManuGH/xg2g/internal/control/admission"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
)

func TestCapacityHarness_EightCoreHostDemotesAV1ToHEVCAtFourTranscodes(t *testing.T) {
	configureStrongAutoCodecHardware(t)

	hostRuntime := hardware.SnapshotHostRuntime(
		true,
		true,
		controladmission.RuntimeState{
			TunerSlots:       3,
			SessionsActive:   4,
			TranscodesActive: 4,
		},
		admissionmonitor.MonitorSnapshot{
			CPU: admissionmonitor.CPUSnapshot{
				Load1m:        2.5,
				CoreCount:     8,
				SampleCount:   15,
				WindowSeconds: 30,
			},
			MaxSessions:    8,
			MaxVAAPITokens: 4,
		},
	)

	if hostRuntime.PerformanceClass != "medium" {
		t.Fatalf("expected medium host under four active transcodes, got %#v", hostRuntime.PerformanceClass)
	}
	if got := playbackprofile.BenchmarkClassForCodec(hostRuntime.Benchmark, "av1"); got != "strong" {
		t.Fatalf("expected strong AV1 benchmark class, got %q", got)
	}

	profileID := PickProfileForCodecsForClientAndHost("av1,hevc,h264", "", profiles.HWAccelAuto, hostRuntime)
	if profileID != profiles.ProfileSafariHEVCHW {
		t.Fatalf("expected HEVC profile after demotion, got %q", profileID)
	}

	trace := DescribeSelection("av1,hevc,h264", profileID, hostRuntime)
	if trace.Policy != "host_aware_bottleneck" {
		t.Fatalf("expected host-aware selection policy, got %#v", trace)
	}
	if trace.SelectedCodec != "hevc" {
		t.Fatalf("expected HEVC selected codec, got %#v", trace)
	}
	if trace.PerformanceClass != "medium" {
		t.Fatalf("expected medium performance class in trace, got %#v", trace)
	}
	if trace.CodecBenchmarkClass != "strong" {
		t.Fatalf("expected strong benchmark class for HEVC, got %#v", trace)
	}
}

func TestCapacityHarness_DefaultAdmissionCeilingBlocksMediumDemotionOnEightCoreHost(t *testing.T) {
	configureStrongAutoCodecHardware(t)

	ctrl := controladmission.NewController(config.AppConfig{
		Engine: config.EngineConfig{
			Enabled:    true,
			TunerSlots: []int{0, 1, 2},
		},
		Limits: config.LimitsConfig{
			MaxSessions:   8,
			MaxTranscodes: 2,
		},
	})

	state := controladmission.RuntimeState{
		TunerSlots:       3,
		SessionsActive:   0,
		TranscodesActive: 0,
	}
	for attempt := 0; attempt < 2; attempt++ {
		decision := ctrl.Check(context.Background(), controladmission.Request{WantsTranscode: true}, state)
		if !decision.Allow {
			t.Fatalf("expected attempt %d to be admitted, got %#v", attempt+1, decision)
		}
		state.SessionsActive++
		state.TranscodesActive++
	}

	rejected := ctrl.Check(context.Background(), controladmission.Request{WantsTranscode: true}, state)
	if rejected.Allow || rejected.Problem == nil || rejected.Problem.Code != controladmission.CodeTranscodesFull {
		t.Fatalf("expected third transcode to be rejected by admission, got %#v", rejected)
	}

	hostRuntime := hardware.SnapshotHostRuntime(
		true,
		true,
		state,
		admissionmonitor.MonitorSnapshot{
			CPU: admissionmonitor.CPUSnapshot{
				Load1m:        2.5,
				CoreCount:     8,
				SampleCount:   15,
				WindowSeconds: 30,
			},
			MaxSessions:    8,
			MaxVAAPITokens: 2,
		},
	)

	if hostRuntime.PerformanceClass != "high" {
		t.Fatalf("expected high host performance with only two admitted transcodes, got %#v", hostRuntime.PerformanceClass)
	}

	profileID := PickProfileForCodecsForClientAndHost("av1,hevc,h264", "", profiles.HWAccelAuto, hostRuntime)
	if profileID != profiles.ProfileAV1HW {
		t.Fatalf("expected AV1 to remain selected when admission ceiling blocks demotion load, got %q", profileID)
	}

	trace := DescribeSelection("av1,hevc,h264", profileID, hostRuntime)
	if trace.SelectedCodec != "av1" || trace.PerformanceClass != "high" {
		t.Fatalf("expected AV1/high trace after blocked load build-up, got %#v", trace)
	}
}

func configureStrongAutoCodecHardware(t *testing.T) {
	t.Helper()

	hardware.SetVAAPIPreflightResult(true)
	hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{
		"av1_vaapi":  {Verified: true, AutoEligible: true, ProbeElapsed: 30 * time.Millisecond},
		"hevc_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 45 * time.Millisecond},
		"h264_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 10 * time.Millisecond},
	})
	t.Cleanup(func() {
		hardware.SetVAAPIPreflightResult(false)
		hardware.SetVAAPIEncoderCapabilities(nil)
	})
}
