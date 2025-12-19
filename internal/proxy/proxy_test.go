package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
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
			name: "valid config (anonymous)",
			cfg: Config{
				ListenAddr:    ":18000",
				TargetURL:     "http://example.com:17999",
				Logger:        logger,
				AuthAnonymous: true,
			},
			wantErr: false,
		},
		{
			name: "valid config (token)",
			cfg: Config{
				ListenAddr: ":18000",
				TargetURL:  "http://example.com:17999",
				Logger:     logger,
				APIToken:   "valid-token",
			},
			wantErr: false,
		},
		{
			name: "valid config with custom port",
			cfg: Config{
				ListenAddr:    "127.0.0.1:8080",
				TargetURL:     "http://example.com:8080",
				Logger:        logger,
				AuthAnonymous: true,
			},
			wantErr: false,
		},
		{
			name: "fail-closed: missing token and not anonymous",
			cfg: Config{
				ListenAddr: ":18000",
				TargetURL:  "http://example.com:17999",
				Logger:     logger,
				// APIToken empty, AuthAnonymous false
			},
			wantErr: true,
		},
		{
			name: "fail-closed: empty token strings",
			cfg: Config{
				ListenAddr: ":18000",
				TargetURL:  "http://example.com:17999",
				Logger:     logger,
				APIToken:   "   ", // Whitespace check
			},
			wantErr: true,
		},
		{
			name: "valid config with receiver host (legacy implicit 8001)",
			cfg: Config{
				ListenAddr:    ":18000",
				ReceiverHost:  "192.168.1.10",
				Logger:        logger,
				AuthAnonymous: true,
			},
			wantErr: false,
		},
		{
			name: "invalid config: target url missing port",
			cfg: Config{
				ListenAddr:    ":18000",
				TargetURL:     "http://example.com", // No port
				Logger:        logger,
				AuthAnonymous: true,
			},
			wantErr: true,
		},
		{
			name: "missing listen addr",
			cfg: Config{
				TargetURL:     "http://example.com:17999",
				Logger:        logger,
				AuthAnonymous: true,
			},
			wantErr: true,
		},
		{
			name: "empty listen addr",
			cfg: Config{
				ListenAddr:    "",
				TargetURL:     "http://example.com:17999",
				Logger:        logger,
				AuthAnonymous: true,
			},
			wantErr: true,
		},
		{
			name: "missing target URL",
			cfg: Config{
				ListenAddr:    ":18000",
				Logger:        logger,
				AuthAnonymous: true,
			},
			wantErr: true,
		},
		{
			name: "empty target URL",
			cfg: Config{
				ListenAddr:    ":18000",
				TargetURL:     "",
				Logger:        logger,
				AuthAnonymous: true,
			},
			wantErr: true,
		},
		{
			name: "invalid target URL - empty scheme",
			cfg: Config{
				ListenAddr:    ":18000",
				TargetURL:     "://invalid",
				Logger:        logger,
				AuthAnonymous: true,
			},
			wantErr: true,
		},
		{
			name: "invalid target URL - just colon",
			cfg: Config{
				ListenAddr:    ":18000",
				TargetURL:     ":",
				Logger:        logger,
				AuthAnonymous: true,
			},
			wantErr: true,
		},
		{
			name: "invalid target URL - invalid characters",
			cfg: Config{
				ListenAddr:    ":18000",
				TargetURL:     "http://exa mple.com",
				Logger:        logger,
				AuthAnonymous: true,
			},
			wantErr: true,
		},
		{
			name: "invalid target URL - malformed scheme",
			cfg: Config{
				ListenAddr:    ":18000",
				TargetURL:     "ht!tp://example.com",
				Logger:        logger,
				AuthAnonymous: true,
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
	target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		// This should NOT be called for HEAD requests
		t.Error("HEAD request was proxied to target (should be handled locally)")
	}))
	defer target.Close()

	// Create proxy server
	srv, err := New(Config{
		ListenAddr:    ":0", // Random port
		TargetURL:     target.URL,
		Logger:        logger,
		AuthAnonymous: true,
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

	// Verify no body is sent for HEAD requests
	if body := rec.Body.Bytes(); len(body) != 0 {
		t.Errorf("Expected empty body for HEAD request, got %d bytes", len(body))
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
		if _, err := w.Write([]byte("stream data")); err != nil {
			t.Errorf("Write failed: %v", err)
		}
	}))
	defer target.Close()

	// Disable transcoding/repair for this test to ensure we test direct proxying
	t.Setenv("XG2G_H264_STREAM_REPAIR", "false")
	t.Setenv("XG2G_ENABLE_AUDIO_TRANSCODING", "false")
	t.Setenv("XG2G_GPU_TRANSCODE", "false")

	// Create proxy server
	srv, err := New(Config{
		ListenAddr:    ":0",
		TargetURL:     target.URL,
		Logger:        logger,
		AuthAnonymous: true,
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

func TestValidateUpstream_Strict(t *testing.T) {
	// Setup allowed authorities manually as we are testing validateUpstream logic in isolation
	// In real usage, New() populates this.
	s := &Server{
		allowedAuthorities: map[string]struct{}{
			"192.168.1.10:8001": {},
			"example.com:80":    {},
			"[::1]:8080":        {},
		},
	}

	tests := []struct {
		upstream string
		wantErr  bool
	}{
		{"http://192.168.1.10:8001/stream", false},
		{"http://example.com:80/stream", false},
		{"http://[::1]:8080/stream", false},

		// Port Pivot Attempts
		{"http://192.168.1.10:80/stream", true}, // Same host, diff port
		{"http://192.168.1.10:22/stream", true}, // Pivot to SSH
		{"http://example.com:443/stream", true}, // Same host, diff port

		// Missing Port (Strict Mode)
		{"http://192.168.1.10/stream", true}, // No port in request

		// Host Mismatch
		{"http://localhost:8001/stream", true}, // Not allowed (even if IP matches in DNS)
		{"http://127.0.0.1:8001/stream", true}, // Not allowed
		{"http://google.com:80/stream", true},

		// SSRF / Bad Schemes / UserInfo
		{"http://169.254.169.254:80/latest/meta-data", true},
		{"file:///etc/passwd", true},
		{"http://user@192.168.1.10:8001/stream", true},
		{"tcp://192.168.1.10:8001", true},

		// Canonicalization Checks
		{"http://Example.com:80/stream", false}, // Case insen host
		{"http://192.168.1.10:8001", false},     // No path is ok
	}

	for _, tt := range tests {
		t.Run(tt.upstream, func(t *testing.T) {
			if err := s.validateUpstream(tt.upstream); (err != nil) != tt.wantErr {
				t.Errorf("validateUpstream(%q) error = %v, wantErr %v", tt.upstream, err, tt.wantErr)
			}
		})
	}
}

func TestHandleRequest_Auth(t *testing.T) {
	logger := zerolog.New(io.Discard)
	s := &Server{
		apiToken: "secret-token",
		logger:   logger,
		registry: NewRegistry(),
	}

	tests := []struct {
		name       string
		token      string // Legacy header
		queryToken string
		authHeader string // Authorization header
		wantStatus int
	}{
		{"No token", "", "", "", http.StatusUnauthorized},
		{"Invalid token", "wrong", "", "", http.StatusUnauthorized},
		{"Valid header token (legacy)", "secret-token", "", "", http.StatusOK},
		{"Valid query token", "", "secret-token", "", http.StatusOK},
		{"Valid Bearer token", "", "", "Bearer secret-token", http.StatusOK},
		{"Invalid Bearer token", "", "", "Bearer wrong", http.StatusUnauthorized},
		{"Malformed Bearer header", "", "", "Basic secret-token", http.StatusUnauthorized},
	}

	// Mock target
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	u, _ := url.Parse(target.URL)
	s.proxy = httputil.NewSingleHostReverseProxy(u)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/stream/ref", nil)
			if tt.token != "" {
				req.Header.Set("X-API-Token", tt.token)
			}
			if tt.queryToken != "" {
				q := req.URL.Query()
				q.Set("token", tt.queryToken)
				req.URL.RawQuery = q.Encode()
			}
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			w := httptest.NewRecorder()
			s.handleRequest(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("Status = %v, want %v", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestShutdown(t *testing.T) {
	logger := zerolog.New(io.Discard)

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	srv, err := New(Config{
		ListenAddr:    ":0",
		TargetURL:     target.URL,
		Logger:        logger,
		AuthAnonymous: true,
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
