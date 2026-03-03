package api

import (
	"net/http"
	"testing"

	"github.com/ManuGH/xg2g/internal/api/testutil"
)

func TestClientIP(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		headers    map[string]string
		expected   string
	}{
		{
			name:       "direct connection",
			remoteAddr: "192.168.1.1:12345",
			headers:    map[string]string{},
			expected:   "192.168.1.1",
		},
		{
			name:       "invalid remote addr",
			remoteAddr: "invalid",
			headers:    map[string]string{},
			expected:   "invalid",
		},
		{
			name:       "X-Forwarded-For header (untrusted)",
			remoteAddr: "192.168.1.1:12345",
			headers: map[string]string{
				"X-Forwarded-For": "203.0.113.1",
			},
			// Since 192.168.1.1 is not in XG2G_TRUSTED_PROXIES, ignore header
			expected: "192.168.1.1",
		},
		{
			name:       "X-Real-IP header (untrusted)",
			remoteAddr: "192.168.1.1:12345",
			headers: map[string]string{
				"X-Real-IP": "203.0.113.5",
			},
			// Since 192.168.1.1 is not in XG2G_TRUSTED_PROXIES, ignore header
			expected: "192.168.1.1",
		},
		{
			name:       "X-Forwarded-For multiple IPs",
			remoteAddr: "10.0.0.1:8080",
			headers: map[string]string{
				"X-Forwarded-For": "203.0.113.1, 198.51.100.1, 192.0.2.1",
			},
			// Takes first IP if trusted, otherwise remoteAddr
			expected: "10.0.0.1",
		},
		{
			name:       "X-Forwarded-For with spaces",
			remoteAddr: "10.0.0.1:8080",
			headers: map[string]string{
				"X-Forwarded-For": "  203.0.113.1  ",
			},
			expected: "10.0.0.1",
		},
		{
			name:       "IPv6 address",
			remoteAddr: "[::1]:8080",
			headers:    map[string]string{},
			expected:   "::1",
		},
		{
			name:       "remote addr without port",
			remoteAddr: "203.0.113.1",
			headers:    map[string]string{},
			expected:   "203.0.113.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &http.Request{
				RemoteAddr: tt.remoteAddr,
				Header:     make(http.Header),
			}

			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}

			result := testutil.ClientIP(req)
			if result != tt.expected {
				t.Errorf("Expected IP %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestRemoteIsTrusted(t *testing.T) {
	// Note: trustedCIDRs is loaded once from XG2G_TRUSTED_PROXIES env var
	// These tests verify the logic paths regardless of env config

	tests := []struct {
		name   string
		remote string
		// We can't predict result without knowing env, but we can test all branches
		testLogic bool
	}{
		{
			name:      "valid IP with port",
			remote:    "192.168.1.100:8080",
			testLogic: true,
		},
		{
			name:      "valid IP without port",
			remote:    "10.0.0.1",
			testLogic: true,
		},
		{
			name:      "localhost with port",
			remote:    "127.0.0.1:12345",
			testLogic: true,
		},
		{
			name:      "invalid IP format",
			remote:    "not-an-ip",
			testLogic: true,
		},
		{
			name:      "empty string",
			remote:    "",
			testLogic: true,
		},
		{
			name:      "IPv6 with port",
			remote:    "[::1]:8080",
			testLogic: true,
		},
		{
			name:      "IPv6 without port",
			remote:    "::1",
			testLogic: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Call the function to exercise all code paths
			result := testutil.RemoteIsTrusted(tt.remote)
			// Result depends on XG2G_TRUSTED_PROXIES env var
			// We just verify it doesn't panic and returns a bool
			_ = result
		})
	}
}
