// SPDX-License-Identifier: MIT

package hdhr

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewServer(t *testing.T) {
	logger := zerolog.New(os.Stdout)

	tests := []struct {
		name     string
		config   Config
		expected Config
	}{
		{
			name: "default values",
			config: Config{
				Logger: logger,
			},
			expected: Config{
				DeviceID:     "XG2G1234",
				FriendlyName: "xg2g",
				ModelName:    "HDHR-xg2g",
				FirmwareName: "xg2g-1.4.0",
				TunerCount:   4,
				Logger:       logger,
			},
		},
		{
			name: "custom values",
			config: Config{
				DeviceID:     "CUSTOM123",
				FriendlyName: "My Custom xg2g",
				ModelName:    "HDHR-custom",
				FirmwareName: "custom-2.0.0",
				TunerCount:   8,
				Logger:       logger,
			},
			expected: Config{
				DeviceID:     "CUSTOM123",
				FriendlyName: "My Custom xg2g",
				ModelName:    "HDHR-custom",
				FirmwareName: "custom-2.0.0",
				TunerCount:   8,
				Logger:       logger,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewServer(tt.config)

			assert.Equal(t, tt.expected.DeviceID, server.config.DeviceID)
			assert.Equal(t, tt.expected.FriendlyName, server.config.FriendlyName)
			assert.Equal(t, tt.expected.ModelName, server.config.ModelName)
			assert.Equal(t, tt.expected.FirmwareName, server.config.FirmwareName)
			assert.Equal(t, tt.expected.TunerCount, server.config.TunerCount)
		})
	}
}

func TestHandleDiscover(t *testing.T) {
	logger := zerolog.New(os.Stdout)
	config := Config{
		DeviceID:     "TEST1234",
		FriendlyName: "Test xg2g",
		ModelName:    "HDHR-test",
		FirmwareName: "test-1.0.0",
		TunerCount:   2,
		Logger:       logger,
	}

	server := NewServer(config)

	tests := []struct {
		name        string
		baseURL     string
		requestURL  string
		expectedURL string
	}{
		{
			name:        "auto-detect base URL",
			baseURL:     "",
			requestURL:  "http://localhost:8080/discover.json",
			expectedURL: "http://localhost:8080",
		},
		{
			name:        "custom base URL",
			baseURL:     "http://192.168.1.100:8080",
			requestURL:  "http://example.com/discover.json",
			expectedURL: "http://192.168.1.100:8080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server.config.BaseURL = tt.baseURL

			req := httptest.NewRequest(http.MethodGet, tt.requestURL, nil)
			w := httptest.NewRecorder()

			server.HandleDiscover(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

			var response DiscoverResponse
			err := json.Unmarshal(w.Body.Bytes(), &response)
			require.NoError(t, err)

			assert.Equal(t, "Test xg2g", response.FriendlyName)
			assert.Equal(t, "HDHR-test", response.ModelNumber)
			assert.Equal(t, "test-1.0.0", response.FirmwareName)
			assert.Equal(t, "TEST1234", response.DeviceID)
			assert.Equal(t, "xg2g", response.DeviceAuth)
			assert.Equal(t, tt.expectedURL, response.BaseURL)
			assert.Equal(t, tt.expectedURL+"/lineup.json", response.LineupURL)
			assert.Equal(t, 2, response.TunerCount)
		})
	}
}

func TestHandleLineupStatus(t *testing.T) {
	logger := zerolog.New(os.Stdout)
	server := NewServer(Config{Logger: logger})

	req := httptest.NewRequest(http.MethodGet, "/lineup_status.json", nil)
	w := httptest.NewRecorder()

	server.HandleLineupStatus(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var response LineupStatus
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, 0, response.ScanInProgress)
	assert.Equal(t, 1, response.ScanPossible)
	assert.Equal(t, "Antenna", response.Source)
	assert.Equal(t, []string{"Antenna"}, response.SourceList)
}

func TestHandleLineup(t *testing.T) {
	logger := zerolog.New(os.Stdout)
	server := NewServer(Config{Logger: logger})

	req := httptest.NewRequest(http.MethodGet, "/lineup.json", nil)
	w := httptest.NewRecorder()

	server.HandleLineup(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var response []LineupEntry
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Empty(t, response) // Currently returns empty array
}

func TestHandleLineupPost(t *testing.T) {
	logger := zerolog.New(os.Stdout)
	server := NewServer(Config{Logger: logger})

	tests := []struct {
		name   string
		action string
	}{
		{"start scan", "start"},
		{"no action", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/lineup.json"
			if tt.action != "" {
				url += "?scan=" + tt.action
			}

			req := httptest.NewRequest(http.MethodPost, url, nil)
			w := httptest.NewRecorder()

			server.HandleLineupPost(w, req)

			assert.Equal(t, http.StatusNoContent, w.Code)
		})
	}
}

func TestHandleDeviceXML(t *testing.T) {
	logger := zerolog.New(os.Stdout)
	config := Config{
		DeviceID:     "XML123",
		FriendlyName: "XML Test",
		ModelName:    "HDHR-xml",
		Logger:       logger,
	}

	server := NewServer(config)

	tests := []struct {
		name       string
		baseURL    string
		requestURL string
		https      bool
	}{
		{
			name:       "http request",
			baseURL:    "",
			requestURL: "http://localhost:8080/device.xml",
			https:      false,
		},
		{
			name:       "custom base URL",
			baseURL:    "http://192.168.1.100:8080",
			requestURL: "http://example.com/device.xml",
			https:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server.config.BaseURL = tt.baseURL

			req := httptest.NewRequest(http.MethodGet, tt.requestURL, nil)
			if tt.https {
				req.TLS = &tls.ConnectionState{} // Mock TLS
			}
			w := httptest.NewRecorder()

			server.HandleDeviceXML(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, "application/xml; charset=utf-8", w.Header().Get("Content-Type"))

			body := w.Body.String()
			assert.Contains(t, body, "<?xml version=\"1.0\" encoding=\"UTF-8\"?>")
			assert.Contains(t, body, "XML Test")
			assert.Contains(t, body, "HDHR-xml")
			assert.Contains(t, body, "XML123")

			if tt.baseURL != "" {
				assert.Contains(t, body, tt.baseURL)
			} else {
				assert.Contains(t, body, "localhost:8080")
			}
		})
	}
}

func TestGetConfigFromEnv(t *testing.T) {
	envVars := []string{
		"XG2G_HDHR_ENABLED",
		"XG2G_HDHR_DEVICE_ID",
		"XG2G_HDHR_FRIENDLY_NAME",
		"XG2G_HDHR_MODEL",
		"XG2G_HDHR_FIRMWARE",
		"XG2G_HDHR_BASE_URL",
		"XG2G_HDHR_TUNER_COUNT",
	}
	logger := zerolog.New(os.Stdout)

	tests := []struct {
		name     string
		envVars  map[string]string
		expected Config
	}{
		{
			name:    "default values",
			envVars: map[string]string{},
			expected: Config{
				Enabled:      true, // Default since v1.4.0 for out-of-the-box Plex/Jellyfin discovery
				DeviceID:     "",
				FriendlyName: "xg2g",
				ModelName:    "HDHR-xg2g",
				FirmwareName: "xg2g-1.4.0",
				BaseURL:      "",
				TunerCount:   4,
				Logger:       logger,
			},
		},
		{
			name: "enabled with custom values",
			envVars: map[string]string{
				"XG2G_HDHR_ENABLED":       "true",
				"XG2G_HDHR_DEVICE_ID":     "ENV123",
				"XG2G_HDHR_FRIENDLY_NAME": "ENV Test",
				"XG2G_HDHR_MODEL":         "HDHR-env",
				"XG2G_HDHR_FIRMWARE":      "env-2.0.0",
				"XG2G_HDHR_BASE_URL":      "http://env.test:8080",
				"XG2G_HDHR_TUNER_COUNT":   "6",
			},
			expected: Config{
				Enabled:      true,
				DeviceID:     "ENV123",
				FriendlyName: "ENV Test",
				ModelName:    "HDHR-env",
				FirmwareName: "env-2.0.0",
				BaseURL:      "http://env.test:8080",
				TunerCount:   6,
				Logger:       logger,
			},
		},
		{
			name: "invalid tuner count falls back to default",
			envVars: map[string]string{
				"XG2G_HDHR_TUNER_COUNT": "invalid",
			},
			expected: Config{
				Enabled:      true, // Default since v1.4.0 for out-of-the-box Plex/Jellyfin discovery
				DeviceID:     "",
				FriendlyName: "xg2g",
				ModelName:    "HDHR-xg2g",
				FirmwareName: "xg2g-1.4.0",
				BaseURL:      "",
				TunerCount:   4,
				Logger:       logger,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, env := range envVars {
				t.Setenv(env, "")
			}

			for key, value := range tt.envVars {
				t.Setenv(key, value)
			}

			config := GetConfigFromEnv(logger)

			assert.Equal(t, tt.expected.Enabled, config.Enabled)
			assert.Equal(t, tt.expected.DeviceID, config.DeviceID)
			assert.Equal(t, tt.expected.FriendlyName, config.FriendlyName)
			assert.Equal(t, tt.expected.ModelName, config.ModelName)
			assert.Equal(t, tt.expected.FirmwareName, config.FirmwareName)
			assert.Equal(t, tt.expected.BaseURL, config.BaseURL)
			assert.Equal(t, tt.expected.TunerCount, config.TunerCount)
		})
	}
}

func TestUtilityFunctions(t *testing.T) {
	t.Run("getEnvDefault", func(t *testing.T) {
		// Test with non-existent env var
		result := getEnvDefault("NON_EXISTENT_VAR_XG2G_TEST", "default")
		assert.Equal(t, "default", result)

		// Test with existing env var
		t.Setenv("TEST_VAR_XG2G", "test_value")

		result = getEnvDefault("TEST_VAR_XG2G", "default")
		assert.Equal(t, "test_value", result)

		// Test with empty env var
		t.Setenv("EMPTY_VAR_XG2G", "")

		result = getEnvDefault("EMPTY_VAR_XG2G", "default")
		assert.Equal(t, "default", result)
	})

	t.Run("getEnvInt", func(t *testing.T) {
		// Test with non-existent env var
		result := getEnvInt("NON_EXISTENT_INT_VAR", 42)
		assert.Equal(t, 42, result)

		// Test with valid integer
		t.Setenv("TEST_INT_VAR", "123")

		result = getEnvInt("TEST_INT_VAR", 42)
		assert.Equal(t, 123, result)

		// Test with invalid integer
		t.Setenv("INVALID_INT_VAR", "not_an_int")

		result = getEnvInt("INVALID_INT_VAR", 42)
		assert.Equal(t, 42, result)
	})
}

func TestServerGetLocalIP(t *testing.T) {
	logger := zerolog.New(os.Stdout)
	server := NewServer(Config{Logger: logger})

	ip := server.getLocalIP()
	// We can't guarantee what IP we'll get in test environment,
	// but we can ensure it's not empty on most systems
	t.Logf("Local IP detected: %s", ip)

	// Basic validation that it looks like an IP if not empty
	if ip != "" {
		parts := strings.Split(ip, ".")
		assert.Len(t, parts, 4, "IP should have 4 parts")
	}
}

// TestSSDPIntegration tests SSDP functionality with timeout
func TestSSDPIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping SSDP integration test in short mode")
	}

	logger := zerolog.New(os.Stdout)
	config := Config{
		DeviceID:     "SSDP123",
		FriendlyName: "SSDP Test",
		BaseURL:      "http://localhost:8080",
		Logger:       logger,
	}

	server := NewServer(config)

	// Create a context with timeout for the test
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start SSDP announcer in background
	done := make(chan error, 1)
	go func() {
		err := server.StartSSDPAnnouncer(ctx)
		done <- err
	}()

	// Wait a bit to let it start
	time.Sleep(500 * time.Millisecond)

	// Cancel context to stop announcer
	cancel()

	// Wait for completion with timeout
	select {
	case err := <-done:
		// SSDP announcer should exit cleanly when context is cancelled
		assert.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("SSDP announcer did not stop within timeout")
	}
}

// BenchmarkHandleDiscover benchmarks the discover endpoint
func BenchmarkHandleDiscover(b *testing.B) {
	logger := zerolog.New(os.Stdout)
	server := NewServer(Config{Logger: logger})

	req := httptest.NewRequest(http.MethodGet, "/discover.json", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		server.HandleDiscover(w, req)
	}
}

// BenchmarkHandleDeviceXML benchmarks the device.xml endpoint
func BenchmarkHandleDeviceXML(b *testing.B) {
	logger := zerolog.New(os.Stdout)
	server := NewServer(Config{Logger: logger})

	req := httptest.NewRequest(http.MethodGet, "/device.xml", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		server.HandleDeviceXML(w, req)
	}
}
