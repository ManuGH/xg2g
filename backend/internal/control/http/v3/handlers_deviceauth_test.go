package v3

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	v3deviceauth "github.com/ManuGH/xg2g/internal/control/http/v3/deviceauth"
	deviceauthmodel "github.com/ManuGH/xg2g/internal/domain/deviceauth/model"
	deviceauthstore "github.com/ManuGH/xg2g/internal/domain/deviceauth/store"
)

func seedDeviceGrant(t *testing.T, store deviceauthstore.StateStore, now time.Time) (string, string) {
	t.Helper()
	device, err := deviceauthmodel.PrepareDeviceRecord(deviceauthmodel.DeviceRecord{
		DeviceID:      "dev-test-1",
		OwnerID:       "user-test-1",
		DeviceName:    "test-device",
		DeviceType:    deviceauthmodel.DeviceTypeAndroidTV,
		PolicyProfile: "default",
		CreatedAt:     now,
		LastSeenAt:    &now,
	})
	if err != nil {
		t.Fatalf("prepare device: %v", err)
	}
	if err := store.PutDevice(context.Background(), &device); err != nil {
		t.Fatalf("put device: %v", err)
	}

	rotateAfter := now.Add(-time.Minute)
	grant, err := deviceauthmodel.PrepareDeviceGrantRecord(deviceauthmodel.DeviceGrantRecord{
		GrantID:     "grant-test-1",
		DeviceID:    "dev-test-1",
		GrantHash:   deviceauthmodel.HashOpaqueSecret("grant-secret-1"),
		IssuedAt:    now.Add(-time.Hour),
		ExpiresAt:   now.Add(24 * time.Hour),
		RotateAfter: &rotateAfter,
	})
	if err != nil {
		t.Fatalf("prepare grant: %v", err)
	}
	if err := store.PutDeviceGrant(context.Background(), &grant); err != nil {
		t.Fatalf("put grant: %v", err)
	}
	return grant.GrantID, "grant-secret-1"
}

func TestDeviceSessionRoute_RefreshIssuesAccessTokenAndRotatesGrant(t *testing.T) {
	currentNow := time.Now().UTC().Truncate(time.Second)
	store := deviceauthstore.NewMemoryStateStore()
	srv := NewServer(config.AppConfig{}, nil, nil)
	srv.deviceAuthStateStore = store
	srv.deviceAuthV3Service = v3deviceauth.NewService(v3deviceauth.Deps{
		StateStore:             store,
		Now:                    func() time.Time { return currentNow },
		DeviceGrantRotateAfter: time.Minute,
	})

	handler, err := newHandlerWithMiddlewares(srv, srv.GetConfig(), nil)
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}

	grantID, grantSecret := seedDeviceGrant(t, store, currentNow)
	currentNow = currentNow.Add(2 * time.Minute)

	refreshResp := doJSONRequest(t, handler, http.MethodPost, "/api/v3/auth/device/session", map[string]any{
		"deviceGrantId": grantID,
		"deviceGrant":   grantSecret,
	}, nil, false)
	if refreshResp.StatusCode != http.StatusOK {
		t.Fatalf("expected device session refresh 200, got %d", refreshResp.StatusCode)
	}
	var refreshed createDeviceSessionResponse
	decodeJSONResponse(t, refreshResp, &refreshed)
	if refreshed.AccessToken == "" || refreshed.AccessSessionID == "" {
		t.Fatalf("expected refreshed access credentials, got %#v", refreshed)
	}
	if refreshed.RotatedDeviceGrantID == "" || refreshed.RotatedDeviceGrant == "" {
		t.Fatalf("expected rotated device grant credentials, got %#v", refreshed)
	}
	if !slices.Contains(refreshed.Scopes, "v3:read") || !slices.Contains(refreshed.Scopes, "v3:write") {
		t.Fatalf("expected refreshed device session scopes to include read+write, got %v", refreshed.Scopes)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v3/system/healthz", nil)
	req.Header.Set("Authorization", "Bearer "+refreshed.AccessToken)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected refreshed access token to authorize protected route, got %d", rr.Code)
	}

	intentReq := httptest.NewRequest(http.MethodPost, "/api/v3/intents", bytes.NewBufferString(`{"type":"stream.start","serviceRef":"1:0:1:test"}`))
	intentReq.Header.Set("Content-Type", "application/json")
	intentReq.Header.Set("Authorization", "Bearer "+refreshed.AccessToken)
	intentRR := httptest.NewRecorder()
	handler.ServeHTTP(intentRR, intentReq)
	if intentRR.Code == http.StatusUnauthorized || intentRR.Code == http.StatusForbidden {
		t.Fatalf("expected refreshed device token to pass intent authz, got %d", intentRR.Code)
	}
}

func TestDeviceSessionRoute_SecretMismatchIsForbidden(t *testing.T) {
	currentNow := time.Now().UTC().Truncate(time.Second)
	store := deviceauthstore.NewMemoryStateStore()
	srv := NewServer(config.AppConfig{}, nil, nil)
	srv.deviceAuthStateStore = store
	srv.deviceAuthV3Service = v3deviceauth.NewService(v3deviceauth.Deps{
		StateStore: store,
		Now:        func() time.Time { return currentNow },
	})
	handler, err := newHandlerWithMiddlewares(srv, srv.GetConfig(), nil)
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}
	grantID, _ := seedDeviceGrant(t, store, currentNow)

	resp := doJSONRequest(t, handler, http.MethodPost, "/api/v3/auth/device/session", map[string]any{
		"deviceGrantId": grantID,
		"deviceGrant":   "wrong-grant",
	}, nil, false)
	assertProblemDetails(t, resp, http.StatusForbidden, "auth/device_session/forbidden")
}

func TestDeviceSessionRoute_ReturnsPublishedEndpoints(t *testing.T) {
	currentNow := time.Now().UTC().Truncate(time.Second)
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
	store := deviceauthstore.NewMemoryStateStore()
	srv := NewServer(cfg, nil, nil)
	srv.deviceAuthStateStore = store
	srv.deviceAuthV3Service = v3deviceauth.NewService(v3deviceauth.Deps{
		StateStore:                 store,
		PublishedEndpointsProvider: serverPublishedEndpointProvider{s: srv},
		Now:                        func() time.Time { return currentNow },
		DeviceGrantRotateAfter:     time.Minute,
	})

	handler, err := newHandlerWithMiddlewares(srv, srv.GetConfig(), nil)
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}

	grantID, grantSecret := seedDeviceGrant(t, store, currentNow)
	currentNow = currentNow.Add(2 * time.Minute)

	refreshResp := doJSONRequest(t, handler, http.MethodPost, "/api/v3/auth/device/session", map[string]any{
		"deviceGrantId": grantID,
		"deviceGrant":   grantSecret,
	}, nil, false)
	if refreshResp.StatusCode != http.StatusOK {
		t.Fatalf("expected device session refresh 200, got %d", refreshResp.StatusCode)
	}

	var refreshed createDeviceSessionResponse
	decodeJSONResponse(t, refreshResp, &refreshed)
	if len(refreshed.Endpoints) != 1 {
		t.Fatalf("expected exactly one published endpoint, got %#v", refreshed.Endpoints)
	}
	if refreshed.Endpoints[0].URL != "https://public.example" {
		t.Fatalf("expected published endpoint url https://public.example, got %q", refreshed.Endpoints[0].URL)
	}
}

func pairAndExchangeDevice(t *testing.T, srv *Server) (http.Handler, exchangePairingResponse) {
	t.Helper()

	handler, err := newHandlerWithMiddlewares(srv, srv.GetConfig(), nil)
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}

	startResp := doPairingRequest(t, handler, http.MethodPost, "/api/v3/pairing/start", map[string]any{
		"deviceType": "android_tablet",
	})
	if startResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected start pairing 201, got %d", startResp.StatusCode)
	}
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
	return handler, exchanged
}

func doJSONRequest(t *testing.T, handler http.Handler, method, path string, body any, headers map[string]string, tlsRequest bool) *http.Response {
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
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	if tlsRequest {
		req.TLS = &tls.ConnectionState{}
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr.Result()
}
