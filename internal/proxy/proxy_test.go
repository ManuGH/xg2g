package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestNew(t *testing.T) {
	logger := zerolog.New(io.Discard)

	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: Config{
				ListenAddr: ":18000",
				TargetURL:  "http://example.com:17999",
				Logger:     logger,
			},
			wantErr: false,
		},
		{
			name: "missing listen addr",
			cfg: Config{
				TargetURL: "http://example.com:17999",
				Logger:    logger,
			},
			wantErr: true,
		},
		{
			name: "missing target URL",
			cfg: Config{
				ListenAddr: ":18000",
				Logger:     logger,
			},
			wantErr: true,
		},
		{
			name: "invalid target URL",
			cfg: Config{
				ListenAddr: ":18000",
				TargetURL:  "://invalid",
				Logger:     logger,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestHandleHeadRequest(t *testing.T) {
	logger := zerolog.New(io.Discard)

	// Create a test target server
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// This should NOT be called for HEAD requests
		t.Error("HEAD request was proxied to target (should be handled locally)")
	}))
	defer target.Close()

	// Create proxy server
	srv, err := New(Config{
		ListenAddr: ":0", // Random port
		TargetURL:  target.URL,
		Logger:     logger,
	})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	// Create test request
	req := httptest.NewRequest(http.MethodHead, "/test/path", nil)
	rec := httptest.NewRecorder()

	// Handle request
	srv.handleRequest(rec, req)

	// Verify response
	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
	}

	if got := rec.Header().Get("Content-Type"); got != "video/mp2t" {
		t.Errorf("got Content-Type %q, want %q", got, "video/mp2t")
	}

	if got := rec.Header().Get("Accept-Ranges"); got != "none" {
		t.Errorf("got Accept-Ranges %q, want %q", got, "none")
	}
}

func TestHandleGetRequest(t *testing.T) {
	logger := zerolog.New(io.Discard)

	// Create a test target server
	targetCalled := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalled = true
		if r.Method != http.MethodGet {
			t.Errorf("got method %s, want %s", r.Method, http.MethodGet)
		}
		if r.URL.Path != "/test/stream" {
			t.Errorf("got path %s, want %s", r.URL.Path, "/test/stream")
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("stream data"))
	}))
	defer target.Close()

	// Create proxy server
	srv, err := New(Config{
		ListenAddr: ":0",
		TargetURL:  target.URL,
		Logger:     logger,
	})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	// Create test request
	req := httptest.NewRequest(http.MethodGet, "/test/stream", nil)
	rec := httptest.NewRecorder()

	// Handle request
	srv.handleRequest(rec, req)

	// Verify response
	if !targetCalled {
		t.Error("GET request was not proxied to target")
	}

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
	}

	if body := rec.Body.String(); body != "stream data" {
		t.Errorf("got body %q, want %q", body, "stream data")
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
			os.Setenv("XG2G_ENABLE_STREAM_PROXY", tt.envValue)
			defer os.Unsetenv("XG2G_ENABLE_STREAM_PROXY")

			if got := IsEnabled(); got != tt.want {
				t.Errorf("IsEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetListenAddr(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		want     string
	}{
		{
			name:     "custom port",
			envValue: "19000",
			want:     ":19000",
		},
		{
			name:     "default port",
			envValue: "",
			want:     ":18000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				os.Setenv("XG2G_PROXY_PORT", tt.envValue)
				defer os.Unsetenv("XG2G_PROXY_PORT")
			}

			if got := GetListenAddr(); got != tt.want {
				t.Errorf("GetListenAddr() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetTargetURL(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		want     string
	}{
		{
			name:     "set",
			envValue: "http://example.com:17999",
			want:     "http://example.com:17999",
		},
		{
			name:     "not set",
			envValue: "",
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				os.Setenv("XG2G_PROXY_TARGET", tt.envValue)
				defer os.Unsetenv("XG2G_PROXY_TARGET")
			}

			if got := GetTargetURL(); got != tt.want {
				t.Errorf("GetTargetURL() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShutdown(t *testing.T) {
	logger := zerolog.New(io.Discard)

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	srv, err := New(Config{
		ListenAddr: ":0",
		TargetURL:  target.URL,
		Logger:     logger,
	})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	// Start server in background
	go func() {
		if err := srv.Start(); err != nil && !strings.Contains(err.Error(), "Server closed") {
			t.Errorf("Start() failed: %v", err)
		}
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown() failed: %v", err)
	}
}
