// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/asn1"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
	"github.com/ManuGH/xg2g/internal/control/http/v3/dpop"
	"github.com/ManuGH/xg2g/internal/domain/identity"
	"github.com/ManuGH/xg2g/internal/domain/identity/store"
	"github.com/ManuGH/xg2g/internal/domain/identity/webauthn"
	"github.com/ManuGH/xg2g/internal/persistence/sqlite"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/legacy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAndroidDualKey_DPoPAndDeviceGrantE2E(t *testing.T) {
	ctx := context.Background()
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "identity.sqlite")

	s, err := store.OpenSQLite(dbPath, sqlite.DefaultConfig())
	require.NoError(t, err)
	defer s.Close()

	now := time.Now().UTC()
	cfg := identity.Config{
		RPID:                "xg2g.home.matrixcentral.de",
		RPName:              "xg2g TV",
		ExpectedOrigin:      "https://xg2g.home.matrixcentral.de",
		SessionTTL:          24 * time.Hour,
		PasskeyChallengeTTL: 5 * time.Minute,
	}

	identitySvc := identity.NewService(cfg, s)
	identitySvc.SetNowFunc(func() time.Time { return now })

	server, handler, identitySvc := setupTestV3ServerWithIdentity(t, dbPath)
	_ = server

	testRPID := "localhost"
	testOrigin := "https://localhost"

	// 1. Setup Initial Admin & Passkey via Bootstrap (Required for system readiness)
	setupToken, err := identitySvc.GenerateBootstrapToken(ctx)
	require.NoError(t, err)
	_, _ = identitySvc.ValidateAndConsumeBootstrapToken(ctx, setupToken)

	regOpts, err := identitySvc.BeginBootstrapRegistration(ctx, "manuel", "Manuel")
	require.NoError(t, err)

	userPrivKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	userAttestation := createTestAttestationResponse(regOpts.Challenge, testRPID, testOrigin, userPrivKey)

	_, bootstrapRes, err := identitySvc.FinishPasskeyRegistration(ctx, userAttestation, "TouchID", "macOS", "127.0.0.1")
	require.NoError(t, err)
	require.NoError(t, identitySvc.AcknowledgeRecoveryCodes(ctx, bootstrapRes.User.ID))

	// 2. Android Device Grant Start (Get Assertion Challenge)
	reqGrantStart := httptest.NewRequest(http.MethodPost, "/api/v3/auth/device/grant/start", nil)
	reqGrantStart.Header.Set("X-Forwarded-Proto", "https")
	wGrantStart := httptest.NewRecorder()
	handler.ServeHTTP(wGrantStart, reqGrantStart)

	require.Equal(t, http.StatusOK, wGrantStart.Code)
	var loginOpts webauthn.RequestOptions
	require.NoError(t, json.Unmarshal(wGrantStart.Body.Bytes(), &loginOpts))
	assert.NotEmpty(t, loginOpts.Challenge)

	// 3. Generate Android Keystore P-256 Device Key & DPoP Proof
	devicePrivKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	devicePub := &devicePrivKey.PublicKey
	deviceJWK := dpop.JWKECPublicKey{
		Kty: "EC",
		Crv: "P-256",
		X:   base64.RawURLEncoding.EncodeToString(devicePub.X.Bytes()),
		Y:   base64.RawURLEncoding.EncodeToString(devicePub.Y.Bytes()),
	}

	jktExpected, err := dpop.ComputeJWKThumbprint(deviceJWK)
	require.NoError(t, err)
	_ = jktExpected

	grantURL := "https://localhost/api/v3/auth/device/grant/finish"
	dpopProof1 := createTestDPoPProof(devicePrivKey, deviceJWK, "POST", grantURL, now.Unix(), "")

	userAssertion := createTestAssertionResponse(loginOpts.Challenge, testRPID, testOrigin, bootstrapRes.Credential.ID, userPrivKey)

	finishPayload := v3.DeviceGrantFinishRequest{
		Assertion:  userAssertion,
		DeviceName: "Pixel 8 Pro",
		Platform:   "android",
		DeviceJWK:  deviceJWK,
		Scopes:     "api playback",
	}
	finishJSON, _ := json.Marshal(finishPayload)

	reqGrantFinish := httptest.NewRequest(http.MethodPost, "/api/v3/auth/device/grant/finish", bytes.NewReader(finishJSON))
	reqGrantFinish.Header.Set("Content-Type", "application/json")
	reqGrantFinish.Header.Set("DPoP", dpopProof1)
	reqGrantFinish.Header.Set("X-Forwarded-Proto", "https")
	reqGrantFinish.Host = "localhost"

	wGrantFinish := httptest.NewRecorder()
	handler.ServeHTTP(wGrantFinish, reqGrantFinish)

	require.Equal(t, http.StatusOK, wGrantFinish.Code)

	// INVARIANT CHECK: Cache-Control MUST BE no-store, no-cache, private
	assert.Contains(t, wGrantFinish.Header().Get("Cache-Control"), "no-store")

	// INVARIANT CHECK: Android Grant MUST NOT return browser xg2g_session cookie!
	for _, c := range wGrantFinish.Result().Cookies() {
		assert.NotEqual(t, "xg2g_session", c.Name, "Android device grant MUST NOT set browser cookie")
	}

	var grantRes identity.DeviceGrantResult
	require.NoError(t, json.Unmarshal(wGrantFinish.Body.Bytes(), &grantRes))
	assert.Equal(t, "DPoP", grantRes.TokenType)
	assert.NotEmpty(t, grantRes.AccessToken)
	assert.NotEmpty(t, grantRes.RefreshToken)
	assert.Equal(t, 900, grantRes.ExpiresIn)

	// 4. Validate Access Token with DPoP Key Binding & API Middleware Integration
	// Case A: Protected API request with valid DPoP Access Token & DPoP Proof -> 200 OK
	apiURL := "https://localhost/api/v3/auth/passkeys"
	apiProof1 := createTestDPoPProof(devicePrivKey, deviceJWK, "GET", apiURL, now.Unix()+1, dpop.ComputeAccessTokenHash(grantRes.AccessToken))

	reqAPI := httptest.NewRequest(http.MethodGet, "/api/v3/auth/passkeys", nil)
	reqAPI.Header.Set("Authorization", "DPoP "+grantRes.AccessToken)
	reqAPI.Header.Set("DPoP", apiProof1)
	reqAPI.Header.Set("X-Forwarded-Proto", "https")
	reqAPI.Host = "localhost"

	wAPI := httptest.NewRecorder()
	handler.ServeHTTP(wAPI, reqAPI)
	assert.Equal(t, http.StatusOK, wAPI.Code, "DPoP Access Token MUST be accepted by API auth middleware")

	// Case B: Global JTI Replay Prevention -> Reusing apiProof1 MUST fail with 401 Unauthorized
	reqAPIReplay := httptest.NewRequest(http.MethodGet, "/api/v3/auth/passkeys", nil)
	reqAPIReplay.Header.Set("Authorization", "DPoP "+grantRes.AccessToken)
	reqAPIReplay.Header.Set("DPoP", apiProof1)
	reqAPIReplay.Header.Set("X-Forwarded-Proto", "https")
	reqAPIReplay.Host = "localhost"

	wAPIReplay := httptest.NewRecorder()
	handler.ServeHTTP(wAPIReplay, reqAPIReplay)
	assert.Equal(t, http.StatusUnauthorized, wAPIReplay.Code, "Global JTI Replay must be rejected across requests")

	// 5. SECURITY FIX TEST (DoS Prevention): Attacker with stolen Refresh Token & wrong DPoP Key
	attackerPrivKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	attackerPub := &attackerPrivKey.PublicKey
	attackerJWK := dpop.JWKECPublicKey{
		Kty: "EC",
		Crv: "P-256",
		X:   base64.RawURLEncoding.EncodeToString(attackerPub.X.Bytes()),
		Y:   base64.RawURLEncoding.EncodeToString(attackerPub.Y.Bytes()),
	}

	refreshURL := "https://localhost/api/v3/auth/device/refresh"
	attackerDPoPProof := createTestDPoPProof(attackerPrivKey, attackerJWK, "POST", refreshURL, now.Unix()+50, "")

	refreshPayload := v3.DeviceRefreshRequest{RefreshToken: grantRes.RefreshToken}
	refreshJSON, _ := json.Marshal(refreshPayload)

	reqAttackerRefresh := httptest.NewRequest(http.MethodPost, "/api/v3/auth/device/refresh", bytes.NewReader(refreshJSON))
	reqAttackerRefresh.Header.Set("Content-Type", "application/json")
	reqAttackerRefresh.Header.Set("DPoP", attackerDPoPProof)
	reqAttackerRefresh.Header.Set("X-Forwarded-Proto", "https")
	reqAttackerRefresh.Host = "localhost"

	wAttackerRefresh := httptest.NewRecorder()
	handler.ServeHTTP(wAttackerRefresh, reqAttackerRefresh)

	assert.Equal(t, http.StatusUnauthorized, wAttackerRefresh.Code, "Attacker refresh request with mismatched key MUST be rejected with 401")

	// INVARIANT CHECK: Legitimate device's refresh token WAS NOT ROTATED or REVOKED by the attacker's failed attempt!
	legitDPoPProof := createTestDPoPProof(devicePrivKey, deviceJWK, "POST", refreshURL, now.Unix()+60, "")

	reqLegitRefresh := httptest.NewRequest(http.MethodPost, "/api/v3/auth/device/refresh", bytes.NewReader(refreshJSON))
	reqLegitRefresh.Header.Set("Content-Type", "application/json")
	reqLegitRefresh.Header.Set("DPoP", legitDPoPProof)
	reqLegitRefresh.Header.Set("X-Forwarded-Proto", "https")
	reqLegitRefresh.Host = "localhost"

	wLegitRefresh := httptest.NewRecorder()
	handler.ServeHTTP(wLegitRefresh, reqLegitRefresh)

	require.Equal(t, http.StatusOK, wLegitRefresh.Code, "Legitimate device MUST still be able to refresh after attacker failed attempt")
	var rotRes identity.DeviceGrantResult
	require.NoError(t, json.Unmarshal(wLegitRefresh.Body.Bytes(), &rotRes))
	assert.Equal(t, "DPoP", rotRes.TokenType)
	assert.NotEqual(t, grantRes.RefreshToken, rotRes.RefreshToken, "Refresh token must be rotated")

	// The refresh endpoint used to live outside api/openapi.yaml, so nothing
	// compared what it served against a contract. It is declared now, and the
	// device that depends on it can only generate a decoder from a response
	// shape the server is actually held to.
	assertMatchesOpenAPIResponse(t, reqLegitRefresh, wLegitRefresh)

	// 6. REPLAY ATTACK TEST: Replaying old Refresh Token must fail with 401 and revoke device grant
	dpopProof3 := createTestDPoPProof(devicePrivKey, deviceJWK, "POST", refreshURL, now.Unix()+10, "")
	reqReplay := httptest.NewRequest(http.MethodPost, "/api/v3/auth/device/refresh", bytes.NewReader(refreshJSON))
	reqReplay.Header.Set("Content-Type", "application/json")
	reqReplay.Header.Set("DPoP", dpopProof3)
	reqReplay.Header.Set("X-Forwarded-Proto", "https")
	reqReplay.Host = "localhost"

	wReplay := httptest.NewRecorder()
	handler.ServeHTTP(wReplay, reqReplay)

	assert.Equal(t, http.StatusUnauthorized, wReplay.Code, "Replaying rotated refresh token must fail with 401")
	assert.Contains(t, wReplay.Body.String(), "invalid_grant")

	// 7. Digital Asset Links Endpoint Test (GET /.well-known/assetlinks.json)
	reqAssetLinks := httptest.NewRequest(http.MethodGet, "/.well-known/assetlinks.json", nil)
	wAssetLinks := httptest.NewRecorder()
	handler.ServeHTTP(wAssetLinks, reqAssetLinks)

	assert.Equal(t, http.StatusOK, wAssetLinks.Code)
	assert.Equal(t, "application/json", wAssetLinks.Header().Get("Content-Type"))
	assert.Empty(t, wAssetLinks.Header().Get("Location"), "Digital AssetLinks endpoint MUST NOT issue redirects")

	var assetStatements []v3.AssetLinkStatement
	require.NoError(t, json.Unmarshal(wAssetLinks.Body.Bytes(), &assetStatements))
	require.Len(t, assetStatements, 1)
	assert.Equal(t, "android_app", assetStatements[0].Target.Namespace)
	assert.Equal(t, "de.matrixcentral.xg2g", assetStatements[0].Target.PackageName)
}

func createTestDPoPProof(priv *ecdsa.PrivateKey, jwk dpop.JWKECPublicKey, htm, htu string, iat int64, ath string) string {
	header := dpop.ProofHeader{
		Typ: "dpop+jwt",
		Alg: "ES256",
		JWK: jwk,
	}
	payload := dpop.ProofPayload{
		JTI: "jti_" + hex.EncodeToString(big.NewInt(iat).Bytes()),
		HTM: htm,
		HTU: htu,
		IAT: iat,
		ATH: ath,
	}

	hJSON, _ := json.Marshal(header)
	pJSON, _ := json.Marshal(payload)

	hB64 := base64.RawURLEncoding.EncodeToString(hJSON)
	pB64 := base64.RawURLEncoding.EncodeToString(pJSON)

	signedData := hB64 + "." + pB64
	hash := sha256.Sum256([]byte(signedData))
	rBig, sBig, _ := ecdsa.Sign(rand.Reader, priv, hash[:])

	rBytes := rBig.Bytes()
	sBytes := sBig.Bytes()
	sigBytes := make([]byte, 64)
	copy(sigBytes[32-len(rBytes):32], rBytes)
	copy(sigBytes[64-len(sBytes):64], sBytes)

	return signedData + "." + base64.RawURLEncoding.EncodeToString(sigBytes)
}

func createTestAttestationResponse(challenge, rpID, origin string, privKey *ecdsa.PrivateKey) webauthn.AttestationResponse {
	credID := []byte("cred_bootstrap_test")
	aaguid, _ := hex.DecodeString("0102030405060708090a0b0c0d0e0f10")

	coseMap := map[any]any{
		int64(1):  int64(2),  // kty: EC2
		int64(3):  int64(-7), // alg: ES256
		int64(-1): int64(1),  // crv: P-256
		int64(-2): privKey.X.Bytes(),
		int64(-3): privKey.Y.Bytes(),
	}
	coseBytes := encodeTestCBORDPoP(coseMap)

	rpIDHash := sha256.Sum256([]byte(rpID))
	authDataReg := make([]byte, 37)
	copy(authDataReg[:32], rpIDHash[:])
	authDataReg[32] = 0x01 | 0x04 | 0x40 | 0x08 | 0x10 // UP | UV | AT | BE | BS
	binary.BigEndian.PutUint32(authDataReg[33:37], 0)

	authDataReg = append(authDataReg, aaguid...)
	credIDLenBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(credIDLenBytes, uint16(len(credID)))
	authDataReg = append(authDataReg, credIDLenBytes...)
	authDataReg = append(authDataReg, credID...)
	authDataReg = append(authDataReg, coseBytes...)

	attObjMap := map[any]any{
		"fmt":      "none",
		"authData": authDataReg,
		"attStmt":  map[any]any{},
	}
	attObjBytes := encodeTestCBORDPoP(attObjMap)

	clientDataReg := webauthn.ClientDataJSON{
		Type:      "webauthn.create",
		Challenge: challenge,
		Origin:    origin,
	}
	clientDataRegJSON, _ := json.Marshal(clientDataReg)

	return webauthn.AttestationResponse{
		ClientDataJSON:    base64.RawURLEncoding.EncodeToString(clientDataRegJSON),
		AttestationObject: base64.RawURLEncoding.EncodeToString(attObjBytes),
		Transports:        []string{"internal"},
	}
}

func createTestAssertionResponse(challenge, rpID, origin, credIDStr string, privKey *ecdsa.PrivateKey) webauthn.AssertionResponse {
	clientData := webauthn.ClientDataJSON{
		Type:      "webauthn.get",
		Challenge: challenge,
		Origin:    origin,
	}
	clientDataJSON, _ := json.Marshal(clientData)
	clientDataHash := sha256.Sum256(clientDataJSON)

	rpIDHash := sha256.Sum256([]byte(rpID))
	authData := make([]byte, 37)
	copy(authData[:32], rpIDHash[:])
	authData[32] = 0x01 | 0x04 | 0x08 | 0x10 // UP | UV | BE | BS
	binary.BigEndian.PutUint32(authData[33:37], 1)

	signedMessage := append(authData, clientDataHash[:]...)
	digest := sha256.Sum256(signedMessage)
	rBig, sBig, _ := ecdsa.Sign(rand.Reader, privKey, digest[:])
	sigDER, _ := asn1.Marshal(struct {
		R, S *big.Int
	}{rBig, sBig})

	return webauthn.AssertionResponse{
		CredentialID:      credIDStr,
		ClientDataJSON:    base64.RawURLEncoding.EncodeToString(clientDataJSON),
		AuthenticatorData: base64.RawURLEncoding.EncodeToString(authData),
		Signature:         base64.RawURLEncoding.EncodeToString(sigDER),
	}
}

func encodeTestCBORDPoP(item any) []byte {
	switch v := item.(type) {
	case int64:
		if v >= 0 {
			if v < 24 {
				return []byte{byte(v)}
			}
			return []byte{24, byte(v)}
		}
		n := -1 - v
		if n < 24 {
			return []byte{0x20 | byte(n)}
		}
		return []byte{0x38, byte(n)}
	case string:
		var b []byte
		b = append(b, 0x60|byte(len(v)))
		b = append(b, []byte(v)...)
		return b
	case []byte:
		var b []byte
		if len(v) < 24 {
			b = append(b, 0x40|byte(len(v)))
		} else {
			b = append(b, 0x58, byte(len(v)))
		}
		b = append(b, v...)
		return b
	case map[any]any:
		var buf []byte
		buf = append(buf, 0xa0|byte(len(v)))
		for k, val := range v {
			buf = append(buf, encodeTestCBORDPoP(k)...)
			buf = append(buf, encodeTestCBORDPoP(val)...)
		}
		return buf
	default:
		return nil
	}
}

// assertMatchesOpenAPIResponse validates one recorded response against
// api/openapi.yaml.
//
// The v3 package has its own copy of this for internal tests; this file is an
// external test package and cannot reach it. Twenty lines duplicated is a
// better trade than exporting test machinery from the production package.
func assertMatchesOpenAPIResponse(t *testing.T, req *http.Request, rr *httptest.ResponseRecorder) {
	t.Helper()

	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	doc, err := loader.LoadFromFile(filepath.Join("..", "..", "..", "..", "api", "openapi.yaml"))
	require.NoError(t, err, "load openapi document")
	require.NoError(t, doc.Validate(context.Background()), "validate openapi document")

	router, err := legacy.NewRouter(doc)
	require.NoError(t, err, "openapi router init")

	route, pathParams, err := router.FindRoute(req)
	require.NoError(t, err, "openapi route lookup")

	input := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{
			Request:    req,
			PathParams: pathParams,
			Route:      route,
		},
		Status:  rr.Code,
		Header:  rr.Header(),
		Options: &openapi3filter.Options{},
	}
	input.SetBodyBytes(rr.Body.Bytes())

	require.NoError(t, openapi3filter.ValidateResponse(context.Background(), input), "openapi response validation")
}
