package middleware

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

// P1.4 Fix E: Proxy-Aware HTTPS Security Proof
// This test verifies that we do NOT honor X-Forwarded-Proto from untrusted sources.
func TestSecurityHeaders_ProxyAwareness(t *testing.T) {
	// Setup trusted proxies (e.g. 10.0.0.1)
	trustedCIDRStrings := []string{"10.0.0.1/32"}
	trustedProxies, err := ParseCIDRs(trustedCIDRStrings)
	if err != nil {
		t.Fatalf("Failed to parse trusted CIDRs: %v", err)
	}

	// Helper to check HSTS
	checkHSTS := func(t *testing.T, desc string, r *http.Request, expectHSTS bool) {
		t.Helper()
		rec := httptest.NewRecorder()

		// Create handler using SecurityHeaders middleware
		handler := SecurityHeaders("", trustedProxies)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
		}))

		handler.ServeHTTP(rec, r)

		hsts := rec.Header().Get("Strict-Transport-Security")
		if expectHSTS && hsts == "" {
			t.Errorf("%s: Expected HSTS header, got none", desc)
		}
		if !expectHSTS && hsts != "" {
			t.Errorf("%s: Expected NO HSTS header, got %q", desc, hsts)
		}
	}

	// Case 1: Untrusted IP sending X-Forwarded-Proto: https
	// This simulates an attacker trying to bypass HSTS check or poison cache
	req1 := httptest.NewRequest("GET", "http://example.com", nil)
	req1.RemoteAddr = "192.168.1.50:1234" // Untrusted
	req1.Header.Set("X-Forwarded-Proto", "https")
	checkHSTS(t, "Untrusted IP with X-Forwarded-Proto", req1, false)

	// Case 2: Trusted IP sending X-Forwarded-Proto: https
	// This simulates a legitimate proxy (LB) terminating SSL
	req2 := httptest.NewRequest("GET", "http://example.com", nil)
	req2.RemoteAddr = "10.0.0.1:5678" // Trusted
	req2.Header.Set("X-Forwarded-Proto", "https")
	checkHSTS(t, "Trusted IP with X-Forwarded-Proto", req2, true)

	// Case 3: Direct TLS (RemoteAddr doesn't matter)
	req3 := httptest.NewRequest("GET", "https://example.com", nil)
	req3.RemoteAddr = "192.168.1.50:1234"
	req3.TLS = &tls.ConnectionState{} // Simulate TLS connection
	checkHSTS(t, "Direct TLS connection", req3, true)
}
