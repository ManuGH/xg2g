package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

func TestOperatorRoutes_InternalAllowlist(t *testing.T) {
	cfg := config.AppConfig{
		APIToken:       "admin-token",
		APITokenScopes: []string{string(v3.ScopeV3Admin), string(v3.ScopeV3Status)},
	}
	s := mustNewServer(t, cfg, config.NewManager(""))
	router := mustBuildChiRouter(s)

	type methodSet map[string]struct{}
	got := map[string]methodSet{}
	err := chi.Walk(router, func(method string, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if !strings.HasPrefix(route, "/internal/") {
			return nil
		}
		if _, ok := got[route]; !ok {
			got[route] = methodSet{}
		}
		got[route][method] = struct{}{}
		return nil
	})
	require.NoError(t, err)

	expected := map[string]methodSet{
		"/internal/setup/validate":        {"POST": {}},
		"/internal/system/config/reload": {"POST": {}},
	}
	require.Equal(t, expected, got)
}

func TestOperatorRoutes_InternalEndpointsRequireAdminScope(t *testing.T) {
	cfg := config.AppConfig{
		APITokens: []config.ScopedToken{
			{Token: "read-token", Scopes: []string{string(v3.ScopeV3Read)}},
			{Token: "admin-token", Scopes: []string{string(v3.ScopeV3Admin)}},
		},
		Network: config.NetworkConfig{
			Outbound: config.OutboundConfig{
				Enabled: true,
				Allow: config.OutboundAllowlist{
					Hosts:   []string{"192.0.2.10"},
					Ports:   []int{80},
					Schemes: []string{"http"},
				},
			},
		},
	}
	s := mustNewServer(t, cfg, config.NewManager(""))
	router := mustBuildChiRouter(s)

	cases := []struct {
		name        string
		path        string
		body        string
		contentType string
	}{
		{
			name: "config reload",
			path: "/internal/system/config/reload",
		},
		{
			name:        "setup validate",
			path:        "/internal/setup/validate",
			body:        `{"baseUrl":"http://192.0.2.10"}`,
			contentType: "application/json",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			readReq := httptest.NewRequest(http.MethodPost, "http://example.com"+tc.path, strings.NewReader(tc.body))
			readReq.Header.Set("Authorization", "Bearer read-token")
			readReq.Header.Set("Origin", "http://example.com")
			if tc.contentType != "" {
				readReq.Header.Set("Content-Type", tc.contentType)
			}
			readRes := httptest.NewRecorder()
			router.ServeHTTP(readRes, readReq)
			require.Equal(t, http.StatusForbidden, readRes.Code)

			adminReq := httptest.NewRequest(http.MethodPost, "http://example.com"+tc.path, strings.NewReader(tc.body))
			adminReq.Header.Set("Authorization", "Bearer admin-token")
			adminReq.Header.Set("Origin", "http://example.com")
			if tc.contentType != "" {
				adminReq.Header.Set("Content-Type", tc.contentType)
			}
			adminRes := httptest.NewRecorder()
			router.ServeHTTP(adminRes, adminReq)
			if adminRes.Code == http.StatusUnauthorized || adminRes.Code == http.StatusForbidden {
				t.Fatalf("expected admin to pass authz gate for %s, got %d", tc.path, adminRes.Code)
			}
		})
	}
}

func mustBuildChiRouter(s *Server) chi.Router {
	r := s.newRouter()
	s.registerPublicRoutes(r)
	rAuth, rRead, rWrite, rAdmin, rStatus := s.scopedRouters(r)
	s.registerOperatorRoutes(rAuth, rAdmin, rStatus)
	s.registerCanonicalV3Routes(r)
	v3.RegisterCompatibilityRoutes(rRead, rWrite, s.v3Handler)
	return r
}
