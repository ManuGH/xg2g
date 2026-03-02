// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"context"

	"github.com/ManuGH/xg2g/internal/control/admission"
	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
	"github.com/ManuGH/xg2g/internal/control/vod"
	"github.com/ManuGH/xg2g/internal/library"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/openwebif"
)

func (s *Server) snapshotV3Dependencies() v3DependencySnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return v3DependencySnapshot{
		handler:             s.v3Handler,
		runtimeDeps:         sanitizeV3RuntimeDependencies(s.v3RuntimeDeps),
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

func sanitizeV3RuntimeDependencies(deps v3.Dependencies) v3.Dependencies {
	return v3.Dependencies{
		Bus:         deps.Bus,
		Store:       deps.Store,
		ResumeStore: deps.ResumeStore,
		Scan:        deps.Scan,
	}
}

func (s *Server) syncV3HandlerDependencies() {
	deps := s.snapshotV3Dependencies()
	if deps.handler == nil {
		return
	}

	var scanSource v3.ScanSource
	if src, ok := deps.runtimeDeps.Scan.(v3.ScanSource); ok {
		scanSource = src
	}

	deps.handler.SetDependencies(v3.Dependencies{
		Bus:               deps.runtimeDeps.Bus,
		Store:             deps.runtimeDeps.Store,
		ResumeStore:       deps.runtimeDeps.ResumeStore,
		Scan:              deps.runtimeDeps.Scan,
		PathMapper:        deps.recordingPathMapper,
		ChannelManager:    deps.channelManager,
		SeriesManager:     deps.seriesManager,
		SeriesEngine:      deps.seriesEngine,
		VODManager:        deps.vodManager,
		EPGCache:          deps.epgCache,
		HealthManager:     deps.healthManager,
		LogSource:         logSourceWrapper{},
		ScanSource:        scanSource,
		DVRSource:         &dvrSourceWrapper{s},
		ServicesSource:    deps.channelManager,
		TimersSource:      &dvrSourceWrapper{s},
		RecordingsService: deps.recordingsService,
		RequestShutdown:   deps.requestShutdown,
		PreflightProvider: deps.preflightProvider,
	})
}

// WireV3Runtime applies runtime-provided v3 dependencies in one DI call.
func (s *Server) WireV3Runtime(runtimeDeps v3.Dependencies, adm *admission.Controller) {
	s.mu.Lock()
	s.v3RuntimeDeps = sanitizeV3RuntimeDependencies(runtimeDeps)
	handler := s.v3Handler
	s.mu.Unlock()

	s.syncV3HandlerDependencies()
	if handler != nil && adm != nil {
		handler.SetAdmission(adm)
	}
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

func (s *Server) receiverClient() *openwebif.Client {
	s.mu.RLock()
	cached := s.owiClient
	cfg := s.cfg
	snap := s.snap
	s.mu.RUnlock()
	if cached != nil {
		return cached
	}

	client := openwebif.NewWithPort(cfg.Enigma2.BaseURL, cfg.Enigma2.StreamPort, openwebif.Options{
		Timeout:               cfg.Enigma2.Timeout,
		ResponseHeaderTimeout: cfg.Enigma2.ResponseHeaderTimeout,
		Username:              cfg.Enigma2.Username,
		Password:              cfg.Enigma2.Password,
		UseWebIFStreams:       cfg.Enigma2.UseWebIFStreams,
		StreamBaseURL:         snap.Runtime.OpenWebIF.StreamBaseURL,
		HTTPMaxConnsPerHost:   snap.Runtime.OpenWebIF.HTTPMaxConnsPerHost,
	})

	s.mu.Lock()
	if s.owiClient == nil {
		s.owiClient = client
	} else {
		client = s.owiClient
	}
	s.mu.Unlock()

	return client
}

type logSourceWrapper struct{}

func (l logSourceWrapper) GetRecentLogs() []log.LogEntry {
	return log.GetRecentLogs()
}

type dvrSourceWrapper struct {
	s *Server
}

func (d *dvrSourceWrapper) GetStatusInfo(ctx context.Context) (*openwebif.StatusInfo, error) {
	return d.s.receiverClient().GetStatusInfo(ctx)
}

func (d *dvrSourceWrapper) DetectTimerChange(ctx context.Context) (openwebif.TimerChangeCap, error) {
	return d.s.receiverClient().DetectTimerChange(ctx)
}

func (d *dvrSourceWrapper) GetTimers(ctx context.Context) ([]openwebif.Timer, error) {
	return d.s.receiverClient().GetTimers(ctx)
}
