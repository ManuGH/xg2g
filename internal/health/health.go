// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

// Package health provides health and readiness check functionality for production deployments.
// It supports Docker HEALTHCHECK and Kubernetes probes with detailed component status.
package health

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
	"golang.org/x/sync/singleflight"
)

// CheckType defines the scope of a health check
type CheckType uint8

const (
	CheckHealth    CheckType = 1 << 0
	CheckReadiness CheckType = 1 << 1
)

// Status represents the overall health/readiness status
type Status string

const (
	StatusHealthy   Status = "healthy"
	StatusDegraded  Status = "degraded"
	StatusUnhealthy Status = "unhealthy"
)

// CheckResult represents the result of a component health check
type CheckResult struct {
	Status  Status `json:"status"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

// HealthResponse represents the full health check response
type HealthResponse struct {
	Status    Status                 `json:"status"`
	Version   string                 `json:"version,omitempty"`
	Uptime    int64                  `json:"uptime,omitempty"` // Uptime in seconds since startup
	Timestamp time.Time              `json:"timestamp"`
	Checks    map[string]CheckResult `json:"checks,omitempty"`
	Details   map[string]interface{} `json:"details,omitempty"`
}

// ReadinessResponse represents the readiness check response
type ReadinessResponse struct {
	Ready     bool                   `json:"ready"`
	Status    Status                 `json:"status"`
	Timestamp time.Time              `json:"timestamp"`
	Error     string                 `json:"error,omitempty"`
	Checks    map[string]CheckResult `json:"checks,omitempty"`
	Details   map[string]interface{} `json:"details,omitempty"`
}

// Checker defines the interface for health checks
type Checker interface {
	Name() string
	Type() CheckType
	Check(ctx context.Context) CheckResult
}

// Manager manages health and readiness checks
type Manager struct {
	version       string
	checkers      []Checker
	startTime     time.Time // Track startup time for uptime calculation
	readyStrict   bool
	mu            sync.RWMutex
	sfg           singleflight.Group
	lastReadyResp ReadinessResponse
	lastReadyTime time.Time
}

// NewManager creates a new health check manager
func NewManager(version string) *Manager {
	return &Manager{
		version:   version,
		checkers:  make([]Checker, 0),
		startTime: time.Now(),
	}
}

// SetReadyStrict enables/disables strict readiness checks (checking only READINESS-scoped checkers)
func (m *Manager) SetReadyStrict(strict bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.readyStrict = strict
}

// RegisterChecker adds a health checker to the manager
func (m *Manager) RegisterChecker(checker Checker) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checkers = append(m.checkers, checker)
}

// Health performs a health check (liveness probe)
// Returns 200 if the process is alive, regardless of service state
func (m *Manager) Health(ctx context.Context, verbose bool) HealthResponse {
	resp := HealthResponse{
		Status:    StatusHealthy,
		Version:   m.version,
		Uptime:    int64(time.Since(m.startTime).Seconds()),
		Timestamp: time.Now(),
	}

	if verbose {
		resp.Checks = make(map[string]CheckResult)
		m.mu.RLock()
		checkers := append([]Checker(nil), m.checkers...)
		m.mu.RUnlock()

		hasUnhealthy := false
		hasDegraded := false

		for _, c := range checkers {
			res := c.Check(ctx)
			resp.Checks[c.Name()] = res
			switch res.Status {
			case StatusUnhealthy:
				hasUnhealthy = true
			case StatusDegraded:
				hasDegraded = true
			}
		}

		if hasUnhealthy {
			resp.Status = StatusUnhealthy
		} else if hasDegraded {
			resp.Status = StatusDegraded
		}
	}

	return resp
}

// Ready performs a readiness check (readiness probe)
// Returns 200 if services are initialized and ready to serve traffic
func (m *Manager) Ready(ctx context.Context, verbose bool) ReadinessResponse {
	// Always run readiness-scoped checkers to ensure 503 until first successful refresh
	// (Production-ready behavior: don't route traffic until data is loaded)

	// Check cache first (1s TTL) to prevent sequential churn
	m.mu.RLock()
	if !m.lastReadyTime.IsZero() && time.Since(m.lastReadyTime) < 1*time.Second {
		cached := m.lastReadyResp
		m.mu.RUnlock()
		// Return computed-at timestamp (preserve original)
		if verbose {
			cached.Checks = cloneChecks(cached.Checks)
		} else {
			cached.Checks = nil
		}
		return cached
	}
	m.mu.RUnlock()

	// Use singleflight to prevent thundering herd on upstream.
	val, err, _ := m.sfg.Do("readiness", func() (interface{}, error) {
		// Use DETACHED context for the shared probe.
		// This prevents the first caller's context cancellation from aborting the shared run.
		probeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		m.mu.RLock()
		checkers := append([]Checker(nil), m.checkers...)
		m.mu.RUnlock()

		var wg sync.WaitGroup
		var mu sync.Mutex

		// Default to ready/healthy, will be downgraded by failures
		result := ReadinessResponse{
			Ready:     true,
			Status:    StatusHealthy,
			Timestamp: time.Now(),
			Checks:    make(map[string]CheckResult),
		}

		for _, c := range checkers {
			// Filter: Only run checks explicitly marked for Readiness
			if c.Type()&CheckReadiness == 0 {
				continue
			}

			wg.Add(1)
			go func(checker Checker) {
				defer wg.Done()
				// Use the shared probeCtx
				res := checker.Check(probeCtx)

				mu.Lock()
				defer mu.Unlock()
				result.Checks[checker.Name()] = res

				// Aggregation logic
				if res.Status == StatusUnhealthy {
					result.Status = StatusUnhealthy
					result.Ready = false
				} else if res.Status == StatusDegraded && result.Status != StatusUnhealthy {
					result.Status = StatusDegraded
				}
			}(c)
		}
		wg.Wait()

		if probeCtx.Err() != nil {
			return result, probeCtx.Err()
		}

		// Update cache
		m.mu.Lock()
		cachedResult := result
		cachedResult.Checks = cloneChecks(result.Checks)
		m.lastReadyResp = cachedResult
		m.lastReadyTime = result.Timestamp // Use computed-at time
		m.mu.Unlock()

		return result, nil
	})

	if err != nil {
		// Stale-on-error fallback: if upstream fails, serve stale cache for up to 5s
		// This prevents transient network glitches from flapping readiness
		m.mu.RLock()
		cached := m.lastReadyResp
		lastTime := m.lastReadyTime
		m.mu.RUnlock()

		if !lastTime.IsZero() && time.Since(lastTime) < 5*time.Second {
			cached.Error = err.Error() // Surface fallback cause
			if verbose {
				cached.Checks = cloneChecks(cached.Checks)
			} else {
				cached.Checks = nil
			}
			return cached
		}

		return ReadinessResponse{
			Ready:     false,
			Status:    StatusUnhealthy,
			Timestamp: time.Now(),
			Error:     err.Error(),
		}
	}

	// Safer type assertion
	respStrict, ok := val.(ReadinessResponse)
	if !ok {
		// Should never happen, but handle gracefully
		resp := ReadinessResponse{
			Ready:     false,
			Status:    StatusUnhealthy,
			Timestamp: time.Now(),
			Error:     "internal type assertion failed",
		}
		if verbose {
			resp.Checks = map[string]CheckResult{"internal": {Status: StatusUnhealthy, Error: "type assertion failed"}}
		}
		return resp
	}

	if !verbose {
		respStrict.Checks = nil
	}

	return respStrict
}

// ServeHealth handles HTTP health check requests
func (m *Manager) ServeHealth(w http.ResponseWriter, r *http.Request) {
	logger := log.WithComponentFromContext(r.Context(), "health")
	verbose := r.URL.Query().Get("verbose") == "true"

	resp := m.Health(r.Context(), verbose)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK) // Always 200 for liveness

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logger.Error().Err(err).Str("event", "health.encode_error").Msg("failed to encode health response")
	}

	logger.Debug().
		Str("event", "health.checked").
		Str("status", string(resp.Status)).
		Bool("verbose", verbose).
		Msg("health check performed")
}

// ServeReady handles HTTP readiness check requests
func (m *Manager) ServeReady(w http.ResponseWriter, r *http.Request) {
	logger := log.WithComponentFromContext(r.Context(), "readiness")
	verbose := r.URL.Query().Get("verbose") == "true"

	resp := m.Ready(r.Context(), verbose)

	w.Header().Set("Content-Type", "application/json")
	if resp.Ready {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logger.Error().Err(err).Str("event", "readiness.encode_error").Msg("failed to encode readiness response")
	}

	logger.Debug().
		Str("event", "readiness.checked").
		Str("status", string(resp.Status)).
		Bool("ready", resp.Ready).
		Bool("verbose", verbose).
		Msg("readiness check performed")
}

// FileChecker checks if a file exists and is readable
type FileChecker struct {
	name string
	path string
}

// NewFileChecker creates a checker for file existence
func NewFileChecker(name, path string) *FileChecker {
	return &FileChecker{
		name: name,
		path: path,
	}
}

func (c *FileChecker) Name() string {
	return c.name
}

func (c *FileChecker) Type() CheckType {
	return CheckHealth | CheckReadiness
}

func (c *FileChecker) Check(ctx context.Context) CheckResult {
	if c.path == "" {
		return CheckResult{
			Status:  StatusHealthy,
			Message: "not configured (optional)",
		}
	}

	info, err := os.Stat(c.path)
	if err != nil {
		if os.IsNotExist(err) {
			return CheckResult{
				Status:  StatusUnhealthy,
				Error:   "file not found",
				Message: c.path,
			}
		}
		return CheckResult{
			Status: StatusUnhealthy,
			Error:  err.Error(),
		}
	}

	if info.IsDir() {
		return CheckResult{
			Status: StatusUnhealthy,
			Error:  "expected file, got directory",
		}
	}

	if info.Size() == 0 {
		return CheckResult{
			Status:  StatusDegraded,
			Message: "file is empty",
		}
	}

	return CheckResult{
		Status:  StatusHealthy,
		Message: "file exists and readable",
	}
}

// LastRunChecker checks if the last job run was successful
type LastRunChecker struct {
	getLastRun func() (time.Time, string)
}

// NewLastRunChecker creates a checker for last job run status
func NewLastRunChecker(getLastRun func() (time.Time, string)) *LastRunChecker {
	return &LastRunChecker{
		getLastRun: getLastRun,
	}
}

func (c *LastRunChecker) Name() string {
	return "last_job_run"
}

func (c *LastRunChecker) Type() CheckType {
	return CheckHealth | CheckReadiness
}

func (c *LastRunChecker) Check(ctx context.Context) CheckResult {
	lastRun, lastError := c.getLastRun()

	if lastRun.IsZero() {
		return CheckResult{
			Status:  StatusUnhealthy,
			Message: "no successful job run yet",
		}
	}

	if lastError != "" {
		return CheckResult{
			Status:  StatusUnhealthy,
			Error:   lastError,
			Message: "last job run failed",
		}
	}

	age := time.Since(lastRun)
	if age > 24*time.Hour {
		return CheckResult{
			Status:  StatusDegraded,
			Message: "last successful run over 24h ago",
		}
	}

	return CheckResult{
		Status:  StatusHealthy,
		Message: "last job run successful",
	}
}

// ReceiverChecker checks if the OpenWebIF receiver is reachable
type ReceiverChecker struct {
	checkConnection func(context.Context) error
}

// NewReceiverChecker creates a checker for receiver connectivity
func NewReceiverChecker(checkConnection func(context.Context) error) *ReceiverChecker {
	return &ReceiverChecker{
		checkConnection: checkConnection,
	}
}

func (c *ReceiverChecker) Name() string {
	return "receiver_connection"
}

func (c *ReceiverChecker) Type() CheckType {
	// Receiver is both general health AND strict readiness
	return CheckReadiness | CheckHealth
}

func (c *ReceiverChecker) Check(ctx context.Context) CheckResult {
	if err := c.checkConnection(ctx); err != nil {
		return CheckResult{
			Status:  StatusUnhealthy,
			Error:   err.Error(),
			Message: "receiver unreachable",
		}
	}

	return CheckResult{
		Status:  StatusHealthy,
		Message: "receiver connected",
	}
}

// ChannelsChecker checks if channels are loaded in the playlist
type ChannelsChecker struct {
	getChannelCount func() int
}

// NewChannelsChecker creates a checker for channel availability
func NewChannelsChecker(getChannelCount func() int) *ChannelsChecker {
	return &ChannelsChecker{
		getChannelCount: getChannelCount,
	}
}

func (c *ChannelsChecker) Name() string {
	return "channels_loaded"
}

func (c *ChannelsChecker) Type() CheckType {
	return CheckHealth | CheckReadiness
}

func (c *ChannelsChecker) Check(ctx context.Context) CheckResult {
	count := c.getChannelCount()

	if count == 0 {
		return CheckResult{
			Status:  StatusUnhealthy,
			Message: "no channels loaded",
		}
	}

	return CheckResult{
		Status:  StatusHealthy,
		Message: "channels available",
	}
}

// EPGChecker checks if EPG data is loaded and fresh
type EPGChecker struct {
	getEPGStatus func() (loaded bool, lastUpdate time.Time)
}

// NewEPGChecker creates a checker for EPG availability
func NewEPGChecker(getEPGStatus func() (bool, time.Time)) *EPGChecker {
	return &EPGChecker{
		getEPGStatus: getEPGStatus,
	}
}

func (c *EPGChecker) Name() string {
	return "epg_status"
}

func (c *EPGChecker) Type() CheckType {
	return CheckHealth
}

func (c *EPGChecker) Check(ctx context.Context) CheckResult {
	loaded, lastUpdate := c.getEPGStatus()

	if !loaded {
		return CheckResult{
			Status:  StatusDegraded,
			Message: "EPG not loaded (optional)",
		}
	}

	if lastUpdate.IsZero() {
		return CheckResult{
			Status:  StatusDegraded,
			Message: "EPG loaded but no update timestamp",
		}
	}

	age := time.Since(lastUpdate)
	if age > 48*time.Hour {
		return CheckResult{
			Status:  StatusDegraded,
			Message: "EPG data is stale (>48h old)",
		}
	}

	return CheckResult{
		Status:  StatusHealthy,
		Message: "EPG data fresh",
	}
}

func cloneChecks(in map[string]CheckResult) map[string]CheckResult {
	if in == nil {
		return nil
	}
	out := make(map[string]CheckResult, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
