// SPDX-License-Identifier: MIT

// Package health provides health and readiness check functionality for production deployments.
// It supports Docker HEALTHCHECK and Kubernetes probes with detailed component status.
package health

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
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
	Timestamp time.Time              `json:"timestamp"`
	Checks    map[string]CheckResult `json:"checks,omitempty"`
	Details   map[string]interface{} `json:"details,omitempty"`
}

// ReadinessResponse represents the readiness check response
type ReadinessResponse struct {
	Ready     bool                   `json:"ready"`
	Status    Status                 `json:"status"`
	Timestamp time.Time              `json:"timestamp"`
	Checks    map[string]CheckResult `json:"checks,omitempty"`
	Details   map[string]interface{} `json:"details,omitempty"`
}

// Checker defines the interface for health checks
type Checker interface {
	Name() string
	Check(ctx context.Context) CheckResult
}

// Manager manages health and readiness checks
type Manager struct {
	version  string
	checkers []Checker
}

// NewManager creates a new health check manager
func NewManager(version string) *Manager {
	return &Manager{
		version:  version,
		checkers: make([]Checker, 0),
	}
}

// RegisterChecker adds a health checker to the manager
func (m *Manager) RegisterChecker(checker Checker) {
	m.checkers = append(m.checkers, checker)
}

// Health performs a health check (liveness probe)
// Returns 200 if the process is alive, regardless of service state
func (m *Manager) Health(ctx context.Context, verbose bool) HealthResponse {
	resp := HealthResponse{
		Status:    StatusHealthy,
		Version:   m.version,
		Timestamp: time.Now(),
	}

	// If verbose, include component checks
	if verbose && len(m.checkers) > 0 {
		resp.Checks = make(map[string]CheckResult)
		hasUnhealthy := false
		hasDegraded := false

		for _, checker := range m.checkers {
			result := checker.Check(ctx)
			resp.Checks[checker.Name()] = result

			switch result.Status {
			case StatusUnhealthy:
				hasUnhealthy = true
			case StatusDegraded:
				hasDegraded = true
			}
		}

		// Overall status based on components
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
func (m *Manager) Ready(ctx context.Context, _ bool) ReadinessResponse {
	resp := ReadinessResponse{
		Ready:     true,
		Status:    StatusHealthy,
		Timestamp: time.Now(),
	}

	if len(m.checkers) == 0 {
		// No checkers registered - consider ready
		return resp
	}

	resp.Checks = make(map[string]CheckResult)
	hasUnhealthy := false
	hasDegraded := false

	for _, checker := range m.checkers {
		result := checker.Check(ctx)
		resp.Checks[checker.Name()] = result

		switch result.Status {
		case StatusUnhealthy:
			hasUnhealthy = true
			resp.Ready = false
		case StatusDegraded:
			hasDegraded = true
		}
	}

	// Overall status
	if hasUnhealthy {
		resp.Status = StatusUnhealthy
	} else if hasDegraded {
		resp.Status = StatusDegraded
	}

	return resp
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
