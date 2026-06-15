package v3

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	householddomain "github.com/ManuGH/xg2g/internal/household"
)

// The M24 no-header downgrade sits on the request hot path. A header-less HLS client fetches
// a segment every few seconds, so the WARN breadcrumb must be rate-limited (one WARN per
// interval, DEBUG otherwise) to avoid flooding the log.
func TestHouseholdNoHeaderWarn_RateLimited(t *testing.T) {
	householdNoHeaderWarnAt.Store(0)
	t.Cleanup(func() { householdNoHeaderWarnAt.Store(0) })

	base := int64(1_000_000_000_000_000)
	win := int64(householdNoHeaderWarnInterval)

	if !householdNoHeaderShouldWarn(base) {
		t.Fatal("first downgrade after reset must emit a WARN breadcrumb")
	}
	if householdNoHeaderShouldWarn(base + 1) {
		t.Fatal("an immediate repeat must be rate-limited to DEBUG (no per-segment flood)")
	}
	if householdNoHeaderShouldWarn(base + win - 1) {
		t.Fatal("a repeat just under the interval must still be rate-limited")
	}
	if !householdNoHeaderShouldWarn(base + win) {
		t.Fatal("after the interval elapses a new WARN breadcrumb must be emitted")
	}
}

// M24: when a household PIN is configured (parental controls active), a request WITHOUT the
// X-Household-Profile header must resolve to the least-trusted restricted profile, not the
// unrestricted adult default — otherwise a restricted user escalates simply by omitting the
// header.
func TestHouseholdMiddleware_NoHeaderWithPinResolvesRestricted(t *testing.T) {
	pinHash, err := householddomain.HashPIN("1234")
	if err != nil {
		t.Fatalf("hash pin: %v", err)
	}

	svc := householddomain.NewService(householddomain.NewMemoryStore())
	srv := NewServer(config.AppConfig{
		TrustedProxies: "0.0.0.0/0,::/0",
		Household:      config.HouseholdConfig{PinHash: pinHash},
	}, nil, nil)
	srv.SetDependencies(Dependencies{Households: svc})
	srv.AuthMiddlewareOverride = testPrincipalAuthMiddleware(t, []string{"v3:read"})

	var resolved householddomain.Profile
	captured := false
	handler := srv.householdMiddleware(srv.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p := householddomain.ProfileFromContext(r.Context()); p != nil {
			resolved = *p
			captured = true
		}
		w.WriteHeader(http.StatusNoContent)
	})))

	// No X-Household-Profile header, PIN configured.
	req := httptest.NewRequest(http.MethodGet, V3BaseURL+"/services/bouquets", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// The restricted profile is not "Protected", so the middleware passes it through; the
	// per-feature gates (DVR/settings) are what deny — verified via permissions below.
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}
	if !captured {
		t.Fatal("expected a household profile in context")
	}
	if resolved.Kind == householddomain.ProfileKindAdult {
		t.Fatalf("SECURITY: no-header request with a PIN configured must NOT resolve to an adult profile (kind=%q id=%q)", resolved.Kind, resolved.ID)
	}
	if householddomain.CanAccessDVRPlayback(resolved) || householddomain.CanManageDVR(resolved) || householddomain.CanAccessSettings(resolved) {
		t.Fatalf("restricted no-header profile must have no DVR/settings permissions, got %+v", resolved.Permissions)
	}
}

// Without a PIN there are no parental controls, so the historical adult default is
// preserved for header-less requests (no behavior change / no surprise downgrade).
func TestHouseholdMiddleware_NoHeaderWithoutPinKeepsDefault(t *testing.T) {
	svc := householddomain.NewService(householddomain.NewMemoryStore())
	srv := NewServer(config.AppConfig{TrustedProxies: "0.0.0.0/0,::/0"}, nil, nil)
	srv.SetDependencies(Dependencies{Households: svc})
	srv.AuthMiddlewareOverride = testPrincipalAuthMiddleware(t, []string{"v3:read"})

	var resolvedID string
	handler := srv.householdMiddleware(srv.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p := householddomain.ProfileFromContext(r.Context()); p != nil {
			resolvedID = p.ID
		}
		w.WriteHeader(http.StatusNoContent)
	})))

	req := httptest.NewRequest(http.MethodGet, V3BaseURL+"/services/bouquets", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if resolvedID != householddomain.DefaultProfileID {
		t.Fatalf("no PIN configured: header-less request should use the default profile, got %q", resolvedID)
	}
}
