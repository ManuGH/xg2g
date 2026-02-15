// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"context"
	"reflect"

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

// V3Overrides applies optional test/runtime overrides that are not part of core runtime wiring.
type V3Overrides struct {
	VerificationStore verification.Store
	VODProber         vod.Prober
	Resolver          recservice.Resolver
	RecordingsService recservice.Service
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

// SetV3Components is a compatibility wrapper for runtime wiring call sites.
func (s *Server) SetV3Components(
	b bus.Bus,
	st store.StateStore,
	rs resume.Store,
	sm *scan.Manager,
) {
	s.WireV3Runtime(b, st, rs, sm, nil)
}

// WireV3Overrides applies optional v3 override dependencies through one typed entrypoint.
func (s *Server) WireV3Overrides(overrides V3Overrides) {
	if !isNilInterface(overrides.VerificationStore) {
		s.mu.Lock()
		s.verificationStore = overrides.VerificationStore
		s.mu.Unlock()
	}

	s.mu.RLock()
	handler := s.v3Handler
	cfg := s.cfg
	owiClient := s.owiClient
	resumeStore := s.resumeStore
	vodManager := s.vodManager
	s.mu.RUnlock()

	if !isNilInterface(overrides.VODProber) && vodManager != nil {
		vodManager.SetProber(overrides.VODProber)
	}

	var resolvedService recservice.Service
	updatedService := false

	if !isNilInterface(overrides.Resolver) {
		if handler != nil {
			handler.SetResolver(overrides.Resolver)
		}
		if isNilInterface(overrides.RecordingsService) {
			owiAdapter := v3.NewOWIAdapter(owiClient)
			resumeAdapter := v3.NewResumeAdapter(resumeStore)
			recSvc, err := recservice.NewService(&cfg, vodManager, overrides.Resolver, owiAdapter, resumeAdapter, overrides.Resolver)
			if err != nil {
				log.L().Error().Err(err).Msg("failed to re-initialize recordings service")
			} else {
				resolvedService = recSvc
				updatedService = true
			}
		}
	}

	if !isNilInterface(overrides.RecordingsService) {
		resolvedService = overrides.RecordingsService
		updatedService = true
	}

	if updatedService {
		s.mu.Lock()
		s.recordingsService = resolvedService
		s.mu.Unlock()
		s.syncV3HandlerDependencies()
	}
}

// SetVerificationStore is a compatibility wrapper for override wiring call sites.
func (s *Server) SetVerificationStore(store verification.Store) {
	s.WireV3Overrides(V3Overrides{VerificationStore: store})
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

func isNilInterface(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
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
