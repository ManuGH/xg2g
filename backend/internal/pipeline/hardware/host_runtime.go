package hardware

import (
	admissionmonitor "github.com/ManuGH/xg2g/internal/admission"
	admissionruntime "github.com/ManuGH/xg2g/internal/control/admission"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
)

// SnapshotHostRuntime combines static host capabilities and live runtime counters into one snapshot.
func SnapshotHostRuntime(ffmpegAvailable, hlsAvailable bool, runtime admissionruntime.RuntimeState, monitor admissionmonitor.MonitorSnapshot) playbackprofile.HostRuntimeSnapshot {
	return playbackprofile.CanonicalizeHostRuntime(playbackprofile.HostRuntimeSnapshot{
		Capabilities: SnapshotTranscodeCapabilities(ffmpegAvailable, hlsAvailable),
		CPU: playbackprofile.HostCPUSnapshot{
			Load1m:        monitor.CPU.Load1m,
			CoreCount:     monitor.CPU.CoreCount,
			SampleCount:   monitor.CPU.SampleCount,
			WindowSeconds: monitor.CPU.WindowSeconds,
		},
		Concurrency: playbackprofile.HostConcurrencySnapshot{
			TunersAvailable:   runtime.TunerSlots,
			SessionsActive:    runtime.SessionsActive,
			TranscodesActive:  runtime.TranscodesActive,
			ActiveVAAPITokens: monitor.ActiveVAAPITokens,
			MaxSessions:       monitor.MaxSessions,
			MaxVAAPITokens:    monitor.MaxVAAPITokens,
		},
	})
}
