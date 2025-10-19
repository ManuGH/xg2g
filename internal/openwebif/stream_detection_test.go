package openwebif

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestMaskServiceRef(t *testing.T) {
	tests := []struct {
		name string
		ref  string
		want string
	}{
		{
			name: "short ref",
			ref:  "1:0:1:6DCF",
			want: "1:0:1:6DCF",
		},
		{
			name: "long ref",
			ref:  "1:0:1:6DCF:44D:1:C00000:0:0:0:",
			want: "1:0:1:6DCF...:0:0:0:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := maskServiceRef(tt.ref); got != tt.want {
				t.Errorf("maskServiceRef() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewStreamDetector(t *testing.T) {
	logger := zerolog.New(io.Discard)
	receiverHost := "192.168.1.100"

	detector := NewStreamDetector(receiverHost, logger)

	if detector == nil {
		t.Fatal("NewStreamDetector returned nil")
		return
	}

	if detector.receiverHost != receiverHost {
		t.Errorf("receiverHost = %v, want %v", detector.receiverHost, receiverHost)
	}

	if detector.cache == nil {
		t.Error("cache not initialized")
	}

	if detector.httpClient == nil {
		t.Error("httpClient not initialized")
	}
}

func TestIsEnabled(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		want     bool
	}{
		{
			name:     "enabled",
			envValue: "true",
			want:     true,
		},
		{
			name:     "disabled",
			envValue: "false",
			want:     false,
		},
		{
			name:     "not set",
			envValue: "",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				t.Setenv("XG2G_SMART_STREAM_DETECTION", tt.envValue)
			} else {
				t.Setenv("XG2G_SMART_STREAM_DETECTION", "")
			}

			if got := IsEnabled(); got != tt.want {
				t.Errorf("IsEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStreamDetector_TestEndpoint(t *testing.T) {
	logger := zerolog.New(io.Discard)

	tests := []struct {
		name       string
		statusCode int
		want       bool
	}{
		{
			name:       "200 OK",
			statusCode: http.StatusOK,
			want:       true,
		},
		{
			name:       "206 Partial Content",
			statusCode: http.StatusPartialContent,
			want:       true,
		},
		{
			name:       "404 Not Found",
			statusCode: http.StatusNotFound,
			want:       false,
		},
		{
			name:       "500 Internal Server Error",
			statusCode: http.StatusInternalServerError,
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodHead {
					t.Errorf("expected HEAD request, got %s", r.Method)
				}
				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			detector := NewStreamDetector("192.168.1.100", logger)

			candidate := streamCandidate{
				URL:  server.URL,
				Port: 8001,
			}

			ctx := context.Background()
			got := detector.testEndpoint(ctx, candidate)

			if got != tt.want {
				t.Errorf("testEndpoint() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStreamDetector_DetectStreamURL(t *testing.T) {
	logger := zerolog.New(io.Discard)

	// Create test server that responds to HEAD on port 8001
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	detector := NewStreamDetector("192.168.1.100", logger)

	// Note: This test will fail to find working endpoint since test server
	// is on different port. This tests the fallback behavior.
	ctx := context.Background()
	serviceRef := "1:0:1:6DCF:44D:1:C00000:0:0:0:"
	channelName := "Test Channel"

	info, err := detector.DetectStreamURL(ctx, serviceRef, channelName)

	if err != nil {
		t.Errorf("DetectStreamURL() error = %v", err)
	}

	if info == nil {
		t.Fatal("DetectStreamURL() returned nil info")
		return
	}

	// Should return fallback URL
	if info.URL == "" {
		t.Error("DetectStreamURL() returned empty URL")
	}

	if info.Port != 8001 {
		t.Errorf("expected fallback port 8001, got %d", info.Port)
	}
}

func TestStreamDetector_Caching(t *testing.T) {
	logger := zerolog.New(io.Discard)
	detector := NewStreamDetector("192.168.1.100", logger)

	serviceRef := "1:0:1:6DCF:44D:1:C00000:0:0:0:"
	testTime := time.Now()

	// Manually cache an entry
	detector.cacheMu.Lock()
	detector.cache[serviceRef] = &StreamInfo{
		URL:      "http://192.168.1.100:8001/" + serviceRef,
		Port:     8001,
		TestedAt: testTime,
	}
	detector.cacheMu.Unlock()

	// Call should use cache
	ctx := context.Background()
	info, err := detector.DetectStreamURL(ctx, serviceRef, "Test Channel")
	if err != nil {
		t.Fatalf("DetectStreamURL() error = %v", err)
	}

	// Should return cached result
	if info.TestedAt != testTime {
		t.Error("cache was not used (TestedAt differs)")
	}

	if info.Port != 8001 {
		t.Errorf("expected cached port 8001, got %d", info.Port)
	}
}

func TestStreamDetector_ClearCache(t *testing.T) {
	logger := zerolog.New(io.Discard)
	detector := NewStreamDetector("192.168.1.100", logger)

	// Add something to cache
	detector.cache["test"] = &StreamInfo{
		URL:      "http://test",
		TestedAt: time.Now(),
	}

	if len(detector.cache) != 1 {
		t.Errorf("expected 1 cached entry, got %d", len(detector.cache))
	}

	// Clear cache
	detector.ClearCache()

	if len(detector.cache) != 0 {
		t.Errorf("expected 0 cached entries after clear, got %d", len(detector.cache))
	}
}

func TestStreamDetector_BuildCandidates(t *testing.T) {
	logger := zerolog.New(io.Discard)

	tests := []struct {
		name            string
		proxyEnabled    bool
		proxyHost       string
		wantCandidates  int
		wantFirstPort   int
		wantContainPort []int
	}{
		{
			name:            "no proxy",
			proxyEnabled:    false,
			proxyHost:       "",
			wantCandidates:  2,
			wantFirstPort:   8001,
			wantContainPort: []int{8001, 17999},
		},
		{
			name:            "with proxy",
			proxyEnabled:    true,
			proxyHost:       "http://192.168.1.50:18000",
			wantCandidates:  3,
			wantFirstPort:   8001,
			wantContainPort: []int{8001, 17999, 18000},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detector := &StreamDetector{
				receiverHost: "192.168.1.100",
				proxyEnabled: tt.proxyEnabled,
				proxyHost:    tt.proxyHost,
				logger:       logger,
			}

			candidates := detector.buildCandidates("1:0:1:6DCF")

			if len(candidates) != tt.wantCandidates {
				t.Errorf("got %d candidates, want %d", len(candidates), tt.wantCandidates)
			}

			if candidates[0].Port != tt.wantFirstPort {
				t.Errorf("first candidate port = %d, want %d", candidates[0].Port, tt.wantFirstPort)
			}

			// Check all expected ports are present
			portMap := make(map[int]bool)
			for _, c := range candidates {
				portMap[c.Port] = true
			}

			for _, port := range tt.wantContainPort {
				if !portMap[port] {
					t.Errorf("expected port %d in candidates", port)
				}
			}
		})
	}
}
