// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"context"
	"fmt"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/admission"
	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
	"github.com/ManuGH/xg2g/internal/dvr"
	"github.com/ManuGH/xg2g/internal/jobs"
	"github.com/ManuGH/xg2g/internal/resilience"
)

// New creates and initializes a new HTTP API server.
func New(cfg config.AppConfig, cfgMgr *config.Manager, opts ...ServerOption) (*Server, error) {
	return NewWithDeps(cfg, cfgMgr, ConstructorDeps{}, opts...)
}

// NewWithDeps creates and initializes a new HTTP API server using externally
// composed constructor dependencies when provided.
func NewWithDeps(cfg config.AppConfig, cfgMgr *config.Manager, constructorDeps ConstructorDeps, opts ...ServerOption) (*Server, error) {
	// 1. Initialized root context for server lifecycle (MUST be before v3Handler)
	rootCtx, rootCancel := context.WithCancel(context.Background())

	deps := resolveConstructorDeps(cfg, constructorDeps)

	s := &Server{
		cfg:                 cfg,
		configManager:       cfgMgr,
		rootCtx:             rootCtx,
		rootCancel:          rootCancel,
		snap:                deps.snapshot,
		channelManager:      deps.channelManager,
		seriesManager:       deps.seriesManager,
		recordingPathMapper: deps.pathMapper,
		status: jobs.Status{
			Version: cfg.Version, // Initialize version from config
		},
		startTime:      time.Now(),
		piconSemaphore: make(chan struct{}, 50),
		v3Factory:      v3.NewServer, // Default factory
	}

	if err := s.initPlaybackSubsystem(cfg); err != nil {
		rootCancel()
		return nil, err
	}

	for _, opt := range opts {
		opt(s)
	}

	// v3Handler expects a valid root cancel function
	if cfgMgr == nil {
		rootCancel()
		return nil, fmt.Errorf("config manager is required for API server initialization")
	}
	if err := s.wireV3Subsystem(cfg, cfgMgr); err != nil {
		rootCancel()
		return nil, err
	}

	// Server (s) implements EpgProvider interface via GetEvents method.
	s.seriesEngine = dvr.NewSeriesEngine(cfg, deps.seriesManager, func() dvr.OWIClient {
		return s.newSeriesOWIClient(cfg)
	})

	// Default refresh function
	s.refreshFn = jobs.Refresh
	// Initialize a conservative default circuit breaker (3 failures -> 30s open)
	s.cb = resilience.NewCircuitBreaker("v2-api", 5, 10, 60*time.Second, 30*time.Second, resilience.WithPanicRecovery(true))

	// Initialize health manager
	s.healthManager = s.newHealthManager(cfg)

	// P10: Wire runtime-provided v3 dependencies and admission via a single DI entrypoint.
	// Initialize with conservative defaults (10 concurrent transcodes, 10 CPU-heavy ops)
	// In the future this should come from config.
	adm := admission.NewController(cfg)
	s.WireV3Runtime(s.v3RuntimeDeps, adm)

	s.initHDHR(cfg, deps.channelManager)
	s.registerHealthCheckers(cfg)

	return s, nil
}
