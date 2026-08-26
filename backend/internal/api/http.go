// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Package api provides HTTP server functionality for the xg2g application.
package api

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ManuGH/xg2g/internal/channels"
	"github.com/ManuGH/xg2g/internal/config"
	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
	recservice "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/control/vod"
	"github.com/ManuGH/xg2g/internal/domain/identity"
	"github.com/ManuGH/xg2g/internal/dvr"
	"github.com/ManuGH/xg2g/internal/epg"
	"github.com/ManuGH/xg2g/internal/hdhr"
	"github.com/ManuGH/xg2g/internal/health"
	"github.com/ManuGH/xg2g/internal/jobs"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/ManuGH/xg2g/internal/recordings"
	"github.com/ManuGH/xg2g/internal/stream/ingest/pipeline"
	ingestsession "github.com/ManuGH/xg2g/internal/stream/ingest/session"
	"github.com/ManuGH/xg2g/internal/verification"

	"github.com/ManuGH/xg2g/internal/resilience"
)

// Server represents the HTTP API server for xg2g.
type Server struct {
	mu             sync.RWMutex
	refreshing     atomic.Bool // serialize refreshes via atomic flag
	cfg            config.AppConfig
	snap           config.Snapshot
	configHolder   ConfigHolder
	status         jobs.Status
	cb             *resilience.CircuitBreaker
	hdhr           *hdhr.Server      // HDHomeRun emulation server
	auditLogger    AuditLogger       // Optional: for audit logging
	healthManager  *health.Manager   // Health and readiness checks
	channelManager *channels.Manager // Channel management
	configManager  *config.Manager   // Config operations
	seriesManager  *dvr.Manager      // Series Recording Rules (DVR v2)
	seriesEngine   *dvr.SeriesEngine // Series Recording Engine (DVR v2.1)

	// refreshFn allows tests to stub the refresh operation; defaults to jobs.Refresh
	refreshFn      func(context.Context, config.Snapshot) (*jobs.Status, error)
	startTime      time.Time
	piconSemaphore chan struct{} // Limit concurrent upstream picon fetches

	// EPG Cache (P1 Performance Fix)
	epgCache *epg.TV

	// Phase B: SOA Refactor - VOD Manager
	vodManager *vod.Manager

	// OpenWebIF Client Cache (P1 Performance Fix)
	owiClient *openwebif.Client // In-memory cache for openWebIF client

	// v3 Integration
	v3Handler         *v3.Server
	v3RuntimeDeps     v3.Dependencies
	identityService   *identity.Service
	verificationStore verification.Store // P8.3: Verification Store
	recordingsService recservice.Service

	// Recording Playback Path Mapper
	recordingPathMapper *recordings.PathMapper

	// P8.2: Hardening & Test Stability
	preflightProvider v3.PreflightProvider

	// P9: Safety & Shutdown
	rootCtx         context.Context
	rootCancel      context.CancelFunc
	shutdownFn      func(context.Context) error
	started         atomic.Bool // P10: Lifecycle Invariant (Deliverable #4)
	topologyService pipeline.TopologyService

	// liveSessionMgr is the shared ingest of the live route. It is kept here so
	// the media path can be given the same manager: a second one would mean a
	// second connection to the receiver for the same service, which is the whole
	// thing shared ingest exists to prevent.
	liveSessionMgr *ingestsession.Manager

	// Dependency Injection (Internal)
	v3Factory func(config.AppConfig, *config.Manager, context.CancelFunc) *v3.Server
}

// AuditLogger interface for audit logging (optional).
type AuditLogger interface {
	ConfigReload(actor, result string, details map[string]string)
	RefreshStart(actor string, bouquets []string)
	RefreshComplete(actor string, channels, bouquets int, durationMS int64)
	RefreshError(actor, reason string)
	AuthSuccess(remoteAddr, endpoint string)
	AuthFailure(remoteAddr, endpoint, reason string)
	AuthMissing(remoteAddr, endpoint string)
	RateLimitExceeded(remoteAddr, endpoint string)
}

// ServerOption allows functional configuration of the Server.
type ServerOption func(*Server)

// WithV3ServerFactory overrides the v3 server implementation (for tests).
func WithV3ServerFactory(f func(config.AppConfig, *config.Manager, context.CancelFunc) *v3.Server) ServerOption {
	return func(s *Server) {
		s.v3Factory = f
	}
}

// WithTopologyService injects the physical receiver topology service for hardware tuner admission.
func WithTopologyService(topoSvc pipeline.TopologyService) ServerOption {
	return func(s *Server) {
		s.topologyService = topoSvc
	}
}

// SetTopologyService configures the active receiver topology service.
func (s *Server) SetTopologyService(topoSvc pipeline.TopologyService) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.topologyService = topoSvc
}

// TopologyService returns the configured topology service.
func (s *Server) TopologyService() pipeline.TopologyService {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.topologyService
}

// LiveSessionManager returns the shared ingest manager backing the live route, or
// nil before the routes are wired. The media path is handed this instance rather
// than building its own: two managers would each dial the receiver for the same
// service, and the coalescing would silently not happen.
func (s *Server) LiveSessionManager() *ingestsession.Manager {
	return s.liveSessionMgr
}

// WithRootContext sets the server root context before subsystem wiring.
// Use this at construction time so lifecycle-bound components are created with the final context.
func WithRootContext(ctx context.Context) ServerOption {
	return func(s *Server) {
		if ctx == nil {
			return
		}
		if s.rootCancel != nil {
			s.rootCancel()
		}
		s.rootCtx, s.rootCancel = context.WithCancel(ctx)
	}
}

// ConfigHolder interface allows hot configuration reloading without import cycles.
// Implemented by config.ConfigHolder.
type ConfigHolder interface {
	Current() *config.Snapshot
	Reload(ctx context.Context) error
}
