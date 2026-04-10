package v3

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
)

func TestGetSystemConnectivity_ReturnsEffectiveContractAndRequestTruth(t *testing.T) {
	cfg := config.AppConfig{
		TrustedProxies: "127.0.0.1/32",
		AllowedOrigins: []string{
			"https://public.example",
		},
		Connectivity: config.ConnectivityConfig{
			Profile: "reverse_proxy",
			PublishedEndpoints: []config.PublishedEndpointConfig{
				{
					URL:             "https://public.example",
					Kind:            "public_https",
					Priority:        10,
					AllowPairing:    true,
					AllowStreaming:  true,
					AllowWeb:        true,
					AllowNative:     true,
					AdvertiseReason: "public reverse proxy",
				},
			},
		},
	}

	srv := NewServer(cfg, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v3/system/connectivity", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "public.example")
	req.Header.Set("Origin", "https://public.example")
	w := httptest.NewRecorder()

	srv.GetSystemConnectivity(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp connectivityContractResponse
	decodeJSONResponse(t, w.Result(), &resp)
	if resp.Profile != "reverse_proxy" {
		t.Fatalf("expected reverse_proxy profile, got %q", resp.Profile)
	}
	if resp.Status != "ok" {
		t.Fatalf("expected ok status, got %q", resp.Status)
	}
	if !resp.Request.EffectiveHTTPS {
		t.Fatal("expected effective https for trusted forwarded proto")
	}
	if !resp.Request.TrustedProxyMatch {
		t.Fatal("expected trusted proxy match for loopback remote address")
	}
	if resp.Request.SchemeSource != "trusted_x_forwarded_proto" {
		t.Fatalf("expected trusted_x_forwarded_proto scheme source, got %q", resp.Request.SchemeSource)
	}
	if resp.Selections.WebPublic.Endpoint == nil || resp.Selections.WebPublic.Endpoint.URL != "https://public.example" {
		t.Fatalf("expected selected public web endpoint, got %#v", resp.Selections.WebPublic)
	}
}
