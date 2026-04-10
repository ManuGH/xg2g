package v3

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	v3pairing "github.com/ManuGH/xg2g/internal/control/http/v3/pairing"
	deviceauthstore "github.com/ManuGH/xg2g/internal/domain/deviceauth/store"
)

func TestPairingRoutes_FlowAndAuthBoundaries(t *testing.T) {
	srv := NewServer(config.AppConfig{}, nil, nil)

	handler, err := newHandlerWithMiddlewares(srv, config.AppConfig{}, nil)
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}

	startResp := doPairingRequest(t, handler, http.MethodPost, "/api/v3/pairing/start", map[string]any{
		"deviceName":             "Living Room TV",
		"deviceType":             "android_tv",
		"requestedPolicyProfile": "tv-default",
	})
	if startResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected start pairing 201, got %d", startResp.StatusCode)
	}
	var started startPairingResponse
	decodeJSONResponse(t, startResp, &started)
	if started.PairingID == "" || started.PairingSecret == "" {
		t.Fatalf("expected pairing credentials, got %#v", started)
	}

	statusResp := doPairingRequest(t, handler, http.MethodPost, "/api/v3/pairing/"+started.PairingID+"/status", map[string]any{
		"pairingSecret": started.PairingSecret,
	})
	if statusResp.StatusCode != http.StatusOK {
		t.Fatalf("expected pairing status 200, got %d", statusResp.StatusCode)
	}
	var pending pairingStatusResponse
	decodeJSONResponse(t, statusResp, &pending)
	if pending.Status != "pending" {
		t.Fatalf("expected pending pairing status, got %q", pending.Status)
	}

	approveWithoutAuth := doPairingRequest(t, handler, http.MethodPost, "/api/v3/pairing/"+started.PairingID+"/approve", map[string]any{
		"approvedPolicyProfile": "tv-approved",
	})
	assertProblemDetails(t, approveWithoutAuth, http.StatusUnauthorized, "error/unauthorized")

	srv.AuthMiddlewareOverride = testPrincipalAuthMiddleware(t, []string{"v3:admin"})
	authedHandler, err := newHandlerWithMiddlewares(srv, config.AppConfig{}, nil)
	if err != nil {
		t.Fatalf("build authed handler: %v", err)
	}

	approveResp := doPairingRequest(t, authedHandler, http.MethodPost, "/api/v3/pairing/"+started.PairingID+"/approve", map[string]any{
		"approvedPolicyProfile": "tv-approved",
	})
	if approveResp.StatusCode != http.StatusOK {
		t.Fatalf("expected approve pairing 200, got %d", approveResp.StatusCode)
	}
	var approved approvePairingResponse
	decodeJSONResponse(t, approveResp, &approved)
	if approved.Status != "approved" {
		t.Fatalf("expected approved pairing status, got %q", approved.Status)
	}
	if approved.OwnerID == "" {
		t.Fatal("expected owner id from authenticated principal")
	}

	exchangeResp := doPairingRequest(t, handler, http.MethodPost, "/api/v3/pairing/"+started.PairingID+"/exchange", map[string]any{
		"pairingSecret": started.PairingSecret,
	})
	if exchangeResp.StatusCode != http.StatusOK {
		t.Fatalf("expected exchange pairing 200, got %d", exchangeResp.StatusCode)
	}
	var exchanged exchangePairingResponse
	decodeJSONResponse(t, exchangeResp, &exchanged)
	if exchanged.DeviceID == "" || exchanged.DeviceGrant == "" || exchanged.AccessToken == "" {
		t.Fatalf("expected exchange credentials, got %#v", exchanged)
	}

	repeatExchange := doPairingRequest(t, handler, http.MethodPost, "/api/v3/pairing/"+started.PairingID+"/exchange", map[string]any{
		"pairingSecret": started.PairingSecret,
	})
	assertProblemDetails(t, repeatExchange, http.StatusGone, "pairing/consumed")
}

func TestPairingRoutes_StatusReflectsExpiryAndRejectsExchange(t *testing.T) {
	currentNow := time.Date(2026, time.April, 9, 12, 0, 0, 0, time.UTC)
	store := deviceauthstore.NewMemoryStateStore()
	srv := NewServer(config.AppConfig{}, nil, nil)
	srv.deviceAuthStateStore = store
	srv.pairingV3Service = v3pairing.NewService(v3pairing.Deps{
		StateStore: store,
		Now: func() time.Time {
			return currentNow
		},
		PairingTTL: 1 * time.Minute,
	})

	handler, err := newHandlerWithMiddlewares(srv, config.AppConfig{}, nil)
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}

	startResp := doPairingRequest(t, handler, http.MethodPost, "/api/v3/pairing/start", map[string]any{
		"deviceType": "android_tv",
	})
	var started startPairingResponse
	decodeJSONResponse(t, startResp, &started)

	currentNow = currentNow.Add(2 * time.Minute)

	statusResp := doPairingRequest(t, handler, http.MethodPost, "/api/v3/pairing/"+started.PairingID+"/status", map[string]any{
		"pairingSecret": started.PairingSecret,
	})
	if statusResp.StatusCode != http.StatusOK {
		t.Fatalf("expected expired status lookup 200, got %d", statusResp.StatusCode)
	}
	var status pairingStatusResponse
	decodeJSONResponse(t, statusResp, &status)
	if status.Status != "expired" {
		t.Fatalf("expected expired status, got %q", status.Status)
	}

	exchangeResp := doPairingRequest(t, handler, http.MethodPost, "/api/v3/pairing/"+started.PairingID+"/exchange", map[string]any{
		"pairingSecret": started.PairingSecret,
	})
	assertProblemDetails(t, exchangeResp, http.StatusGone, "pairing/expired")
}

func TestPairingRoutes_SecretMismatchIsForbidden(t *testing.T) {
	srv := NewServer(config.AppConfig{}, nil, nil)
	srv.AuthMiddlewareOverride = testPrincipalAuthMiddleware(t, []string{"v3:admin"})
	handler, err := newHandlerWithMiddlewares(srv, config.AppConfig{}, nil)
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}

	startResp := doPairingRequest(t, handler, http.MethodPost, "/api/v3/pairing/start", map[string]any{
		"deviceType": "android_phone",
	})
	var started startPairingResponse
	decodeJSONResponse(t, startResp, &started)

	approveResp := doPairingRequest(t, handler, http.MethodPost, "/api/v3/pairing/"+started.PairingID+"/approve", map[string]any{})
	if approveResp.StatusCode != http.StatusOK {
		t.Fatalf("expected approve pairing 200, got %d", approveResp.StatusCode)
	}

	statusResp := doPairingRequest(t, handler, http.MethodPost, "/api/v3/pairing/"+started.PairingID+"/status", map[string]any{
		"pairingSecret": "WRONGSECRET",
	})
	assertProblemDetails(t, statusResp, http.StatusForbidden, "pairing/secret_mismatch")
}

func TestPairingRoutes_ExchangedAccessTokenAuthorizesProtectedRoute(t *testing.T) {
	srv := NewServer(config.AppConfig{}, nil, nil)

	handler, err := newHandlerWithMiddlewares(srv, config.AppConfig{}, nil)
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}

	startResp := doPairingRequest(t, handler, http.MethodPost, "/api/v3/pairing/start", map[string]any{
		"deviceType": "android_tablet",
	})
	var started startPairingResponse
	decodeJSONResponse(t, startResp, &started)

	srv.AuthMiddlewareOverride = testPrincipalAuthMiddleware(t, []string{"v3:admin"})
	authedHandler, err := newHandlerWithMiddlewares(srv, config.AppConfig{}, nil)
	if err != nil {
		t.Fatalf("build authed handler: %v", err)
	}
	approveResp := doPairingRequest(t, authedHandler, http.MethodPost, "/api/v3/pairing/"+started.PairingID+"/approve", map[string]any{})
	if approveResp.StatusCode != http.StatusOK {
		t.Fatalf("expected approve pairing 200, got %d", approveResp.StatusCode)
	}

	srv.AuthMiddlewareOverride = nil
	handler, err = newHandlerWithMiddlewares(srv, config.AppConfig{}, nil)
	if err != nil {
		t.Fatalf("rebuild handler: %v", err)
	}
	exchangeResp := doPairingRequest(t, handler, http.MethodPost, "/api/v3/pairing/"+started.PairingID+"/exchange", map[string]any{
		"pairingSecret": started.PairingSecret,
	})
	if exchangeResp.StatusCode != http.StatusOK {
		t.Fatalf("expected exchange pairing 200, got %d", exchangeResp.StatusCode)
	}
	var exchanged exchangePairingResponse
	decodeJSONResponse(t, exchangeResp, &exchanged)

	req := httptest.NewRequest(http.MethodGet, "/api/v3/system/healthz", nil)
	req.Header.Set("Authorization", "Bearer "+exchanged.AccessToken)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected exchanged access token to authorize protected route, got %d", rr.Code)
	}
	if !slices.Contains(exchanged.Scopes, "v3:read") || !slices.Contains(exchanged.Scopes, "v3:write") {
		t.Fatalf("expected exchanged device session scopes to include read+write, got %v", exchanged.Scopes)
	}

	intentReq := httptest.NewRequest(http.MethodPost, "/api/v3/intents", bytes.NewBufferString(`{"type":"stream.start","serviceRef":"1:0:1:test"}`))
	intentReq.Header.Set("Content-Type", "application/json")
	intentReq.Header.Set("Authorization", "Bearer "+exchanged.AccessToken)
	intentRR := httptest.NewRecorder()
	handler.ServeHTTP(intentRR, intentReq)
	if intentRR.Code == http.StatusUnauthorized || intentRR.Code == http.StatusForbidden {
		t.Fatalf("expected exchanged device token to pass intent authz, got %d", intentRR.Code)
	}
}

func TestPairingRoutes_ExchangeReturnsPublishedEndpoints(t *testing.T) {
	cfg := config.AppConfig{
		Connectivity: config.ConnectivityConfig{
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

	handler, err := newHandlerWithMiddlewares(srv, srv.GetConfig(), nil)
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}

	startResp := doPairingRequest(t, handler, http.MethodPost, "/api/v3/pairing/start", map[string]any{
		"deviceType": "android_tablet",
	})
	var started startPairingResponse
	decodeJSONResponse(t, startResp, &started)

	srv.AuthMiddlewareOverride = testPrincipalAuthMiddleware(t, []string{"v3:admin"})
	authedHandler, err := newHandlerWithMiddlewares(srv, srv.GetConfig(), nil)
	if err != nil {
		t.Fatalf("build authed handler: %v", err)
	}
	approveResp := doPairingRequest(t, authedHandler, http.MethodPost, "/api/v3/pairing/"+started.PairingID+"/approve", map[string]any{})
	if approveResp.StatusCode != http.StatusOK {
		t.Fatalf("expected approve pairing 200, got %d", approveResp.StatusCode)
	}

	srv.AuthMiddlewareOverride = nil
	handler, err = newHandlerWithMiddlewares(srv, srv.GetConfig(), nil)
	if err != nil {
		t.Fatalf("rebuild handler: %v", err)
	}
	exchangeResp := doPairingRequest(t, handler, http.MethodPost, "/api/v3/pairing/"+started.PairingID+"/exchange", map[string]any{
		"pairingSecret": started.PairingSecret,
	})
	if exchangeResp.StatusCode != http.StatusOK {
		t.Fatalf("expected exchange pairing 200, got %d", exchangeResp.StatusCode)
	}

	var exchanged exchangePairingResponse
	decodeJSONResponse(t, exchangeResp, &exchanged)
	if len(exchanged.Endpoints) != 1 {
		t.Fatalf("expected exactly one published endpoint, got %#v", exchanged.Endpoints)
	}
	if exchanged.Endpoints[0].URL != "https://public.example" {
		t.Fatalf("expected published endpoint url https://public.example, got %q", exchanged.Endpoints[0].URL)
	}
}

func TestPairingRoutes_BlockWhenPublicContractBroken(t *testing.T) {
	cfg := config.AppConfig{
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

	handler, err := newHandlerWithMiddlewares(srv, srv.GetConfig(), nil)
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}

	resp := doPairingRequest(t, handler, http.MethodPost, "/api/v3/pairing/start", map[string]any{
		"deviceType": "android_tv",
	})
	assertProblemDetails(t, resp, http.StatusServiceUnavailable, "connectivity/contract_blocked")
}

func doPairingRequest(t *testing.T, handler http.Handler, method, path string, body any) *http.Response {
	t.Helper()
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr.Result()
}

func decodeJSONResponse(t *testing.T, resp *http.Response, out any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}
