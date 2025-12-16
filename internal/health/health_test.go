// SPDX-License-Identifier: MIT

package health

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewManager(t *testing.T) {
	m := NewManager("v1.2.3")
	assert.NotNil(t, m)
	assert.Equal(t, "v1.2.3", m.version)
	assert.Empty(t, m.checkers)
}

func TestManager_Health_NoCheckers(t *testing.T) {
	m := NewManager("v1.0.0")

	resp := m.Health(context.Background(), false)
	assert.Equal(t, StatusHealthy, resp.Status)
	assert.Equal(t, "v1.0.0", resp.Version)
	assert.GreaterOrEqual(t, resp.Uptime, int64(0)) // Uptime should be >= 0
	assert.Nil(t, resp.Checks)
}

func TestManager_Health_WithCheckers(t *testing.T) {
	m := NewManager("v1.0.0")

	// Add mock checkers
	m.RegisterChecker(&mockChecker{name: "healthy", status: StatusHealthy})
	m.RegisterChecker(&mockChecker{name: "degraded", status: StatusDegraded})

	// Non-verbose: no checks included
	resp := m.Health(context.Background(), false)
	assert.Equal(t, StatusHealthy, resp.Status)
	assert.Nil(t, resp.Checks)

	// Verbose: checks included
	resp = m.Health(context.Background(), true)
	assert.Equal(t, StatusDegraded, resp.Status) // Overall status degraded
	assert.Len(t, resp.Checks, 2)
	assert.Equal(t, StatusHealthy, resp.Checks["healthy"].Status)
	assert.Equal(t, StatusDegraded, resp.Checks["degraded"].Status)
}

func TestManager_Health_Unhealthy(t *testing.T) {
	m := NewManager("v1.0.0")
	m.RegisterChecker(&mockChecker{name: "unhealthy", status: StatusUnhealthy})

	resp := m.Health(context.Background(), true)
	assert.Equal(t, StatusUnhealthy, resp.Status)
	assert.Len(t, resp.Checks, 1)
}

func TestManager_Health_Uptime(t *testing.T) {
	m := NewManager("v1.0.0")

	// Check uptime immediately
	resp1 := m.Health(context.Background(), false)
	assert.GreaterOrEqual(t, resp1.Uptime, int64(0))

	// Wait 1 second and check again
	time.Sleep(1 * time.Second)
	resp2 := m.Health(context.Background(), false)
	assert.GreaterOrEqual(t, resp2.Uptime, int64(1))
	assert.Greater(t, resp2.Uptime, resp1.Uptime) // Uptime should increase
}

func TestManager_Ready_NoCheckers(t *testing.T) {
	m := NewManager("v1.0.0")

	resp := m.Ready(context.Background(), false)
	assert.True(t, resp.Ready)
	assert.Equal(t, StatusHealthy, resp.Status)
}

func TestManager_Ready_AllHealthy(t *testing.T) {
	m := NewManager("v1.0.0")
	m.RegisterChecker(&mockChecker{name: "check1", status: StatusHealthy})
	m.RegisterChecker(&mockChecker{name: "check2", status: StatusHealthy})

	resp := m.Ready(context.Background(), false)
	assert.True(t, resp.Ready)
	assert.Equal(t, StatusHealthy, resp.Status)
	assert.Len(t, resp.Checks, 2)
}

func TestManager_Ready_Degraded(t *testing.T) {
	m := NewManager("v1.0.0")
	m.RegisterChecker(&mockChecker{name: "degraded", status: StatusDegraded})

	resp := m.Ready(context.Background(), false)
	assert.True(t, resp.Ready) // Degraded is still ready
	assert.Equal(t, StatusDegraded, resp.Status)
}

func TestManager_Ready_Unhealthy(t *testing.T) {
	m := NewManager("v1.0.0")
	m.RegisterChecker(&mockChecker{name: "unhealthy", status: StatusUnhealthy})

	resp := m.Ready(context.Background(), false)
	assert.False(t, resp.Ready) // Unhealthy = not ready
	assert.Equal(t, StatusUnhealthy, resp.Status)
}

func TestManager_ServeHealth(t *testing.T) {
	m := NewManager("v1.0.0")
	m.RegisterChecker(&mockChecker{name: "test", status: StatusHealthy})

	// Test without verbose
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	m.ServeHealth(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var resp HealthResponse
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, StatusHealthy, resp.Status)
	assert.GreaterOrEqual(t, resp.Uptime, int64(0)) // Uptime should be present
	assert.Nil(t, resp.Checks)                      // Not verbose

	// Test with verbose
	req = httptest.NewRequest(http.MethodGet, "/healthz?verbose=true", nil)
	w = httptest.NewRecorder()
	m.ServeHealth(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	err = json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.NotNil(t, resp.Checks)
	assert.Len(t, resp.Checks, 1)
	assert.GreaterOrEqual(t, resp.Uptime, int64(0)) // Uptime present in verbose too
}

func TestManager_ServeHealth_EncodingError(t *testing.T) {
	m := NewManager("v1.0.0")

	// Use a broken ResponseWriter that fails to write
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := &brokenWriter{header: make(http.Header)}

	// Should not panic even if encoding fails
	m.ServeHealth(w, req)
}

func TestManager_ServeReady(t *testing.T) {
	tests := []struct {
		name           string
		checker        Checker
		expectedStatus int
		expectedReady  bool
	}{
		{
			name:           "healthy",
			checker:        &mockChecker{name: "test", status: StatusHealthy},
			expectedStatus: http.StatusOK,
			expectedReady:  true,
		},
		{
			name:           "degraded",
			checker:        &mockChecker{name: "test", status: StatusDegraded},
			expectedStatus: http.StatusOK,
			expectedReady:  true,
		},
		{
			name:           "unhealthy",
			checker:        &mockChecker{name: "test", status: StatusUnhealthy},
			expectedStatus: http.StatusServiceUnavailable,
			expectedReady:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewManager("v1.0.0")
			m.RegisterChecker(tt.checker)

			req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
			w := httptest.NewRecorder()
			m.ServeReady(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			var resp ReadinessResponse
			err := json.NewDecoder(w.Body).Decode(&resp)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedReady, resp.Ready)
		})
	}
}

func TestManager_ServeReady_EncodingError(t *testing.T) {
	m := NewManager("v1.0.0")

	// Use a broken ResponseWriter that fails to write
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := &brokenWriter{header: make(http.Header)}

	// Should not panic even if encoding fails
	m.ServeReady(w, req)
}

func TestFileChecker_Name(t *testing.T) {
	checker := NewFileChecker("xmltv-file", "/path/to/file.xml")
	assert.Equal(t, "xmltv-file", checker.Name())
}

func TestFileChecker(t *testing.T) {
	tempDir := t.TempDir()

	tests := []struct {
		name           string
		setup          func() string
		expectedStatus Status
		expectedError  string
	}{
		{
			name: "file exists",
			setup: func() string {
				path := filepath.Join(tempDir, "test.txt")
				require.NoError(t, os.WriteFile(path, []byte("content"), 0600))
				return path
			},
			expectedStatus: StatusHealthy,
		},
		{
			name: "empty file",
			setup: func() string {
				path := filepath.Join(tempDir, "empty.txt")
				require.NoError(t, os.WriteFile(path, []byte{}, 0600))
				return path
			},
			expectedStatus: StatusDegraded,
		},
		{
			name: "file not found",
			setup: func() string {
				return filepath.Join(tempDir, "nonexistent.txt")
			},
			expectedStatus: StatusUnhealthy,
			expectedError:  "file not found",
		},
		{
			name: "is directory",
			setup: func() string {
				path := filepath.Join(tempDir, "dir")
				require.NoError(t, os.Mkdir(path, 0750))
				return path
			},
			expectedStatus: StatusUnhealthy,
			expectedError:  "expected file, got directory",
		},
		{
			name: "not configured",
			setup: func() string {
				return ""
			},
			expectedStatus: StatusHealthy,
		},
		{
			name: "permission denied or other stat error",
			setup: func() string {
				if os.Geteuid() == 0 {
					return filepath.Join(tempDir, "force_fail_root.txt")
				}
				// Create a file in a directory, then remove read permissions on parent
				dirPath := filepath.Join(tempDir, "restricted")
				require.NoError(t, os.Mkdir(dirPath, 0750))
				filePath := filepath.Join(dirPath, "file.txt")
				require.NoError(t, os.WriteFile(filePath, []byte("test"), 0600))

				// Remove all permissions on directory (will cause stat to fail on some systems)
				require.NoError(t, os.Chmod(dirPath, 0000))

				// Clean up after test
				t.Cleanup(func() {
					// #nosec G302 -- Test cleanup: restoring directory permissions for cleanup
					_ = os.Chmod(dirPath, 0750) // Restore permissions for cleanup
				})

				return filePath
			},
			expectedStatus: StatusUnhealthy,
			expectedError:  "", // Error message varies by system (permission denied or other)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := tt.setup()
			checker := NewFileChecker("test", path)

			result := checker.Check(context.Background())
			assert.Equal(t, tt.expectedStatus, result.Status)
			if tt.expectedError != "" {
				assert.Contains(t, result.Error, tt.expectedError)
			}
		})
	}
}

func TestLastRunChecker_Name(t *testing.T) {
	checker := NewLastRunChecker(func() (time.Time, string) {
		return time.Now(), ""
	})
	assert.Equal(t, "last_job_run", checker.Name())
}

func TestLastRunChecker(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name           string
		lastRun        time.Time
		lastError      string
		expectedStatus Status
		expectedMsg    string
	}{
		{
			name:           "no run yet",
			lastRun:        time.Time{},
			lastError:      "",
			expectedStatus: StatusUnhealthy,
			expectedMsg:    "no successful job run yet",
		},
		{
			name:           "last run failed",
			lastRun:        now,
			lastError:      "connection failed",
			expectedStatus: StatusUnhealthy,
			expectedMsg:    "last job run failed",
		},
		{
			name:           "recent success",
			lastRun:        now.Add(-1 * time.Hour),
			lastError:      "",
			expectedStatus: StatusHealthy,
			expectedMsg:    "last job run successful",
		},
		{
			name:           "old success",
			lastRun:        now.Add(-48 * time.Hour),
			lastError:      "",
			expectedStatus: StatusDegraded,
			expectedMsg:    "last successful run over 24h ago",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := NewLastRunChecker(func() (time.Time, string) {
				return tt.lastRun, tt.lastError
			})

			result := checker.Check(context.Background())
			assert.Equal(t, tt.expectedStatus, result.Status)
			assert.Contains(t, result.Message, tt.expectedMsg)
		})
	}
}

// Mock checker for testing
type mockChecker struct {
	name    string
	status  Status
	message string
	err     string
}

func (m *mockChecker) Name() string {
	return m.name
}

func (m *mockChecker) Check(_ context.Context) CheckResult {
	return CheckResult{
		Status:  m.status,
		Message: m.message,
		Error:   m.err,
	}
}

// brokenWriter is a mock ResponseWriter that always fails to write
type brokenWriter struct {
	header http.Header
}

func (w *brokenWriter) Header() http.Header {
	return w.header
}

func (w *brokenWriter) Write([]byte) (int, error) {
	return 0, assert.AnError // Always fail
}

func (w *brokenWriter) WriteHeader(statusCode int) {
	// No-op
}
