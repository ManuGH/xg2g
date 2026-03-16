package hardware

import (
	"testing"

	admissionmonitor "github.com/ManuGH/xg2g/internal/admission"
	admissionruntime "github.com/ManuGH/xg2g/internal/control/admission"
)

func TestSnapshotHostRuntime_CombinesCapabilitiesAndRuntime(t *testing.T) {
	resetVaapiState(t)
	SetVAAPIPreflightResult(true)
	SetVAAPIEncoderPreflight(map[string]bool{
		"h264_vaapi": true,
		"hevc_vaapi": true,
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
