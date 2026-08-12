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

	"github.com/ManuGH/xg2g/internal/config"
	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
	"github.com/ManuGH/xg2g/internal/domain/identity"
	"github.com/ManuGH/xg2g/internal/domain/identity/store"
	"github.com/ManuGH/xg2g/internal/domain/identity/webauthn"
	"github.com/ManuGH/xg2g/internal/persistence/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestV3ServerWithIdentity(t *testing.T, dbPath string) (*v3.Server, http.Handler, *identity.Service) {
	sStore, err := store.OpenSQLite(dbPath, sqlite.DefaultConfig())
	require.NoError(t, err)

	now := time.Date(2026, 8, 12, 22, 0, 0, 0, time.UTC)
	idCfg := identity.Config{
		RPID:                "localhost",
		RPName:              "xg2g Test Server",
		ExpectedOrigin:      "https://localhost",
		SessionTTL:          24 * time.Hour,
		PasskeyChallengeTTL: 5 * time.Minute,
	}
	idSvc := identity.NewService(idCfg, sStore)
	idSvc.SetNowFunc(func() time.Time { return now })

	appCfg := config.AppConfig{
		APIToken:       "test-admin-token-12345",
		APITokenScopes: []string{"*"},
		TrustedProxies: "127.0.0.1/32,10.0.0.0/8",
	}

	server := v3.NewServer(appCfg, nil, nil)
	server.SetJWTSecret([]byte("test-jwt-secret-32-bytes-long-1234"))
	server.SetIdentityService(idSvc)

	handler, err := v3.NewHandler(server, appCfg)
	require.NoError(t, err)

	return server, handler, idSvc
}

func TestPasskeyAndWebSession_E2EWorkflow(t *testing.T) {
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "identity.sqlite")

	server, handler, idSvc := setupTestV3ServerWithIdentity(t, dbPath)
	defer idSvc.Store().Close()

	// ---------------- 1. Passkey Registration Start (Bootstrap) ----------------
	req := httptest.NewRequest(http.MethodPost, "/api/v3/auth/passkey/register/start", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var createOpts webauthn.CreationOptions
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &createOpts))
	assert.NotEmpty(t, createOpts.Challenge)

	// Simulate Client Authenticator P-256 Key generation
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	credID := []byte("passkey_cred_e2e_123")
	aaguid, _ := hex.DecodeString("0102030405060708090a0b0c0d0e0f10")

	coseMap := map[any]any{
		int64(1):  int64(2),  // kty: EC2
		int64(3):  int64(-7), // alg: ES256
		int64(-1): int64(1),  // crv: P-256
		int64(-2): privKey.X.Bytes(),
		int64(-3): privKey.Y.Bytes(),
	}
	coseBytes := encodeTestCBOR(coseMap)

	rpIDHash := sha256.Sum256([]byte("localhost"))
	authDataReg := make([]byte, 37)
	copy(authDataReg[:32], rpIDHash[:])
	authDataReg[32] = 0x01 | 0x40 | 0x08 | 0x10 // UP | AT | BE | BS
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
	attObjBytes := encodeTestCBOR(attObjMap)

	clientDataReg := webauthn.ClientDataJSON{
		Type:      "webauthn.create",
		Challenge: createOpts.Challenge,
		Origin:    "https://localhost",
	}
	clientDataRegJSON, _ := json.Marshal(clientDataReg)

	regPayload := v3.FinishRegistrationRequest{
		Response: webauthn.AttestationResponse{
			ClientDataJSON:    base64.RawURLEncoding.EncodeToString(clientDataRegJSON),
			AttestationObject: base64.RawURLEncoding.EncodeToString(attObjBytes),
			Transports:        []string{"internal"},
		},
		Nickname: "Safari TouchID",
	}
	regPayloadBytes, _ := json.Marshal(regPayload)

	// ---------------- 2. Passkey Registration Finish ----------------
	reqRegFinish := httptest.NewRequest(http.MethodPost, "/api/v3/auth/passkey/register/finish", bytes.NewReader(regPayloadBytes))
	reqRegFinish.Header.Set("Content-Type", "application/json")
	reqRegFinish.Header.Set("X-Forwarded-Proto", "https")
	reqRegFinish.RemoteAddr = "127.0.0.1:12345"
	wRegFinish := httptest.NewRecorder()
	handler.ServeHTTP(wRegFinish, reqRegFinish)

	require.Equal(t, http.StatusOK, wRegFinish.Code)
	var regFinishResp map[string]any
	require.NoError(t, json.Unmarshal(wRegFinish.Body.Bytes(), &regFinishResp))
	assert.Equal(t, "Safari TouchID", regFinishResp["nickname"])
	assert.True(t, regFinishResp["backupEligible"].(bool))
	assert.True(t, regFinishResp["backupState"].(bool))

	// ---------------- 3. Passkey Login Start ----------------
	loginStartPayload, _ := json.Marshal(map[string]string{"username": "admin"})
	reqLoginStart := httptest.NewRequest(http.MethodPost, "/api/v3/auth/passkey/login/start", bytes.NewReader(loginStartPayload))
	reqLoginStart.Header.Set("Content-Type", "application/json")
	reqLoginStart.Header.Set("X-Forwarded-Proto", "https")
	reqLoginStart.RemoteAddr = "127.0.0.1:12345"
	wLoginStart := httptest.NewRecorder()
	handler.ServeHTTP(wLoginStart, reqLoginStart)

	require.Equal(t, http.StatusOK, wLoginStart.Code)
	var reqOpts webauthn.RequestOptions
	require.NoError(t, json.Unmarshal(wLoginStart.Body.Bytes(), &reqOpts))
	assert.NotEmpty(t, reqOpts.Challenge)

	// ---------------- 4. Passkey Login Finish ----------------
	clientDataLogin := webauthn.ClientDataJSON{
		Type:      "webauthn.get",
		Challenge: reqOpts.Challenge,
		Origin:    "https://localhost",
	}
	clientDataLoginJSON, _ := json.Marshal(clientDataLogin)
	clientDataHash := sha256.Sum256(clientDataLoginJSON)

	authDataLogin := make([]byte, 37)
	copy(authDataLogin[:32], rpIDHash[:])
	authDataLogin[32] = 0x01 | 0x08 | 0x10 // UP | BE | BS
	binary.BigEndian.PutUint32(authDataLogin[33:37], 1)

	signedMessage := append(authDataLogin, clientDataHash[:]...)
	digest := sha256.Sum256(signedMessage)
	rBig, sBig, _ := ecdsa.Sign(rand.Reader, privKey, digest[:])
	sigDER, _ := asn1.Marshal(struct {
		R, S *big.Int
	}{rBig, sBig})

	loginPayload := v3.LoginFinishRequest{
		Response: webauthn.AssertionResponse{
			CredentialID:      base64.RawURLEncoding.EncodeToString(credID),
			ClientDataJSON:    base64.RawURLEncoding.EncodeToString(clientDataLoginJSON),
			AuthenticatorData: base64.RawURLEncoding.EncodeToString(authDataLogin),
			Signature:         base64.RawURLEncoding.EncodeToString(sigDER),
		},
	}
	loginPayloadBytes, _ := json.Marshal(loginPayload)

	reqLoginFinish := httptest.NewRequest(http.MethodPost, "/api/v3/auth/passkey/login/finish", bytes.NewReader(loginPayloadBytes))
	reqLoginFinish.Header.Set("Content-Type", "application/json")
	reqLoginFinish.Header.Set("X-Forwarded-Proto", "https")
	reqLoginFinish.RemoteAddr = "127.0.0.1:12345"
	wLoginFinish := httptest.NewRecorder()
	handler.ServeHTTP(wLoginFinish, reqLoginFinish)

	require.Equal(t, http.StatusOK, wLoginFinish.Code)

	// Verify Set-Cookie header
	cookies := wLoginFinish.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "xg2g_session" {
			sessionCookie = c
			break
		}
	}
	require.NotNil(t, sessionCookie, "Response must include xg2g_session cookie")
	assert.NotEmpty(t, sessionCookie.Value)
	assert.True(t, sessionCookie.HttpOnly)
	assert.True(t, sessionCookie.Secure)
	assert.Equal(t, http.SameSiteLaxMode, sessionCookie.SameSite)

	// ---------------- 5. Access Protected Endpoint with xg2g_session Cookie ----------------
	reqPasskeys := httptest.NewRequest(http.MethodGet, "/api/v3/auth/passkeys", nil)
	reqPasskeys.AddCookie(sessionCookie)
	reqPasskeys.Header.Set("X-Forwarded-Proto", "https")
	reqPasskeys.RemoteAddr = "127.0.0.1:12345"
	wPasskeys := httptest.NewRecorder()
	handler.ServeHTTP(wPasskeys, reqPasskeys)

	require.Equal(t, http.StatusOK, wPasskeys.Code)
	var passkeysList []identity.PasskeyCredential
	require.NoError(t, json.Unmarshal(wPasskeys.Body.Bytes(), &passkeysList))
	assert.Len(t, passkeysList, 1)
	assert.Equal(t, "Safari TouchID", passkeysList[0].Nickname)

	// ---------------- 6. Network Roaming Simulation (IP change from 127.0.0.1 to 198.51.100.77) ----------------
	reqRoaming := httptest.NewRequest(http.MethodGet, "/api/v3/auth/passkeys", nil)
	reqRoaming.AddCookie(sessionCookie)
	reqRoaming.Header.Set("X-Forwarded-Proto", "https")
	reqRoaming.RemoteAddr = "10.0.0.1:54321" // Trusted proxy subnet simulating roaming
	wRoaming := httptest.NewRecorder()
	handler.ServeHTTP(wRoaming, reqRoaming)

	require.Equal(t, http.StatusOK, wRoaming.Code, "Session must remain active across roaming IP changes")

	// ---------------- 7. Recovery Code Login ----------------
	// Retrieve the admin user's recovery codes from DB
	adminUser, err := idSvc.Store().GetUserByUsername(context.Background(), "admin")
	require.NoError(t, err)
	recCodes, err := idSvc.Store().ListRecoveryCodesByUser(context.Background(), adminUser.ID)
	require.NoError(t, err)
	assert.NotEmpty(t, recCodes)

	// Generate a known recovery code to test login
	knownRawCodes, knownRecords, err := identity.GenerateRecoveryCodes(adminUser.ID, 1, time.Now().UTC())
	require.NoError(t, err)
	require.NoError(t, idSvc.Store().PutRecoveryCodes(context.Background(), knownRecords))

	recLoginPayload, _ := json.Marshal(map[string]string{
		"username": "admin",
		"code":     knownRawCodes[0],
	})
	reqRecLogin := httptest.NewRequest(http.MethodPost, "/api/v3/auth/recovery", bytes.NewReader(recLoginPayload))
	reqRecLogin.Header.Set("Content-Type", "application/json")
	reqRecLogin.Header.Set("X-Forwarded-Proto", "https")
	reqRecLogin.RemoteAddr = "127.0.0.1:12345"
	wRecLogin := httptest.NewRecorder()
	handler.ServeHTTP(wRecLogin, reqRecLogin)

	require.Equal(t, http.StatusOK, wRecLogin.Code)
	var recSessionResp v3.AuthSessionResponse
	require.NoError(t, json.Unmarshal(wRecLogin.Body.Bytes(), &recSessionResp))
	assert.NotEmpty(t, recSessionResp.SessionID)

	// Re-using the same recovery code must fail with 401
	wRecLogin2 := httptest.NewRecorder()
	reqRecLogin2 := httptest.NewRequest(http.MethodPost, "/api/v3/auth/recovery", bytes.NewReader(recLoginPayload))
	reqRecLogin2.Header.Set("Content-Type", "application/json")
	reqRecLogin2.Header.Set("X-Forwarded-Proto", "https")
	reqRecLogin2.RemoteAddr = "127.0.0.1:12345"
	handler.ServeHTTP(wRecLogin2, reqRecLogin2)
	assert.Equal(t, http.StatusUnauthorized, wRecLogin2.Code)

	// ---------------- 8. Daemon Restart Persistence Verification ----------------
	// Close store and re-open to simulate full process restart
	require.NoError(t, idSvc.Store().Close())

	server2, handler2, idSvc2 := setupTestV3ServerWithIdentity(t, dbPath)
	defer idSvc2.Store().Close()
	_ = server
	_ = server2

	reqAfterRestart := httptest.NewRequest(http.MethodGet, "/api/v3/auth/passkeys", nil)
	reqAfterRestart.AddCookie(sessionCookie)
	reqAfterRestart.Header.Set("X-Forwarded-Proto", "https")
	reqAfterRestart.RemoteAddr = "127.0.0.1:12345"
	wAfterRestart := httptest.NewRecorder()
	handler2.ServeHTTP(wAfterRestart, reqAfterRestart)

	require.Equal(t, http.StatusOK, wAfterRestart.Code, "Session cookie must survive daemon restart and re-authenticate via SQLite")
}

func encodeTestCBOR(item any) []byte {
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
			buf = append(buf, encodeTestCBOR(k)...)
			buf = append(buf, encodeTestCBOR(val)...)
		}
		return buf
	default:
		return nil
	}
}
