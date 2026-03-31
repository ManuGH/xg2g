package v3

import (
	"context"

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
)

const (
	hostPressureMinActionableCPUSamples = 15
	hostPressureElevatedSessionRatio    = 0.85
	hostPressureElevatedVAAPIRatio      = 0.75
)

func (s *Server) currentHostPressure(ctx context.Context) playbackprofile.HostPressureAssessment {
	snapshot, tracker, ok := s.hostRuntimeSnapshot(ctx)
	if !ok || tracker == nil || !hostPressureActionable(snapshot) {
		return playbackprofile.HostPressureAssessment{}
	}
	return tracker.Evaluate(snapshot)
}

func (s *Server) currentHostRuntime(ctx context.Context) playbackprofile.HostRuntimeSnapshot {
	snapshot, _, ok := s.hostRuntimeSnapshot(ctx)
	if !ok {
		return playbackprofile.HostRuntimeSnapshot{}
	}
	return snapshot
}

func (s *Server) hostRuntimeSnapshot(ctx context.Context) (playbackprofile.HostRuntimeSnapshot, *hardware.PressureTracker, bool) {
	deps := s.sessionsModuleDeps()

	s.mu.RLock()
	monitor := s.hostPressureMonitor
	tracker := s.hostPressureTracker
	s.mu.RUnlock()

	if deps.admissionState == nil || monitor == nil {
		return playbackprofile.HostRuntimeSnapshot{}, tracker, false
	}

	runtimeState := CollectRuntimeState(ctx, deps.admissionState)
	snapshot := hardware.SnapshotHostRuntime(deps.cfg.FFmpeg.Bin != "", deps.cfg.HLS.Root != "", runtimeState, monitor.Snapshot())
	return snapshot, tracker, true
}

func hostPressureActionable(snapshot playbackprofile.HostRuntimeSnapshot) bool {
	if snapshot.CPU.CoreCount > 0 && snapshot.CPU.SampleCount >= hostPressureMinActionableCPUSamples {
		return true
	}
	if maxSessions := snapshot.Concurrency.MaxSessions; maxSessions > 0 {
		sessionRatio := float64(snapshot.Concurrency.SessionsActive) / float64(maxSessions)
		if sessionRatio >= hostPressureElevatedSessionRatio {
			return true
		}
	}
	if maxTokens := snapshot.Concurrency.MaxVAAPITokens; maxTokens > 0 {
		vaapiRatio := float64(snapshot.Concurrency.ActiveVAAPITokens) / float64(maxTokens)
		if vaapiRatio >= hostPressureElevatedVAAPIRatio {
			return true
		}
	}
	return false
}
