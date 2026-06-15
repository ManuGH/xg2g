package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
)

// TestAPIRateLimitBurst_IsInert documents M21's core property as an observable behavior: the
// configured api.rateLimit.burst value has NO effect on the API limiter. Two servers identical
// except for RateLimitBurst (1 vs a large value) must throttle at the exact same point — the
// rps-derived window limit (RateLimitGlobal*60), never the burst value. This is the honest way
// to test a dead knob: it pins "burst changes nothing" rather than claiming a fix. Post-A1 the
// burst is also structurally disconnected (gone from StackConfig/APIRateLimit), so this guards
// against any future re-wiring that would silently let burst back in.
func TestAPIRateLimitBurst_IsInert(t *testing.T) {
	const (
		rps          = 1 // -> window limit = 60 requests/minute
		windowLimit  = rps * 60
		requestCount = windowLimit + 5
		clientIP     = "203.0.113.7:54321" // not whitelisted
	)

	baseCfg := func(burst int) config.AppConfig {
		return config.AppConfig{
			APIToken:         "test-token",
			APITokenScopes:   []string{"v3:read"},
			DataDir:          t.TempDir(),
			Enigma2:          config.Enigma2Settings{StreamPort: 8001, BaseURL: "http://127.0.0.1:1"},
			Version:          "test",
			RateLimitEnabled: true,
			RateLimitGlobal:  rps,
			RateLimitBurst:   burst, // the inert knob under test
		}
	}

	allowed := func(t *testing.T, burst int) int {
		t.Helper()
		s := mustNewServer(t, baseCfg(burst), nil)
		handler := s.Handler()
		ok := 0
		for i := 0; i < requestCount; i++ {
			req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
			req.RemoteAddr = clientIP
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusTooManyRequests {
				ok++
			}
		}
		return ok
	}

	allowedSmallBurst := allowed(t, 1)
	allowedHugeBurst := allowed(t, 2000)

	// The limiter must actually fire (sanity: the test exercises the rate-limit path).
	if allowedSmallBurst >= requestCount {
		t.Fatalf("rate limiter never engaged (%d/%d allowed) — test is not exercising the limiter", allowedSmallBurst, requestCount)
	}
	// Burst is inert: identical throttling regardless of its value...
	if allowedSmallBurst != allowedHugeBurst {
		t.Errorf("burst is NOT inert: burst=1 allowed %d but burst=2000 allowed %d", allowedSmallBurst, allowedHugeBurst)
	}
	// ...and the cap follows the rps-derived window, never the burst value (else burst=2000
	// would have let far more than ~60 through).
	if allowedHugeBurst > windowLimit+1 {
		t.Errorf("limit appears to follow burst, not rps: allowed %d with burst=2000 (window limit is %d)", allowedHugeBurst, windowLimit)
	}
}
