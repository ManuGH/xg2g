package main

import (
	"context"
	"fmt"

	"github.com/ManuGH/xg2g/internal/config"
	v3recordings "github.com/ManuGH/xg2g/internal/control/http/v3/recordings"
	"github.com/ManuGH/xg2g/internal/control/playback"
	domainrecordings "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/control/recordings/capreg"
	decisionaudit "github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
)

type storageDecisionSweepRecordingsService struct{}

func (storageDecisionSweepRecordingsService) ResolvePlayback(context.Context, string, string) (domainrecordings.PlaybackResolution, error) {
	return domainrecordings.PlaybackResolution{}, fmt.Errorf("recording playback is not used for live decision sweeps")
}

func (storageDecisionSweepRecordingsService) GetMediaTruth(context.Context, string) (playback.MediaTruth, error) {
	return playback.MediaTruth{}, fmt.Errorf("recording truth is not used for live decision sweeps")
}

type storageDecisionSweepDeps struct {
	cfg          config.AppConfig
	truthSource  v3recordings.ChannelTruthSource
	decisionSink v3recordings.DecisionAuditSink
}

type storageDecisionSweepScanStoreTruthSource struct {
	store *scan.SqliteStore
}

func (s storageDecisionSweepScanStoreTruthSource) GetCapability(serviceRef string) (scan.Capability, bool) {
	if s.store == nil {
		return scan.Capability{}, false
	}
	return s.store.Get(serviceRef)
}

type storageDecisionSweepOriginSink struct {
	inner v3recordings.DecisionAuditSink
}

func (s storageDecisionSweepOriginSink) Record(ctx context.Context, event decisionaudit.Event) error {
	if s.inner == nil {
		return nil
	}
	event.Origin = decisionaudit.OriginSweep
	return s.inner.Record(ctx, event)
}

func (d storageDecisionSweepDeps) RecordingsService() v3recordings.RecordingsService {
	return storageDecisionSweepRecordingsService{}
}

func (d storageDecisionSweepDeps) ChannelTruthSource() v3recordings.ChannelTruthSource {
	return d.truthSource
}

func (d storageDecisionSweepDeps) DecisionAuditSink() v3recordings.DecisionAuditSink {
	return d.decisionSink
}

func (d storageDecisionSweepDeps) Config() config.AppConfig {
	return d.cfg
}

func (d storageDecisionSweepDeps) HostPressure(context.Context) playbackprofile.HostPressureAssessment {
	return playbackprofile.HostPressureAssessment{}
}

func (d storageDecisionSweepDeps) HostRuntime(context.Context) playbackprofile.HostRuntimeSnapshot {
	return playbackprofile.HostRuntimeSnapshot{}
}

func (d storageDecisionSweepDeps) CapabilityRegistry() capreg.Store {
	return nil
}

func (d storageDecisionSweepDeps) ReceiverContext(context.Context) *capreg.ReceiverContext {
	return nil
}
