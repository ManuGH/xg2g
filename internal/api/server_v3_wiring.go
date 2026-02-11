// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"context"

	"github.com/ManuGH/xg2g/internal/channels"
	"github.com/ManuGH/xg2g/internal/control/admission"
	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
	recservice "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/control/vod"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/ManuGH/xg2g/internal/dvr"
	"github.com/ManuGH/xg2g/internal/epg"
	"github.com/ManuGH/xg2g/internal/health"
	"github.com/ManuGH/xg2g/internal/library"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/ManuGH/xg2g/internal/pipeline/bus"
	"github.com/ManuGH/xg2g/internal/pipeline/resume"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
	"github.com/ManuGH/xg2g/internal/recordings"
	"github.com/ManuGH/xg2g/internal/verification"
)

type v3DependencySnapshot struct {
	handler             *v3.Server
	bus                 bus.Bus
	store               store.StateStore
	resume              resume.Store
	scan                *scan.Manager
	recordingPathMapper *recordings.PathMapper
	channelManager      *channels.Manager
	seriesManager       *dvr.Manager
	seriesEngine        *dvr.SeriesEngine
	vodManager          *vod.Manager
	epgCache            *epg.TV
	healthManager       *health.Manager
	recordingsService   recservice.Service
	requestShutdown     func(context.Context) error
	preflightProvider   v3.PreflightProvider
}

func (s *Server) snapshotV3Dependencies() v3DependencySnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return v3DependencySnapshot{
		handler:             s.v3Handler,
		bus:                 s.v3Bus,
		store:               s.v3Store,
		resume:              s.resumeStore,
		scan:                s.v3Scan,
		recordingPathMapper: s.recordingPathMapper,
		channelManager:      s.channelManager,
		seriesManager:       s.seriesManager,
		seriesEngine:        s.seriesEngine,
		vodManager:          s.vodManager,
		epgCache:            s.epgCache,
		healthManager:       s.healthManager,
		recordingsService:   s.recordingsService,
		requestShutdown:     s.requestShutdown,
		preflightProvider:   s.preflightProvider,
	}
}

func (s *Server) syncV3HandlerDependencies() {
	deps := s.snapshotV3Dependencies()
	if deps.handler == nil {
		return
	}

	deps.handler.SetDependencies(v3.Dependencies{
		Bus:               deps.bus,
		Store:             deps.store,
		ResumeStore:       deps.resume,
		Scan:              deps.scan,
		PathMapper:        deps.recordingPathMapper,
		ChannelManager:    deps.channelManager,
		SeriesManager:     deps.seriesManager,
		SeriesEngine:      deps.seriesEngine,
		VODManager:        deps.vodManager,
		EPGCache:          deps.epgCache,
		HealthManager:     deps.healthManager,
		LogSource:         logSourceWrapper{},
		ScanSource:        deps.scan,
		DVRSource:         &dvrSourceWrapper{s},
		ServicesSource:    deps.channelManager,
		TimersSource:      &dvrSourceWrapper{s},
		RecordingsService: deps.recordingsService,
		RequestShutdown:   deps.requestShutdown,
		PreflightProvider: deps.preflightProvider,
	})
}

// WireV3Runtime applies runtime-provided v3 dependencies in one DI call.
func (s *Server) WireV3Runtime(
	b bus.Bus,
	st store.StateStore,
	rs resume.Store,
	sm *scan.Manager,
	adm *admission.Controller,
) {
	s.mu.Lock()
	s.v3Bus = b
	s.v3Store = st
	s.resumeStore = rs
	s.v3Scan = sm
	handler := s.v3Handler
	s.mu.Unlock()

	s.syncV3HandlerDependencies()
	if handler != nil && adm != nil {
		handler.SetAdmission(adm)
	}
}

// SetVerificationStore configures the verification store for drift detection status.
func (s *Server) SetVerificationStore(st verification.Store) {
	s.mu.Lock()
	s.verificationStore = st
	s.mu.Unlock()
}

// LibraryService returns the underlying library service from v3 handler.
func (s *Server) LibraryService() *library.Service {
	if s.v3Handler != nil {
		return s.v3Handler.LibraryService()
	}
	return nil
}

// VODManager returns the underlying VOD manager.
func (s *Server) VODManager() *vod.Manager {
	return s.vodManager
}

// SetVODProber injects a custom prober into the VOD manager for testing.
func (s *Server) SetVODProber(p vod.Prober) {
	if s.vodManager != nil {
		s.vodManager.SetProber(p)
	}
}

// SetResolver injects a resolver into the v3 handler (tests).
func (s *Server) SetResolver(r recservice.Resolver) {
	if s.v3Handler != nil {
		s.v3Handler.SetResolver(r)
	}
	if r == nil {
		return
	}

	owiAdapter := v3.NewOWIAdapter(s.owiClient)
	resumeAdapter := v3.NewResumeAdapter(s.resumeStore)
	recSvc, err := recservice.NewService(&s.cfg, s.vodManager, r, owiAdapter, resumeAdapter, r)
	if err != nil {
		log.L().Error().Err(err).Msg("failed to re-initialize recordings service")
		return
	}
	s.recordingsService = recSvc
	s.syncV3HandlerDependencies()
}

// SetRecordingsService injects a recordings service into the v3 handler (tests).
func (s *Server) SetRecordingsService(svc recservice.Service) {
	s.recordingsService = svc
	s.syncV3HandlerDependencies()
}

type logSourceWrapper struct{}

func (l logSourceWrapper) GetRecentLogs() []log.LogEntry {
	return log.GetRecentLogs()
}

type dvrSourceWrapper struct {
	s *Server
}

func (d *dvrSourceWrapper) GetStatusInfo(ctx context.Context) (*openwebif.StatusInfo, error) {
	d.s.mu.RLock()
	cfg := d.s.cfg
	d.s.mu.RUnlock()
	client := openwebif.New(cfg.Enigma2.BaseURL)
	return client.GetStatusInfo(ctx)
}

func (d *dvrSourceWrapper) DetectTimerChange(ctx context.Context) (openwebif.TimerChangeCap, error) {
	d.s.mu.RLock()
	cfg := d.s.cfg
	d.s.mu.RUnlock()
	client := openwebif.New(cfg.Enigma2.BaseURL)
	return client.DetectTimerChange(ctx)
}

func (d *dvrSourceWrapper) GetTimers(ctx context.Context) ([]openwebif.Timer, error) {
	d.s.mu.RLock()
	cfg := d.s.cfg
	d.s.mu.RUnlock()
	client := openwebif.New(cfg.Enigma2.BaseURL)
	return client.GetTimers(ctx)
}
