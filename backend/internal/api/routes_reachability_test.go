// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
	"github.com/stretchr/testify/require"
)

// TestProductionRouterReachesHandwrittenRoutes drives the router the daemon actually builds.
//
// The v3 handwritten routes used to live in two hand-maintained lists: one consumed by
// v3.NewHandler (which the v3 package tests use) and one consumed by this wiring function.
// They drifted, and 20 routes - the entire Android device grant surface, the household policy
// and approval endpoints, and the notification endpoints - existed only in the list the tests
// exercised. Every one of them answered 404 in production while the test suite stayed green.
//
// Asserting against v3.NewHandler cannot catch that, because that is the path that was correct.
// This test therefore goes through buildRouterWithBindings, the same function cmd/daemon uses.
func TestProductionRouterReachesHandwrittenRoutes(t *testing.T) {
	s := mustNewServer(t, config.AppConfig{}, config.NewManager(""))
	router, _, err := s.buildRouterWithBindings(ConfigVariantProdStatic)
	require.NoError(t, err)
	require.NotNil(t, router)

	cases := []struct{ method, path string }{
		// Android / native device grant
		{http.MethodPost, "/api/v3/auth/device/grant/start"},
		{http.MethodPost, "/api/v3/auth/device/grant/finish"},
		{http.MethodPost, "/api/v3/auth/device/refresh"},

		// Passkey / identity
		{http.MethodGet, "/api/v3/auth/status"},
		{http.MethodPost, "/api/v3/auth/passkey/login/start"},
		{http.MethodPost, "/api/v3/auth/passkey/register/start"},
		{http.MethodPost, "/api/v3/auth/login/password"},
		{http.MethodPost, "/api/v3/auth/recovery"},

		// Household policies and approvals (consumed by the WebUI admin sections)
		{http.MethodGet, "/api/v3/household/policies/access"},
		{http.MethodGet, "/api/v3/household/approvals"},
		{http.MethodGet, "/api/v3/household/resource-policy"},

		// Notifications (consumed by the WebUI notification centre)
		{http.MethodGet, "/api/v3/notifications"},
		{http.MethodGet, "/api/v3/notifications/vapid-key"},
		{http.MethodPost, "/api/v3/notifications/mark-read"},

		// Profiles
		{http.MethodGet, "/api/v3/profiles"},
	}

	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "http://localhost"+tc.path, strings.NewReader("{}"))
			req.Host = "localhost"
			req.Header.Set("Content-Type", "application/json")
			// The CSRF middleware rejects unsafe methods without a same-origin Origin header
			// before routing is even reached; supply one so a 403 cannot mask a 404.
			req.Header.Set("Origin", "http://localhost")

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			require.NotEqual(t, http.StatusNotFound, rec.Code,
				"%s %s is not routed in the production router (body: %s)", tc.method, tc.path, truncate(rec.Body.String()))
			require.NotEqual(t, http.StatusMethodNotAllowed, rec.Code,
				"%s %s is routed but rejects its own method", tc.method, tc.path)
		})
	}
}

// TestProductionRouterMatchesHandlerRouter pins the two construction paths to the same
// route inventory, so a route added to one can no longer go missing in the other.
func TestProductionRouterMatchesHandlerRouter(t *testing.T) {
	s := mustNewServer(t, config.AppConfig{}, config.NewManager(""))
	_, snapshot, err := s.buildRouterWithBindings(ConfigVariantProdStatic)
	require.NoError(t, err)

	production := map[string]bool{}
	for key := range snapshotAsMap(snapshot) {
		if key.RouterID == "v3" {
			production[key.Method+" "+key.Pattern] = true
		}
	}

	var missing []string
	for _, route := range v3.HandwrittenRoutePatterns() {
		key := route[0] + " " + v3.V3BaseURL + route[1]
		if !production[key] {
			missing = append(missing, key)
		}
	}
	require.Empty(t, missing,
		"handwritten v3 routes did not reach the production router; they must be registered "+
			"through v3.RegisterPasskeyRoutesWithRegistrar in buildRouterWithBindings: %v", missing)
}

func truncate(s string) string {
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}
