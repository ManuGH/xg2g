package v3

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestCORS_AllowlistEnforcement(t *testing.T) {
	s := &Server{
		cfg: config.AppConfig{
			AllowedOrigins: []string{"https://trusted.example.com", "https://also-trusted.dev"},
		},
	}

	tests := []struct {
		name              string
		origin            string
		expectACAO        string // Access-Control-Allow-Origin
		expectCredentials string // Access-Control-Allow-Credentials
		expectVary        string
	}{
		{
			name:              "Allowed origin reflected with credentials",
			origin:            "https://trusted.example.com",
			expectACAO:        "https://trusted.example.com",
			expectCredentials: "true",
			expectVary:        "Origin",
		},
		{
			name:              "Second allowed origin also reflected",
			origin:            "https://also-trusted.dev",
			expectACAO:        "https://also-trusted.dev",
			expectCredentials: "true",
			expectVary:        "Origin",
		},
		{
			name:              "Disallowed origin gets no CORS headers (browser blocks)",
			origin:            "https://evil.attacker.com",
			expectACAO:        "",
			expectCredentials: "",
			expectVary:        "",
		},
		{
			name:              "No origin = no CORS headers (same-origin)",
			origin:            "",
			expectACAO:        "",
			expectCredentials: "",
			expectVary:        "",
		},
	}

	for _, tt := range tests {
		t.Run("OPTIONS/"+tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodOptions, "/api/v3/recordings/test/resume", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			w := httptest.NewRecorder()
			s.HandleRecordingResumeOptions(w, req)

			res := w.Result()
			assert.Equal(t, tt.expectACAO, res.Header.Get("Access-Control-Allow-Origin"))
			assert.Equal(t, tt.expectCredentials, res.Header.Get("Access-Control-Allow-Credentials"))
			if tt.expectVary != "" {
				assert.Equal(t, tt.expectVary, res.Header.Get("Vary"))
			}
			assert.NotEqual(t, "*", res.Header.Get("Access-Control-Allow-Origin"),
				"ACAO must NEVER be * when Credentials is true (Fetch spec violation)")
		})
	}

	t.Run("No AllowedOrigins configured = default deny", func(t *testing.T) {
		emptyServer := &Server{cfg: config.AppConfig{}}
		req := httptest.NewRequest(http.MethodOptions, "/api/v3/recordings/test/resume", nil)
		req.Header.Set("Origin", "https://any-origin.com")
		w := httptest.NewRecorder()
		emptyServer.HandleRecordingResumeOptions(w, req)

		res := w.Result()
		assert.Equal(t, "", res.Header.Get("Access-Control-Allow-Origin"),
			"Empty AllowedOrigins must deny all cross-origin requests")
	})
}
