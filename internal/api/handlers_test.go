package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/hdhr"
	"github.com/ManuGH/xg2g/internal/jobs"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetConfig tests the GetConfig method.
func TestGetConfig(t *testing.T) {
	cfg := config.AppConfig{
		Bouquet:     "test-bouquet",
		OWIUsername: "testuser",
		DataDir:     "/tmp/test",
	}

	s := &Server{
		cfg: cfg,
	}

	got := s.GetConfig()
	if got.Bouquet != cfg.Bouquet {
		t.Errorf("expected Bouquet %q, got %q", cfg.Bouquet, got.Bouquet)
	}
	if got.OWIUsername != cfg.OWIUsername {
		t.Errorf("expected OWIUsername %q, got %q", cfg.OWIUsername, got.OWIUsername)
	}
	if got.DataDir != cfg.DataDir {
		t.Errorf("expected DataDir %q, got %q", cfg.DataDir, got.DataDir)
	}
}

// TestHandleRefreshInternal tests the HandleRefreshInternal wrapper.
func TestHandleRefreshInternal(t *testing.T) {
	// Create a mock refresh function that succeeds immediately
	mockRefreshFn := func(ctx context.Context, cfg config.AppConfig, _ *openwebif.StreamDetector) (*jobs.Status, error) {
		return &jobs.Status{
			Version:  "test",
			Channels: 42,
			LastRun:  time.Now(),
		}, nil
	}

	s := &Server{
		cfg:       config.AppConfig{Bouquet: "test"},
		refreshFn: mockRefreshFn,
		cb:        NewCircuitBreaker(3, 5*time.Second),
	}

	req := httptest.NewRequest(http.MethodPost, "/api/refresh", nil)
	req = req.WithContext(context.Background())
	rr := httptest.NewRecorder()

	s.HandleRefreshInternal(rr, req)

	// Should delegate to handleRefresh and return success
	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
}

// TestHandleRefreshV1Direct tests handleRefreshV1 directly.
func TestHandleRefreshV1Direct(t *testing.T) {
	mockRefreshFn := func(ctx context.Context, cfg config.AppConfig, _ *openwebif.StreamDetector) (*jobs.Status, error) {
		return &jobs.Status{
			Version:  "test",
			Channels: 5,
			LastRun:  time.Now(),
		}, nil
	}

	s := &Server{
		cfg:       config.AppConfig{Bouquet: "test"},
		refreshFn: mockRefreshFn,
		cb:        NewCircuitBreaker(3, 5*time.Second),
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/refresh", nil)
	req = req.WithContext(context.Background())
	rr := httptest.NewRecorder()

	s.handleRefreshV1(rr, req)

	// Should set X-API-Version header
	if got := rr.Header().Get("X-API-Version"); got != "1" {
		t.Errorf("expected X-API-Version header %q, got %q", "1", got)
	}

	// Should return success
	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
}

// fakeConfigHolder is a test double for ConfigHolder.
type fakeConfigHolder struct {
	cfg       config.AppConfig
	reloadErr error
}

func (f *fakeConfigHolder) Get() config.AppConfig {
	return f.cfg
}

func (f *fakeConfigHolder) Reload(ctx context.Context) error {
	return f.reloadErr
}

// TestHandleConfigReloadV1 tests the config reload endpoint.
func TestHandleConfigReloadV1(t *testing.T) {
	t.Run("no_config_holder", func(t *testing.T) {
		s := &Server{
			configHolder: nil, // Hot reload not enabled
		}

		req := httptest.NewRequest(http.MethodPost, "/api/v1/config/reload", nil)
		req = req.WithContext(context.Background())
		rr := httptest.NewRecorder()

		s.handleConfigReloadV1(rr, req)

		if rr.Code != http.StatusServiceUnavailable {
			t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, rr.Code)
		}

		if got := rr.Header().Get("X-API-Version"); got != "1" {
			t.Errorf("expected X-API-Version header %q, got %q", "1", got)
		}
	})

	t.Run("reload_success", func(t *testing.T) {
		newCfg := config.AppConfig{
			Bouquet: "updated-bouquet",
			DataDir: "/tmp/new",
		}
		holder := &fakeConfigHolder{
			cfg:       newCfg,
			reloadErr: nil,
		}

		s := &Server{
			cfg:          config.AppConfig{Bouquet: "old"},
			configHolder: holder,
		}

		req := httptest.NewRequest(http.MethodPost, "/api/v1/config/reload", nil)
		req = req.WithContext(context.Background())
		rr := httptest.NewRecorder()

		s.handleConfigReloadV1(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
		}

		// Verify config was updated
		got := s.GetConfig()
		if got.Bouquet != newCfg.Bouquet {
			t.Errorf("expected config Bouquet %q, got %q", newCfg.Bouquet, got.Bouquet)
		}
	})

	t.Run("reload_failure", func(t *testing.T) {
		holder := &fakeConfigHolder{
			cfg:       config.AppConfig{},
			reloadErr: context.DeadlineExceeded,
		}

		s := &Server{
			cfg:          config.AppConfig{Bouquet: "old"},
			configHolder: holder,
		}

		req := httptest.NewRequest(http.MethodPost, "/api/v1/config/reload", nil)
		req = req.WithContext(context.Background())
		rr := httptest.NewRecorder()

		s.handleConfigReloadV1(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rr.Code)
		}
	})
}

// TestHandleLineupJSON tests the HDHomeRun lineup.json endpoint.
func TestHandleLineupJSON(t *testing.T) {
	t.Run("valid_playlist", func(t *testing.T) {
		// Disable H264 repair for this test (testing basic lineup, not URL rewriting)
		t.Setenv("XG2G_H264_STREAM_REPAIR", "false")

		// Create temp directory with M3U file
		tmpDir := t.TempDir()
		m3uContent := `#EXTM3U
#EXTINF:-1 tvg-chno="1" tvg-id="sref-1" tvg-name="Channel One",Channel One
http://example.com/stream1
#EXTINF:-1 tvg-chno="2" tvg-id="sref-2" tvg-name="Channel Two",Channel Two
http://example.com/stream2
#EXTINF:-1 tvg-chno="3" tvg-id="sref-3" tvg-name="Channel Three",Channel Three
http://example.com/stream3
`
		m3uPath := filepath.Join(tmpDir, "playlist.m3u")
		if err := os.WriteFile(m3uPath, []byte(m3uContent), 0600); err != nil {
			t.Fatal(err)
		}

		s := &Server{
			cfg: config.AppConfig{
				DataDir: tmpDir,
			},
		}

		req := httptest.NewRequest(http.MethodGet, "/lineup.json", nil)
		req = req.WithContext(context.Background())
		rr := httptest.NewRecorder()

		s.handleLineupJSON(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
		}

		var lineup []hdhr.LineupEntry
		if err := json.NewDecoder(rr.Body).Decode(&lineup); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if len(lineup) != 3 {
			t.Errorf("expected 3 channels, got %d", len(lineup))
		}

		// Verify first channel
		if lineup[0].GuideNumber != "1" {
			t.Errorf("expected GuideNumber %q, got %q", "1", lineup[0].GuideNumber)
		}
		if lineup[0].GuideName != "Channel One" {
			t.Errorf("expected GuideName %q, got %q", "Channel One", lineup[0].GuideName)
		}
		if lineup[0].URL != "http://example.com/stream1" {
			t.Errorf("expected URL %q, got %q", "http://example.com/stream1", lineup[0].URL)
		}
	})

	t.Run("missing_playlist", func(t *testing.T) {
		tmpDir := t.TempDir()
		// Don't create playlist.m3u

		s := &Server{
			cfg: config.AppConfig{
				DataDir: tmpDir,
			},
		}

		req := httptest.NewRequest(http.MethodGet, "/lineup.json", nil)
		req = req.WithContext(context.Background())
		rr := httptest.NewRecorder()

		s.handleLineupJSON(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rr.Code)
		}
	})
}

// TestRespondError tests the structured error response function.
func TestRespondError(t *testing.T) {
	t.Run("basic_error", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req = req.WithContext(context.Background())
		rr := httptest.NewRecorder()

		RespondError(rr, req, http.StatusBadRequest, ErrInvalidInput)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
		}

		var apiErr APIError
		if err := json.NewDecoder(rr.Body).Decode(&apiErr); err != nil {
			t.Fatalf("failed to decode error response: %v", err)
		}

		if apiErr.Code != "INVALID_INPUT" {
			t.Errorf("expected code %q, got %q", "INVALID_INPUT", apiErr.Code)
		}
		if apiErr.Message != "Invalid input parameters" {
			t.Errorf("expected message %q, got %q", "Invalid input parameters", apiErr.Message)
		}
	})

	t.Run("error_with_details", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req = req.WithContext(context.Background())
		rr := httptest.NewRecorder()

		details := map[string]string{"field": "bouquet", "reason": "invalid format"}
		RespondError(rr, req, http.StatusBadRequest, ErrInvalidInput, details)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
		}

		var apiErr APIError
		if err := json.NewDecoder(rr.Body).Decode(&apiErr); err != nil {
			t.Fatalf("failed to decode error response: %v", err)
		}

		if apiErr.Details == nil {
			t.Error("expected Details to be set")
		}
	})
}

// TestAPIError_Error tests the Error method of APIError.
func TestAPIError_Error(t *testing.T) {
	err := &APIError{
		Code:    "TEST_ERROR",
		Message: "This is a test error",
	}

	if err.Error() != "This is a test error" {
		t.Errorf("expected Error() %q, got %q", "This is a test error", err.Error())
	}
}

// TestSetConfigHolder tests the SetConfigHolder method.
func TestSetConfigHolder(t *testing.T) {
	s := &Server{}
	holder := &fakeConfigHolder{
		cfg: config.AppConfig{Bouquet: "test"},
	}

	s.SetConfigHolder(holder)

	if s.configHolder == nil {
		t.Error("expected configHolder to be set")
	}
}

// fakeAuditLogger implements AuditLogger for testing.
type fakeAuditLogger struct{}

func (f fakeAuditLogger) ConfigReload(actor, result string, details map[string]string)           {}
func (f fakeAuditLogger) RefreshStart(actor string, bouquets []string)                           {}
func (f fakeAuditLogger) RefreshComplete(actor string, channels, bouquets int, durationMS int64) {}
func (f fakeAuditLogger) RefreshError(actor, reason string)                                      {}
func (f fakeAuditLogger) AuthSuccess(remoteAddr, endpoint string)                                {}
func (f fakeAuditLogger) AuthFailure(remoteAddr, endpoint, reason string)                        {}
func (f fakeAuditLogger) AuthMissing(remoteAddr, endpoint string)                                {}
func (f fakeAuditLogger) RateLimitExceeded(remoteAddr, endpoint string)                          {}

// TestSetAuditLogger tests the SetAuditLogger method.
func TestSetAuditLogger(t *testing.T) {
	s := &Server{}
	logger := fakeAuditLogger{}

	s.SetAuditLogger(logger)

	// Since auditLogger is not exported, we can't directly check it
	// But we've exercised the code path
}

// TestHandleStatusV2Placeholder tests the v2 placeholder endpoint.
func TestHandleStatusV2Placeholder(t *testing.T) {
	s := &Server{}

	req := httptest.NewRequest(http.MethodGet, "/api/v2/status", nil)
	req = req.WithContext(context.Background())
	rr := httptest.NewRecorder()

	s.handleStatusV2Placeholder(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	if got := rr.Header().Get("X-API-Version"); got != "2" {
		t.Errorf("expected X-API-Version header %q, got %q", "2", got)
	}

	var resp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["status"] != "preview" {
		t.Errorf("expected status %q, got %q", "preview", resp["status"])
	}
	if resp["message"] != "API v2 is under development" {
		t.Errorf("expected message %q, got %q", "API v2 is under development", resp["message"])
	}
}

// TestHDHomeRunServer tests the HDHomeRunServer getter.
func TestHDHomeRunServer(t *testing.T) {
	s := &Server{
		hdhr: nil,
	}

	if got := s.HDHomeRunServer(); got != nil {
		t.Errorf("expected nil HDHomeRun server, got %v", got)
	}
}

// TestHandler tests the Handler method with and without audit logger.
func TestHandler(t *testing.T) {
	t.Run("without_audit_logger", func(t *testing.T) {
		cfg := config.AppConfig{
			DataDir: t.TempDir(),
			Bouquet: "test",
		}
		s := New(cfg, nil)
		handler := s.Handler()

		if handler == nil {
			t.Fatal("expected handler, got nil")
		}

		// Test basic endpoint
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
		}
	})

	t.Run("with_audit_logger", func(t *testing.T) {
		cfg := config.AppConfig{
			DataDir: t.TempDir(),
			Bouquet: "test",
		}
		s := New(cfg, nil)
		s.SetAuditLogger(fakeAuditLogger{})
		handler := s.Handler()

		if handler == nil {
			t.Fatal("expected handler, got nil")
		}

		// Test basic endpoint
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
		}
	})
}

// TestNewRouter tests the NewRouter function.
func TestNewRouter(t *testing.T) {
	cfg := config.AppConfig{
		DataDir: t.TempDir(),
		Bouquet: "test",
	}

	handler := NewRouter(cfg)
	if handler == nil {
		t.Fatal("expected handler, got nil")
	}

	// Test that basic health endpoint works
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
}

// TestAuthMiddleware_Standalone tests AuthMiddleware directly (auth disabled, valid, missing, invalid).
func TestAuthMiddleware_Standalone(t *testing.T) {
	mockHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("authenticated"))
	})

	t.Run("auth_disabled_no_env_token", func(t *testing.T) {
		// Clear any existing token
		oldToken := os.Getenv("XG2G_API_TOKEN")
		_ = os.Unsetenv("XG2G_API_TOKEN")
		defer func() {
			if oldToken != "" {
				_ = os.Setenv("XG2G_API_TOKEN", oldToken)
			}
		}()

		wrapped := AuthMiddleware(mockHandler)
		req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
		req = req.WithContext(context.Background())
		rr := httptest.NewRecorder()

		wrapped.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d when auth disabled, got %d", http.StatusOK, rr.Code)
		}
		if body := rr.Body.String(); body != "authenticated" {
			t.Errorf("expected body %q, got %q", "authenticated", body)
		}
	})

	t.Run("auth_enabled_valid_token", func(t *testing.T) {
		oldToken := os.Getenv("XG2G_API_TOKEN")
		_ = os.Setenv("XG2G_API_TOKEN", "secret-test-token")
		defer func() {
			if oldToken != "" {
				_ = os.Setenv("XG2G_API_TOKEN", oldToken)
			} else {
				_ = os.Unsetenv("XG2G_API_TOKEN")
			}
		}()

		wrapped := AuthMiddleware(mockHandler)
		req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
		req.Header.Set("X-API-Token", "secret-test-token")
		req = req.WithContext(context.Background())
		rr := httptest.NewRecorder()

		wrapped.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d with valid token, got %d", http.StatusOK, rr.Code)
		}
	})

	t.Run("auth_enabled_missing_token", func(t *testing.T) {
		oldToken := os.Getenv("XG2G_API_TOKEN")
		_ = os.Setenv("XG2G_API_TOKEN", "secret-test-token")
		defer func() {
			if oldToken != "" {
				_ = os.Setenv("XG2G_API_TOKEN", oldToken)
			} else {
				_ = os.Unsetenv("XG2G_API_TOKEN")
			}
		}()

		wrapped := AuthMiddleware(mockHandler)
		req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
		// No X-API-Token header
		req = req.WithContext(context.Background())
		rr := httptest.NewRecorder()

		wrapped.ServeHTTP(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected status %d with missing token, got %d", http.StatusUnauthorized, rr.Code)
		}
		if !strings.Contains(rr.Body.String(), "Missing API token") {
			t.Errorf("expected body to contain 'Missing API token', got: %s", rr.Body.String())
		}
	})

	t.Run("auth_enabled_invalid_token", func(t *testing.T) {
		oldToken := os.Getenv("XG2G_API_TOKEN")
		_ = os.Setenv("XG2G_API_TOKEN", "secret-test-token")
		defer func() {
			if oldToken != "" {
				_ = os.Setenv("XG2G_API_TOKEN", oldToken)
			} else {
				_ = os.Unsetenv("XG2G_API_TOKEN")
			}
		}()

		wrapped := AuthMiddleware(mockHandler)
		req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
		req.Header.Set("X-API-Token", "wrong-token")
		req = req.WithContext(context.Background())
		rr := httptest.NewRecorder()

		wrapped.ServeHTTP(rr, req)

		if rr.Code != http.StatusForbidden {
			t.Errorf("expected status %d with invalid token, got %d", http.StatusForbidden, rr.Code)
		}
		if !strings.Contains(rr.Body.String(), "Invalid API token") {
			t.Errorf("expected body to contain 'Invalid API token', got: %s", rr.Body.String())
		}
	})
}

// checkFile Tests

func TestCheckFile_RegularFile(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.txt")
	require.NoError(t, os.WriteFile(filePath, []byte("test content"), 0600))

	result := checkFile(context.Background(), filePath)
	assert.True(t, result)
}

func TestCheckFile_Directory(t *testing.T) {
	tmpDir := t.TempDir()
	result := checkFile(context.Background(), tmpDir)
	assert.False(t, result)
}

func TestCheckFile_NotExist(t *testing.T) {
	result := checkFile(context.Background(), "/nonexistent/file.txt")
	assert.False(t, result)
}

func TestCheckFile_NoPermission(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "restricted.txt")
	require.NoError(t, os.WriteFile(filePath, []byte("test"), 0000))

	// Clean up permissions after test
	t.Cleanup(func() {
		_ = os.Chmod(filePath, 0600)
	})

	result := checkFile(context.Background(), filePath)
	assert.False(t, result)
}

// V2 Routes Tests

func TestRegisterV2Routes_StatusEndpoint(t *testing.T) {
	// Enable API_V2 feature flag
	t.Setenv("XG2G_FEATURE_API_V2", "true")

	cfg := config.AppConfig{
		DataDir: t.TempDir(),
		Version: "test-v2",
	}

	srv := New(cfg, nil)
	srv.SetStatus(jobs.Status{
		Version:  "test-v2",
		Channels: 100,
		LastRun:  time.Now(),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v2/status", nil)
	w := httptest.NewRecorder()

	// The server's routes() method includes registerV2Routes when feature flag is enabled
	srv.routes().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	assert.Equal(t, "2", w.Header().Get("X-API-Version"))

	var resp map[string]interface{}
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)

	assert.Equal(t, "API v2 is under development", resp["message"])
	assert.Equal(t, "preview", resp["status"])
}

// TestHandleLineupJSON_PlexForceHLS_Disabled tests lineup.json returns direct MPEG-TS URLs when PlexForceHLS=false
func TestHandleLineupJSON_PlexForceHLS_Disabled(t *testing.T) {
	// Create temp directory for test data
	tempDir := t.TempDir()

	// Create test M3U playlist
	m3uContent := `#EXTM3U
#EXTINF:-1 tvg-chno="1" tvg-id="sref-test" tvg-name="Test Channel",Test Channel
http://10.10.55.14:18000/1:0:19:132F:3EF:1:C00000:0:0:0:
#EXTINF:-1 tvg-chno="2" tvg-id="sref-test2" tvg-name="Test Channel 2",Test Channel 2
http://10.10.55.14:18000/1:0:19:1334:3EF:1:C00000:0:0:0:
`
	m3uPath := filepath.Join(tempDir, "playlist.m3u")
	require.NoError(t, os.WriteFile(m3uPath, []byte(m3uContent), 0600))

	// Create server with PlexForceHLS disabled
	cfg := config.AppConfig{
		DataDir: tempDir,
	}
	srv := New(cfg, nil)

	// Initialize HDHomeRun with PlexForceHLS=false
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()
	hdhrCfg := hdhr.Config{
		Enabled:      true,
		PlexForceHLS: false,
		Logger:       logger,
	}
	srv.hdhr = hdhr.NewServer(hdhrCfg, nil)

	// Make request with Host header for URL rewriting
	req := httptest.NewRequest(http.MethodGet, "/lineup.json", nil)
	req.Host = "10.10.55.14:8080"
	w := httptest.NewRecorder()

	srv.handleLineupJSON(w, req)

	// Verify response
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var lineup []hdhr.LineupEntry
	err := json.NewDecoder(w.Body).Decode(&lineup)
	require.NoError(t, err)
	require.Len(t, lineup, 2)

	// Verify URLs do NOT have /hls/ prefix
	assert.Equal(t, "http://10.10.55.14:18000/1:0:19:132F:3EF:1:C00000:0:0:0:", lineup[0].URL)
	assert.Equal(t, "http://10.10.55.14:18000/1:0:19:1334:3EF:1:C00000:0:0:0:", lineup[1].URL)
	assert.NotContains(t, lineup[0].URL, "/hls/")
	assert.NotContains(t, lineup[1].URL, "/hls/")
}

// TestHandleLineupJSON_PlexForceHLS_Enabled tests lineup.json returns HLS URLs when PlexForceHLS=true
func TestHandleLineupJSON_PlexForceHLS_Enabled(t *testing.T) {
	// Create temp directory for test data
	tempDir := t.TempDir()

	// Create test M3U playlist
	m3uContent := `#EXTM3U
#EXTINF:-1 tvg-chno="1" tvg-id="sref-test" tvg-name="Test Channel",Test Channel
http://10.10.55.14:18000/1:0:19:132F:3EF:1:C00000:0:0:0:
#EXTINF:-1 tvg-chno="2" tvg-id="sref-test2" tvg-name="Test Channel 2",Test Channel 2
http://10.10.55.14:18000/1:0:19:1334:3EF:1:C00000:0:0:0:
`
	m3uPath := filepath.Join(tempDir, "playlist.m3u")
	require.NoError(t, os.WriteFile(m3uPath, []byte(m3uContent), 0600))

	// Create server with PlexForceHLS enabled
	cfg := config.AppConfig{
		DataDir: tempDir,
	}
	srv := New(cfg, nil)

	// Initialize HDHomeRun with PlexForceHLS=true
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()
	hdhrCfg := hdhr.Config{
		Enabled:      true,
		PlexForceHLS: true,
		Logger:       logger,
	}
	srv.hdhr = hdhr.NewServer(hdhrCfg, nil)

	// Make request with Host header for URL rewriting
	req := httptest.NewRequest(http.MethodGet, "/lineup.json", nil)
	req.Host = "10.10.55.14:8080"
	w := httptest.NewRecorder()

	srv.handleLineupJSON(w, req)

	// Verify response
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var lineup []hdhr.LineupEntry
	err := json.NewDecoder(w.Body).Decode(&lineup)
	require.NoError(t, err)
	require.Len(t, lineup, 2)

	// Verify URLs have /hls/ prefix added (keeping original port 18000)
	// The /hls/ handler exists on the stream proxy (port 18000), not the API server (port 8080)
	assert.Equal(t, "http://10.10.55.14:18000/hls/1:0:19:132F:3EF:1:C00000:0:0:0:", lineup[0].URL)
	assert.Equal(t, "http://10.10.55.14:18000/hls/1:0:19:1334:3EF:1:C00000:0:0:0:", lineup[1].URL)
	assert.Contains(t, lineup[0].URL, "/hls/")
	assert.Contains(t, lineup[1].URL, "/hls/")
}

// TestHandleLineupJSON_PlexForceHLS_NoDoublePrefix tests that /hls/ is not added twice
func TestHandleLineupJSON_PlexForceHLS_NoDoublePrefix(t *testing.T) {
	// Create temp directory for test data
	tempDir := t.TempDir()

	// Create test M3U playlist with URL that already has /hls/ prefix
	m3uContent := `#EXTM3U
#EXTINF:-1 tvg-chno="1" tvg-id="sref-test" tvg-name="Test Channel",Test Channel
http://10.10.55.14:18000/hls/1:0:19:132F:3EF:1:C00000:0:0:0:
`
	m3uPath := filepath.Join(tempDir, "playlist.m3u")
	require.NoError(t, os.WriteFile(m3uPath, []byte(m3uContent), 0600))

	// Create server with PlexForceHLS enabled
	cfg := config.AppConfig{
		DataDir: tempDir,
	}
	srv := New(cfg, nil)

	// Initialize HDHomeRun with PlexForceHLS=true
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()
	hdhrCfg := hdhr.Config{
		Enabled:      true,
		PlexForceHLS: true,
		Logger:       logger,
	}
	srv.hdhr = hdhr.NewServer(hdhrCfg, nil)

	// Make request with Host header for URL rewriting
	req := httptest.NewRequest(http.MethodGet, "/lineup.json", nil)
	req.Host = "10.10.55.14:8080"
	w := httptest.NewRecorder()

	srv.handleLineupJSON(w, req)

	// Verify response
	assert.Equal(t, http.StatusOK, w.Code)

	var lineup []hdhr.LineupEntry
	err := json.NewDecoder(w.Body).Decode(&lineup)
	require.NoError(t, err)
	require.Len(t, lineup, 1)

	// Verify URL still has only ONE /hls/ prefix (not /hls/hls/)
	assert.Equal(t, "http://10.10.55.14:18000/hls/1:0:19:132F:3EF:1:C00000:0:0:0:", lineup[0].URL)
	assert.NotContains(t, lineup[0].URL, "/hls/hls/")
}
