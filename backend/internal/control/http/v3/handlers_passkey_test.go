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

	// ---------------- 0. Initial Auth Status Check ----------------
	reqStatusInit := httptest.NewRequest(http.MethodGet, "/api/v3/auth/status", nil)
	reqStatusInit.Header.Set("X-Forwarded-Proto", "https")
	wStatusInit := httptest.NewRecorder()
	handler.ServeHTTP(wStatusInit, reqStatusInit)
	require.Equal(t, http.StatusOK, wStatusInit.Code)
	var statusInit map[string]any
	require.NoError(t, json.Unmarshal(wStatusInit.Body.Bytes(), &statusInit))
	assert.True(t, statusInit["setupRequired"].(bool))
	assert.False(t, statusInit["publicReady"].(bool))

	// ---------------- 1. Passkey Registration Start (Bootstrap with Setup Token) ----------------
	// 1a. Unauthenticated attempt without setup token or operator token MUST FAIL with 401
	reqUnauth := httptest.NewRequest(http.MethodPost, "/api/v3/auth/passkey/register/start", nil)
	reqUnauth.Header.Set("X-Forwarded-Proto", "https")
	reqUnauth.RemoteAddr = "127.0.0.1:12345"
	wUnauth := httptest.NewRecorder()
	handler.ServeHTTP(wUnauth, reqUnauth)
	require.Equal(t, http.StatusUnauthorized, wUnauth.Code, "Unauthenticated bootstrap without setup token must be rejected")

	// 1b. Generate persistent 15-minute setup token via SQLite
	setupToken, err := idSvc.GenerateBootstrapToken(context.Background())
	require.NoError(t, err)
	assert.NotEmpty(t, setupToken)

	// 1c. Authorized bootstrap using X-Setup-Token
	req := httptest.NewRequest(http.MethodPost, "/api/v3/auth/passkey/register/start", nil)
	req.Header.Set("X-Setup-Token", setupToken)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var createOpts webauthn.CreationOptions
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &createOpts))
	assert.NotEmpty(t, createOpts.Challenge)
	assert.Equal(t, "required", createOpts.AuthenticatorSelection.UserVerification)

	// Invariant: DB must still have 0 users before passkey finish!
	hasUsersBeforeFinish, err := idSvc.HasUsers(context.Background())
	require.NoError(t, err)
	assert.False(t, hasUsersBeforeFinish, "Database must not have any users created before passkey ceremony finishes")

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
	attObjBytes := encodeTestCBOR(attObjMap)

	clientDataReg := webauthn.ClientDataJSON{
		Type:      "webauthn.create",
		Challenge: createOpts.Challenge,
		Origin:    "https://localhost",
	}
	clientDataRegJSON, _ := json.Marshal(clientDataReg)

	transports := []string{"internal"}
	nickname := "Safari TouchID"
	regPayload := v3.PasskeyRegisterFinishRequest{
		Response: v3.WebAuthnAttestationResponse{
			ClientDataJSON:    base64.RawURLEncoding.EncodeToString(clientDataRegJSON),
			AttestationObject: base64.RawURLEncoding.EncodeToString(attObjBytes),
			Transports:        &transports,
		},
		Nickname: &nickname,
	}
	regPayloadBytes, _ := json.Marshal(regPayload)

	// ---------------- 2. Passkey Registration Finish (Atomic Commit) ----------------
	reqRegFinish := httptest.NewRequest(http.MethodPost, "/api/v3/auth/passkey/register/finish", bytes.NewReader(regPayloadBytes))
	reqRegFinish.Header.Set("Content-Type", "application/json")
	reqRegFinish.Header.Set("X-Forwarded-Proto", "https")
	reqRegFinish.RemoteAddr = "127.0.0.1:12345"
	wRegFinish := httptest.NewRecorder()
	handler.ServeHTTP(wRegFinish, reqRegFinish)

	require.Equal(t, http.StatusOK, wRegFinish.Code)
	assert.Equal(t, "no-store", wRegFinish.Header().Get("Cache-Control"), "Recovery codes must be served with no-store")

	var regFinishResp map[string]any
	require.NoError(t, json.Unmarshal(wRegFinish.Body.Bytes(), &regFinishResp))
	assert.Equal(t, "bootstrap_completed", regFinishResp["status"])
	rawRecCodes := regFinishResp["recoveryCodes"].([]any)
	assert.Len(t, rawRecCodes, 10, "Response must include 10 recovery codes for one-time display")

	// Verify Set-Cookie header
	cookiesInit := wRegFinish.Result().Cookies()
	var adminSessionCookie *http.Cookie
	for _, c := range cookiesInit {
		if c.Name == "xg2g_session" {
			adminSessionCookie = c
			break
		}
	}
	require.NotNil(t, adminSessionCookie, "Bootstrap finish must set xg2g_session cookie")

	// ---------------- 2b. Public Ready Invariant Check (Must be False before Ack) ----------------
	reqStatusBeforeAck := httptest.NewRequest(http.MethodGet, "/api/v3/auth/status", nil)
	reqStatusBeforeAck.Header.Set("X-Forwarded-Proto", "https")
	wStatusBeforeAck := httptest.NewRecorder()
	handler.ServeHTTP(wStatusBeforeAck, reqStatusBeforeAck)
	var statusBeforeAck map[string]any
	require.NoError(t, json.Unmarshal(wStatusBeforeAck.Body.Bytes(), &statusBeforeAck))
	assert.False(t, statusBeforeAck["publicReady"].(bool), "publicReady must be false before recovery codes are acknowledged")
	assert.False(t, statusBeforeAck["recoveryAcknowledged"].(bool))

	// ---------------- 2c. Acknowledge Recovery Codes ----------------
	reqAck := httptest.NewRequest(http.MethodPost, "/api/v3/auth/bootstrap/acknowledge-recovery", nil)
	reqAck.AddCookie(adminSessionCookie)
	reqAck.Header.Set("X-Forwarded-Proto", "https")
	reqAck.RemoteAddr = "127.0.0.1:12345"
	wAck := httptest.NewRecorder()
	handler.ServeHTTP(wAck, reqAck)
	require.Equal(t, http.StatusOK, wAck.Code)

	// Check Auth Status after Ack: identityReady must NOW be TRUE
	reqStatusAfterAck := httptest.NewRequest(http.MethodGet, "/api/v3/auth/status", nil)
	reqStatusAfterAck.Header.Set("X-Forwarded-Proto", "https")
	wStatusAfterAck := httptest.NewRecorder()
	handler.ServeHTTP(wStatusAfterAck, reqStatusAfterAck)
	var statusAfterAck map[string]any
	require.NoError(t, json.Unmarshal(wStatusAfterAck.Body.Bytes(), &statusAfterAck))
	assert.True(t, statusAfterAck["identityReady"].(bool), "identityReady must be true after recovery codes acknowledged")
	assert.True(t, statusAfterAck["recoveryAcknowledged"].(bool))

	// ---------------- 2d. Re-attempt unauthenticated register/start -> MUST FAIL 401 ----------------
	reqUnauth2 := httptest.NewRequest(http.MethodPost, "/api/v3/auth/passkey/register/start", nil)
	reqUnauth2.Header.Set("X-Forwarded-Proto", "https")
	reqUnauth2.RemoteAddr = "127.0.0.1:12345"
	wUnauth2 := httptest.NewRecorder()
	handler.ServeHTTP(wUnauth2, reqUnauth2)
	require.Equal(t, http.StatusUnauthorized, wUnauth2.Code, "Unauthenticated register/start must be rejected once users exist")

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
	assert.Equal(t, "required", reqOpts.UserVerification)

	// ---------------- 4. Passkey Login Finish (Zero JS Session Token Exposure) ----------------
	clientDataLogin := webauthn.ClientDataJSON{
		Type:      "webauthn.get",
		Challenge: reqOpts.Challenge,
		Origin:    "https://localhost",
	}
	clientDataLoginJSON, _ := json.Marshal(clientDataLogin)
	clientDataHash := sha256.Sum256(clientDataLoginJSON)

	authDataLogin := make([]byte, 37)
	copy(authDataLogin[:32], rpIDHash[:])
	authDataLogin[32] = 0x01 | 0x04 | 0x08 | 0x10 // UP | UV | BE | BS
	binary.BigEndian.PutUint32(authDataLogin[33:37], 1)

	signedMessage := append(authDataLogin, clientDataHash[:]...)
	digest := sha256.Sum256(signedMessage)
	rBig, sBig, _ := ecdsa.Sign(rand.Reader, privKey, digest[:])
	sigDER, _ := asn1.Marshal(struct {
		R, S *big.Int
	}{rBig, sBig})

	loginPayload := v3.PasskeyLoginFinishRequest{
		Response: v3.WebAuthnAssertionResponse{
			Id:                base64.RawURLEncoding.EncodeToString(credID),
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

	// Zero JavaScript Exposure Invariant: response body must NOT contain session ID / token!
	var authSessResp v3.AuthSessionResponse
	require.NoError(t, json.Unmarshal(wLoginFinish.Body.Bytes(), &authSessResp))
	assert.Equal(t, "admin", authSessResp.User.Username)
	assert.NotContains(t, wLoginFinish.Body.String(), sessionCookie.Value, "Raw response JSON body must NEVER leak the session cookie token")
	assert.NotContains(t, wLoginFinish.Body.String(), "sessionId", "Response JSON body must not have sessionId field")

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

	// ---------------- 7. Recovery Code Login & Cross-User Protection ----------------
	adminUser, err := idSvc.Store().GetUserByUsername(context.Background(), "admin")
	require.NoError(t, err)

	// Create a second user (e.g. member) with their own recovery codes
	memberUser := &identity.User{
		ID:          "usr_member_test",
		Username:    "member1",
		DisplayName: "Member One",
		Role:        identity.RoleMember,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	require.NoError(t, idSvc.Store().PutUser(context.Background(), memberUser))
	memberRawCodes, memberRecords, err := identity.GenerateRecoveryCodes(memberUser.ID, 1, time.Now().UTC())
	require.NoError(t, err)
	require.NoError(t, idSvc.Store().PutRecoveryCodes(context.Background(), memberRecords))

	// Generate known recovery code for admin
	knownRawCodes, knownRecords, err := identity.GenerateRecoveryCodes(adminUser.ID, 1, time.Now().UTC())
	require.NoError(t, err)
	require.NoError(t, idSvc.Store().PutRecoveryCodes(context.Background(), knownRecords))

	// Cross-User Attack Test: Attacker tries to use member's recovery code against admin account -> MUST FAIL 401
	crossUserPayload, _ := json.Marshal(map[string]string{
		"username": "admin",
		"code":     memberRawCodes[0],
	})
	reqCrossUser := httptest.NewRequest(http.MethodPost, "/api/v3/auth/recovery", bytes.NewReader(crossUserPayload))
	reqCrossUser.Header.Set("Content-Type", "application/json")
	reqCrossUser.Header.Set("X-Forwarded-Proto", "https")
	reqCrossUser.RemoteAddr = "127.0.0.1:12345"
	wCrossUser := httptest.NewRecorder()
	handler.ServeHTTP(wCrossUser, reqCrossUser)
	require.Equal(t, http.StatusUnauthorized, wCrossUser.Code, "Cross-user recovery code usage must be strictly rejected")

	// Valid Admin Recovery Login
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
	assert.NotContains(t, wRecLogin.Body.String(), "sessionId", "Recovery response body must not leak sessionId")

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
