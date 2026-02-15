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
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/ManuGH/xg2g/internal/dvr"
	"github.com/ManuGH/xg2g/internal/epg"
	"github.com/ManuGH/xg2g/internal/hdhr"
	"github.com/ManuGH/xg2g/internal/health"
	"github.com/ManuGH/xg2g/internal/jobs"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/ManuGH/xg2g/internal/pipeline/bus"
	"github.com/ManuGH/xg2g/internal/pipeline/resume"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
	"github.com/ManuGH/xg2g/internal/recordings"
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
	v3Bus             bus.Bus
	v3Store           store.StateStore
	verificationStore verification.Store // P8.3: Verification Store
	resumeStore       resume.Store
	v3Scan            *scan.Manager
	recordingsService recservice.Service

	// Recording Playback Path Mapper
	recordingPathMapper *recordings.PathMapper

	// P8.2: Hardening & Test Stability
	preflightProvider v3.PreflightProvider

	// P9: Safety & Shutdown
	rootCtx    context.Context
	rootCancel context.CancelFunc
	shutdownFn func(context.Context) error
	started    atomic.Bool // P10: Lifecycle Invariant (Deliverable #4)

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

// ConfigHolder interface allows hot configuration reloading without import cycles.
// Implemented by config.ConfigHolder.
type ConfigHolder interface {
	Current() *config.Snapshot
	Reload(ctx context.Context) error
}
