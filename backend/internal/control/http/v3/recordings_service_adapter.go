package v3

import (
	"context"

	"github.com/ManuGH/xg2g/internal/config"
	v3recordings "github.com/ManuGH/xg2g/internal/control/http/v3/recordings"
	"github.com/ManuGH/xg2g/internal/control/recordings/capreg"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
)

type serverRecordingsDeps struct {
	s *Server
}

var _ v3recordings.Deps = (*serverRecordingsDeps)(nil)

func (d *serverRecordingsDeps) RecordingsService() v3recordings.RecordingsService {
	return d.s.recordingsModuleDeps().recordingsService
}

func (d *serverRecordingsDeps) ChannelTruthSource() v3recordings.ChannelTruthSource {
	d.s.mu.RLock()
	defer d.s.mu.RUnlock()
	return d.s.v3Scan
}

func (d *serverRecordingsDeps) DecisionAuditSink() v3recordings.DecisionAuditSink {
	d.s.mu.RLock()
	defer d.s.mu.RUnlock()
	return d.s.decisionAudit
}

func (d *serverRecordingsDeps) CapabilityRegistry() capreg.Store {
	d.s.mu.RLock()
	defer d.s.mu.RUnlock()
	return d.s.capabilityRegistry
}

func (d *serverRecordingsDeps) Config() config.AppConfig {
	return d.s.GetConfig()
}

func (d *serverRecordingsDeps) HostPressure(ctx context.Context) playbackprofile.HostPressureAssessment {
	return d.s.currentHostPressure(ctx)
}

func (d *serverRecordingsDeps) HostRuntime(ctx context.Context) playbackprofile.HostRuntimeSnapshot {
	return d.s.currentHostRuntime(ctx)
}

func (d *serverRecordingsDeps) ReceiverContext(ctx context.Context) *capreg.ReceiverContext {
	return d.s.currentReceiverContext(ctx)
}

func (s *Server) recordingsProcessor() *v3recordings.Service {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.recordingsV3Service == nil {
		s.recordingsV3Service = v3recordings.NewService(&serverRecordingsDeps{s: s})
	}
	return s.recordingsV3Service
}
