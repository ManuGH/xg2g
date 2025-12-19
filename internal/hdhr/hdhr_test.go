// SPDX-License-Identifier: MIT

package hdhr

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func getFreeUDPPort(t *testing.T) int {
	t.Helper()

	addr, err := net.ResolveUDPAddr("udp4", "127.0.0.1:0")
	require.NoError(t, err)

	conn, err := net.ListenUDP("udp4", addr)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	udpAddr, ok := conn.LocalAddr().(*net.UDPAddr)
	require.True(t, ok)

	return udpAddr.Port
}

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
			server := NewServer(tt.config, nil)

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

	server := NewServer(config, nil)

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
	server := NewServer(Config{Logger: logger}, nil)

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
	tmpDir := t.TempDir()

	// Create empty playlist
	err := os.WriteFile(filepath.Join(tmpDir, "playlist.m3u"), []byte(""), 0600)
	require.NoError(t, err)

	server := NewServer(Config{
		Logger:  logger,
		DataDir: tmpDir,
	}, nil)

	req := httptest.NewRequest(http.MethodGet, "/lineup.json", nil)
	w := httptest.NewRecorder()

	server.HandleLineup(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var response []LineupEntry
	err = json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Empty(t, response) // Currently returns empty array
}

func TestHandleLineupPost(t *testing.T) {
	logger := zerolog.New(os.Stdout)
	server := NewServer(Config{Logger: logger}, nil)

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

	server := NewServer(config, nil)

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

func TestServerGetLocalIP(t *testing.T) {
	logger := zerolog.New(os.Stdout)
	server := NewServer(Config{Logger: logger}, nil)

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

	ssdpPort := getFreeUDPPort(t)

	logger := zerolog.New(os.Stdout)
	config := Config{
		DeviceID:     "SSDP123",
		FriendlyName: "SSDP Test",
		BaseURL:      "http://localhost:8080",
		SSDPPort:     ssdpPort,
		Logger:       logger,
	}

	server := NewServer(config, nil)

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
	server := NewServer(Config{Logger: logger}, nil)

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
	server := NewServer(Config{Logger: logger}, nil)

	req := httptest.NewRequest(http.MethodGet, "/device.xml", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		server.HandleDeviceXML(w, req)
	}
}
