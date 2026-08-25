// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/stretchr/testify/require"
)

// The pairing endpoints are the only unauthenticated write surface in v3 and the
// only place where a native client learns its credentials. They had route and
// auth-boundary coverage but no response-shape coverage, so the hand-written
// hand-written exchange response struct could drift from the published contract
// unnoticed — which it did: the spec still described the retired
// deviceGrant/accessSession quartet long after the handler had moved to
// identity-shaped tokens, and the Android client faithfully decoded the spec
// into a runtime failure.
//
// These tests close that gap by validating every pairing response against
// api/openapi.yaml itself, so handler and contract can no longer diverge in CI.

func doPairingContractRequest(t *testing.T, doc *openapi3.T, handler http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		require.NoError(t, err, "marshal request body")
	}

	req := httptest.NewRequest(method, path, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	validateOpenAPIResponse(t, doc, req, rr, &openapi3filter.Options{})
	return rr
}

func TestV3Contract_PairingFlowResponsesMatchOpenAPI(t *testing.T) {
	doc := loadOpenAPIDoc(t)

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
	withIdentityDeviceEnrollment(t, srv, "viewer")
	jwk := generateTestDeviceJWK(t)

	handler, err := newHandlerWithMiddlewares(srv, srv.GetConfig(), nil)
	require.NoError(t, err, "build handler")

	startRR := doPairingContractRequest(t, doc, handler, http.MethodPost, "/api/v3/pairing/start", map[string]any{
		"deviceName":             "Living Room TV",
		"deviceType":             "android_tv",
		"requestedPolicyProfile": "tv-default",
	})
	require.Equal(t, http.StatusCreated, startRR.Code, "start pairing")

	var started StartPairingResponse
	require.NoError(t, json.Unmarshal(startRR.Body.Bytes(), &started), "decode start response")
	require.NotEmpty(t, started.PairingId)
	require.NotEmpty(t, started.PairingSecret)

	statusRR := doPairingContractRequest(t, doc, handler, http.MethodPost, "/api/v3/pairing/"+started.PairingId+"/status", map[string]any{
		"pairingSecret": started.PairingSecret,
	})
	require.Equal(t, http.StatusOK, statusRR.Code, "pairing status")

	srv.AuthMiddlewareOverride = testPrincipalAuthMiddleware(t, []string{"v3:admin"})
	authedHandler, err := newHandlerWithMiddlewares(srv, srv.GetConfig(), nil)
	require.NoError(t, err, "build authed handler")

	approveRR := doPairingContractRequest(t, doc, authedHandler, http.MethodPost, "/api/v3/pairing/"+started.PairingId+"/approve", map[string]any{})
	require.Equal(t, http.StatusOK, approveRR.Code, "approve pairing")

	srv.AuthMiddlewareOverride = nil
	handler, err = newHandlerWithMiddlewares(srv, srv.GetConfig(), nil)
	require.NoError(t, err, "rebuild handler")

	exchangeRR := doPairingContractRequest(t, doc, handler, http.MethodPost, "/api/v3/pairing/"+started.PairingId+"/exchange", map[string]any{
		"pairingSecret": started.PairingSecret,
		"deviceJwk":     jwk,
	})
	require.Equal(t, http.StatusOK, exchangeRR.Code, "exchange pairing")
}

// The exchange response is the contract a freshly enrolled device depends on
// before it can make any other call, so it is asserted field by field rather
// than only schema-validated: a schema stays satisfied when the server drops a
// field the client cannot work without and the spec drops it in lock-step.
func TestV3Contract_PairingExchangeResponseIsIdentityShaped(t *testing.T) {
	doc := loadOpenAPIDoc(t)

	srv := NewServer(config.AppConfig{}, nil, nil)
	withIdentityDeviceEnrollment(t, srv, "viewer")
	jwk := generateTestDeviceJWK(t)

	handler, err := newHandlerWithMiddlewares(srv, srv.GetConfig(), nil)
	require.NoError(t, err, "build handler")

	startRR := doPairingContractRequest(t, doc, handler, http.MethodPost, "/api/v3/pairing/start", map[string]any{
		"deviceType": "android_tv",
	})
	require.Equal(t, http.StatusCreated, startRR.Code)

	var started StartPairingResponse
	require.NoError(t, json.Unmarshal(startRR.Body.Bytes(), &started))

	srv.AuthMiddlewareOverride = testPrincipalAuthMiddleware(t, []string{"v3:admin"})
	authedHandler, err := newHandlerWithMiddlewares(srv, srv.GetConfig(), nil)
	require.NoError(t, err)
	approveRR := doPairingContractRequest(t, doc, authedHandler, http.MethodPost, "/api/v3/pairing/"+started.PairingId+"/approve", map[string]any{})
	require.Equal(t, http.StatusOK, approveRR.Code)

	srv.AuthMiddlewareOverride = nil
	handler, err = newHandlerWithMiddlewares(srv, srv.GetConfig(), nil)
	require.NoError(t, err)

	exchangeRR := doPairingContractRequest(t, doc, handler, http.MethodPost, "/api/v3/pairing/"+started.PairingId+"/exchange", map[string]any{
		"pairingSecret": started.PairingSecret,
		"deviceJwk":     jwk,
	})
	require.Equal(t, http.StatusOK, exchangeRR.Code)

	var wire map[string]any
	require.NoError(t, json.Unmarshal(exchangeRR.Body.Bytes(), &wire), "decode exchange response")

	for _, field := range []string{
		"pairingId",
		"deviceId",
		"tokenType",
		"accessToken",
		"expiresIn",
		"refreshToken",
		"scope",
		"policyVersion",
		"endpoints",
	} {
		require.Contains(t, wire, field, "exchange response must carry %q", field)
	}

	// The retired deviceauth vocabulary must not come back by accident: a client
	// that still reads these fields is decoding a contract that no longer exists.
	for _, retired := range []string{
		"deviceGrant",
		"deviceGrantId",
		"deviceGrantExpiresAt",
		"accessSessionId",
		"accessTokenExpiresAt",
		"scopes",
	} {
		require.NotContains(t, wire, retired, "retired deviceauth field %q must stay gone", retired)
	}

	require.Equal(t, "DPoP", wire["tokenType"], "access token is sender-constrained to the device key")
}
