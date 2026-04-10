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
	v3pairing "github.com/ManuGH/xg2g/internal/control/http/v3/pairing"
	deviceauthmodel "github.com/ManuGH/xg2g/internal/domain/deviceauth/model"
	deviceauthstore "github.com/ManuGH/xg2g/internal/domain/deviceauth/store"
)

func TestDeviceSessionRoute_RefreshIssuesAccessTokenAndRotatesGrant(t *testing.T) {
	currentNow := time.Now().UTC().Truncate(time.Second)
	store := deviceauthstore.NewMemoryStateStore()
	srv := NewServer(config.AppConfig{}, nil, nil)
	srv.deviceAuthStateStore = store
	srv.pairingV3Service = v3pairing.NewService(v3pairing.Deps{
		StateStore:             store,
		Now:                    func() time.Time { return currentNow },
		DeviceGrantRotateAfter: time.Minute,
	})
	srv.deviceAuthV3Service = v3deviceauth.NewService(v3deviceauth.Deps{
		StateStore:             store,
		Now:                    func() time.Time { return currentNow },
		DeviceGrantRotateAfter: time.Minute,
	})

	handler, exchanged := pairAndExchangeDevice(t, srv)

	currentNow = currentNow.Add(2 * time.Minute)

	refreshResp := doJSONRequest(t, handler, http.MethodPost, "/api/v3/auth/device/session", map[string]any{
		"deviceGrantId": exchanged.DeviceGrantID,
		"deviceGrant":   exchanged.DeviceGrant,
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
	srv := NewServer(config.AppConfig{}, nil, nil)
	handler, exchanged := pairAndExchangeDevice(t, srv)

	resp := doJSONRequest(t, handler, http.MethodPost, "/api/v3/auth/device/session", map[string]any{
		"deviceGrantId": exchanged.DeviceGrantID,
		"deviceGrant":   "wrong-grant",
	}, nil, false)
	assertProblemDetails(t, resp, http.StatusForbidden, "auth/device_session/forbidden")
}

func TestWebBootstrapRoute_IssuesCookieAndRedirects(t *testing.T) {
	srv := NewServer(config.AppConfig{}, nil, nil)
	handler, exchanged := pairAndExchangeDevice(t, srv)

	startResp := doJSONRequest(t, handler, http.MethodPost, "/api/v3/auth/web-bootstrap", map[string]any{
		"targetPath": "/ui/?mode=tv",
	}, map[string]string{
		"Authorization": "Bearer " + exchanged.AccessToken,
	}, false)
	if startResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected web bootstrap start 201, got %d", startResp.StatusCode)
	}
	var started createWebBootstrapResponse
	decodeJSONResponse(t, startResp, &started)
	if started.BootstrapID == "" || started.BootstrapToken == "" || started.CompletePath == "" {
		t.Fatalf("expected web bootstrap credentials, got %#v", started)
	}

	completeResp := doJSONRequest(t, handler, http.MethodGet, started.CompletePath, nil, map[string]string{
		webBootstrapHeaderName: started.BootstrapToken,
	}, true)
	if completeResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected web bootstrap completion redirect 303, got %d", completeResp.StatusCode)
	}
	if location := completeResp.Header.Get("Location"); location != "/ui/?mode=tv" {
		t.Fatalf("expected redirect target /ui/?mode=tv, got %q", location)
	}
	var sessionCookie *http.Cookie
	for _, cookie := range completeResp.Cookies() {
		if cookie.Name == "xg2g_session" {
			sessionCookie = cookie
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected xg2g_session cookie on web bootstrap completion")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v3/system/healthz", nil)
	req.AddCookie(sessionCookie)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected web bootstrap cookie to authorize protected route, got %d", rr.Code)
	}

	repeatResp := doJSONRequest(t, handler, http.MethodGet, started.CompletePath, nil, map[string]string{
		webBootstrapHeaderName: started.BootstrapToken,
	}, true)
	assertProblemDetails(t, repeatResp, http.StatusGone, "auth/web_bootstrap/consumed")
}

func TestWebBootstrapRoute_FailsWhenSourceSessionIsRevokedBeforeCompletion(t *testing.T) {
	currentNow := time.Now().UTC().Truncate(time.Second)
	store := deviceauthstore.NewMemoryStateStore()
	srv := NewServer(config.AppConfig{}, nil, nil)
	srv.deviceAuthStateStore = store
	srv.pairingV3Service = v3pairing.NewService(v3pairing.Deps{
		StateStore: store,
		Now: func() time.Time {
			return currentNow
		},
	})
	srv.deviceAuthV3Service = v3deviceauth.NewService(v3deviceauth.Deps{
		StateStore: store,
		Now:        func() time.Time { return currentNow },
	})

	handler, exchanged := pairAndExchangeDevice(t, srv)

	startResp := doJSONRequest(t, handler, http.MethodPost, "/api/v3/auth/web-bootstrap", map[string]any{}, map[string]string{
		"Authorization": "Bearer " + exchanged.AccessToken,
	}, false)
	if startResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected web bootstrap start 201, got %d", startResp.StatusCode)
	}
	var started createWebBootstrapResponse
	decodeJSONResponse(t, startResp, &started)

	if _, err := store.UpdateAccessSession(context.Background(), exchanged.AccessSessionID, func(current *deviceauthmodel.AccessSessionRecord) error {
		revokedAt := currentNow
		current.RevokedAt = &revokedAt
		return nil
	}); err != nil {
		t.Fatalf("revoke source access session: %v", err)
	}

	completeResp := doJSONRequest(t, handler, http.MethodGet, started.CompletePath, nil, map[string]string{
		webBootstrapHeaderName: started.BootstrapToken,
	}, true)
	assertProblemDetails(t, completeResp, http.StatusGone, "auth/web_bootstrap/revoked")
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
	srv.pairingV3Service = v3pairing.NewService(v3pairing.Deps{
		StateStore:                 store,
		PublishedEndpointsProvider: serverPublishedEndpointProvider{s: srv},
		Now:                        func() time.Time { return currentNow },
		DeviceGrantRotateAfter:     time.Minute,
	})
	srv.deviceAuthV3Service = v3deviceauth.NewService(v3deviceauth.Deps{
		StateStore:                 store,
		PublishedEndpointsProvider: serverPublishedEndpointProvider{s: srv},
		Now:                        func() time.Time { return currentNow },
		DeviceGrantRotateAfter:     time.Minute,
	})

	handler, exchanged := pairAndExchangeDevice(t, srv)
	currentNow = currentNow.Add(2 * time.Minute)

	refreshResp := doJSONRequest(t, handler, http.MethodPost, "/api/v3/auth/device/session", map[string]any{
		"deviceGrantId": exchanged.DeviceGrantID,
		"deviceGrant":   exchanged.DeviceGrant,
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

func TestWebBootstrapRoute_BlocksWhenPublicWebContractIsBroken(t *testing.T) {
	cfg := config.AppConfig{
		TrustedProxies: "127.0.0.1/32",
		Connectivity: config.ConnectivityConfig{
			Profile: "reverse_proxy",
			PublishedEndpoints: []config.PublishedEndpointConfig{
				{
					URL:             "https://public.example",
					Kind:            "public_https",
					Priority:        10,
					AllowPairing:    true,
					AllowStreaming:  true,
					AllowWeb:        false,
					AllowNative:     true,
					AdvertiseReason: "native-only public reverse proxy",
				},
			},
		},
	}

	srv := NewServer(cfg, nil, nil)
	handler, exchanged := pairAndExchangeDevice(t, srv)

	resp := doJSONRequest(t, handler, http.MethodPost, "/api/v3/auth/web-bootstrap", map[string]any{
		"targetPath": "/ui/?mode=tv",
	}, map[string]string{
		"Authorization": "Bearer " + exchanged.AccessToken,
	}, false)
	assertProblemDetails(t, resp, http.StatusServiceUnavailable, "connectivity/contract_blocked")
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
