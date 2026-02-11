// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"context"

	"github.com/ManuGH/xg2g/internal/control/admission"
	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
	recservice "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/control/vod"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/ManuGH/xg2g/internal/library"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/ManuGH/xg2g/internal/pipeline/bus"
	"github.com/ManuGH/xg2g/internal/pipeline/resume"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
	"github.com/ManuGH/xg2g/internal/verification"
)

// SetV3Components configures v3 event bus, store, and scan manager.
func (s *Server) SetV3Components(b bus.Bus, st store.StateStore, rs resume.Store, sm *scan.Manager) {
	s.mu.Lock()
	s.v3Bus = b
	s.v3Store = st
	s.resumeStore = rs
	s.v3Scan = sm
	s.mu.Unlock()

	// Update sub-handler dependencies (it maintains its own state)
	if s.v3Handler != nil {
		s.v3Handler.SetDependencies(
			b, st, rs, sm,
			s.recordingPathMapper,
			s.channelManager,
			s.seriesManager,
			s.seriesEngine,
			s.vodManager,
			s.epgCache,
			s.healthManager,
			logSourceWrapper{},
			s.v3Scan,
			&dvrSourceWrapper{s},
			s.channelManager,
			&dvrSourceWrapper{s},
			s.recordingsService,
			s.requestShutdown,
			s.preflightProvider,
		)
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
	if s.v3Handler != nil {
		s.v3Handler.SetDependencies(
			s.v3Bus,
			s.v3Store,
			s.resumeStore,
			s.v3Scan,
			s.recordingPathMapper,
			s.channelManager,
			s.seriesManager,
			s.seriesEngine,
			s.vodManager,
			s.epgCache,
			s.healthManager,
			logSourceWrapper{},
			s.v3Scan,
			&dvrSourceWrapper{s},
			s.channelManager,
			&dvrSourceWrapper{s},
			s.recordingsService,
			s.requestShutdown,
			s.preflightProvider,
		)
	}
}

// SetRecordingsService injects a recordings service into the v3 handler (tests).
func (s *Server) SetRecordingsService(svc recservice.Service) {
	if s.v3Handler != nil {
		s.v3Handler.SetRecordingsService(svc)
	}
	s.recordingsService = svc
}

// SetAdmission sets the controller for admission control.
func (s *Server) SetAdmission(adm *admission.Controller) {
	if s.v3Handler != nil {
		s.v3Handler.SetAdmission(adm)
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
