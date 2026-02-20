package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
)

func TestInternalSetupValidate_RequiresAdminScopeAndOutboundPolicy(t *testing.T) {
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
		Streaming: config.StreamingConfig{
			DeliveryPolicy: "universal",
		},
	}
	s := mustNewServer(t, cfg, config.NewManager(""))
	router := s.routes()

	makeReq := func(token string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, "http://example.com/internal/setup/validate", strings.NewReader(`{"baseUrl":"http://192.0.2.11"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Origin", "http://example.com")
		return req
	}

	t.Run("read scope denied", func(t *testing.T) {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, makeReq("read-token"))
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", w.Code)
		}
	})

	t.Run("admin scope reaches handler and outbound policy rejects host", func(t *testing.T) {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, makeReq("admin-token"))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}
		body, _ := io.ReadAll(w.Result().Body)
		if !strings.Contains(string(body), "outbound policy") {
			t.Fatalf("expected outbound policy rejection message, got: %s", string(body))
		}
	})
}
