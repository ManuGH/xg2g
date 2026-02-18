// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/admission"
	"github.com/ManuGH/xg2g/internal/control/http/v3/recordings/artifacts"
	"github.com/ManuGH/xg2g/internal/control/read"
	recservice "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/control/vod"
	"github.com/ManuGH/xg2g/internal/epg"
	"github.com/ManuGH/xg2g/internal/health"
	"github.com/ManuGH/xg2g/internal/jobs"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/pipeline/bus"
	"github.com/ManuGH/xg2g/internal/pipeline/resume"
	recinfra "github.com/ManuGH/xg2g/internal/recordings"
)

// sessionsModuleDeps scopes the dependencies used by session/intent/stream handlers.
type sessionsModuleDeps struct {
	cfg            config.AppConfig
	snap           config.Snapshot
	store          SessionStateStore
	bus            bus.Bus
	resumeStore    resume.Store
	scanSource     ScanSource
	epgCache       *epg.TV
	channelScanner ChannelScanner
	preflight      PreflightProvider
	receiver       receiverControlFactory
	admission      *admission.Controller
	admissionState AdmissionState
}

func (s *Server) sessionsModuleDeps() sessionsModuleDeps {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return sessionsModuleDeps{
		cfg:            s.cfg,
		snap:           s.snap,
		store:          s.v3Store,
		bus:            s.v3Bus,
		resumeStore:    s.resumeStore,
		scanSource:     s.scanSource,
		epgCache:       s.epgCache,
		channelScanner: s.v3Scan,
		preflight:      s.preflightProvider,
		receiver:       s.owi,
		admission:      s.admission,
		admissionState: s.admissionState,
	}
}

// recordingsModuleDeps scopes the dependencies used by recordings/VOD handlers.
type recordingsModuleDeps struct {
	cfg               config.AppConfig
	recordingsService recservice.Service
	artifacts         artifacts.Resolver
	pathMapper        *recinfra.PathMapper
	vodManager        *vod.Manager
	resumeStore       resume.Store
}

func (s *Server) recordingsModuleDeps() recordingsModuleDeps {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return recordingsModuleDeps{
		cfg:               s.cfg,
		recordingsService: s.recordingsService,
		artifacts:         s.artifacts,
		pathMapper:        s.recordingPathMapper,
		vodManager:        s.vodManager,
		resumeStore:       s.resumeStore,
	}
}

// systemModuleDeps scopes dependencies used by system/status/health handlers.
type systemModuleDeps struct {
	cfg            config.AppConfig
	snap           config.Snapshot
	status         jobs.Status
	healthManager  *health.Manager
	logSource      interface{ GetRecentLogs() []log.LogEntry }
	channelScanner ChannelScanner
	scanSource     ScanSource
	dvrSource      RecordingStatusProvider
	servicesSource ServiceStateReader
	timersSource   TimerReader
	epgSource      read.EpgSource
}

func (s *Server) systemModuleDeps() systemModuleDeps {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return systemModuleDeps{
		cfg:            s.cfg,
		snap:           s.snap,
		status:         s.status,
		healthManager:  s.healthManager,
		logSource:      s.logSource,
		channelScanner: s.v3Scan,
		scanSource:     s.scanSource,
		dvrSource:      s.dvrSource,
		servicesSource: s.servicesSource,
		timersSource:   s.timersSource,
		epgSource:      s.epgSource,
	}
}
