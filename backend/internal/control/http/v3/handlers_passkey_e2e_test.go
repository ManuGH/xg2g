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
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/identity/webauthn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type e2eECDSASignature struct {
	R, S *big.Int
}

func buildE2EAttestationObject(pubKey *ecdsa.PublicKey, rpID string, credentialID []byte) []byte {
	rpHash := sha256.Sum256([]byte(rpID))
	flags := byte(0x45) // User Present (0x01) + User Verified (0x04) + Attested Credential Data Present (0x40)

	var authData []byte
	authData = append(authData, rpHash[:]...)
	authData = append(authData, flags)
	signCount := make([]byte, 4)
	binary.BigEndian.PutUint32(signCount, 1)
	authData = append(authData, signCount...)

	aaguid := make([]byte, 16)
	authData = append(authData, aaguid...)

	credIDLen := make([]byte, 2)
	binary.BigEndian.PutUint16(credIDLen, uint16(len(credentialID)))
	authData = append(authData, credIDLen...)
	authData = append(authData, credentialID...)

	// COSE Key encoding for P-256
	xBytes := pubKey.X.Bytes()
	yBytes := pubKey.Y.Bytes()
	if len(xBytes) < 32 {
		padded := make([]byte, 32)
		copy(padded[32-len(xBytes):], xBytes)
		xBytes = padded
	}
	if len(yBytes) < 32 {
		padded := make([]byte, 32)
		copy(padded[32-len(yBytes):], yBytes)
		yBytes = padded
	}

	coseKey := []byte{
		0xa5,       // map(5)
		0x01, 0x02, // 1: kty -> 2 (EC2)
		0x03, 0x26, // 3: alg -> -7 (ES256)
		0x20, 0x01, // -1: crv -> 1 (P-256)
		0x21, 0x58, 0x20, // -2: x (byte string 32)
	}
	coseKey = append(coseKey, xBytes...)
	coseKey = append(coseKey, 0x22, 0x58, 0x20) // -3: y (byte string 32)
	coseKey = append(coseKey, yBytes...)

	authData = append(authData, coseKey...)

	// Minimal CBOR Attestation Object
	attObj := []byte{
		0xa2,                                          // map(2)
		0x63, 'f', 'm', 't', 0x64, 'n', 'o', 'n', 'e', // "fmt": "none"
		0x68, 'a', 'u', 't', 'h', 'D', 'a', 't', 'a', 0x58, byte(len(authData)), // "authData": bytes(...)
	}
	attObj = append(attObj, authData...)
	return attObj
}

func buildE2EAuthenticatorData(rpID string) []byte {
	rpHash := sha256.Sum256([]byte(rpID))
	flags := byte(0x05) // User Present (0x01) + User Verified (0x04)

	var authData []byte
	authData = append(authData, rpHash[:]...)
	authData = append(authData, flags)
	signCount := make([]byte, 4)
	binary.BigEndian.PutUint32(signCount, 2)
	authData = append(authData, signCount...)
	return authData
}

func TestPasskeyFullHTTPCeremony_E2EWithAssertionCredentialID(t *testing.T) {
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "identity_e2e.sqlite")

	_, handler, idSvc := setupTestV3ServerWithIdentity(t, dbPath)
	defer idSvc.Store().Close()

	// 1. Initial Auth Status Check
	reqStatus := httptest.NewRequest(http.MethodGet, "/api/v3/auth/status", nil)
	wStatus := httptest.NewRecorder()
	handler.ServeHTTP(wStatus, reqStatus)
	require.Equal(t, http.StatusOK, wStatus.Code)

	// 2. Generate Setup Token for Bootstrap
	setupToken, err := idSvc.GenerateBootstrapToken(context.Background())
	require.NoError(t, err)

	// 3. Register Start (Bootstrap)
	reqRegStart := httptest.NewRequest(http.MethodPost, "/api/v3/auth/passkey/register/start", nil)
	reqRegStart.Header.Set("X-Setup-Token", setupToken)
	reqRegStart.Header.Set("X-Forwarded-Proto", "https")
	wRegStart := httptest.NewRecorder()
	handler.ServeHTTP(wRegStart, reqRegStart)
	require.Equal(t, http.StatusOK, wRegStart.Code)

	var regStartOpts webauthn.CreationOptions
	require.NoError(t, json.Unmarshal(wRegStart.Body.Bytes(), &regStartOpts))
	assert.NotEmpty(t, regStartOpts.Challenge)

	// 4. Generate Authenticator P-256 Key Pair
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	rawCredID := []byte("cred_e2e_test_99999")
	credIDBase64 := base64.RawURLEncoding.EncodeToString(rawCredID)

	// Build ClientDataJSON & AttestationObject
	clientData := map[string]any{
		"type":      "webauthn.create",
		"challenge": regStartOpts.Challenge,
		"origin":    "https://localhost",
	}
	clientDataBytes, err := json.Marshal(clientData)
	require.NoError(t, err)

	attObjBytes := buildE2EAttestationObject(&privKey.PublicKey, "localhost", rawCredID)

	// 5. Register Finish (Bootstrap Commit)
	finishRegPayload := map[string]any{
		"response": map[string]any{
			"clientDataJSON":    base64.RawURLEncoding.EncodeToString(clientDataBytes),
			"attestationObject": base64.RawURLEncoding.EncodeToString(attObjBytes),
		},
		"nickname": "Admin E2E Key",
	}
	bodyRegFinish, _ := json.Marshal(finishRegPayload)

	reqRegFinish := httptest.NewRequest(http.MethodPost, "/api/v3/auth/passkey/register/finish", bytes.NewReader(bodyRegFinish))
	reqRegFinish.Header.Set("Content-Type", "application/json")
	reqRegFinish.Header.Set("X-Forwarded-Proto", "https")
	wRegFinish := httptest.NewRecorder()
	handler.ServeHTTP(wRegFinish, reqRegFinish)

	require.Equal(t, http.StatusOK, wRegFinish.Code)
	var regFinishRes map[string]any
	require.NoError(t, json.Unmarshal(wRegFinish.Body.Bytes(), &regFinishRes))
	assert.Equal(t, "bootstrap_completed", regFinishRes["status"])
	recoveryCodes, ok := regFinishRes["recoveryCodes"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, recoveryCodes)

	// Extract session cookie set by bootstrap finish
	sessionCookie := wRegFinish.Result().Cookies()[0]
	require.Equal(t, "xg2g_session", sessionCookie.Name)

	// 6. Acknowledge Recovery Codes
	reqAck := httptest.NewRequest(http.MethodPost, "/api/v3/auth/bootstrap/acknowledge-recovery", nil)
	reqAck.AddCookie(sessionCookie)
	wAck := httptest.NewRecorder()
	handler.ServeHTTP(wAck, reqAck)
	require.Equal(t, http.StatusOK, wAck.Code)

	// ---------------- PASSKEY LOGIN CEREMONY (WITH CREDENTIAL ID IN RESPONSE) ----------------
	// 7. Login Start
	reqLoginStart := httptest.NewRequest(http.MethodPost, "/api/v3/auth/passkey/login/start", nil)
	wLoginStart := httptest.NewRecorder()
	handler.ServeHTTP(wLoginStart, reqLoginStart)
	require.Equal(t, http.StatusOK, wLoginStart.Code)

	var loginStartOpts webauthn.RequestOptions
	require.NoError(t, json.Unmarshal(wLoginStart.Body.Bytes(), &loginStartOpts))
	assert.NotEmpty(t, loginStartOpts.Challenge)

	// 8. Sign Assertion Challenge with P-256 Private Key
	loginClientData := map[string]any{
		"type":      "webauthn.get",
		"challenge": loginStartOpts.Challenge,
		"origin":    "https://localhost",
	}
	loginClientDataBytes, err := json.Marshal(loginClientData)
	require.NoError(t, err)

	authData := buildE2EAuthenticatorData("localhost")
	clientDataHash := sha256.Sum256(loginClientDataBytes)

	var sigBuf bytes.Buffer
	sigBuf.Write(authData)
	sigBuf.Write(clientDataHash[:])
	sigHash := sha256.Sum256(sigBuf.Bytes())

	rSig, sSig, err := ecdsa.Sign(rand.Reader, privKey, sigHash[:])
	require.NoError(t, err)

	derSig, err := asn1.Marshal(e2eECDSASignature{R: rSig, S: sSig})
	require.NoError(t, err)

	// 9. Login Finish: Send payload WITH id inside response object!
	finishLoginPayload := map[string]any{
		"response": map[string]any{
			"id":                credIDBase64, // Explicit Credential ID inside response!
			"clientDataJSON":    base64.RawURLEncoding.EncodeToString(loginClientDataBytes),
			"authenticatorData": base64.RawURLEncoding.EncodeToString(authData),
			"signature":         base64.RawURLEncoding.EncodeToString(derSig),
		},
	}
	bodyLoginFinish, _ := json.Marshal(finishLoginPayload)

	reqLoginFinish := httptest.NewRequest(http.MethodPost, "/api/v3/auth/passkey/login/finish", bytes.NewReader(bodyLoginFinish))
	reqLoginFinish.Header.Set("Content-Type", "application/json")
	reqLoginFinish.Header.Set("X-Forwarded-Proto", "https")
	wLoginFinish := httptest.NewRecorder()
	handler.ServeHTTP(wLoginFinish, reqLoginFinish)

	require.Equal(t, http.StatusOK, wLoginFinish.Code, "Passkey login finish with Credential ID inside response must succeed")

	var loginSessionRes map[string]any
	require.NoError(t, json.Unmarshal(wLoginFinish.Body.Bytes(), &loginSessionRes))
	userObj, ok := loginSessionRes["user"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "admin", userObj["username"])

	loginCookie := wLoginFinish.Result().Cookies()[0]

	// 10. List Registered Passkeys with newly created session cookie
	reqListPasskeys := httptest.NewRequest(http.MethodGet, "/api/v3/auth/passkeys", nil)
	reqListPasskeys.AddCookie(loginCookie)
	wListPasskeys := httptest.NewRecorder()
	handler.ServeHTTP(wListPasskeys, reqListPasskeys)

	require.Equal(t, http.StatusOK, wListPasskeys.Code)
	var passkeysList []map[string]any
	require.NoError(t, json.Unmarshal(wListPasskeys.Body.Bytes(), &passkeysList))
	require.Len(t, passkeysList, 1)
	assert.Equal(t, "Admin E2E Key", passkeysList[0]["nickname"])

	// 11. Revoke Other Sessions
	reqRevoke := httptest.NewRequest(http.MethodPost, "/api/v3/auth/sessions/revoke-others", nil)
	reqRevoke.AddCookie(loginCookie)
	wRevoke := httptest.NewRecorder()
	handler.ServeHTTP(wRevoke, reqRevoke)
	require.Equal(t, http.StatusNoContent, wRevoke.Code)
}
